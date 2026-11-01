package sessiond

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent/fakeeditor"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
)

// Lifecycle traces: each scenario drives a fake-mode sessiond (the fake
// editor behind the agent socket and tmux) and a fake gateway through
// the HTTP API, and compares the ordered trace of /events frames and
// gateway reports with testdata/traces/<scenario>.trace (UPDATE_TRACES=1
// rewrites them). The trace is cut into sections, one per action: a
// section waits for the lines the action must produce (those the
// scenario names and those the golden trace lists), then for quiet, and
// lists the section's events in order followed by its gateway reports.
// The step engine runs with a 10 ms debounce, so quiet is well above
// any evaluation delay. Key, state, prefix, file, echo, minibuffer, mode, nudge,
// hello and frame events are left out; consecutive identical check lines
// are written once.

// Lessons the fake gateway serves. A: three steps a learner completes
// with keys; B: needs the editing module (a restart); C: a shell
// condition whose script writes into the watched tree; D: one step.
const (
	lessonA = "01-life/01-three"
	lessonB = "01-life/02-modules"
	lessonC = "01-life/03-shell"
	lessonD = "01-life/04-other"
)

const (
	demoOpen  = `C-x C-f "b.txt" RET`
	demoWrite = `"done" C-x C-s`
	demoSplit = `C-x 2`
)

func lifecycleLessons(t *testing.T) map[string]gwLesson {
	return map[string]gwLesson{
		lessonA: {scenario: `{"id": "` + lessonA + `", "title": "Three steps", "modules": ["base", "agent"],
  "setup": {"root": "life", "open": ["life/a.txt"]},
  "steps": [
    {"id": "open", "goal": "Open b.txt", "demo": ` + jsonString(demoOpen) + `, "conditions": [{"buffer": {"name": "b.txt"}}]},
    {"id": "write", "goal": "Write done into b.txt", "demo": ` + jsonString(demoWrite) + `, "conditions": [{"file": {"path": "life/b.txt", "contains": "done"}}]},
    {"id": "split", "goal": "Split the window", "demo": "C-x 2", "conditions": [{"event": {"command": "split-window-below"}}]}
  ]}`, tarball: makeTarball(t, map[string]string{"a.txt": "alpha\n", "b.txt": "beta\n"})},
		lessonB: {scenario: `{"id": "` + lessonB + `", "title": "Modules", "modules": ["base", "agent", "editing"],
  "setup": {"root": "mods", "open": ["mods/m.txt"]},
  "steps": [{"id": "quit", "goal": "Quit", "demo": "C-g", "conditions": [{"event": {"command": "keyboard-quit"}}]}]}`,
			tarball: makeTarball(t, map[string]string{"m.txt": "modules\n"})},
		lessonC: {scenario: `{"id": "` + lessonC + `", "title": "Shell", "modules": ["base", "agent"],
  "setup": {"root": "sh", "open": ["sh/s.txt"]},
  "steps": [
    {"id": "flag", "goal": "Make the flag", "conditions": [{"shell": {"run": "echo run >> runs.log; test -f done.flag"}}]},
    {"id": "last", "goal": "Quit", "demo": "C-g", "conditions": [{"event": {"command": "keyboard-quit"}}]}
  ]}`, tarball: makeTarball(t, map[string]string{"s.txt": "shell\n"})},
		lessonD: {scenario: `{"id": "` + lessonD + `", "title": "Other", "modules": ["base", "agent"],
  "setup": {"root": "other", "open": ["other/o.txt"]},
  "steps": [{"id": "quit", "goal": "Quit", "demo": "C-g", "conditions": [{"event": {"command": "keyboard-quit"}}]}]}`,
			tarball: makeTarball(t, map[string]string{"o.txt": "other\n"})},
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// --- the fake gateway's progress store -----------------------------------------

// applyLocked records a progress report as the gateway applies it
// (progress.Service.Apply): stale generations are discarded, statuses
// only rise, a lap counts once per lap id (without one, once per (at,
// ms)).
func (g *fakeGateway) applyLocked(p protocol.ProgressReport) {
	line := fmt.Sprintf("gw:%s/%s:%s:gen%d", p.LessonID, p.StepID, p.Status, p.Generation)
	if p.LapMs > 0 {
		line += fmt.Sprintf(":lap%d", p.LapMs)
	}
	if p.LapID != "" {
		line += ":" + p.LapID
	}
	if p.Generation < g.gens.ForLesson(p.LessonID) {
		g.lines = append(g.lines, line+":stale")
		return
	}
	g.lines = append(g.lines, line)
	if g.store == nil {
		g.store = map[[2]string]protocol.ProgressStatus{}
	}
	k := [2]string{p.LessonID, p.StepID}
	if protocol.ProgressRank(p.Status) > protocol.ProgressRank(g.store[k]) {
		g.store[k] = p.Status
	}
	if p.LapMs > 0 {
		if g.laps == nil {
			g.laps = map[string]gwLap{}
		}
		lr := g.laps[p.LessonID]
		if p.LapID != "" && p.LapID != lr.lastID || p.LapID == "" && (p.At == 0 || lr.lastAt != p.At || lr.lastMs != p.LapMs) {
			lr.count++
			lr.lastAt, lr.lastMs, lr.lastID = p.At, p.LapMs, p.LapID
		}
		g.laps[p.LessonID] = lr
	}
}

// Down makes every gateway endpoint answer 503; Up undoes it.
func (g *fakeGateway) Down() { g.mu.Lock(); g.down = true; g.mu.Unlock() }
func (g *fakeGateway) Up()   { g.mu.Lock(); g.down = false; g.mu.Unlock() }

// Reset is the learner's progress reset (gateway resetProgress): a new
// generation, the lesson's (or with "" every) record gone, then drop-step
// to the running session, best effort.
func (g *fakeGateway) Reset(t *testing.T, lessonID string) {
	t.Helper()
	g.mu.Lock()
	var gen int64
	for _, n := range g.gens {
		gen = max(gen, n)
	}
	gen++
	if g.gens == nil {
		g.gens = protocol.ProgressGenerations{}
	}
	if lessonID == "" {
		g.gens = protocol.ProgressGenerations{"": gen}
		g.store, g.laps = nil, nil
	} else {
		g.gens[lessonID] = gen
		for k := range g.store {
			if k[0] == lessonID {
				delete(g.store, k)
			}
		}
		delete(g.laps, lessonID)
	}
	g.lines = append(g.lines, "gw:reset:"+lessonID)
	session := g.session
	g.mu.Unlock()
	if session == nil {
		return
	}
	if ts := session(); ts != nil {
		if code := ts.rpc("drop-step", protocol.DropStepRequest{LessonID: lessonID, Generation: gen}, nil); code != 200 {
			t.Logf("drop-step %d", code)
		}
	}
}

// Replace is a store replaced by an empty one (lost or restored from an
// old backup): no progress, no generations.
func (g *fakeGateway) Replace() {
	g.mu.Lock()
	g.gens, g.store, g.laps = nil, nil, nil
	g.lines = append(g.lines, "gw:replaced")
	g.mu.Unlock()
}

func (g *fakeGateway) lapCount(lessonID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.laps[lessonID].count
}

// --- the harness -------------------------------------------------------------------

// testClock is a clock that moves only when told.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type lifecycle struct {
	t     *testing.T
	ws    string
	run   string
	gw    *fakeGateway
	gwURL string
	ed    *fakeeditor.Editor
	clock *testClock

	mu    sync.Mutex
	ts    *testServer
	es    *eventStream
	evIdx int
	gwIdx int
	out   []string
	// sections counts the sections written (the scenario's goroutine only).
	sections int
}

func newLifecycle(t *testing.T, opts ...func(*lifecycle)) *lifecycle {
	t.Helper()
	for _, bin := range []string{"bash", "git"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " not installed")
		}
	}
	base := t.TempDir()
	run, err := os.MkdirTemp("", "pgrun")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	_ = os.MkdirAll(filepath.Join(base, "pristine"), 0o755)
	_ = os.WriteFile(filepath.Join(base, "pristine", "init.el"), []byte(";; init\n"), 0o644)
	l := &lifecycle{t: t, ws: filepath.Join(base, "work"), run: run,
		gw: &fakeGateway{lessons: lifecycleLessons(t), tarball: makeTarball(t, map[string]string{"README.md": "# hello\n"})}}
	l.gw.session = l.server
	gwSrv := httptest.NewServer(l.gw.handler(t))
	t.Cleanup(gwSrv.Close)
	l.gwURL = gwSrv.URL
	l.ed = fakeeditor.New(l.ws)
	for _, o := range opts {
		o(l)
	}
	t.Cleanup(func() {
		if ts := l.server(); ts != nil {
			ts.stop()
		}
	})
	l.start("")
	return l
}

// withClock runs the step engine on a clock the scenario moves.
func withClock(l *lifecycle) {
	l.clock = &testClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}

func (l *lifecycle) server() *testServer {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ts
}

// start starts sessiond and its event stream (after: the replay point, ""
// for none).
func (l *lifecycle) start(after string) {
	l.t.Helper()
	ts := startServer(l.t, l.ws, l.run, l.gw, l.gwURL, func(o *Options) {
		o.Editor = l.ed
		o.Debounce = 10 * time.Millisecond
		o.ProgressBackoff = 200 * time.Millisecond
		if l.clock != nil {
			o.Clock = l.clock.Now
		}
	})
	es := ts.events(after)
	es.first()
	l.mu.Lock()
	l.ts, l.es, l.evIdx = ts, es, 0
	l.mu.Unlock()
}

// stop stops sessiond gracefully.
func (l *lifecycle) stop() {
	l.t.Helper()
	ts := l.server()
	ts.stop()
	l.mu.Lock()
	l.ts = nil
	l.mu.Unlock()
}

// traceLine renders a traced event, "" for the kinds left out.
func traceLine(ev protocol.Event) string {
	switch e := ev.(type) {
	case *protocol.StepEvent:
		s := "step:" + e.StepID + ":" + string(e.Status)
		if e.LessonComplete {
			s += ":lesson-complete"
		}
		return s
	case *protocol.CheckEvent:
		return fmt.Sprintf("check:%s:%v", e.StepID, e.Complete)
	case *protocol.CheckpointEvent:
		return "checkpoint:" + e.Label
	case *protocol.ModulesEvent:
		return fmt.Sprintf("modules:%v", e.Restarting)
	case *protocol.EmacsEvent:
		// TODO(sessiond, backend.md §3.3): the supervisor emits
		// frame-opened after releasing its lock, so a restart's
		// daemon-restarted (or a resume's step event) can overtake it;
		// frame events stay out of traces until setFrame emits under the
		// lock.
		if e.Event == protocol.EmacsFrameOpened || e.Event == protocol.EmacsFrameClosed {
			return ""
		}
		return "emacs:" + string(e.Event)
	case *protocol.ErrorEvent:
		return "error:" + string(e.Code)
	}
	return ""
}

// pending returns the traced lines not yet in a section and the lengths
// of the event and gateway logs they run to, each read under one lock.
func (l *lifecycle) pending() (evs, gws []string, evN, gwN int) {
	l.mu.Lock()
	es, from, gwFrom := l.es, l.evIdx, l.gwIdx
	l.mu.Unlock()
	es.mu.Lock()
	evN = len(es.evs)
	for _, ev := range es.evs[min(from, evN):] {
		if s := traceLine(ev); s != "" {
			evs = append(evs, s)
		}
	}
	es.mu.Unlock()
	l.gw.mu.Lock()
	gwN = len(l.gw.lines)
	gws = slices.Clone(l.gw.lines[min(gwFrom, gwN):])
	l.gw.mu.Unlock()
	return evs, gws, evN, gwN
}

// has reports whether every wanted line (as often as listed) is pending.
func has(lines []string, want []string) bool {
	count := map[string]int{}
	for _, s := range lines {
		count[s]++
	}
	for _, w := range want {
		if count[w] == 0 {
			return false
		}
		count[w]--
	}
	return true
}

// section waits for the lines the last action must produce (want, and
// the golden trace's lines for this section, given up on after 5 s so the
// comparison reports a difference), then for quiet (nothing new for
// 200 ms; far above the test debounce), and appends the pending lines
// under label.
func (l *lifecycle) section(label string, want ...string) {
	l.t.Helper()
	expected := l.goldenSection(l.sections)
	l.sections++
	start := time.Now()
	for {
		evs, gws, _, _ := l.pending()
		lines := append(evs, gws...)
		if has(lines, want) {
			if has(lines, expected) || time.Since(start) > 5*time.Second {
				break
			}
		} else if time.Since(start) > 10*time.Second {
			l.t.Fatalf("%s: waiting for %v; pending events %v, gateway %v", label, want, evs, gws)
		}
		time.Sleep(20 * time.Millisecond)
	}
	prev := -1
	for i := 0; i < 10; i++ {
		evs, gws, _, _ := l.pending()
		if n := len(evs) + len(gws); n == prev {
			break
		} else {
			prev = n
		}
		time.Sleep(200 * time.Millisecond)
	}
	evs, gws, evN, gwN := l.pending()
	l.mu.Lock()
	l.evIdx, l.gwIdx = evN, gwN
	l.out = append(l.out, "# "+label)
	last := ""
	for _, s := range evs {
		if strings.HasPrefix(s, "check:") && s == last {
			continue
		}
		l.out = append(l.out, s)
		last = s
	}
	l.out = append(l.out, gws...)
	l.mu.Unlock()
}

// goldenSection returns the lines of the golden trace's section i (notes
// left out), none when the trace is being rewritten or does not exist.
func (l *lifecycle) goldenSection(i int) []string {
	if os.Getenv("UPDATE_TRACES") != "" {
		return nil
	}
	b, err := os.ReadFile(l.tracePath())
	if err != nil {
		return nil
	}
	n := -1
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			n++
		case n == i && line != "" && !strings.HasPrefix(line, "= "):
			out = append(out, line)
		}
	}
	return out
}

func (l *lifecycle) tracePath() string {
	name := l.t.Name()
	if i := strings.IndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return filepath.Join("testdata", "traces", name+".trace")
}

// note adds a line stating what the scenario checked.
func (l *lifecycle) note(format string, args ...any) {
	l.mu.Lock()
	l.out = append(l.out, "= "+fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

// golden compares the trace with testdata/traces/<name>.trace.
func (l *lifecycle) golden() {
	l.t.Helper()
	got := strings.Join(l.out, "\n") + "\n"
	path := l.tracePath()
	if os.Getenv("UPDATE_TRACES") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			l.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			l.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		l.t.Fatalf("%v (UPDATE_TRACES=1 writes it)", err)
	}
	if got != string(want) {
		l.t.Errorf("trace %s differs (UPDATE_TRACES=1 rewrites it):\n%s", path, lineDiff(string(want), got))
	}
}

// lineDiff lists the lines of want and got that differ, aligned by index.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < max(len(w), len(g)); i++ {
		var a, c string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			c = g[i]
		}
		mark := "  "
		if a != c {
			mark = "≠ "
		}
		fmt.Fprintf(&b, "%s%-45s | %s\n", mark, a, c)
	}
	return b.String()
}

// --- actions --------------------------------------------------------------------------

func (l *lifecycle) rpc(op string, body, out any) int {
	l.t.Helper()
	return l.server().rpc(op, body, out)
}

func (l *lifecycle) load(req protocol.ScenarioRequest) protocol.ScenarioResult {
	l.t.Helper()
	var res protocol.ScenarioResult
	if code := l.rpc("scenario", req, &res); code != 200 {
		l.t.Fatalf("scenario %+v: %d", req, code)
	}
	return res
}

// play types a demo through rpc keys (the browser's "Watch it" and a
// learner's keys), ten times faster than the browser.
func (l *lifecycle) play(demo string) {
	l.t.Helper()
	err := protocol.PlayDemo(context.Background(), demo, 10, func(_ context.Context, req protocol.KeysRequest) error {
		if code := l.rpc("keys", req, nil); code != 200 {
			return fmt.Errorf("keys %+v: %d", req, code)
		}
		return nil
	})
	if err != nil {
		l.t.Fatal(err)
	}
}

func (l *lifecycle) keys(keys string) {
	l.t.Helper()
	if code := l.rpc("keys", protocol.KeysRequest{Keys: keys}, nil); code != 200 {
		l.t.Fatalf("keys %s: %d", keys, code)
	}
}

// completeOpen loads lesson A and completes its first step.
func (l *lifecycle) loadA() {
	l.load(protocol.ScenarioRequest{LessonID: lessonA})
	l.section("load "+lessonA, "step:open:loaded", "gw:"+lessonA+"/open:in-progress:gen0")
}

func (l *lifecycle) completeOpen() {
	l.play(demoOpen)
	l.section("learner opens b.txt", "step:open:complete", "step:write:loaded", "gw:"+lessonA+"/write:in-progress:gen0")
}

// editorNote notes a.txt's and b.txt's buffers and the kill ring.
func (l *lifecycle) editorNote(when string) {
	for _, name := range []string{"a.txt", "b.txt"} {
		if b := l.ed.Buffer(name); b != nil {
			l.note("%s %s: %q modified=%v undo=%d", name, when, b.Text, b.Modified, len(b.Undo))
		} else {
			l.note("%s %s: no buffer", name, when)
		}
	}
	kr, _ := json.Marshal(l.ed.Snapshot().Globals["kill-ring"])
	l.note("kill ring %s: %s", when, kr)
}

// whileHeld sends op and, while the editor holds the agent op held, the
// request goes away; it returns the status the request saw (0: none).
func (l *lifecycle) whileHeld(held protocol.AgentOp, op string, body any) int {
	l.t.Helper()
	arrived, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	l.ed.SetFault(held, fakeeditor.Fault{Hook: func() {
		once.Do(func() { close(arrived) })
		<-release
	}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.server().rpcCtx(ctx, op, body) }()
	<-arrived
	cancel()
	code := <-done
	l.ed.ClearFaults()
	close(release)
	return code
}

// diesAt runs git until the command op, where sessiond dies (die) and git
// fails.
type diesAt struct {
	checkpoint.Runner
	op  string
	die func()
}

func (r diesAt) Run(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	if slices.Contains(args, r.op) {
		r.die()
		return nil, errors.New("sessiond died")
	}
	return r.Runner.Run(ctx, dir, env, args...)
}

// statePath is state.json.
func (l *lifecycle) statePath() string { return filepath.Join(l.ws, ".playground", "state.json") }

// writeState writes state.json (before a start).
func (l *lifecycle) writeState(content string) {
	l.t.Helper()
	if err := os.MkdirAll(filepath.Dir(l.statePath()), 0o755); err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(l.statePath(), []byte(content), 0o644); err != nil {
		l.t.Fatal(err)
	}
}

// readStateFile reads state.json as JSON.
func (l *lifecycle) readStateFile() map[string]any {
	l.t.Helper()
	b, err := os.ReadFile(l.statePath())
	if err != nil {
		l.t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(b, &st); err != nil {
		l.t.Fatal(err)
	}
	return st
}

// editState rewrites state.json (while sessiond is stopped).
func (l *lifecycle) editState(edit func(map[string]any)) {
	l.t.Helper()
	st := l.readStateFile()
	edit(st)
	b, _ := json.Marshal(st)
	l.writeState(string(b))
}

// outboxNote lists the outbox entries in state.json as step:status.
func (l *lifecycle) outboxNote() string {
	var st struct {
		Outbox []protocol.ProgressReport `json:"outbox"`
	}
	b, _ := os.ReadFile(l.statePath())
	_ = json.Unmarshal(b, &st)
	var out []string
	for _, e := range st.Outbox {
		out = append(out, fmt.Sprintf("%s:%s:gen%d", e.StepID, e.Status, e.Generation))
	}
	return "[" + strings.Join(out, " ") + "]"
}

// waitFor polls cond for up to 2 s.
func (l *lifecycle) waitFor(what string, cond func() bool) {
	l.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			l.t.Fatalf("waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// --- scenarios (notes.md §3, behavioural harness) -----------------------------------------

func TestLifecycle(t *testing.T) {
	scenarios := []struct {
		name string
		opts []func(*lifecycle)
		run  func(l *lifecycle)
	}{
		{name: "complete-three-steps", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.play(demoWrite)
			l.section("learner writes done and saves", "step:write:complete", "step:split:loaded")
			l.play(demoSplit)
			l.section("learner splits", "step:split:complete:lesson-complete", "gw:"+lessonA+"/split:done:gen0")
		}},
		{name: "reset-mid-step", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.play(`"xx"`)
			if code := l.rpc("restore", protocol.RestoreRequest{}, nil); code != 200 {
				l.t.Fatalf("restore %d", code)
			}
			l.section("Reset", "step:write:reset", "gw:"+lessonA+"/write:in-progress:gen0")
			b := l.ed.Buffer("b.txt")
			l.note("b.txt after Reset: %q modified=%v", b.Text, b.Modified)
		}},
		{name: "do-it-yourself", run: func(l *lifecycle) {
			l.loadA()
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, nil)
			l.play(demoOpen)
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoEnd}, nil)
			l.section("Watch it", "step:open:demonstrated", "gw:"+lessonA+"/open:demonstrated:gen0")
			if code := l.rpc("restore", protocol.RestoreRequest{}, nil); code != 200 {
				l.t.Fatalf("restore %d", code)
			}
			l.section("Do it yourself", "step:open:reset")
			l.play(demoOpen)
			l.section("learner opens b.txt", "step:open:complete", "gw:"+lessonA+"/open:done:gen0")
		}},
		{name: "watch-it-clean", run: func(l *lifecycle) {
			l.loadA()
			var dr protocol.DemoResult
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, &dr)
			l.note("demo start restored=%v", dr.Restored)
			l.play(demoOpen)
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoEnd}, nil)
			l.section("Watch it", "step:open:demonstrated", "gw:"+lessonA+"/open:demonstrated:gen0")
			var st protocol.StatusResult
			l.rpc("status", nil, &st)
			l.note("active after the demo: %s", st.Step.StepID)
		}},
		{name: "watch-it-dirty", run: func(l *lifecycle) {
			l.loadA()
			l.keys("C-n")
			var dr protocol.DemoResult
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, &dr)
			l.note("demo start restored=%v", dr.Restored)
			l.section("demo start after a command", "step:open:reset")
			l.play(demoOpen)
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoEnd}, nil)
			l.section("the demo plays", "step:open:demonstrated", "gw:"+lessonA+"/open:demonstrated:gen0")
		}},
		{name: "watch-it-abandoned-by-pty-detach", run: func(l *lifecycle) {
			l.loadA()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			pc, _, err := websocket.Dial(ctx, strings.Replace(l.server().base, "http", "ws", 1)+"/pty?cols=80&rows=20", nil)
			if err != nil {
				l.t.Fatal(err)
			}
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, nil)
			l.play(`C-x C-f`)
			_ = pc.Close(websocket.StatusNormalClosure, "page gone")
			l.waitFor("the demo to be abandoned", func() bool { return !l.server().srv.core.Watching() })
			l.section("the page with the demo goes away")
			l.play(`"b.txt" RET`)
			l.section("learner finishes the step", "step:open:complete", "gw:"+lessonA+"/open:done:gen0")
		}},
		{name: "watch-it-idle-expiry", opts: []func(*lifecycle){withClock}, run: func(l *lifecycle) {
			l.loadA()
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, nil)
			l.clock.Advance(66 * time.Second)
			// A key typed in the terminal (not rpc keys, which is demo traffic).
			if err := l.ed.Press("C-n"); err != nil {
				l.t.Fatal(err)
			}
			l.waitFor("the idle demo to expire", func() bool { return !l.server().srv.core.Watching() })
			l.section("the demo idles past DemoIdle")
			l.play(demoOpen)
			l.section("learner opens b.txt", "step:open:complete", "gw:"+lessonA+"/open:done:gen0")
		}},
		{name: "skip-clean", run: func(l *lifecycle) {
			l.loadA()
			var sk protocol.SkipResult
			if code := l.rpc("skip", protocol.SkipRequest{StepID: "open"}, &sk); code != 200 {
				l.t.Fatalf("skip %d", code)
			}
			l.note("skip next=%s demoCompleted=%v", *sk.Next, sk.DemoCompleted)
			l.section("Skip", "step:open:skipped", "step:write:loaded", "gw:"+lessonA+"/open:skipped:gen0")
		}},
		{name: "skip-dirty", run: func(l *lifecycle) {
			l.loadA()
			l.keys("C-n")
			var sk protocol.SkipResult
			if code := l.rpc("skip", protocol.SkipRequest{StepID: "open"}, &sk); code != 200 {
				l.t.Fatalf("skip %d", code)
			}
			l.note("skip next=%s demoCompleted=%v", *sk.Next, sk.DemoCompleted)
			l.section("Skip after a command", "step:open:reset", "step:open:skipped", "step:write:loaded", "gw:"+lessonA+"/open:skipped:gen0")
		}},
		{name: "go-to-step-with-checkpoint", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.play(demoWrite)
			l.section("learner writes done", "step:write:complete", "step:split:loaded")
			l.load(protocol.ScenarioRequest{LessonID: lessonA, StepID: "write"})
			l.section("Go to step write", "step:write:loaded")
			b := l.ed.Buffer("b.txt")
			l.note("b.txt after the restore: %q", b.Text)
		}},
		{name: "go-to-step-without-checkpoint", run: func(l *lifecycle) {
			l.load(protocol.ScenarioRequest{LessonID: lessonA, StepID: "split"})
			l.section("Go to step split on a fresh session", "step:split:loaded")
			disk, _ := os.ReadFile(filepath.Join(l.ws, "life", "b.txt"))
			l.note("life/b.txt after the fast-forward: %q", disk)
			l.note("find-file calls replayed: %v", l.ed.Buffer("b.txt") != nil)
		}},
		{name: "start-this-lesson-over", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.gw.Reset(l.t, lessonA)
			l.section("gateway reset of the lesson", "step:write:dropped")
			l.load(protocol.ScenarioRequest{LessonID: lessonA, Reset: true})
			l.section("load with reset", "step:open:loaded", "gw:"+lessonA+"/open:in-progress:gen1")
		}},
		{name: "start-over-during-a-step", run: func(l *lifecycle) {
			l.loadA()
			l.gw.Reset(l.t, "")
			l.section("gateway reset of everything", "step:open:dropped")
			var st protocol.StatusResult
			l.rpc("status", nil, &st)
			l.note("active after the drop: %v", st.Step)
		}},
		{name: "module-restart-on-load", run: func(l *lifecycle) {
			res := l.load(protocol.ScenarioRequest{LessonID: lessonB})
			l.note("scenario restarting=%v modules=%v", res.Restarting, res.Modules)
			l.section("load a lesson needing editing", "modules:false", "step:quit:loaded")
			l.note("hello modules after the restart: %v", l.ed.Snapshot().Modules)
		}},
		{name: "second-load-during-module-restart", run: func(l *lifecycle) {
			res := l.load(protocol.ScenarioRequest{LessonID: lessonB})
			l.note("first scenario restarting=%v", res.Restarting)
			res = l.load(protocol.ScenarioRequest{LessonID: lessonB})
			l.note("second scenario restarting=%v step=%s", res.Restarting, res.Step.StepID)
			l.section("load twice", "modules:false", "step:quit:loaded")
		}},
		{name: "rpc-modules", run: func(l *lifecycle) {
			var mr protocol.ModulesResult
			l.rpc("modules", protocol.ModulesRequest{Enabled: []string{"base", "agent", "files"}}, &mr)
			l.note("modules restarting=%v", mr.Restarting)
			l.section("rpc modules", "modules:false")
		}},
		{name: "resume-with-snapshot", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.stop()
			snap, _ := os.ReadFile(filepath.Join(l.ws, ".playground", "snapshot.json"))
			var spec protocol.WindowSpec
			_ = json.Unmarshal(snap, &spec)
			l.note("snapshot.json layout %s", spec.Layout)
			l.start("?after=0")
			l.section("sessiond restarts", "step:write:loaded")
			l.note("open calls: %d, setup calls: %d", l.ed.CountCalls(protocol.AgentOpOpen), l.ed.CountCalls(protocol.AgentOpSetup))
		}},
		{name: "resume-without-snapshot", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.stop()
			_ = os.Remove(filepath.Join(l.ws, ".playground", "snapshot.json"))
			setups := l.ed.CountCalls(protocol.AgentOpSetup)
			l.start("?after=0")
			l.section("sessiond restarts without a snapshot", "step:write:loaded")
			var args protocol.AgentSetupArgs
			for _, c := range l.ed.Calls() {
				if c.Op == protocol.AgentOpSetup {
					_ = json.Unmarshal(c.Args, &args)
				}
			}
			l.note("setup calls %d, last reset=%v", l.ed.CountCalls(protocol.AgentOpSetup)-setups, args.Reset)
		}},
		{name: "drop-while-a-skip-plays", run: func(l *lifecycle) {
			l.loadA()
			done := make(chan int, 1)
			go func() { done <- l.server().rpc("skip", protocol.SkipRequest{StepID: "open"}, nil) }()
			l.waitFor("the demo to start", func() bool { return l.ed.Snapshot().Minibuffer != nil })
			l.gw.Reset(l.t, lessonA)
			code := <-done
			l.note("skip answered %d", code)
			l.section("gateway reset while the skip plays", "step:open:dropped")
			// Re-recorded in the session core phase (defect fix): the skip
			// is an op, so the drop cancels it and the rest of its demo
			// never reaches Emacs (it used to visit b.txt after the drop).
			l.note("b.txt visited by the rest of the demo: %v", l.ed.Buffer("b.txt") != nil)
		}},
		{name: "drop-during-fast-forward", run: func(l *lifecycle) {
			// The cancelled load ends its rehearsal: the step active
			// before it is the one the drop forgets.
			l.loadA()
			done := make(chan int, 1)
			go func() {
				done <- l.server().rpc("scenario", protocol.ScenarioRequest{LessonID: lessonA, StepID: "split"}, nil)
			}()
			l.waitFor("the replay to open a prompt", func() bool { return l.ed.Snapshot().Minibuffer != nil })
			start := time.Now()
			l.gw.Reset(l.t, lessonA)
			took := time.Since(start)
			l.note("scenario answered %d", <-done)
			l.note("dropped within 1 s: %v", took < time.Second)
			l.section("gateway reset during the fast-forward", "step:open:dropped")
			time.Sleep(500 * time.Millisecond)
			l.section("later")
			var st protocol.StatusResult
			l.rpc("status", nil, &st)
			l.note("active after the drop: %v", st.Step)
		}},
		{name: "request-gone-mid-load", run: func(l *lifecycle) {
			code := l.whileHeld(protocol.AgentOpSetup, "scenario", protocol.ScenarioRequest{LessonID: lessonA})
			l.note("scenario answered %d after the page went away", code)
			l.section("the load runs on", "step:open:loaded", "gw:"+lessonA+"/open:in-progress:gen0")
		}},
		{name: "request-gone-mid-restore", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.play(`"xx"`)
			code := l.whileHeld(protocol.AgentOpRestoreEditor, "restore", protocol.RestoreRequest{})
			l.note("restore answered %d after the page went away", code)
			l.section("the Reset runs on", "step:write:reset", "gw:"+lessonA+"/write:in-progress:gen0")
			b := l.ed.Buffer("b.txt")
			l.note("b.txt after the Reset: %q modified=%v", b.Text, b.Modified)
		}},
		{name: "gateway-down-during-completion", run: func(l *lifecycle) {
			l.loadA()
			l.gw.Down()
			l.play(demoOpen)
			l.section("learner completes with the gateway down", "step:open:complete", "step:write:loaded")
			l.gw.Up()
			l.section("the gateway comes back", "gw:"+lessonA+"/open:done:gen0")
			time.Sleep(time.Second)
			l.section("later")
		}},
		{name: "gateway-reset-while-stopped", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.stop()
			l.gw.Reset(l.t, lessonA)
			l.start("?after=0")
			l.section("sessiond restarts after the reset")
			var st protocol.StatusResult
			l.rpc("status", nil, &st)
			l.note("active after the restart: %v", st.Step)
			l.load(protocol.ScenarioRequest{LessonID: lessonA})
			l.section("load the lesson again", "step:open:loaded")
		}},
		{name: "gateway-replace-then-restart", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.play(demoWrite)
			l.section("learner writes done", "step:write:complete", "step:split:loaded")
			l.play(demoSplit)
			l.section("learner splits", "step:split:complete:lesson-complete", "gw:"+lessonA+"/split:done:gen0")
			l.stop()
			l.gw.Replace()
			l.start("?after=0")
			l.section("sessiond restarts against an empty store",
				"gw:"+lessonA+"/open:done:gen0", "gw:"+lessonA+"/write:done:gen0", "gw:"+lessonA+"/split:done:gen0")
		}},
		{name: "duplicate-lap-delivery", opts: []func(*lifecycle){withClock}, run: func(l *lifecycle) {
			res := l.load(protocol.ScenarioRequest{LessonID: lessonA, Lap: true})
			l.note("lap started: %v, id %s", res.Lap != nil, res.Lap.ID)
			l.section("start the lap", "step:open:loaded")
			l.clock.Advance(time.Second)
			l.play(demoOpen)
			l.section("open", "step:write:loaded")
			l.clock.Advance(time.Second)
			l.play(demoWrite)
			l.section("write", "step:split:loaded")
			l.clock.Advance(time.Second)
			l.play(demoSplit)
			l.section("split", "step:split:complete:lesson-complete", "gw:"+lessonA+"/split:done:gen0:lap3000:"+res.Lap.ID)
			l.gw.mu.Lock()
			last := l.gw.progress[len(l.gw.progress)-1]
			l.gw.mu.Unlock()
			b, _ := json.Marshal(last)
			resp, err := http.Post(l.gwURL+"/internal/sessions/s1/progress", "application/json", bytes.NewReader(b))
			if err != nil {
				l.t.Fatal(err)
			}
			resp.Body.Close()
			l.section("the same report delivered again", "gw:"+lessonA+"/split:done:gen0:lap3000:"+res.Lap.ID)
			l.note("laps counted: %d", l.gw.lapCount(lessonA))
		}},
		{name: "lap-refuses-help", opts: []func(*lifecycle){withClock}, run: func(l *lifecycle) {
			res := l.load(protocol.ScenarioRequest{LessonID: lessonA, Lap: true})
			l.section("start the lap", "step:open:loaded")
			l.note("hint answered %d", l.rpc("hint", protocol.HintRequest{StepID: "open", Level: 1}, nil))
			l.note("demo start answered %d", l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, nil))
			l.note("skip answered %d", l.rpc("skip", protocol.SkipRequest{StepID: "open"}, nil))
			l.section("hint, demo and skip during the lap")
			l.clock.Advance(2 * time.Second)
			l.play(demoOpen)
			l.section("open", "step:write:loaded")
			l.play(demoWrite)
			l.section("write", "step:split:loaded")
			l.play(demoSplit)
			l.section("split", "step:split:complete:lesson-complete", "gw:"+lessonA+"/split:done:gen0:lap2000:"+res.Lap.ID)
			l.note("laps counted: %d", l.gw.lapCount(lessonA))
		}},
		{name: "agent-disconnected-during-restore", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.play(`"xx"`)
			l.ed.Disconnect()
			l.waitFor("the agent to drop", func() bool { return !l.server().srv.agent.Connected() })
			code := l.rpc("restore", protocol.RestoreRequest{}, nil)
			l.note("restore answered %d", code)
			l.section("Reset without an agent", "step:write:reset")
			spec, _ := os.ReadFile(filepath.Join(l.ws, ".playground", "snapshot.json"))
			l.note("snapshot.json is the checkpoint's: %v", strings.Contains(string(spec), `"layout":"life/b.txt"`))
			l.ed.Reconnect()
			l.waitFor("the agent to reconnect", func() bool { return l.server().srv.agent.Connected() })
			// The agent's hello gets the checkpoint's editor state.
			l.waitFor("the editor state", func() bool { b := l.ed.Buffer("b.txt"); return b != nil && !b.Modified })
			l.section("the agent comes back")
			b := l.ed.Buffer("b.txt")
			l.note("b.txt in Emacs after the restore: %q modified=%v", b.Text, b.Modified)
		}},
		{name: "agent-restore-step-failure", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.ed.Fail(protocol.AgentOpRestoreEditor, errors.New("busy"))
			code := l.rpc("restore", protocol.RestoreRequest{}, nil)
			l.note("restore answered %d", code)
			l.section("Reset while restore-editor fails")
			l.ed.ClearFaults()
			var st protocol.StatusResult
			l.rpc("status", nil, &st)
			l.note("active: %s", st.Step.StepID)
		}},
		{name: "stopped-between-restore-phases", run: func(l *lifecycle) {
			l.loadA()
			l.completeOpen()
			l.play(`"xx"`)
			// Emacs and sessiond die after git restored the files, before
			// the editor was reset or the step activated: nothing flushes
			// the unsaved edit.
			ts := l.server()
			l.ed.SetFault(protocol.AgentOpRestoreEditor, fakeeditor.Fault{Err: errors.New("Emacs died"), Hook: func() {
				l.ed.Disconnect()
				ts.cancel()
			}})
			code := l.rpc("restore", protocol.RestoreRequest{}, nil)
			l.note("restore answered %d", code)
			l.stop()
			l.ed.ClearFaults()
			disk, _ := os.ReadFile(filepath.Join(l.ws, "life", "b.txt"))
			l.note("life/b.txt after the crash: %q", disk)
			l.ed.Reconnect()
			l.start("?after=0")
			l.section("sessiond restarts", "step:write:loaded", "check:write:false")
			l.editorNote("after the restart")
			if code := l.rpc("restore", protocol.RestoreRequest{}, nil); code != 200 {
				l.t.Fatalf("restore after the restart: %d", code)
			}
			l.section("Reset again", "step:write:reset", "check:write:false")
			disk, _ = os.ReadFile(filepath.Join(l.ws, "life", "b.txt"))
			l.note("life/b.txt after Reset: %q", disk)
		}},
		{name: "stopped-between-manifest-and-ref", run: func(l *lifecycle) {
			// sessiond dies after the first step's manifest is written and
			// before its ref is published: no checkpoint exists, and the
			// next load takes it again.
			ts := l.server()
			ts.srv.snap.Files.Git = diesAt{Runner: ts.srv.snap.Files.Git, op: "update-ref", die: ts.cancel}
			l.note("scenario answered %d", l.rpc("scenario", protocol.ScenarioRequest{LessonID: lessonA}, nil))
			l.stop()
			_, err := os.Stat(filepath.Join(l.ws, ".playground", "checkpoints", "01-life", "01-three", "open.json"))
			l.note("manifest written: %v", err == nil)
			l.start("?after=0")
			l.load(protocol.ScenarioRequest{LessonID: lessonA})
			l.section("load again", "checkpoint:"+lessonA+"/open", "step:open:loaded")
		}},
		{name: "legacy-ref-without-manifest", run: func(l *lifecycle) {
			// A ref whose manifest is missing (taken before manifests were
			// written first) is no checkpoint: restoring it is a 404, it is
			// not listed, and loading its step takes it again.
			l.loadA()
			l.completeOpen()
			ref := checkpoint.StepRef(lessonA, "open")
			if err := os.Remove(filepath.Join(l.ws, ".playground", "checkpoints", "01-life", "01-three", "open.json")); err != nil {
				l.t.Fatal(err)
			}
			l.note("restore of the ref answered %d", l.rpc("restore", protocol.RestoreRequest{Ref: ref}, nil))
			var cps protocol.CheckpointsResult
			l.rpc("checkpoints", nil, &cps)
			for _, it := range cps.Items {
				l.note("listed %s", it.Label)
			}
			l.load(protocol.ScenarioRequest{LessonID: lessonA, StepID: "open"})
			l.section("Go to step open", "checkpoint:"+lessonA+"/open", "step:open:loaded")
		}},
		{name: "watch-it-leaves-no-kills-or-history", run: func(l *lifecycle) {
			l.ed.Update(func(m *fakeeditor.Model) { m.Globals["extended-command-history"] = []string{"learner-command"} })
			l.loadA()
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoStart}, nil)
			l.play(`C-k`)
			l.ed.Update(func(m *fakeeditor.Model) {
				m.Globals["extended-command-history"] = []string{"demo-command", "learner-command"}
			})
			l.play(demoOpen)
			l.rpc("demo", protocol.DemoRequest{StepID: "open", Phase: protocol.DemoEnd}, nil)
			l.section("Watch it kills a line", "step:open:demonstrated", "gw:"+lessonA+"/open:demonstrated:gen0")
			l.editorNote("after the demo")
			if code := l.rpc("restore", protocol.RestoreRequest{}, nil); code != 200 {
				l.t.Fatalf("restore %d", code)
			}
			l.section("Do it yourself", "step:open:reset")
			l.editorNote("after Do it yourself")
			h, _ := json.Marshal(l.ed.Snapshot().Globals["extended-command-history"])
			l.note("command history after Do it yourself: %s", h)
		}},
		{name: "go-to-step-after-an-unsaved-edit", run: func(l *lifecycle) {
			l.loadA()
			// An unsaved edit and a kill in a.txt before the step
			// completes: the next step's checkpoint keeps both.
			l.play(`"xx" C-k`)
			l.section("learner edits a.txt without saving")
			l.completeOpen()
			l.play(demoWrite)
			l.section("learner writes done", "step:write:complete", "step:split:loaded", "check:split:false")
			l.play(`C-k`)
			l.section("learner kills in b.txt")
			l.editorNote("before Go to")
			l.load(protocol.ScenarioRequest{LessonID: lessonA, StepID: "write"})
			l.section("Go to step write", "step:write:loaded", "check:write:false")
			l.editorNote("after Go to")
			l.play(`"yy"`)
			var rr protocol.RestoreResult
			if code := l.rpc("restore", protocol.RestoreRequest{}, &rr); code != 200 {
				l.t.Fatalf("restore %d", code)
			}
			l.note("Reset restoredFiles=%d", rr.RestoredFiles)
			l.section("Reset", "step:write:reset", "check:write:false")
			l.editorNote("after Reset")
		}},
		{name: "self-writing-shell-condition", run: func(l *lifecycle) {
			l.load(protocol.ScenarioRequest{LessonID: lessonC})
			l.section("load "+lessonC, "step:flag:loaded")
			if err := os.WriteFile(filepath.Join(l.ws, "sh", "done.flag"), nil, 0o644); err != nil {
				l.t.Fatal(err)
			}
			l.section("the learner makes the flag", "step:flag:complete", "step:last:loaded", "gw:"+lessonC+"/flag:done:gen0")
			time.Sleep(500 * time.Millisecond)
			l.section("later")
		}},
		{name: "gateway-down-during-three-completions", run: func(l *lifecycle) {
			l.loadA()
			l.gw.Down()
			l.play(demoOpen)
			l.section("open with the gateway down", "step:write:loaded")
			l.play(demoWrite)
			l.section("write with the gateway down", "step:split:loaded")
			l.play(demoSplit)
			l.section("split with the gateway down", "step:split:complete:lesson-complete")
			l.gw.Up()
			l.section("the gateway comes back",
				"gw:"+lessonA+"/open:done:gen0", "gw:"+lessonA+"/write:done:gen0", "gw:"+lessonA+"/split:done:gen0")
			time.Sleep(time.Second)
			l.section("later")
		}},
		{name: "gateway-reset-while-stopped-with-pending-reports", run: func(l *lifecycle) {
			l.loadA()
			l.gw.Down()
			l.play(demoOpen)
			l.section("open with the gateway down", "step:write:loaded")
			l.stop()
			l.note("pending in state.json: %s", l.outboxNote())
			l.gw.Reset(l.t, lessonA)
			l.gw.Up()
			l.start("?after=0")
			l.section("sessiond restarts after the reset")
			l.note("pending after the restart: %s", l.outboxNote())
			l.load(protocol.ScenarioRequest{LessonID: lessonA})
			l.section("load the lesson again", "step:open:loaded", "gw:"+lessonA+"/open:in-progress:gen1")
		}},
		{name: "stopped-after-delivery-before-persisting", opts: []func(*lifecycle){withClock}, run: func(l *lifecycle) {
			l.load(protocol.ScenarioRequest{LessonID: lessonA, Lap: true})
			l.section("start the lap", "step:open:loaded")
			l.clock.Advance(time.Second)
			l.play(demoOpen)
			l.section("open", "step:write:loaded", "gw:"+lessonA+"/write:in-progress:gen0")
			l.clock.Advance(time.Second)
			l.play(demoWrite)
			l.section("write", "step:split:loaded", "gw:"+lessonA+"/split:in-progress:gen0")
			l.clock.Advance(time.Second)
			l.play(demoSplit)
			l.section("split ends the lap", "step:split:complete:lesson-complete")
			l.stop()
			// sessiond stopped after the gateway accepted the lap but
			// before state.json lost it.
			l.gw.mu.Lock()
			last := l.gw.progress[len(l.gw.progress)-1]
			before := maps.Clone(l.gw.store)
			l.gw.mu.Unlock()
			l.editState(func(st map[string]any) {
				st["outbox"] = []protocol.ProgressReport{last}
			})
			l.start("?after=0")
			l.section("sessiond restarts", "gw:"+lessonA+"/split:done:gen0:lap3000:"+last.LapID)
			l.gw.mu.Lock()
			unchanged := maps.Equal(before, l.gw.store)
			l.gw.mu.Unlock()
			l.note("gateway progress unchanged: %v, laps counted: %d", unchanged, l.gw.lapCount(lessonA))
		}},
		{name: "state-version-1-upgrade", opts: []func(*lifecycle){func(l *lifecycle) {
			l.writeState(`{"version": 1, "lessons": {"` + lessonA + `": {"done": {"open": 100, "write": 200}, "hints": {"write": 1}}}}`)
		}}, run: func(l *lifecycle) {
			l.section("sessiond starts from a version 1 state.json", "gw:"+lessonA+"/open:done:gen0", "gw:"+lessonA+"/write:done:gen0")
			l.note("state.json version %v, pending %s", l.readStateFile()["version"], l.outboxNote())
		}},
		{name: "two-event-streams", run: func(l *lifecycle) {
			es2 := l.server().events("")
			es2.first()
			l.loadA()
			l.load(protocol.ScenarioRequest{LessonID: lessonD})
			l.section("the second tab loads "+lessonD, "step:quit:loaded")
			es2.wait("step:quit:loaded")
			l.note("the second stream saw %s loaded", lessonD)
		}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			l := newLifecycle(t, sc.opts...)
			sc.run(l)
			l.golden()
		})
	}
}
