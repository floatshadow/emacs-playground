package steps

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// fakeChecker answers elisp forms from a table; unknown forms are unknown.
type fakeChecker struct {
	mu      sync.Mutex
	answers map[string]protocol.Truth
	err     error
	calls   [][]string
}

func (f *fakeChecker) Check(ctx context.Context, forms []string, timeout time.Duration) ([]protocol.Truth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), forms...))
	if f.err != nil {
		return nil, f.err
	}
	out := make([]protocol.Truth, len(forms))
	for i, form := range forms {
		t, ok := f.answers[form]
		if !ok {
			t = protocol.Unknown
		}
		out[i] = t
	}
	return out, nil
}

func (f *fakeChecker) set(form string, t protocol.Truth) {
	f.mu.Lock()
	if f.answers == nil {
		f.answers = map[string]protocol.Truth{}
	}
	f.answers[form] = t
	f.mu.Unlock()
}

func (f *fakeChecker) nCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeShell answers scripts from a table and counts runs.
type fakeShell struct {
	mu   sync.Mutex
	runs map[string]int
	res  map[string]ShellResult
}

func (f *fakeShell) Run(ctx context.Context, dir, script string, timeout time.Duration) ShellResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runs == nil {
		f.runs = map[string]int{}
	}
	f.runs[script]++
	return f.res[script]
}

type sink struct {
	mu      sync.Mutex
	events  []protocol.Event
	reports []protocol.ProgressReport
	cps     []string
	saved   []Saved
}

func (s *sink) hooks() Hooks {
	return Hooks{
		Emit:   func(ev protocol.Event) { s.mu.Lock(); s.events = append(s.events, ev); s.mu.Unlock() },
		Report: func(r protocol.ProgressReport) { s.mu.Lock(); s.reports = append(s.reports, r); s.mu.Unlock() },
		Checkpoint: func(ctx context.Context, l, st string) {
			s.mu.Lock()
			s.cps = append(s.cps, l+"/"+st)
			s.mu.Unlock()
		},
		Persist: func(sv Saved) { s.mu.Lock(); s.saved = append(s.saved, sv); s.mu.Unlock() },
	}
}

func (s *sink) types() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, ev := range s.events {
		switch e := ev.(type) {
		case *protocol.StepEvent:
			out = append(out, "step:"+e.StepID+":"+string(e.Status))
		case *protocol.CheckEvent:
			out = append(out, "check:"+e.StepID)
		case *protocol.NudgeEvent:
			out = append(out, "nudge:"+e.ID)
		default:
			out = append(out, string(ev.Type()))
		}
	}
	return strings.Join(out, " ")
}

func (s *sink) clear() {
	s.mu.Lock()
	s.events, s.reports, s.cps = nil, nil, nil
	s.mu.Unlock()
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func scenarioFromJSON(t *testing.T, s string) *protocol.Scenario {
	t.Helper()
	var sc protocol.Scenario
	if err := json.Unmarshal([]byte(s), &sc); err != nil {
		t.Fatal(err)
	}
	sc.AssignIDs()
	return &sc
}

type fixture struct {
	e     *Engine
	sink  *sink
	elisp *fakeChecker
	shell *fakeShell
	clock *clock
	ws    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ws := t.TempDir()
	root, err := os.OpenRoot(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	f := &fixture{sink: &sink{}, elisp: &fakeChecker{}, shell: &fakeShell{}, clock: &clock{t: time.Unix(1761472000, 0)}, ws: ws}
	f.e = New(Options{Root: root, Workspace: ws, Checker: f.elisp, Shell: f.shell, Hooks: f.sink.hooks(), Now: f.clock.now})
	return f
}

func (f *fixture) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(f.ws, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cond(t *testing.T, s string) protocol.Condition {
	t.Helper()
	var c protocol.Condition
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConditionKinds(t *testing.T) {
	f := newFixture(t)
	f.write(t, "hello/src/main.rs", "fn main() { println!(\"Hi\"); }\n")
	f.write(t, "hello/Cargo.json", `{"package":{"name":"hello","version":"0.1.0","edition":2021},"deps":[{"name":"serde"}]}`)
	f.write(t, "big.txt", strings.Repeat("a", MaxFileRead)+"NEEDLE")
	f.shell.res = map[string]ShellResult{
		"cargo build": {OK: true, Output: "Finished"},
		"false":       {OK: false, Output: "nope"},
		"sleep 10":    {TimedOut: true},
	}
	f.elisp.set("(t)", protocol.True)
	f.elisp.set("(nil)", protocol.False)
	file := "/work/hello/src/main.rs"
	st := &protocol.State{
		Buf: "main.rs", File: &file, Mode: "rust-ts-mode", Narrowed: true,
		Windows:    []protocol.Window{{ID: 3, Buf: "main.rs<hello>", Selected: true}, {ID: 7, Buf: "*compilation*", Mode: "compilation-mode"}},
		Minibuffer: &protocol.Minibuffer{Kind: protocol.MinibufferPrompt, Prompt: "Find file: "},
	}
	_ = json.Unmarshal([]byte(`["vsplit",3,7]`), &st.Layout)

	cases := []struct {
		name string
		cond string
		want protocol.Truth
	}{
		{"event met", `{"event":{"command":"find-file"}}`, protocol.True},
		{"event min", `{"event":{"command":"find-file","min":3}}`, protocol.False},
		{"event lesson", `{"event":{"command":"save-buffer","since":"lesson"}}`, protocol.True},
		{"event step only", `{"event":{"command":"save-buffer"}}`, protocol.False},
		{"elisp true", `{"elisp":"(t)"}`, protocol.True},
		{"elisp false", `{"elisp":"(nil)"}`, protocol.False},
		{"elisp unknown", `{"elisp":"(error)"}`, protocol.Unknown},
		{"file exists", `{"file":{"path":"hello/src/main.rs","exists":true}}`, protocol.True},
		{"file exists absolute /work", `{"file":{"path":"/work/hello/src/main.rs","exists":true}}`, protocol.True},
		{"file missing", `{"file":{"path":"hello/src/lib.rs","exists":true}}`, protocol.False},
		{"file not exists", `{"file":{"path":"hello/src/lib.rs","exists":false}}`, protocol.True},
		{"file contains", `{"file":{"path":"hello/src/main.rs","contains":"println!"}}`, protocol.True},
		{"file contains missing file", `{"file":{"path":"nope.rs","contains":"x"}}`, protocol.False},
		{"file not_contains", `{"file":{"path":"hello/src/main.rs","not_contains":"Hello, world"}}`, protocol.True},
		{"file not_contains missing file", `{"file":{"path":"nope.rs","not_contains":"x"}}`, protocol.False},
		{"file regex", `{"file":{"path":"hello/src/main.rs","regex":"fn\\s+main\\(\\)"}}`, protocol.True},
		{"file bad regex", `{"file":{"path":"hello/src/main.rs","regex":"("}}`, protocol.Unknown},
		{"file json_path", `{"file":{"path":"hello/Cargo.json","json_path":"package.version","equals":"0.1.0"}}`, protocol.True},
		{"file json_path number", `{"file":{"path":"hello/Cargo.json","json_path":"package.edition","equals":2021}}`, protocol.True},
		{"file json_path index", `{"file":{"path":"hello/Cargo.json","json_path":"deps.0.name","equals":"serde"}}`, protocol.True},
		{"file json_path wrong", `{"file":{"path":"hello/Cargo.json","json_path":"package.name","equals":"world"}}`, protocol.False},
		{"file escape", `{"file":{"path":"../etc/passwd","exists":true}}`, protocol.Unknown},
		{"file capped at 1 MB", `{"file":{"path":"big.txt","contains":"NEEDLE"}}`, protocol.False},
		{"shell ok", `{"shell":{"run":"cargo build"}}`, protocol.True},
		{"shell fail", `{"shell":{"run":"false"}}`, protocol.False},
		{"shell timeout", `{"shell":{"run":"sleep 10"}}`, protocol.Unknown},
		{"shell missing cwd", `{"shell":{"run":"cargo build","cwd":"nowhere"}}`, protocol.False},
		{"layout", `{"layout":"(vsplit main.rs *compilation*)"}`, protocol.True},
		{"layout wildcard", `{"layout":"(vsplit * *)"}`, protocol.True},
		{"layout wrong orientation", `{"layout":"(hsplit main.rs *compilation*)"}`, protocol.False},
		{"layout single", `{"layout":"single"}`, protocol.False},
		{"layout bad", `{"layout":"(vsplit"}`, protocol.Unknown},
		{"state", `{"state":{"narrowed":true}}`, protocol.True},
		{"state dotted", `{"state":{"minibuffer.kind":"prompt"}}`, protocol.True},
		{"state op", `{"state":{"windows.length":{"gte":3}}}`, protocol.False},
		{"state otherWindows", `{"state":{"otherWindows.mode":{"in":["compilation-mode","help-mode"]}}}`, protocol.True},
		{"all true", `{"all":[{"elisp":"(t)"},{"event":{"command":"find-file"}}]}`, protocol.True},
		{"all with false", `{"all":[{"elisp":"(error)"},{"elisp":"(nil)"}]}`, protocol.False},
		{"all with unknown", `{"all":[{"elisp":"(t)"},{"elisp":"(error)"}]}`, protocol.Unknown},
		{"any true", `{"any":[{"elisp":"(error)"},{"elisp":"(t)"}]}`, protocol.True},
		{"any unknown", `{"any":[{"elisp":"(nil)"},{"elisp":"(error)"}]}`, protocol.Unknown},
		{"any false", `{"any":[{"elisp":"(nil)"},{"file":{"path":"nope","exists":true}}]}`, protocol.False},
		{"not", `{"not":{"elisp":"(nil)"}}`, protocol.True},
		{"not unknown", `{"not":{"elisp":"(error)"}}`, protocol.Unknown},
		{"nested", `{"all":[{"not":{"any":[{"elisp":"(nil)"},{"state":{"narrowed":false}}]}},{"layout":"(vsplit * *compilation*)"}]}`, protocol.True},
		{"empty", `{}`, protocol.Unknown},
	}
	ec := &evalContext{
		ctx:          context.Background(),
		counts:       map[string]int{"find-file": 1},
		lessonCounts: map[string]int{"find-file": 1, "save-buffer": 1},
		state:        st,
		stateDoc:     protocol.StateDoc(st),
		elisp:        map[string]protocol.Truth{"(t)": protocol.True, "(nil)": protocol.False},
		root:         f.e.opt.Root,
		workspace:    f.ws,
		setupRoot:    "hello",
		shell:        f.shell,
		shellCache:   &shellCache{},
		patterns:     &patternCache{},
	}
	for _, c := range cases {
		cc := cond(t, c.cond)
		if got := ec.eval(&cc); got.t != c.want {
			t.Errorf("%s: got %v want %v (%s)", c.name, got.t, c.want, got.detail)
		}
	}
	// Without a state, layout and state conditions are unknown.
	ec.state, ec.stateDoc = nil, nil
	for _, s := range []string{`{"layout":"single"}`, `{"state":{"narrowed":true}}`} {
		cc := cond(t, s)
		if got := ec.eval(&cc); got.t != protocol.Unknown {
			t.Errorf("%s without state: %v", s, got.t)
		}
	}
	// Shell results are cached until cleared.
	before := f.shell.runs["cargo build"]
	cc := cond(t, `{"shell":{"run":"cargo build"}}`)
	ec.eval(&cc)
	if f.shell.runs["cargo build"] != before {
		t.Fatal("shell result should be cached")
	}
	ec.shellCache.clear()
	ec.eval(&cc)
	if f.shell.runs["cargo build"] != before+1 {
		t.Fatal("cleared cache should re-run")
	}
}

func TestExecShell(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "x"), []byte("1"), 0o644)
	sh := ExecShell{}
	if r := sh.Run(context.Background(), dir, "test -f x && echo found", 5*time.Second); !r.OK || r.Output != "found\n" {
		t.Fatalf("ok: %+v", r)
	}
	if r := sh.Run(context.Background(), dir, "exit 3", 5*time.Second); r.OK || r.TimedOut || r.Err != nil {
		t.Fatalf("exit 3: %+v", r)
	}
	start := time.Now()
	// The child of the shell must die with it (process group kill).
	r := sh.Run(context.Background(), dir, "sleep 30 & sleep 30", 200*time.Millisecond)
	if !r.TimedOut || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout: %+v after %v", r, time.Since(start))
	}
	if r := sh.Run(context.Background(), dir, "yes | head -c 10000", 5*time.Second); len(r.Output) != MaxShellOutput {
		t.Fatalf("output cap: %d", len(r.Output))
	}
}

const lessonJSON = `{
  "id": "02-buffers-and-files/03-opening-files",
  "title": "Opening files",
  "modules": ["base","agent","editing","files"],
  "setup": {"workspace": "fixtures/hello-rust", "root": "hello"},
  "steps": [
    {"id": "open", "goal": "Open src/main.rs",
     "conditions": [
       {"text": "the Find file prompt was used", "event": {"command": "find-file"}},
       {"text": "main.rs is current", "elisp": "(equal (buffer-file-name) \"/work/hello/src/main.rs\")"}
     ],
     "stuck": [
       {"when": {"minibuffer_seconds": 30}, "say": "Still in the prompt? RET accepts, C-g cancels."},
       {"when": {"step_seconds": 300}, "say": "Reset this step.", "title": "Taking a while", "kind": "ask"}
     ]},
    {"id": "save", "goal": "Save it", "checkpoint": false,
     "conditions": [{"all": [{"event": {"command": "save-buffer"}}, {"file": {"path": "hello/src/main.rs", "not_contains": "Hello, world"}}]}]},
    {"id": "layout", "goal": "Code above, compile below",
     "conditions": [{"layout": "(vsplit main.rs *compilation*)"}],
     "stuck": [
       {"when": {"idle_seconds": 60}, "say": "Idle."},
       {"when": {"quits_in_window": {"count": 3, "seconds": 8}}, "say": "Three C-g."},
       {"when": {"unbound_repeats": 2}, "say": "Unbound twice."},
       {"when": {"idle_seconds": 10, "step_seconds": 100}, "say": "Both."}
     ]}
  ]
}`

func TestStepLifecycle(t *testing.T) {
	f := newFixture(t)
	f.write(t, "hello/src/main.rs", "fn main() { println!(\"Hello, world\"); }\n")
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()

	if _, ok := f.e.Check(ctx); ok {
		t.Fatal("check without a scenario must report no active step")
	}
	ref := f.e.Activate(sc, f.e.FirstIncomplete(sc))
	if ref.StepID != "open" {
		t.Fatalf("activate %+v", ref)
	}
	if f.sink.types() != "step:open:loaded" {
		t.Fatalf("events %s", f.sink.types())
	}
	if len(f.sink.reports) != 1 || f.sink.reports[0].Status != protocol.ProgressInProgress {
		t.Fatalf("in-progress report %+v", f.sink.reports)
	}
	f.sink.clear()

	// Nothing done yet.
	res, _ := f.e.Check(ctx)
	if res.Complete || res.Conditions[0].ID != "open/1" || res.Conditions[0].OK || !res.Conditions[1].Unknown {
		t.Fatalf("initial %+v", res)
	}
	// The elisp form was sent in one batch.
	if f.elisp.nCalls() != 1 || len(f.elisp.calls[0]) != 1 {
		t.Fatalf("elisp calls %v", f.elisp.calls)
	}
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file"})
	f.elisp.set(`(equal (buffer-file-name) "/work/hello/src/main.rs")`, protocol.True)
	f.sink.clear()
	res, _ = f.e.Check(ctx)
	if !res.Complete {
		t.Fatalf("should complete: %+v", res)
	}
	// check, complete, then the next step loads (its checkpoint is off).
	if got := f.sink.types(); got != "check:open step:open:complete step:save:loaded" {
		t.Fatalf("events %s", got)
	}
	if len(f.sink.cps) != 0 {
		t.Fatalf("save has checkpoint:false, got %v", f.sink.cps)
	}
	var done *protocol.ProgressReport
	for i := range f.sink.reports {
		if f.sink.reports[i].Status == protocol.ProgressDone {
			done = &f.sink.reports[i]
		}
	}
	if done == nil || done.StepID != "open" || done.At == 0 {
		t.Fatalf("done report %+v", f.sink.reports)
	}
	if a := f.e.Active(); a == nil || a.StepID != "save" {
		t.Fatalf("active %+v", a)
	}
	// Commands from the previous step do not count.
	f.sink.clear()
	res, _ = f.e.Check(ctx)
	if res.Complete || res.Conditions[0].ID != "save/1" {
		t.Fatalf("save initial %+v", res)
	}
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-x C-s", Cmd: "save-buffer"})
	res, _ = f.e.Check(ctx)
	if res.Complete {
		t.Fatal("file still has Hello, world")
	}
	f.write(t, "hello/src/main.rs", "fn main() {}\n")
	f.e.OnFile(&protocol.FileEvent{Path: "hello/src/main.rs", Event: protocol.FileModified})
	f.sink.clear()
	res, _ = f.e.Check(ctx)
	if !res.Complete {
		t.Fatalf("save should complete: %+v", res)
	}
	// Layout step takes a checkpoint before loading.
	if len(f.sink.cps) != 1 || f.sink.cps[0] != "02-buffers-and-files/03-opening-files/layout" {
		t.Fatalf("checkpoints %v", f.sink.cps)
	}
	st := &protocol.State{Buf: "main.rs", Windows: []protocol.Window{{ID: 1, Buf: "main.rs", Selected: true}, {ID: 2, Buf: "*compilation*"}}}
	_ = json.Unmarshal([]byte(`["vsplit",1,2]`), &st.Layout)
	f.e.OnState(st)
	f.sink.clear()
	res, _ = f.e.Check(ctx)
	if !res.Complete {
		t.Fatalf("layout should complete: %+v", res)
	}
	f.sink.mu.Lock()
	last := f.sink.events[len(f.sink.events)-1].(*protocol.StepEvent)
	f.sink.mu.Unlock()
	if !last.LessonComplete || last.Status != protocol.StepComplete || last.Index != 2 || last.Count != 3 {
		t.Fatalf("last step event %+v", last)
	}
	if f.e.Active() != nil {
		t.Fatal("no step is active after the lesson completes")
	}
	saved := f.e.Saved()
	if saved.Active != nil || len(saved.Lessons[sc.ID].Done) != 3 {
		t.Fatalf("saved %+v", saved.Lessons[sc.ID])
	}
	// A completed lesson restarts at the first step.
	if f.e.FirstIncomplete(sc) != 0 {
		t.Fatal("FirstIncomplete after completion")
	}
}

func TestHintsResetsDropAndPersistence(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	f.e.Activate(sc, 0)
	f.sink.clear()

	if !f.e.Hint("open", 1) || !f.e.Hint("open", 2) || !f.e.Hint("open", 1) {
		t.Fatal("hint")
	}
	if f.e.Hint("nope", 1) {
		t.Fatal("hint for unknown step")
	}
	if len(f.sink.reports) != 2 || *f.sink.reports[1].HintsUsed != 2 {
		t.Fatalf("hint reports %+v", f.sink.reports)
	}
	f.e.OnKey(&protocol.KeyEvent{Cmd: "find-file"})
	f.sink.clear()
	if !f.e.ResetTo(sc.ID, "open") {
		t.Fatal("reset")
	}
	if f.sink.types() != "step:open:reset" {
		t.Fatalf("reset events %s", f.sink.types())
	}
	r := f.sink.reports[0]
	if *r.Resets != 1 || *r.HintsUsed != 2 || r.Status != protocol.ProgressInProgress {
		t.Fatalf("reset report %+v", r)
	}
	res, _ := f.e.Check(ctx)
	if res.Conditions[0].OK {
		t.Fatal("counts must clear on reset")
	}
	if f.e.ResetTo("other/lesson", "open") {
		t.Fatal("reset of another lesson")
	}

	// Persisted state round-trips and drives FirstIncomplete.
	saved := f.e.Saved()
	saved.Lessons[sc.ID].Done["open"] = 1
	g := newFixture(t)
	g.e.Restore(saved)
	if g.e.FirstIncomplete(sc) != 1 {
		t.Fatalf("first incomplete %d", g.e.FirstIncomplete(sc))
	}
	if g.e.Active() != nil {
		t.Fatal("Restore must not activate")
	}

	if !f.e.Drop(sc.ID) || f.e.Active() != nil || f.e.Scenario() != nil {
		t.Fatal("drop")
	}
	if _, ok := f.e.Saved().Lessons[sc.ID]; ok {
		t.Fatal("drop must clear the lesson's progress")
	}
	if f.e.Drop(sc.ID) {
		t.Fatal("second drop")
	}
}

func TestElispErrorsAreUnknown(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	f.e.Activate(sc, 0)
	f.elisp.err = errors.New("agent: call timed out")
	res, _ := f.e.Check(context.Background())
	if !res.Conditions[1].Unknown || res.Conditions[1].OK || !strings.Contains(res.Conditions[1].Detail, "timed out") {
		t.Fatalf("got %+v", res.Conditions[1])
	}
}

func TestCheckEventsOnlyOnChange(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	f.e.Activate(sc, 0)
	f.sink.clear()
	ctx := context.Background()
	f.e.evaluate(ctx, false)
	f.e.evaluate(ctx, false)
	if f.sink.types() != "check:open" {
		t.Fatalf("unchanged results must not repeat: %s", f.sink.types())
	}
	f.e.OnKey(&protocol.KeyEvent{Cmd: "find-file"})
	f.e.evaluate(ctx, false)
	if f.sink.types() != "check:open check:open" {
		t.Fatalf("changed results: %s", f.sink.types())
	}
	f.e.Check(ctx) // forced
	if f.sink.types() != "check:open check:open check:open" {
		t.Fatalf("forced: %s", f.sink.types())
	}
}

func TestStuckRules(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	f.e.Activate(sc, 0)
	f.sink.clear()

	nudges := func() []*protocol.NudgeEvent {
		f.sink.mu.Lock()
		defer f.sink.mu.Unlock()
		var out []*protocol.NudgeEvent
		for _, ev := range f.sink.events {
			if n, ok := ev.(*protocol.NudgeEvent); ok {
				out = append(out, n)
			}
		}
		return out
	}

	// minibuffer_seconds: open for 30 s.
	f.e.OnMinibuffer(&protocol.MinibufferEvent{Open: true, Kind: protocol.MinibufferPrompt})
	f.clock.add(29 * time.Second)
	f.e.evaluate(ctx, false)
	if len(nudges()) != 0 {
		t.Fatal("too early")
	}
	f.clock.add(2 * time.Second)
	f.e.evaluate(ctx, false)
	n := nudges()
	if len(n) != 1 || n[0].ID != "open/stuck/1" || n[0].Kind != protocol.NudgeCalm || n[0].Title != "Still in the prompt?" {
		t.Fatalf("minibuffer nudge %+v", n)
	}
	// Edge-triggered: no repeat while it holds; re-arms after closing.
	f.e.evaluate(ctx, false)
	if len(nudges()) != 1 {
		t.Fatal("repeated nudge")
	}
	f.e.OnState(&protocol.State{Minibuffer: nil})
	f.e.evaluate(ctx, false)
	f.e.OnState(&protocol.State{Minibuffer: &protocol.Minibuffer{Kind: protocol.MinibufferPrompt}})
	f.clock.add(31 * time.Second)
	f.e.evaluate(ctx, false)
	if len(nudges()) != 2 {
		t.Fatalf("re-armed nudge: %d", len(nudges()))
	}
	// step_seconds with explicit title and kind.
	f.clock.add(300 * time.Second)
	f.e.evaluate(ctx, false)
	n = nudges()
	if len(n) != 3 || n[2].ID != "open/stuck/2" || n[2].Kind != protocol.NudgeAsk || n[2].Title != "Taking a while" {
		t.Fatalf("step nudge %+v", n[len(n)-1])
	}

	// Layout step: idle, quits, unbound, conjunction.
	g := newFixture(t)
	g.e.Activate(sc, 2)
	g.sink.clear()
	f = g
	g.clock.add(59 * time.Second)
	g.e.evaluate(ctx, false)
	if len(nudges()) != 0 {
		t.Fatal("idle too early")
	}
	g.clock.add(2 * time.Second)
	g.e.evaluate(ctx, false)
	if n := nudges(); len(n) != 1 || n[0].ID != "layout/stuck/1" {
		t.Fatalf("idle nudge %+v", n)
	}
	// A key resets idleness (and re-arms).
	g.e.OnKey(&protocol.KeyEvent{Cmd: "keyboard-quit"})
	g.clock.add(time.Second)
	g.e.OnKey(&protocol.KeyEvent{Cmd: "keyboard-quit"})
	g.e.evaluate(ctx, false)
	if len(nudges()) != 1 {
		t.Fatal("two quits are not three")
	}
	g.clock.add(time.Second)
	g.e.OnKey(&protocol.KeyEvent{Cmd: "abort-minibuffers"})
	g.e.evaluate(ctx, false)
	if n := nudges(); len(n) != 2 || n[1].ID != "layout/stuck/2" {
		t.Fatalf("quits nudge %+v", n)
	}
	// Quits outside the window do not count.
	g.clock.add(20 * time.Second)
	g.e.OnKey(&protocol.KeyEvent{Cmd: "keyboard-quit"})
	g.e.evaluate(ctx, false)
	g.e.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	g.e.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	g.e.evaluate(ctx, false)
	n = nudges()
	if len(n) != 3 || n[2].ID != "layout/stuck/3" {
		t.Fatalf("unbound nudge %+v", n)
	}
	// A different command breaks the run.
	g.e.OnKey(&protocol.KeyEvent{Keys: "C-n", Cmd: "next-line"})
	g.e.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	g.e.evaluate(ctx, false)
	if len(nudges()) != 3 {
		t.Fatal("unbound run should restart")
	}
	// A prefix cancelled with C-g is a quit, not an unbound key.
	g.e.OnKey(&protocol.KeyEvent{Keys: "C-x C-g", Cmd: "undefined"})
	g.e.OnKey(&protocol.KeyEvent{Keys: "C-x C-g", Cmd: "undefined"})
	g.e.evaluate(ctx, false)
	unbound := 0
	for _, x := range nudges() {
		if x.ID == "layout/stuck/3" {
			unbound++
		}
	}
	if unbound != 1 {
		t.Fatalf("a cancelled prefix counted as unbound: %s", g.sink.types())
	}
	// Conjunction: idle 10 s and step 100 s (step is now ~85 s old).
	g.clock.add(11 * time.Second)
	g.e.evaluate(ctx, false)
	for _, x := range nudges() {
		if x.ID == "layout/stuck/4" {
			t.Fatal("conjunction fired before step_seconds")
		}
	}
	g.clock.add(10 * time.Second)
	g.e.evaluate(ctx, false)
	found := false
	for _, x := range nudges() {
		found = found || x.ID == "layout/stuck/4"
	}
	if !found {
		t.Fatalf("conjunction nudge missing: %s", g.sink.types())
	}
}

// countingChecker counts evaluations through the elisp batch.
type countingChecker struct{ n atomic.Int32 }

func (c *countingChecker) Check(ctx context.Context, forms []string, timeout time.Duration) ([]protocol.Truth, error) {
	c.n.Add(1)
	return make([]protocol.Truth, len(forms)), nil
}

func TestDebounce(t *testing.T) {
	cc := &countingChecker{}
	s := &sink{}
	e := New(Options{Checker: cc, Hooks: s.hooks(), Debounce: 100 * time.Millisecond, Tick: time.Hour})
	e.Activate(scenarioFromJSON(t, lessonJSON), 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)
	// The activation trigger plus a burst of 20 events within the window
	// produce one evaluation.
	for i := 0; i < 20; i++ {
		e.OnKey(&protocol.KeyEvent{Cmd: "next-line"})
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(250 * time.Millisecond)
	if n := cc.n.Load(); n != 1 {
		t.Fatalf("evaluations after a burst: %d", n)
	}
	// Spaced triggers evaluate once each.
	for i := 0; i < 3; i++ {
		e.OnKey(&protocol.KeyEvent{Cmd: "next-line"})
		time.Sleep(200 * time.Millisecond)
	}
	if n := cc.n.Load(); n != 4 {
		t.Fatalf("evaluations after spaced triggers: %d", n)
	}
}

func TestTickerEvaluates(t *testing.T) {
	cc := &countingChecker{}
	e := New(Options{Checker: cc, Debounce: time.Hour, Tick: 30 * time.Millisecond})
	e.Activate(scenarioFromJSON(t, lessonJSON), 0)
	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()
	if n := cc.n.Load(); n < 3 {
		t.Fatalf("ticker evaluations: %d", n)
	}
}

func TestFirstSentence(t *testing.T) {
	for in, want := range map[string]string{
		"Still in the prompt? RET accepts.": "Still in the prompt?",
		"No period":                         "No period",
		"Use v1.2 now. Then go.":            "Use v1.2 now.",
	} {
		if got := firstSentence(in); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}
