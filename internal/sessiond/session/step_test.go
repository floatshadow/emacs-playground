package session

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// Step transitions over the real evaluator: activation, completion,
// advance, demo latch, skip, rehearsal, drops, laps and stuck rules.

// fakeChecker answers elisp forms from a table; unknown forms are unknown.
type fakeChecker struct {
	mu      sync.Mutex
	answers map[string]protocol.Truth
	calls   [][]string
}

func (f *fakeChecker) Check(ctx context.Context, forms []string, timeout time.Duration) ([]protocol.Truth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), forms...))
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

const stepLessonJSON = `{
  "id": "02-buffers-and-files/03-opening-files",
  "title": "Opening files",
  "setup": {"root": "hello"},
  "steps": [
    {"id": "open", "goal": "Open src/main.rs", "demo": "C-x C-f",
     "conditions": [
       {"text": "the Find file prompt was used", "event": {"command": "find-file"}},
       {"text": "main.rs is current", "elisp": "(main-rs-p)"}
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

const projectJSON = `{
  "id": "projects/p1-hello-cargo", "title": "Hello, cargo",
  "steps": [
    {"id": "scaffold", "goal": "Scaffold", "demo": "a",
     "conditions": [{"event": {"command": "eshell"}}],
     "stuck": [{"when": {"step_seconds": 1}, "say": "Taking a while."}]},
    {"id": "commit", "goal": "Commit", "demo": "b",
     "conditions": [{"event": {"command": "magit-commit-create"}}]}
  ]
}`

type stepFixture struct {
	t     *testing.T
	core  *Core
	ev    *steps.Evaluator
	elisp *fakeChecker
	clock *clock
	snaps *fakeSnaps
	sup   *fakeSup
	ws    string

	mu      sync.Mutex
	events  []protocol.Event
	reports []protocol.ProgressReport
	saved   []Saved
}

// newStepFixture runs a core and an evaluator (evaluated only on Check)
// over fake snapshots, a fake supervisor and a test clock; with prev it
// starts from prev's saved state.
func newStepFixture(t *testing.T, prev ...Saved) *stepFixture {
	t.Helper()
	ws := t.TempDir()
	root, err := os.OpenRoot(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	f := &stepFixture{t: t, ws: ws, elisp: &fakeChecker{}, clock: &clock{t: time.Unix(1761472000, 0)}, snaps: &fakeSnaps{restorable: map[string]bool{}}}
	f.ev = steps.New(steps.Options{Root: root, Workspace: ws, Checker: f.elisp, Now: f.clock.now, Tick: time.Hour,
		Observe: func(o steps.Observation) { f.core.Observe(o) }})
	f.sup = &fakeSup{eval: f.ev}
	f.core = New(Deps{
		Workspace: ws, Evaluator: f.ev, Snapshots: f.snaps, Sup: f.sup, Now: f.clock.now,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Emit:    func(ev protocol.Event) { f.mu.Lock(); f.events = append(f.events, ev); f.mu.Unlock() },
		Report:  func(r protocol.ProgressReport) { f.mu.Lock(); f.reports = append(f.reports, r); f.mu.Unlock() },
		Persist: func(s Saved) { f.mu.Lock(); f.saved = append(f.saved, s); f.mu.Unlock() },
	})
	for _, s := range prev {
		f.core.RestoreSaved(s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go f.core.Run(ctx)
	t.Cleanup(cancel)
	return f
}

func (f *stepFixture) write(rel, content string) {
	f.t.Helper()
	p := filepath.Join(f.ws, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// activate is an ordinary load's activation of step idx.
func (f *stepFixture) activate(sc *protocol.Scenario, idx int, lap ...lapMode) {
	mode := lapEnd
	if len(lap) > 0 {
		mode = lap[0]
	}
	f.core.activate(sc, idx, activation{status: protocol.StepLoaded, phase: Active, lap: mode})
}

// check evaluates now and waits for the ops it queued (an advance).
func (f *stepFixture) check() protocol.CheckResult {
	f.t.Helper()
	res, _ := f.ev.Check(context.Background())
	f.idle()
	return res
}

func (f *stepFixture) idle() {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.core.mu.Lock()
		busy := f.core.running != nil || len(f.core.queue) > 0
		f.core.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatal("an op is still running")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *stepFixture) types() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, ev := range f.events {
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

func (f *stepFixture) statuses() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.reports {
		out = append(out, r.StepID+":"+string(r.Status))
	}
	return strings.Join(out, " ")
}

func (f *stepFixture) nudges() []*protocol.NudgeEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*protocol.NudgeEvent
	for _, ev := range f.events {
		if n, ok := ev.(*protocol.NudgeEvent); ok {
			out = append(out, n)
		}
	}
	return out
}

func (f *stepFixture) lastStep() *protocol.StepEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.events) - 1; i >= 0; i-- {
		if e, ok := f.events[i].(*protocol.StepEvent); ok {
			return e
		}
	}
	return nil
}

func (f *stepFixture) clear() {
	f.mu.Lock()
	f.events, f.reports, f.saved = nil, nil, nil
	f.mu.Unlock()
	f.snaps.mu.Lock()
	f.snaps.cps = nil
	f.snaps.mu.Unlock()
}

// completeOpen makes the "open" step of stepLessonJSON hold.
func (f *stepFixture) completeOpen() {
	f.ev.OnKey(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file"})
	f.elisp.set("(main-rs-p)", protocol.True)
}

func (f *stepFixture) phase() Phase {
	f.core.mu.Lock()
	defer f.core.mu.Unlock()
	return f.core.lesson.Phase
}

func TestStepLifecycle(t *testing.T) {
	f := newStepFixture(t)
	f.write("hello/src/main.rs", "fn main() { println!(\"Hello, world\"); }\n")
	sc := scenarioFromJSON(t, stepLessonJSON)
	if _, ok := f.core.Check(context.Background()); ok {
		t.Fatal("check without a scenario must report no active step")
	}
	f.activate(sc, f.core.firstIncomplete(sc))
	if f.types() != "step:open:loaded" || f.statuses() != "open:in-progress" {
		t.Fatalf("events %s reports %s", f.types(), f.statuses())
	}
	f.clear()
	if res := f.check(); res.Complete || res.Conditions[0].OK {
		t.Fatalf("initial %+v", res)
	}
	f.completeOpen()
	f.clear()
	if res := f.check(); !res.Complete {
		t.Fatalf("should complete: %+v", res)
	}
	// check, complete, then the next step loads (its checkpoint is off).
	if got := f.types(); got != "check:open step:open:complete step:save:loaded" {
		t.Fatalf("events %s", got)
	}
	if f.snaps.captured() != "" {
		t.Fatalf("save has checkpoint:false, got %v", f.snaps.captured())
	}
	if got := f.statuses(); got != "open:done save:in-progress" {
		t.Fatalf("reports %s", got)
	}
	if a := f.core.Active(); a == nil || a.StepID != "save" {
		t.Fatalf("active %+v", a)
	}
	// Commands from the previous step do not count.
	if res := f.check(); res.Complete {
		t.Fatalf("save initial %+v", res)
	}
	f.ev.OnKey(&protocol.KeyEvent{Keys: "C-x C-s", Cmd: "save-buffer"})
	if res := f.check(); res.Complete {
		t.Fatal("file still has Hello, world")
	}
	f.write("hello/src/main.rs", "fn main() {}\n")
	f.clear()
	if res := f.check(); !res.Complete {
		t.Fatalf("save should complete: %+v", res)
	}
	// The layout step takes its checkpoint before loading.
	if got := f.types(); got != "check:save step:save:complete step:layout:loaded" || f.snaps.captured() != sc.ID+"/layout" {
		t.Fatalf("events %s checkpoints %s", got, f.snaps.captured())
	}
	st := &protocol.State{Buf: "main.rs", Windows: []protocol.Window{{ID: 1, Buf: "main.rs", Selected: true}, {ID: 2, Buf: "*compilation*"}}}
	_ = json.Unmarshal([]byte(`["vsplit",1,2]`), &st.Layout)
	f.ev.OnState(st)
	if res := f.check(); !res.Complete {
		t.Fatalf("layout should complete: %+v", res)
	}
	if last := f.lastStep(); !last.LessonComplete || last.Status != protocol.StepComplete || last.Index != 2 || last.Count != 3 {
		t.Fatalf("last step event %+v", last)
	}
	if f.core.Active() != nil || f.phase() != Idle {
		t.Fatal("no step is active after the lesson completes")
	}
	if _, ok := f.core.Check(context.Background()); ok {
		t.Fatal("check after the lesson completed")
	}
	saved := f.core.Saved()
	if saved.Active != nil || len(saved.Lessons[sc.ID].Done) != 3 {
		t.Fatalf("saved %+v", saved.Lessons[sc.ID])
	}
	// A completed lesson restarts at the first step.
	if f.core.firstIncomplete(sc) != 0 {
		t.Fatal("firstIncomplete after completion")
	}
}

func TestHintsResetsDropAndPersistence(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	f.activate(sc, 0)
	f.clear()
	for _, level := range []int{1, 2, 1} {
		if err := f.core.Hint("open", level); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.core.Hint("nope", 1); httpx.AsError(err).Status != 409 {
		t.Fatalf("hint for an unknown step: %v", err)
	}
	if len(f.reports) != 2 || *f.reports[1].HintsUsed != 2 {
		t.Fatalf("hint reports %+v", f.reports)
	}
	f.ev.OnKey(&protocol.KeyEvent{Cmd: "find-file"})
	f.clear()
	if !f.core.resetTo(sc.ID, "open", true) {
		t.Fatal("reset")
	}
	if f.types() != "step:open:reset" {
		t.Fatalf("reset events %s", f.types())
	}
	r := f.reports[0]
	if *r.Resets != 1 || *r.HintsUsed != 2 || r.Status != protocol.ProgressInProgress {
		t.Fatalf("reset report %+v", r)
	}
	if res := f.check(); res.Conditions[0].OK {
		t.Fatal("counts must clear on reset")
	}
	if f.core.resetTo("other/lesson", "open", true) {
		t.Fatal("reset of another lesson")
	}

	// Persisted state round-trips and drives firstIncomplete.
	saved := f.core.Saved()
	saved.Lessons[sc.ID].Done["open"] = 1
	g := newStepFixture(t, saved)
	if g.core.firstIncomplete(sc) != 1 || g.core.Active() != nil {
		t.Fatalf("restored: first incomplete %d active %v", g.core.firstIncomplete(sc), g.core.Active())
	}

	if !f.core.dropGeneration(sc.ID, 0) || f.core.Active() != nil || f.core.Scenario() != nil {
		t.Fatal("drop")
	}
	if _, ok := f.core.Saved().Lessons[sc.ID]; ok {
		t.Fatal("drop must clear the lesson's progress")
	}
	if f.core.dropGeneration(sc.ID, 0) {
		t.Fatal("second drop")
	}
}

// Check events go out when the results change, or when Check forces them.
func TestCheckEventsOnlyOnChange(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	f.activate(sc, 0)
	f.clear()
	f.core.mu.Lock()
	a := f.core.lesson.Attempt
	f.core.mu.Unlock()
	st := steps.Stats{StartedAt: a.StartedAt}
	results := []protocol.CondResult{{ID: "open/1"}, {ID: "open/2", Unknown: true}}
	f.core.Observe(steps.Observation{Attempt: a, Results: results, Stats: st})
	f.core.Observe(steps.Observation{Attempt: a, Results: results, Stats: st})
	if f.types() != "check:open" {
		t.Fatalf("unchanged results must not repeat: %s", f.types())
	}
	changed := []protocol.CondResult{{ID: "open/1", OK: true}, {ID: "open/2", Unknown: true}}
	f.core.Observe(steps.Observation{Attempt: a, Results: changed, Stats: st})
	f.core.Observe(steps.Observation{Attempt: a, Results: changed, Forced: true, Stats: st})
	if f.types() != "check:open check:open check:open" {
		t.Fatalf("changed and forced: %s", f.types())
	}
}

// Go to this step: an observation of the attempt it replaced, arriving
// late, is ignored; the new attempt's completes it.
func TestStaleObservationAfterGoTo(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	f.activate(sc, 1)
	f.core.mu.Lock()
	old := f.core.lesson.Attempt
	f.core.mu.Unlock()
	f.activate(sc, 0)
	f.clear()
	done := []protocol.CondResult{{ID: "save/1", OK: true}}
	f.core.Observe(steps.Observation{Attempt: old, Results: done, Complete: true})
	f.idle()
	if f.types() != "" || f.statuses() != "" || f.core.Active().StepID != "open" {
		t.Fatalf("a stale observation counted: events %s reports %s", f.types(), f.statuses())
	}
	f.completeOpen()
	f.check()
	if got := f.types(); got != "check:open step:open:complete step:save:loaded" {
		t.Fatalf("events %s", got)
	}
}

// A completion observed again while the advance is under way records
// nothing more.
func TestCompletionDuringAdvancing(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	f.activate(sc, 1)
	release := make(chan struct{})
	capturing := make(chan struct{})
	f.snaps.mu.Lock()
	f.snaps.onCapture = func() { close(capturing); <-release }
	f.snaps.mu.Unlock()
	f.ev.OnKey(&protocol.KeyEvent{Cmd: "save-buffer"})
	f.write("hello/src/main.rs", "fn main() {}\n")
	f.clear()
	f.ev.Check(context.Background())
	<-capturing
	if f.phase() != Advancing || f.core.Active() != nil {
		t.Fatalf("phase %v active %v", f.phase(), f.core.Active())
	}
	f.ev.Check(context.Background())
	f.ev.Check(context.Background())
	close(release)
	f.idle()
	if got := f.types(); got != "check:save step:save:complete step:layout:loaded" {
		t.Fatalf("events %s", got)
	}
	if got := f.statuses(); got != "save:done layout:in-progress" {
		t.Fatalf("reports %s", got)
	}
}

func TestDemoLatch(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	ctx := context.Background()
	f.activate(sc, 0)
	if _, err := f.core.DemoStart(ctx, "save"); httpx.AsError(err).Status != 409 {
		t.Fatalf("demo of a step that is not active: %v", err)
	}
	if res, err := f.core.DemoStart(ctx, "open"); err != nil || res.Restored || !f.core.Watching() {
		t.Fatalf("demo start %+v %v", res, err)
	}
	f.clear()
	f.completeOpen()
	if res := f.check(); !res.Complete {
		t.Fatalf("complete %+v", res)
	}
	// Demonstrated, once; no advance and no next checkpoint.
	if got := f.types(); got != "check:open step:open:demonstrated" {
		t.Fatalf("events %s", got)
	}
	if got := f.statuses(); got != "open:demonstrated" {
		t.Fatalf("reports %s", got)
	}
	f.check()
	if got := f.types(); got != "check:open step:open:demonstrated check:open" {
		t.Fatalf("demonstrated twice: %s", got)
	}
	if a := f.core.Active(); a == nil || a.StepID != "open" || f.snaps.captured() != "" {
		t.Fatalf("the step must stay active: %+v %v", a, f.snaps.captured())
	}
	// A demonstrated step is resumed, not passed over.
	if f.core.firstIncomplete(sc) != 0 {
		t.Fatal("firstIncomplete passes over a demonstrated step")
	}
	// End keeps the latch.
	if err := f.core.DemoEnd("open"); err != nil || !f.core.Watching() {
		t.Fatal("demo end must keep the latch")
	}
	// Demo start after commands restores without counting a reset and
	// starts a fresh latch; Do it yourself counts one and clears it.
	f.snaps.mu.Lock()
	f.snaps.restorable["open"] = true
	f.snaps.mu.Unlock()
	f.clear()
	if res, err := f.core.DemoStart(ctx, "open"); err != nil || !res.Restored || !f.core.Watching() {
		t.Fatalf("dirty demo start %+v %v", res, err)
	}
	if got := f.types(); got != "step:open:reset" || f.statuses() != "" {
		t.Fatalf("restart events %s reports %s", got, f.statuses())
	}
	if !f.core.resetTo(sc.ID, "open", true) || f.core.Watching() {
		t.Fatal("reset must clear the latch")
	}
	// The learner's own completion is done and advances.
	f.elisp.set("(main-rs-p)", protocol.False)
	f.clear()
	f.completeOpen()
	f.check()
	if got := f.types(); got != "check:open step:open:complete step:save:loaded" {
		t.Fatalf("events %s", got)
	}
	if !strings.Contains(f.statuses(), "open:done") {
		t.Fatalf("reports %s", f.statuses())
	}
}

// A completion Observe records before demo start or skip enters its phase
// wins: enter checks and writes in one hold of mu, refuses the Advancing
// step and leaves it to the queued advance.
func TestEnterAfterCompletion(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	for _, p := range []Phase{Watching, Skipping} {
		f.activate(sc, 0)
		if _, err := f.core.enter("open", p); err != nil || f.phase() != p {
			t.Fatalf("enter %v: %v, phase %v", p, err, f.phase())
		}
		f.activate(sc, 0)
		f.core.mu.Lock()
		f.core.lesson.Phase = Advancing
		f.core.mu.Unlock()
		if _, err := f.core.enter("open", p); httpx.AsError(err).Status != 409 || f.phase() != Advancing {
			t.Fatalf("enter %v after a completion: %v, phase %v", p, err, f.phase())
		}
	}
}

func TestDemoLongPause(t *testing.T) {
	for _, pause := range []time.Duration{12 * time.Second, 60 * time.Second} {
		t.Run(pause.String(), func(t *testing.T) {
			f := newStepFixture(t)
			f.activate(scenarioFromJSON(t, stepLessonJSON), 0)
			if _, err := f.core.DemoStart(context.Background(), "open"); err != nil {
				t.Fatal(err)
			}
			f.clock.add(pause)
			f.check()
			f.clear()
			f.completeOpen()
			f.check()
			if got := f.statuses(); got != "open:demonstrated" {
				t.Fatalf("after pause: %s", got)
			}
		})
	}
}

func TestNoStuckRulesWhileDemoPlays(t *testing.T) {
	f := newStepFixture(t)
	f.activate(scenarioFromJSON(t, stepLessonJSON), 0)
	if _, err := f.core.DemoStart(context.Background(), "open"); err != nil {
		t.Fatal(err)
	}
	f.ev.OnMinibuffer(&protocol.MinibufferEvent{Open: true})
	for range 8 {
		f.clock.add(5 * time.Second)
		f.core.DemoTraffic()
	}
	f.clear()
	f.check()
	if strings.Contains(f.types(), "nudge") {
		t.Fatalf("nudge while the demo plays: %s", f.types())
	}
	_ = f.core.DemoEnd("open")
	f.check()
	if !strings.Contains(f.types(), "nudge:open/stuck/1") {
		t.Fatalf("nudge after the demo: %s", f.types())
	}
}

// TestIdleDemoEnds: a demonstration whose page went away without rpc demo
// end stops muting stuck rules after DemoIdle without keys, or at once
// when its terminal connection closed (AbandonDemo), and since it never
// completed the step, its latch drops and the learner's own completion
// counts as done.
func TestIdleDemoEnds(t *testing.T) {
	sc := scenarioFromJSON(t, stepLessonJSON)
	ctx := context.Background()
	f := newStepFixture(t)
	f.activate(sc, 0)
	_, _ = f.core.DemoStart(ctx, "open")
	f.ev.OnMinibuffer(&protocol.MinibufferEvent{Open: true})
	f.clock.add(DemoIdle - time.Second)
	f.core.DemoTraffic()
	f.clear()
	f.check()
	if strings.Contains(f.types(), "nudge") || !f.core.Watching() {
		t.Fatalf("the demo still plays: %s watching %v", f.types(), f.core.Watching())
	}
	// The learner's commands do not keep the demonstration alive.
	f.clock.add(DemoIdle - time.Second)
	f.ev.OnKey(&protocol.KeyEvent{Keys: "a", Cmd: "self-insert-command"})
	f.clock.add(15 * time.Second)
	f.check()
	if !strings.Contains(f.types(), "nudge:open/stuck/1") || f.core.Watching() {
		t.Fatalf("an idle demo must end: %s watching %v", f.types(), f.core.Watching())
	}
	f.clear()
	f.completeOpen()
	f.check()
	if got := f.types(); got != "check:open step:open:complete step:save:loaded" || !strings.Contains(f.statuses(), "open:done") {
		t.Fatalf("events %s reports %s", got, f.statuses())
	}

	// A demonstration whose page went away ends at once, and its latch
	// drops when it never demonstrated.
	a := newStepFixture(t)
	a.activate(sc, 0)
	_, _ = a.core.DemoStart(ctx, "open")
	a.core.AbandonDemo()
	if a.core.Watching() || a.phase() != Active {
		t.Fatal("an abandoned demonstration kept its latch")
	}
	a.completeOpen()
	a.check()
	if !strings.Contains(a.statuses(), "open:done") {
		t.Fatalf("reports after an abandoned demo %s", a.statuses())
	}

	// A demonstration that completed the step keeps its latch when it ends
	// for idleness ("Do it yourself" clears it).
	g := newStepFixture(t)
	g.activate(sc, 0)
	_, _ = g.core.DemoStart(ctx, "open")
	g.completeOpen()
	g.check()
	g.clock.add(DemoIdle + time.Second)
	g.check()
	g.core.AbandonDemo()
	if !g.core.Watching() {
		t.Fatal("a demonstrated step lost its latch")
	}
}

func TestSkip(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	ctx := context.Background()
	f.activate(sc, 1)
	if _, err := f.core.Skip(ctx, "open"); httpx.AsError(err).Status != 409 {
		t.Fatalf("skip of a step that is not active: %v", err)
	}
	// save has no demo; layout takes a checkpoint when save is skipped.
	f.clear()
	res, err := f.core.Skip(ctx, "save")
	if err != nil || res.Next == nil || *res.Next != "layout" || res.DemoCompleted {
		t.Fatalf("skip %+v %v", res, err)
	}
	if got := f.types(); got != "step:save:skipped step:layout:loaded" || f.snaps.captured() != sc.ID+"/layout" {
		t.Fatalf("events %s checkpoints %s", got, f.snaps.captured())
	}
	if got := f.statuses(); got != "save:skipped layout:in-progress" {
		t.Fatalf("reports %s", got)
	}
	if f.core.firstIncomplete(sc) != 0 {
		t.Fatal("open was not finished")
	}
	f.clear()
	res, err = f.core.Skip(ctx, "layout")
	if err != nil || res.Next != nil || f.core.Active() != nil {
		t.Fatalf("skipping the last step %+v %v", res, err)
	}
	if last := f.lastStep(); last.Status != protocol.StepSkipped || !last.LessonComplete {
		t.Fatalf("last %+v", last)
	}
	saved := f.core.Saved()
	if saved.Active != nil || saved.Lessons[sc.ID].Skipped["layout"] == 0 {
		t.Fatalf("saved %+v", saved)
	}
}

// A skip whose demo completes the step records exactly one skipped (no
// done) and reports it once.
func TestSkipDemoCompletes(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	f.elisp.set("(main-rs-p)", protocol.True)
	f.activate(sc, 0)
	f.clear()
	// Evaluations of the skipped step while its demo plays.
	f.sup.mu.Lock()
	f.sup.onKeys = func(context.Context) error { f.ev.Check(context.Background()); return nil }
	f.sup.mu.Unlock()
	res, err := f.core.Skip(context.Background(), "open")
	if err != nil || !res.DemoCompleted || res.Next == nil || *res.Next != "save" {
		t.Fatalf("skip %+v %v", res, err)
	}
	f.idle()
	f.check()
	if got := f.statuses(); got != "open:skipped save:in-progress" {
		t.Fatalf("reports %s", got)
	}
	if strings.Contains(f.types(), "step:open:complete") || strings.Count(f.types(), "step:open:skipped") != 1 {
		t.Fatalf("events %s", f.types())
	}
	if ls := f.core.Saved().Lessons[sc.ID]; ls.Done["open"] != 0 || ls.Skipped["open"] == 0 {
		t.Fatalf("saved %+v", ls)
	}
}

func TestRehearseIsQuiet(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	ctx := context.Background()
	f.core.activate(sc, 0, activation{phase: Rehearsing})
	if f.core.Active() != nil {
		t.Fatal("a rehearsed step is not active")
	}
	f.completeOpen()
	res, ok := f.ev.Check(ctx)
	f.idle()
	if !ok || !res.Complete || res.StepID != "open" {
		t.Fatalf("rehearsal check %+v %v", res, ok)
	}
	f.mu.Lock()
	quiet := len(f.events) == 0 && len(f.reports) == 0 && len(f.saved) == 0
	f.mu.Unlock()
	if !quiet {
		t.Fatalf("rehearsal emitted %s %s", f.types(), f.statuses())
	}
	if _, err := f.core.DemoStart(ctx, "open"); err == nil {
		t.Fatal("rehearsed steps take no demo")
	}
	if _, err := f.core.Skip(ctx, "open"); err == nil {
		t.Fatal("rehearsed steps take no skip")
	}
	if f.core.resetTo(sc.ID, "open", true) {
		t.Fatal("rehearsed steps take no reset")
	}
	// Counts restart per rehearsed step.
	f.core.activate(sc, 1, activation{phase: Rehearsing})
	f.ev.OnKey(&protocol.KeyEvent{Cmd: "save-buffer"})
	f.activate(sc, 2)
	if got := f.types(); got != "step:layout:loaded" {
		t.Fatalf("activate after rehearsal %s", got)
	}
	f.core.mu.Lock()
	dirty := f.core.dirtyLocked()
	f.core.mu.Unlock()
	if dirty {
		t.Fatal("commands of the rehearsal count for the activated step")
	}
}

func TestDropAll(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	f.activate(sc, 1)
	_ = f.core.Hint("save", 1)
	other := f.core.Saved()
	other.Lessons["00-orientation/01-reading-the-screen"] = &LessonState{Done: map[string]int64{"q1": 1}}
	f.core.RestoreSaved(other)
	f.clear()
	if !f.core.dropGeneration("", 0) {
		t.Fatal("drop all forgets the active step")
	}
	if ev := f.lastStep(); ev == nil || ev.Status != protocol.StepDropped || ev.StepID != "save" || ev.Index != 1 || ev.Count != 3 {
		t.Fatalf("dropped event %+v", ev)
	}
	if s := f.core.Saved(); len(s.Lessons) != 0 || s.Active != nil {
		t.Fatalf("saved after drop all %+v", s)
	}
	if _, ok := f.core.Check(context.Background()); ok {
		t.Fatal("check after the drop")
	}
	if f.core.dropGeneration("", 0) {
		t.Fatal("nothing left to drop")
	}
	// Dropping another lesson keeps the active step.
	f.activate(sc, 0)
	f.clear()
	if f.core.dropGeneration("00-orientation/01-reading-the-screen", 0) || f.core.Active() == nil || f.types() != "" {
		t.Fatal("drop of another lesson")
	}
}

func TestStuckRules(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, stepLessonJSON)
	f.activate(sc, 0)
	f.clear()

	// minibuffer_seconds: open for 30 s.
	f.ev.OnMinibuffer(&protocol.MinibufferEvent{Open: true, Kind: protocol.MinibufferPrompt})
	f.clock.add(29 * time.Second)
	f.check()
	if len(f.nudges()) != 0 {
		t.Fatal("too early")
	}
	f.clock.add(2 * time.Second)
	f.check()
	n := f.nudges()
	if len(n) != 1 || n[0].ID != "open/stuck/1" || n[0].Kind != protocol.NudgeCalm || n[0].Title != "Still in the prompt?" {
		t.Fatalf("minibuffer nudge %+v", n)
	}
	// Edge-triggered: no repeat while it holds; re-arms after closing.
	f.check()
	if len(f.nudges()) != 1 {
		t.Fatal("repeated nudge")
	}
	f.ev.OnState(&protocol.State{Minibuffer: nil})
	f.check()
	f.ev.OnState(&protocol.State{Minibuffer: &protocol.Minibuffer{Kind: protocol.MinibufferPrompt}})
	f.clock.add(31 * time.Second)
	f.check()
	if len(f.nudges()) != 2 {
		t.Fatalf("re-armed nudge: %d", len(f.nudges()))
	}
	// step_seconds with explicit title and kind.
	f.clock.add(300 * time.Second)
	f.check()
	n = f.nudges()
	if len(n) != 3 || n[2].ID != "open/stuck/2" || n[2].Kind != protocol.NudgeAsk || n[2].Title != "Taking a while" {
		t.Fatalf("step nudge %+v", n[len(n)-1])
	}

	// Layout step: idle, quits, unbound, conjunction.
	g := newStepFixture(t)
	g.activate(sc, 2)
	g.clear()
	g.clock.add(59 * time.Second)
	g.check()
	if len(g.nudges()) != 0 {
		t.Fatal("idle too early")
	}
	g.clock.add(2 * time.Second)
	g.check()
	if n := g.nudges(); len(n) != 1 || n[0].ID != "layout/stuck/1" {
		t.Fatalf("idle nudge %+v", n)
	}
	// A key resets idleness (and re-arms).
	g.ev.OnKey(&protocol.KeyEvent{Cmd: "keyboard-quit"})
	g.clock.add(time.Second)
	g.ev.OnKey(&protocol.KeyEvent{Cmd: "keyboard-quit"})
	g.check()
	if len(g.nudges()) != 1 {
		t.Fatal("two quits are not three")
	}
	g.clock.add(time.Second)
	g.ev.OnKey(&protocol.KeyEvent{Cmd: "abort-minibuffers"})
	g.check()
	if n := g.nudges(); len(n) != 2 || n[1].ID != "layout/stuck/2" {
		t.Fatalf("quits nudge %+v", n)
	}
	// Quits outside the window do not count.
	g.clock.add(20 * time.Second)
	g.ev.OnKey(&protocol.KeyEvent{Cmd: "keyboard-quit"})
	g.check()
	g.ev.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	g.ev.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	g.check()
	n = g.nudges()
	if len(n) != 3 || n[2].ID != "layout/stuck/3" {
		t.Fatalf("unbound nudge %+v", n)
	}
	// A different command breaks the run.
	g.ev.OnKey(&protocol.KeyEvent{Keys: "C-n", Cmd: "next-line"})
	g.ev.OnKey(&protocol.KeyEvent{Keys: "C-c C-z", Cmd: "undefined"})
	g.check()
	if len(g.nudges()) != 3 {
		t.Fatal("unbound run should restart")
	}
	// A prefix cancelled with C-g is a quit, not an unbound key.
	g.ev.OnKey(&protocol.KeyEvent{Keys: "C-x C-g", Cmd: "undefined"})
	g.ev.OnKey(&protocol.KeyEvent{Keys: "C-x C-g", Cmd: "undefined"})
	g.check()
	unbound := 0
	for _, x := range g.nudges() {
		if x.ID == "layout/stuck/3" {
			unbound++
		}
	}
	if unbound != 1 {
		t.Fatalf("a cancelled prefix counted as unbound: %s", g.types())
	}
	// Conjunction: idle 10 s and step 100 s (step is now ~85 s old).
	g.clock.add(11 * time.Second)
	g.check()
	for _, x := range g.nudges() {
		if x.ID == "layout/stuck/4" {
			t.Fatal("conjunction fired before step_seconds")
		}
	}
	g.clock.add(10 * time.Second)
	g.check()
	found := false
	for _, x := range g.nudges() {
		found = found || x.ID == "layout/stuck/4"
	}
	if !found {
		t.Fatalf("conjunction nudge missing: %s", g.types())
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

func TestProgressReportsAfterRestore(t *testing.T) {
	f := newStepFixture(t, Saved{Lessons: map[string]*LessonState{"a/b": {
		Done: map[string]int64{"done": 100}, Skipped: map[string]int64{"skip": 200},
		Demonstrated: map[string]int64{"demo": 300, "done": 50}, Hints: map[string]int{"done": 2}, Resets: map[string]int{"done": 1},
	}}})
	reports := f.core.ProgressReports()
	if len(reports) != 3 {
		t.Fatalf("reports %+v", reports)
	}
	for _, r := range reports {
		want := map[string]struct {
			status protocol.ProgressStatus
			at     int64
		}{
			"done": {protocol.ProgressDone, 100}, "skip": {protocol.ProgressSkipped, 200}, "demo": {protocol.ProgressDemonstrated, 300},
		}[r.StepID]
		if r.Status != want.status || r.At != want.at || r.LapMs != 0 {
			t.Fatalf("report %+v", r)
		}
		if r.StepID == "done" && (r.HintsUsed == nil || *r.HintsUsed != 2 || r.Resets == nil || *r.Resets != 1) {
			t.Fatalf("totals %+v", r)
		}
	}
	f.core.dropGeneration("a/b", 0)
	if got := f.core.ProgressReports(); len(got) != 0 {
		t.Fatalf("reports after drop %+v", got)
	}
}

func TestProgressGenerations(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, projectJSON)
	f.activate(sc, 0)
	f.core.dropGeneration("", 2)
	f.activate(sc, 0)
	if g := f.core.Saved().Generations.ForLesson(sc.ID); g != 2 {
		t.Fatalf("generation %d", g)
	}
	f.core.dropGeneration(sc.ID, 4)
	f.activate(sc, 0)
	f.core.dropGeneration("", 3)
	f.core.dropGeneration(sc.ID, 4)
	if f.core.Active() == nil {
		t.Fatal("older or duplicate reset dropped newer lesson")
	}
	_ = f.core.Hint("scaffold", 1)
	f.ev.OnKey(&protocol.KeyEvent{Cmd: "eshell"})
	f.check()
	if reports := f.core.ProgressReports(); len(reports) != 1 || reports[0].Generation != 4 {
		t.Fatalf("reconciled reports %+v", reports)
	}
	if reports := f.reports; reports[len(reports)-1].Generation != 4 {
		t.Fatalf("completion reports %+v", reports)
	}
	if saved := f.core.Saved(); saved.Generations.ForLesson(sc.ID) != 4 {
		t.Fatalf("saved %+v", saved)
	}
	g := newStepFixture(t, f.core.Saved())
	g.core.dropGeneration("", 5)
	if saved := g.core.Saved(); saved.Active != nil || len(saved.Lessons) != 0 || saved.Generations.ForLesson(sc.ID) != 5 {
		t.Fatalf("reset before resume %+v", saved)
	}
}

func TestResetGenerationAfterGatewayLoss(t *testing.T) {
	for _, scope := range []string{"", "projects/p1-hello-cargo"} {
		t.Run(scope, func(t *testing.T) {
			sc := scenarioFromJSON(t, projectJSON)
			f := newStepFixture(t, Saved{Generations: protocol.ProgressGenerations{sc.ID: 18}})
			f.activate(sc, 0)
			f.ev.OnKey(&protocol.KeyEvent{Cmd: "eshell"})
			f.check()
			if saved := f.core.Saved(); saved.Active == nil || len(saved.Lessons[sc.ID].Done) != 1 {
				t.Fatalf("state before reset %+v", saved)
			}
			const generation int64 = 1_800_000_000_000
			if !f.core.dropGeneration(scope, generation) {
				t.Fatal("reset did not drop the active step")
			}
			saved := f.core.Saved()
			if saved.Active != nil || len(saved.Lessons) != 0 || saved.Generations.ForLesson(sc.ID) != generation {
				t.Fatalf("state after reset %+v", saved)
			}
			g := newStepFixture(t, saved)
			if reports := g.core.ProgressReports(); len(reports) != 0 {
				t.Fatalf("reset outcomes replayed after restart: %+v", reports)
			}
			g.activate(sc, 0)
			g.ev.OnKey(&protocol.KeyEvent{Cmd: "eshell"})
			g.check()
			if reports := g.core.ProgressReports(); len(reports) != 1 || reports[0].Generation != generation || reports[0].Status != protocol.ProgressDone {
				t.Fatalf("completion after reset %+v", reports)
			}
		})
	}
}

// A lap: hint, demo start and skip answer 409, stuck rules are silent, a
// Reset keeps the lap, and the last completion reports lapMs once.
func TestLap(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, projectJSON)
	ctx := context.Background()
	f.activate(sc, 0, lapStart)
	lap := f.core.Lap()
	if lap == nil || lap.LessonID != sc.ID || lap.StartedAt != f.clock.now().UnixMilli() {
		t.Fatalf("lap %+v", lap)
	}
	if ev := f.lastStep(); !ev.Lap {
		t.Fatalf("loaded event without lap: %+v", ev)
	}
	errHint := f.core.Hint("scaffold", 1)
	_, errDemo := f.core.DemoStart(ctx, "scaffold")
	_, errSkip := f.core.Skip(ctx, "scaffold")
	for _, err := range []error{errHint, errDemo, errSkip} {
		if httpx.AsError(err).Status != 409 || !strings.Contains(err.Error(), "lap") {
			t.Fatalf("hint, demo and skip must be refused during a lap: %v %v %v", errHint, errDemo, errSkip)
		}
	}
	// Stuck rules are silent.
	f.clock.add(5 * time.Second)
	f.clear()
	f.check()
	if strings.Contains(f.types(), "nudge") {
		t.Fatalf("nudge during a lap: %s", f.types())
	}
	// Reset this step keeps the lap.
	if !f.core.resetTo(sc.ID, "scaffold", true) || f.core.Lap() == nil {
		t.Fatal("reset ended the lap")
	}
	f.ev.OnKey(&protocol.KeyEvent{Keys: "M-x", Cmd: "eshell"})
	f.check()
	f.clock.add(90 * time.Second)
	f.ev.OnKey(&protocol.KeyEvent{Keys: "c c", Cmd: "magit-commit-create"})
	f.clear()
	f.check()
	f.check()
	last := f.lastStep()
	if last == nil || !last.LessonComplete || !last.Lap || last.LapMs != 95000 {
		t.Fatalf("lap completion %+v", last)
	}
	var laps []protocol.ProgressReport
	for _, r := range f.reports {
		if r.LapMs != 0 {
			laps = append(laps, r)
		}
	}
	if len(laps) != 1 || laps[0].StepID != "commit" || laps[0].Status != protocol.ProgressDone || laps[0].LapMs != 95000 {
		t.Fatalf("lap reports %+v", f.reports)
	}
	if f.core.Lap() != nil || f.core.Saved().Lap != nil {
		t.Fatal("the lap should be over")
	}
}

func TestLapEndsAndResumes(t *testing.T) {
	f := newStepFixture(t)
	sc := scenarioFromJSON(t, projectJSON)
	f.activate(sc, 0, lapStart)
	saved := f.core.Saved()
	if saved.Lap == nil {
		t.Fatal("lap not persisted")
	}
	// A restarted sessiond resumes the lap.
	g := newStepFixture(t, saved)
	g.activate(sc, 1, lapKeep)
	if l := g.core.Lap(); l == nil || l.StartedAt != saved.Lap.StartedAt {
		t.Fatalf("resumed lap %+v", l)
	}
	// An ordinary load ends it.
	g.activate(sc, 1)
	if g.core.Lap() != nil {
		t.Fatal("an ordinary activation kept the lap")
	}
	// So does a progress reset.
	f.core.dropGeneration(sc.ID, 0)
	if f.core.Lap() != nil {
		t.Fatal("the drop kept the lap")
	}
	// lapKeep without a lap starts none.
	f.activate(sc, 0, lapKeep)
	if f.core.Lap() != nil {
		t.Fatal("lapKeep started a lap")
	}
}
