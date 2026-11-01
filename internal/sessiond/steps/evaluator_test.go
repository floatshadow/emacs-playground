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
	"syscall"
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

// sink records observations.
type sink struct {
	mu  sync.Mutex
	obs []Observation
}

func (s *sink) observe(o Observation) { s.mu.Lock(); s.obs = append(s.obs, o); s.mu.Unlock() }

func (s *sink) all() []Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Observation(nil), s.obs...)
}

func (s *sink) last() Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.obs) == 0 {
		return Observation{}
	}
	return s.obs[len(s.obs)-1]
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
	e     *Evaluator
	sink  *sink
	elisp *fakeChecker
	shell *fakeShell
	clock *clock
	ws    string
	seq   uint64
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
	f.e = New(Options{Root: root, Workspace: ws, Checker: f.elisp, Shell: f.shell, Observe: f.sink.observe, Now: f.clock.now})
	return f
}

// begin begins the next attempt of step idx of sc.
func (f *fixture) begin(sc *protocol.Scenario, idx int) Attempt {
	f.seq++
	a := Attempt{Lesson: sc.ID, Step: sc.Steps[idx].ID, Seq: f.seq, StartedAt: f.clock.now()}
	f.e.Begin(a, &sc.Steps[idx], sc.Setup.Root)
	return a
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

func TestFileConditionRejectsFIFO(t *testing.T) {
	f := newFixture(t)
	p := filepath.Join(f.ws, "pipe")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	ec := &evalContext{ctx: context.Background(), root: f.e.opt.Root}
	c := cond(t, `{"file":{"path":"pipe","contains":"done"}}`)
	done := make(chan result, 1)
	go func() { done <- ec.eval(&c) }()
	select {
	case r := <-done:
		if r.t != protocol.Unknown || !strings.Contains(r.detail, "not a regular file") {
			t.Fatalf("FIFO result %+v", r)
		}
	case <-time.After(time.Second):
		// Release a blocking reader before reporting the failed invariant.
		writer, err := os.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			writer.Close()
		}
		t.Fatal("FIFO blocks evaluation")
	}
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
  "setup": {"workspace": "fixtures/hello-rust", "root": "hello"},
  "steps": [
    {"id": "open", "goal": "Open src/main.rs",
     "conditions": [
       {"text": "the Find file prompt was used", "event": {"command": "find-file"}},
       {"text": "main.rs is current", "elisp": "(equal (buffer-file-name) \"/work/hello/src/main.rs\")"}
     ]},
    {"id": "again", "goal": "Open it again", "conditions": [{"event": {"command": "find-file"}}]},
    {"id": "lesson", "goal": "Opened in the lesson", "conditions": [{"event": {"command": "find-file", "since": "lesson"}}]}
  ]
}`

// An attempt's evaluations reach Observe with its identity; a new attempt
// starts the step counts afresh, the lesson counts only with another
// lesson.
func TestAttempts(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	if _, ok := f.e.Check(ctx); ok {
		t.Fatal("check before an attempt began")
	}
	a := f.begin(sc, 0)
	res, ok := f.e.Check(ctx)
	if !ok || res.Complete || res.LessonID != sc.ID || res.Conditions[0].ID != "open/1" || res.Conditions[0].OK || !res.Conditions[1].Unknown {
		t.Fatalf("initial %+v", res)
	}
	if f.elisp.nCalls() != 1 || len(f.elisp.calls[0]) != 1 {
		t.Fatalf("elisp calls %v", f.elisp.calls)
	}
	if o := f.sink.last(); o.Attempt != a || !o.Forced || o.Complete || len(o.Results) != 2 || o.Stats.StartedAt != a.StartedAt {
		t.Fatalf("observation %+v", o)
	}
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file"})
	f.elisp.set(`(equal (buffer-file-name) "/work/hello/src/main.rs")`, protocol.True)
	f.e.evaluate(ctx, false)
	if o := f.sink.last(); !o.Complete || o.Forced || o.Stats.Commands != 1 || f.e.Commands() != 1 {
		t.Fatalf("complete %+v", o)
	}
	// The next attempt counts afresh; the lesson count stays.
	f.begin(sc, 1)
	if res, _ := f.e.Check(ctx); res.Complete || f.e.Commands() != 0 {
		t.Fatalf("step counts carried over: %+v", res)
	}
	f.begin(sc, 2)
	if res, _ := f.e.Check(ctx); !res.Complete {
		t.Fatalf("lesson counts lost: %+v", res)
	}
	other := scenarioFromJSON(t, strings.Replace(lessonJSON, "03-opening-files", "04-other", 1))
	f.begin(other, 2)
	if res, _ := f.e.Check(ctx); res.Complete {
		t.Fatalf("lesson counts of another lesson: %+v", res)
	}
	// Nothing to evaluate.
	f.e.Begin(Attempt{Seq: 99}, nil, "")
	n := len(f.sink.all())
	if _, ok := f.e.Check(ctx); ok || len(f.sink.all()) != n {
		t.Fatal("an attempt without a step was evaluated")
	}
}

// blockingChecker holds the agent check until released.
type blockingChecker struct {
	entered chan struct{}
	release chan struct{}
}

func (b *blockingChecker) Check(ctx context.Context, forms []string, timeout time.Duration) ([]protocol.Truth, error) {
	b.entered <- struct{}{}
	<-b.release
	return make([]protocol.Truth, len(forms)), nil
}

// An evaluation that finishes after another attempt began is not observed.
func TestStaleEvaluationIsDropped(t *testing.T) {
	bc := &blockingChecker{entered: make(chan struct{}), release: make(chan struct{})}
	s := &sink{}
	e := New(Options{Checker: bc, Observe: s.observe})
	sc := scenarioFromJSON(t, lessonJSON)
	e.Begin(Attempt{Lesson: sc.ID, Step: "open", Seq: 1}, &sc.Steps[0], "")
	done := make(chan protocol.CheckResult, 1)
	go func() {
		res, _ := e.Check(context.Background())
		done <- res
	}()
	<-bc.entered
	e.Begin(Attempt{Lesson: sc.ID, Step: "again", Seq: 2}, &sc.Steps[1], "")
	close(bc.release)
	if res := <-done; res.StepID != "open" {
		t.Fatalf("result %+v", res)
	}
	if obs := s.all(); len(obs) != 0 {
		t.Fatalf("stale observation %+v", obs)
	}
}

func TestElispErrorsAreUnknown(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	f.begin(sc, 0)
	f.elisp.err = errors.New("agent: call timed out")
	res, _ := f.e.Check(context.Background())
	if !res.Conditions[1].Unknown || res.Conditions[1].OK || !strings.Contains(res.Conditions[1].Detail, "timed out") {
		t.Fatalf("got %+v", res.Conditions[1])
	}
}

// The key stream's statistics: quits within the attempt, the unbound run
// (a prefix cancelled with C-g is a quit), the open minibuffer and the
// last command, which outlives the attempt.
func TestStats(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	start := f.clock.now()
	f.begin(sc, 0)
	f.e.OnMinibuffer(&protocol.MinibufferEvent{Open: true, Kind: protocol.MinibufferPrompt})
	f.clock.add(time.Second)
	f.e.OnKey(&protocol.KeyEvent{Cmd: "keyboard-quit"})
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-x C-g", Cmd: "undefined"})
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	f.e.Check(ctx)
	st := f.sink.last().Stats
	if len(st.Quits) != 2 || st.UnboundRun != 2 || st.Commands != 4 || !st.MiniSince.Equal(start) || !st.LastKeyAt.Equal(start.Add(time.Second)) {
		t.Fatalf("stats %+v", st)
	}
	// A different command breaks the run; a closed minibuffer clears.
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-n", Cmd: "next-line"})
	f.e.OnState(&protocol.State{})
	f.e.Check(ctx)
	if st := f.sink.last().Stats; st.UnboundRun != 0 || !st.MiniSince.IsZero() {
		t.Fatalf("stats %+v", st)
	}
	f.clock.add(time.Second)
	f.begin(sc, 1)
	f.e.Check(ctx)
	if st := f.sink.last().Stats; len(st.Quits) != 0 || st.Commands != 0 || !st.LastKeyAt.Equal(start.Add(time.Second)) || !st.StartedAt.Equal(start.Add(2*time.Second)) {
		t.Fatalf("stats of the next attempt %+v", st)
	}
}

// countingChecker counts evaluations through the elisp batch.
type countingChecker struct{ n atomic.Int32 }

func (c *countingChecker) Check(ctx context.Context, forms []string, timeout time.Duration) ([]protocol.Truth, error) {
	c.n.Add(1)
	return make([]protocol.Truth, len(forms)), nil
}

func beginFirst(t *testing.T, e *Evaluator) {
	sc := scenarioFromJSON(t, lessonJSON)
	e.Begin(Attempt{Lesson: sc.ID, Step: "open", Seq: 1}, &sc.Steps[0], "")
	e.Trigger()
}

func TestDebounce(t *testing.T) {
	cc := &countingChecker{}
	e := New(Options{Checker: cc, Debounce: 100 * time.Millisecond, Tick: time.Hour})
	beginFirst(t, e)
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
	beginFirst(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()
	if n := cc.n.Load(); n < 3 {
		t.Fatalf("ticker evaluations: %d", n)
	}
}

func TestTrailingDebounceWithCap(t *testing.T) {
	cc := &countingChecker{}
	e := New(Options{Checker: cc, Tick: time.Hour})
	if e.opt.Debounce != 40*time.Millisecond || e.opt.DebounceMax != 120*time.Millisecond {
		t.Fatalf("defaults %v %v", e.opt.Debounce, e.opt.DebounceMax)
	}
	e = New(Options{Checker: cc, Debounce: 50 * time.Millisecond, DebounceMax: 150 * time.Millisecond, Tick: time.Hour})
	beginFirst(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)
	time.Sleep(120 * time.Millisecond)
	base := cc.n.Load()
	if base != 1 {
		t.Fatalf("activation evaluations %d", base)
	}
	// A key and its state a few ms later: one evaluation, about Debounce
	// after the last trigger.
	start := time.Now()
	e.OnKey(&protocol.KeyEvent{Cmd: "next-line"})
	time.Sleep(5 * time.Millisecond)
	e.OnState(&protocol.State{Buf: "x"})
	for cc.n.Load() == base && time.Since(start) < time.Second {
		time.Sleep(2 * time.Millisecond)
	}
	if took := time.Since(start); took < 50*time.Millisecond || took > 140*time.Millisecond {
		t.Fatalf("trailing evaluation after %v", took)
	}
	// A continuous stream cannot postpone evaluations beyond the cap.
	time.Sleep(100 * time.Millisecond)
	base = cc.n.Load()
	stop := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(stop) {
		e.OnKey(&protocol.KeyEvent{Cmd: "next-line"})
		time.Sleep(10 * time.Millisecond)
	}
	if n := cc.n.Load() - base; n < 2 {
		t.Fatalf("evaluations during a 400 ms stream: %d", n)
	}
}

// A save completes nothing on a shell result from before it: the step is
// confirmed with a fresh run (the file event that clears the cache comes
// later).
func TestStaleShellResultIsConfirmed(t *testing.T) {
	f := newFixture(t)
	f.shell.res = map[string]ShellResult{"test out -nt src": {OK: true}}
	sc := scenarioFromJSON(t, `{"id": "a/b", "title": "t", "steps": [{"id": "build", "goal": "g", "conditions": [
	  {"text": "saved", "event": {"command": "save-buffer"}},
	  {"text": "built", "shell": {"run": "test out -nt src"}}]}]}`)
	f.begin(sc, 0)
	ctx := context.Background()
	f.e.evaluate(ctx, false)
	if f.shell.runs["test out -nt src"] != 1 {
		t.Fatalf("runs: %v", f.shell.runs)
	}
	f.shell.mu.Lock()
	f.shell.res["test out -nt src"] = ShellResult{ExitCode: 1}
	f.shell.mu.Unlock()
	f.clock.add(time.Second)
	f.e.OnKey(&protocol.KeyEvent{Cmd: "save-buffer"})
	res, _ := f.e.evaluate(ctx, false)
	if res.Complete || f.shell.runs["test out -nt src"] != 2 {
		t.Fatalf("stale result completed the step: %+v runs %v", res, f.shell.runs)
	}
	// Fresh results are used as they are.
	f.e.evaluate(ctx, false)
	if f.shell.runs["test out -nt src"] != 2 {
		t.Fatalf("fresh result re-ran: %v", f.shell.runs)
	}
}
