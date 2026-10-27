package steps

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// completeOpen makes the "open" step of lessonJSON hold.
func (f *fixture) completeOpen() {
	f.e.OnKey(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file"})
	f.elisp.set(`(equal (buffer-file-name) "/work/hello/src/main.rs")`, protocol.True)
}

func (s *sink) statuses() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.reports {
		out = append(out, r.StepID+":"+string(r.Status))
	}
	return strings.Join(out, " ")
}

func TestDemoLatch(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	f.e.Activate(sc, 0)
	if f.e.Dirty() {
		t.Fatal("a fresh step is not dirty")
	}
	if f.e.StartDemo("save") {
		t.Fatal("demo of a step that is not active")
	}
	if !f.e.StartDemo("open") || !f.e.Latched() {
		t.Fatal("demo start")
	}
	f.sink.clear()
	f.completeOpen()
	if !f.e.Dirty() {
		t.Fatal("a command ran")
	}
	res, _ := f.e.Check(ctx)
	if !res.Complete {
		t.Fatalf("complete %+v", res)
	}
	// Demonstrated, once; no advance and no next checkpoint.
	if got := f.sink.types(); got != "check:open step:open:demonstrated" {
		t.Fatalf("events %s", got)
	}
	if got := f.sink.statuses(); got != "open:demonstrated" {
		t.Fatalf("reports %s", got)
	}
	f.e.Check(ctx)
	f.e.evaluate(ctx, false)
	if got := f.sink.types(); got != "check:open step:open:demonstrated check:open" {
		t.Fatalf("demonstrated twice: %s", got)
	}
	if a := f.e.Active(); a == nil || a.StepID != "open" || len(f.sink.cps) != 0 {
		t.Fatalf("the step must stay active: %+v %v", a, f.sink.cps)
	}
	// A demonstrated step is resumed, not passed over.
	if f.e.FirstIncomplete(sc) != 0 {
		t.Fatal("FirstIncomplete passes over a demonstrated step")
	}
	// End keeps the latch.
	if !f.e.EndDemo("open") || !f.e.Latched() {
		t.Fatal("demo end must keep the latch")
	}
	// Restart (demo start after commands) does not count a reset and clears
	// the latch; ResetTo ("Do it yourself") counts one.
	f.sink.clear()
	if !f.e.Restart(sc.ID, "open") || f.e.Latched() || f.e.Dirty() {
		t.Fatal("restart")
	}
	if got := f.sink.types(); got != "step:open:reset" || len(f.sink.reports) != 0 {
		t.Fatalf("restart events %s reports %s", got, f.sink.statuses())
	}
	f.e.StartDemo("open")
	if !f.e.ResetTo(sc.ID, "open") || f.e.Latched() {
		t.Fatal("reset must clear the latch")
	}
	// The learner's own completion is done and advances.
	f.elisp.set(`(equal (buffer-file-name) "/work/hello/src/main.rs")`, protocol.False)
	f.sink.clear()
	f.completeOpen()
	f.e.Check(ctx)
	if got := f.sink.types(); got != "check:open step:open:complete step:save:loaded" {
		t.Fatalf("events %s", got)
	}
	if !strings.Contains(f.sink.statuses(), "open:done") {
		t.Fatalf("reports %s", f.sink.statuses())
	}
}

func TestNoStuckRulesWhileDemoPlays(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	f.e.Activate(sc, 0)
	f.e.StartDemo("open")
	f.e.OnMinibuffer(&protocol.MinibufferEvent{Open: true})
	for range 8 {
		f.clock.add(5 * time.Second)
		f.e.DemoTraffic()
	}
	f.sink.clear()
	f.e.evaluate(context.Background(), false)
	if strings.Contains(f.sink.types(), "nudge") {
		t.Fatalf("nudge while the demo plays: %s", f.sink.types())
	}
	f.e.EndDemo("open")
	f.e.evaluate(context.Background(), false)
	if !strings.Contains(f.sink.types(), "nudge:open/stuck/1") {
		t.Fatalf("nudge after the demo: %s", f.sink.types())
	}
}

// TestIdleDemoEnds: a demonstration whose page went away without rpc demo
// end stops muting stuck rules after DemoIdle without keys, or at once
// when its terminal connection closed (AbandonDemo), and since it never
// completed the step, the learner's own completion counts as done.
func TestIdleDemoEnds(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	f.e.Activate(sc, 0)
	f.e.StartDemo("open")
	f.e.OnMinibuffer(&protocol.MinibufferEvent{Open: true})
	f.clock.add(DemoIdle - time.Second)
	f.e.DemoTraffic()
	f.sink.clear()
	f.e.evaluate(ctx, false)
	if strings.Contains(f.sink.types(), "nudge") || !f.e.Latched() {
		t.Fatalf("the demo still plays: %s latched %v", f.sink.types(), f.e.Latched())
	}
	// The learner's commands do not keep the demonstration alive.
	f.clock.add(DemoIdle - time.Second)
	f.e.OnKey(&protocol.KeyEvent{Keys: "a", Cmd: "self-insert-command"})
	f.clock.add(15 * time.Second)
	f.e.evaluate(ctx, false)
	if !strings.Contains(f.sink.types(), "nudge:open/stuck/1") || f.e.Latched() {
		t.Fatalf("an idle demo must end: %s latched %v", f.sink.types(), f.e.Latched())
	}
	f.sink.clear()
	f.completeOpen()
	f.e.Check(ctx)
	if got := f.sink.types(); got != "check:open step:open:complete step:save:loaded" {
		t.Fatalf("events %s", got)
	}
	if !strings.Contains(f.sink.statuses(), "open:done") {
		t.Fatalf("reports %s", f.sink.statuses())
	}

	// A demonstration whose page went away ends at once.
	a := newFixture(t)
	a.e.Activate(sc, 0)
	a.e.StartDemo("open")
	a.e.AbandonDemo()
	if a.e.Latched() {
		t.Fatal("an abandoned demonstration kept its latch")
	}
	a.completeOpen()
	a.e.Check(ctx)
	if !strings.Contains(a.sink.statuses(), "open:done") {
		t.Fatalf("reports after an abandoned demo %s", a.sink.statuses())
	}

	// A demonstration that completed the step keeps its latch when it ends
	// for idleness ("Do it yourself" clears it).
	g := newFixture(t)
	g.e.Activate(sc, 0)
	g.e.StartDemo("open")
	g.completeOpen()
	g.e.Check(ctx)
	g.clock.add(DemoIdle + time.Second)
	g.e.evaluate(ctx, false)
	g.e.AbandonDemo()
	if !g.e.Latched() {
		t.Fatal("a demonstrated step lost its latch")
	}
}

func TestSkip(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	f.e.Activate(sc, 0)
	if f.e.BeginSkip("save") {
		t.Fatal("skip of a step that is not active")
	}
	if !f.e.BeginSkip("open") || f.e.BeginSkip("open") {
		t.Fatal("begin skip, once")
	}
	f.sink.clear()
	f.completeOpen()
	res, _ := f.e.Check(ctx)
	if !res.Complete {
		t.Fatal("the demo completed the step")
	}
	if got := f.sink.types(); got != "check:open" || len(f.sink.reports) != 0 {
		t.Fatalf("a skip in progress records nothing: %s %s", got, f.sink.statuses())
	}
	next, ok := f.e.FinishSkip(ctx, "open")
	if !ok || next == nil || *next != "save" {
		t.Fatalf("finish skip %v %v", next, ok)
	}
	if got := f.sink.types(); got != "check:open step:open:skipped step:save:loaded" {
		t.Fatalf("events %s", got)
	}
	if got := f.sink.statuses(); !strings.HasPrefix(got, "open:skipped") {
		t.Fatalf("reports %s", got)
	}
	// save has checkpoint:false; layout takes one when save is skipped.
	f.e.BeginSkip("save")
	if next, _ := f.e.FinishSkip(ctx, "save"); next == nil || *next != "layout" || len(f.sink.cps) != 1 {
		t.Fatalf("second skip %v %v", next, f.sink.cps)
	}
	if f.e.FirstIncomplete(sc) != 2 {
		t.Fatalf("FirstIncomplete passes over skipped steps: %d", f.e.FirstIncomplete(sc))
	}
	f.sink.clear()
	f.e.BeginSkip("layout")
	next, ok = f.e.FinishSkip(ctx, "layout")
	if !ok || next != nil || f.e.Active() != nil {
		t.Fatalf("skipping the last step %v %v", next, ok)
	}
	f.sink.mu.Lock()
	last := f.sink.events[len(f.sink.events)-1].(*protocol.StepEvent)
	f.sink.mu.Unlock()
	if last.Status != protocol.StepSkipped || !last.LessonComplete {
		t.Fatalf("last %+v", last)
	}
	// AbortSkip releases the hold.
	f.e.Activate(sc, 0)
	f.e.BeginSkip("open")
	f.e.AbortSkip()
	f.sink.clear()
	f.completeOpen()
	f.e.Check(ctx)
	if !strings.Contains(f.sink.types(), "step:open:complete") {
		t.Fatalf("after abort %s", f.sink.types())
	}
}

func TestRehearseIsQuiet(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	ctx := context.Background()
	f.e.Rehearse(sc, 0)
	if f.e.Active() != nil {
		t.Fatal("a rehearsed step is not active")
	}
	f.completeOpen()
	res, ok := f.e.Check(ctx)
	if !ok || !res.Complete || res.StepID != "open" {
		t.Fatalf("rehearsal check %+v %v", res, ok)
	}
	f.sink.mu.Lock()
	quiet := len(f.sink.events) == 0 && len(f.sink.reports) == 0 && len(f.sink.saved) == 0
	f.sink.mu.Unlock()
	if !quiet {
		t.Fatalf("rehearsal emitted %s %s", f.sink.types(), f.sink.statuses())
	}
	if f.e.StartDemo("open") || f.e.BeginSkip("open") || f.e.ResetTo(sc.ID, "open") {
		t.Fatal("rehearsed steps take no demo, skip or reset")
	}
	// Counts restart per rehearsed step.
	f.e.Rehearse(sc, 1)
	f.e.OnKey(&protocol.KeyEvent{Cmd: "save-buffer"})
	f.e.Activate(sc, 2)
	if got := f.sink.types(); got != "step:layout:loaded" {
		t.Fatalf("activate after rehearsal %s", got)
	}
	if f.e.Dirty() {
		t.Fatal("commands of the rehearsal count for the activated step")
	}
}

func TestDropAll(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, lessonJSON)
	f.e.Activate(sc, 1)
	f.e.Hint("save", 1)
	other := f.e.Saved()
	other.Lessons["00-orientation/01-reading-the-screen"] = &LessonState{Done: map[string]int64{"q1": 1}}
	f.e.Restore(other)
	f.sink.clear()
	if !f.e.Drop("") {
		t.Fatal("drop all forgets the active step")
	}
	f.sink.mu.Lock()
	ev, _ := f.sink.events[0].(*protocol.StepEvent)
	f.sink.mu.Unlock()
	if ev == nil || ev.Status != protocol.StepDropped || ev.StepID != "save" || ev.Index != 1 || ev.Count != 3 {
		t.Fatalf("dropped event %+v", ev)
	}
	if s := f.e.Saved(); len(s.Lessons) != 0 || s.Active != nil {
		t.Fatalf("saved after drop all %+v", s)
	}
	if f.e.Drop("") {
		t.Fatal("nothing left to drop")
	}
	// Dropping another lesson keeps the active step.
	f.e.Activate(sc, 0)
	f.sink.clear()
	if f.e.Drop("00-orientation/01-reading-the-screen") || f.e.Active() == nil || f.sink.types() != "" {
		t.Fatal("drop of another lesson")
	}
}

func TestTrailingDebounceWithCap(t *testing.T) {
	cc := &countingChecker{}
	e := New(Options{Checker: cc, Tick: time.Hour})
	if e.opt.Debounce != 40*time.Millisecond || e.opt.DebounceMax != 120*time.Millisecond {
		t.Fatalf("defaults %v %v", e.opt.Debounce, e.opt.DebounceMax)
	}
	e = New(Options{Checker: cc, Debounce: 50 * time.Millisecond, DebounceMax: 150 * time.Millisecond, Tick: time.Hour})
	e.Activate(scenarioFromJSON(t, lessonJSON), 0)
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
