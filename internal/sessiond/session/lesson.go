package session

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// Attempt names one activation of a step (steps.Attempt): every
// observation, demo and skip carries it, and anything whose Seq is not
// the current one is ignored.
type Attempt = steps.Attempt

// Phase says what the core does with the active step's completion
// (backend.md §3.8).
type Phase uint8

const (
	// Idle: no step is active (nothing loaded, the lesson complete, or
	// dropped).
	Idle Phase = iota
	// Active: the learner works on the step; a completion is recorded
	// done and the next step activates.
	Active
	// Watching: a "Watch it" latch; a completion is recorded demonstrated
	// once and the step stays active (Demo).
	Watching
	// Skipping: the skip op plays the step's demo; completions are left to
	// it.
	Skipping
	// Rehearsing: a fast-forward replays an earlier step; nothing is
	// recorded or emitted.
	Rehearsing
	// Advancing: the completion is recorded; the advance op takes the next
	// step's checkpoint and activates it.
	Advancing
)

var phaseNames = [...]string{"idle", "active", "watching", "skipping", "rehearsing", "advancing"}

func (p Phase) String() string {
	if int(p) < len(phaseNames) {
		return phaseNames[p]
	}
	return "phase?"
}

// Demo is the state of a "Watch it" latch (phase Watching).
type Demo struct {
	// Playing: between demo start and demo end (stuck rules wait).
	Playing bool
	// Demonstrated: the demonstrated event went out for this latch.
	Demonstrated bool
	// Seen is the last demo traffic (DemoIdle).
	Seen time.Time
}

// Lesson is the lesson state, guarded by Core.mu. Ops, Observe (also
// while an op runs) and the demo bracket write it under mu; an op never
// holds mu across I/O, and after any I/O it re-reads the attempt and phase
// in the same hold of mu in which it writes.
type Lesson struct {
	Scenario *protocol.Scenario
	// Index is the active step's index, -1 when none.
	Index   int
	Attempt Attempt
	Phase   Phase
	Demo    Demo
	Saved   Saved
}

// stepPhase reports whether the phase has a step the evaluator works on.
func (l *Lesson) stepPhase() bool {
	return l.Phase == Active || l.Phase == Watching || l.Phase == Skipping || l.Phase == Rehearsing
}

// hasStep reports whether Index names a step of Scenario in a step phase.
func (l *Lesson) hasStep() bool {
	return l.Scenario != nil && l.Index >= 0 && l.Index < len(l.Scenario.Steps) && l.stepPhase()
}

// DemoIdle ends a "Watch it" demonstration that has sent no keys (rpc keys)
// for this long: the page that played it went away without rpc demo end.
// This exceeds the longest allowed demo pause (60 s). The learner's own
// commands do not keep it alive.
const DemoIdle = 65 * time.Second

// lapMode says what an activation does with the "again, without hints" lap.
type lapMode int

const (
	// lapAsIs leaves the lap alone (reset, advance, rehearsal).
	lapAsIs lapMode = iota
	// lapEnd ends a running lap (any ordinary load).
	lapEnd
	// lapStart starts a lap of the lesson now (scenario {lap}).
	lapStart
	// lapKeep keeps a lap of the same lesson running (resume).
	lapKeep
)

// activation says how activate starts a step.
type activation struct {
	// change is the lesson event's: loaded or reset.
	change protocol.LessonChange
	// phase is Active, or Rehearsing for a fast-forward (no event, report
	// or persist).
	phase Phase
	lap   lapMode
	// counted: a reset the learner asked for (Reset, Do it yourself),
	// counted and reported; the restore before demo start and skip is not.
	counted bool
}

// activate makes step idx of sc the current attempt: the attempt number
// rises, demo flags clear, the evaluator begins the attempt, Saved.Active
// is persisted, the lesson event goes out, an in-progress entry goes to the
// outbox, and an evaluation is scheduled. A reset forgets the step's
// saved outcomes; a counted one adds to its resets.
func (c *Core) activate(sc *protocol.Scenario, idx int, how activation) protocol.StepRef {
	sc.AssignIDs()
	if idx < 0 || idx >= len(sc.Steps) {
		idx = 0
	}
	step := &sc.Steps[idx]
	ref := protocol.StepRef{LessonID: sc.ID, StepID: step.ID}
	now := c.now()
	c.mu.Lock()
	l := &c.lesson
	switch how.lap {
	case lapStart:
		l.Saved.Lap = &protocol.LapInfo{ID: protocol.LapID(sc.ID, now.UnixMilli()), LessonID: sc.ID, StartedAt: now.UnixMilli()}
	case lapKeep:
		if l.Saved.Lap != nil && l.Saved.Lap.LessonID != sc.ID {
			l.Saved.Lap = nil
		}
	case lapEnd:
		l.Saved.Lap = nil
	}
	l.Scenario, l.Index, l.Phase, l.Demo = sc, idx, how.phase, Demo{}
	l.Attempt = Attempt{Lesson: sc.ID, Step: step.ID, Seq: l.Attempt.Seq + 1, StartedAt: now}
	c.obs = observed{nudged: map[string]bool{}}
	c.dirty = false
	c.beginLocked(step, sc.Setup.Root)
	if how.phase == Rehearsing {
		c.mu.Unlock()
		c.trigger()
		return ref
	}
	l.Saved.Active = &ref
	ls := l.Saved.lesson(sc.ID)
	if how.change == protocol.ChangeReset {
		if how.counted {
			ls.Resets[step.ID]++
		}
		delete(ls.Done, step.ID)
		delete(ls.Skipped, step.ID)
		delete(ls.Demonstrated, step.ID)
	}
	// Every activation reports in-progress (with a counted reset's new
	// total); the gateway ignores what it already has. A step finished or
	// demonstrated earlier stays so.
	if statusOf(ls, step.ID) == protocol.ProgressInProgress {
		c.recordLocked(sc.ID, step.ID, protocol.ProgressInProgress, now)
	}
	c.publishLocked(how.change, step.ID, 0)
	c.mu.Unlock()
	c.persist()
	c.trigger()
	return ref
}

// beginLocked begins the current attempt in the evaluator (step nil:
// nothing to evaluate). Lock order: Core.mu, then the evaluator's state
// lock; the evaluator calls Observe holding neither.
func (c *Core) beginLocked(step *protocol.Step, setupRoot string) {
	if c.d.Evaluator != nil {
		c.d.Evaluator.Begin(c.lesson.Attempt, step, setupRoot)
	}
}

// idleLocked leaves no step active: sc stays loaded (nil: none), a new
// attempt with nothing to evaluate begins, so observations of the old one
// are ignored.
func (c *Core) idleLocked(sc *protocol.Scenario) {
	l := &c.lesson
	l.Scenario, l.Index, l.Phase, l.Demo = sc, -1, Idle, Demo{}
	l.Attempt = Attempt{Seq: l.Attempt.Seq + 1}
	if sc != nil {
		l.Attempt.Lesson = sc.ID
	}
	c.obs = observed{}
	c.dirty = false
	c.beginLocked(nil, "")
}

func (c *Core) trigger() {
	if c.d.Evaluator != nil {
		c.d.Evaluator.Trigger()
	}
}

// resetTo restarts step stepID of the loaded lesson after its checkpoint
// was restored, as reset (counted when the learner asked). It reports
// false when the step is not part of the loaded lesson.
func (c *Core) resetTo(lessonID, stepID string, counted bool) bool {
	c.mu.Lock()
	sc := c.lesson.Scenario
	ok := sc != nil && sc.ID == lessonID && c.lesson.Phase != Rehearsing
	idx := -1
	if ok {
		idx = sc.StepIndex(stepID)
	}
	c.mu.Unlock()
	if idx < 0 {
		return false
	}
	c.activate(sc, idx, activation{change: protocol.ChangeReset, phase: Active, counted: counted})
	return true
}

// observed is what the core keeps of the current attempt's observations.
type observed struct {
	results  []protocol.CondResult
	complete bool
	// nudged holds the stuck rules that fired and have not re-armed.
	nudged map[string]bool
}

// Observe applies an evaluation (the evaluator's Observe hook): an
// observation of another attempt, or in a phase without a step, is
// ignored; a check event goes out when the results changed (or Check
// forced them); a rehearsal stays silent; an incomplete step runs the
// stuck rules unless a demo plays, a skip runs or a lap is on; a
// completion is recorded done in Active (the advance op follows),
// demonstrated once in Watching, and left to the skip op in Skipping. It
// never blocks.
func (c *Core) Observe(o steps.Observation) {
	now := c.now()
	c.mu.Lock()
	l := &c.lesson
	if o.Attempt.Seq != l.Attempt.Seq || !l.hasStep() {
		c.mu.Unlock()
		return
	}
	c.expireDemoLocked(now)
	sc, idx := l.Scenario, l.Index
	c.publishLocked(protocol.ChangePhase, sc.Steps[idx].ID, 0)
	step := &sc.Steps[idx]
	changed := o.Forced || !reflect.DeepEqual(o.Results, c.obs.results) || o.Complete != c.obs.complete
	c.obs.results, c.obs.complete = o.Results, o.Complete
	if l.Phase == Rehearsing {
		c.mu.Unlock()
		return
	}
	if changed {
		c.emitLocked(&protocol.CheckEvent{LessonID: sc.ID, StepID: step.ID, Attempt: l.Attempt.Seq, Conditions: o.Results, Complete: o.Complete})
	}
	if !o.Complete {
		if !(l.Phase == Watching && l.Demo.Playing) && l.Phase != Skipping && !c.inLapLocked() {
			for _, ev := range c.stuckLocked(step, o.Stats, now) {
				c.emitLocked(ev)
			}
		}
		c.mu.Unlock()
		return
	}
	switch l.Phase {
	case Skipping:
		c.mu.Unlock()
		return
	case Watching:
		// The demonstration completed the step: recorded once as
		// demonstrated; the step stays active for the learner.
		if l.Demo.Demonstrated {
			c.mu.Unlock()
			return
		}
		l.Demo.Demonstrated = true
		l.Saved.lesson(sc.ID).Demonstrated[step.ID] = now.UnixMilli()
		c.recordLocked(sc.ID, step.ID, protocol.ProgressDemonstrated, now)
		c.publishLocked(protocol.ChangeDemonstrated, step.ID, 0)
		c.mu.Unlock()
		c.persist()
		return
	}
	// Active: record done, then advance.
	l.Saved.lesson(sc.ID).Done[step.ID] = now.UnixMilli()
	last := idx+1 >= len(sc.Steps)
	e := l.Saved.lesson(sc.ID).report(sc.ID, step.ID, protocol.ProgressDone, now)
	if last && c.inLapLocked() {
		lap := l.Saved.Lap
		e.LapMs, e.LapID = max(now.UnixMilli()-lap.StartedAt, 1), lap.ID
		if e.LapID == "" {
			// A lap saved before laps had ids.
			e.LapID = protocol.LapID(lap.LessonID, lap.StartedAt)
		}
		l.Saved.Lap = nil
	}
	c.d.Progress.Record(e)
	c.finishLocked(last, true)
	c.publishLocked(protocol.ChangeComplete, step.ID, e.LapMs)
	c.mu.Unlock()
	c.persist()
}

// finishLocked follows a recorded completion or skip of the active step:
// phase Advancing and, after the last step, no active step saved. Observe
// queues the advance op (enqueue); the skip op runs advance inline.
func (c *Core) finishLocked(last, enqueue bool) {
	l := &c.lesson
	l.Phase = Advancing
	if last {
		l.Saved.Active = nil
	}
	if enqueue {
		c.submitLocked(c.advanceOp(l.Attempt))
	}
}

// advanceOp is the advance op for attempt a, run before anything queued.
func (c *Core) advanceOp(a Attempt) *Op {
	return &Op{Kind: OpAdvance, Req: a.Seq, Deadline: AdvanceDeadline, Preempt: true, info: OpInfo{LessonID: a.Lesson, StepID: a.Step}}
}

// advance is the advance op for attempt seq, a no-op unless that attempt
// is still current and Advancing: after the last step no step is active
// (the snapshot already said complete); otherwise the next step's
// checkpoint is taken when it takes one (a failure is logged) and the step
// activates, unless a drop cancelled the op. It returns the next step's id.
func (c *Core) advance(ctx context.Context, seq uint64) (*string, error) {
	c.mu.Lock()
	l := &c.lesson
	if l.Attempt.Seq != seq || l.Phase != Advancing {
		c.mu.Unlock()
		return nil, nil
	}
	sc, next := l.Scenario, l.Index+1
	if next >= len(sc.Steps) {
		c.idleLocked(sc)
		c.publishLocked(protocol.ChangePhase, "", 0)
		c.mu.Unlock()
		return nil, nil
	}
	c.mu.Unlock()
	st := &sc.Steps[next]
	if st.TakesCheckpoint() {
		if err := CaptureStep(ctx, c.d.Snapshots, sc.ID, st.ID, true); err != nil {
			c.log.Warn("step checkpoint", "lesson", sc.ID, "step", st.ID, "err", err)
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, interrupted(ctx)
	}
	c.mu.Lock()
	still := l.Attempt.Seq == seq && l.Phase == Advancing
	c.mu.Unlock()
	if !still {
		return nil, nil
	}
	c.activate(sc, next, activation{change: protocol.ChangeLoaded, phase: Active})
	id := st.ID
	return &id, nil
}

// endRehearsal ends a rehearsal for a load that activates nothing: the
// step active before it (prev) is Active again, whatever its phase was
// (Watching included), with no demo latch and a new attempt; without a
// step before it the lesson is idle. The step counts as dirty, since the
// load changed the workspace, so demo start and skip restore its
// checkpoint first.
func (c *Core) endRehearsal(prev Lesson) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lesson.Phase != Rehearsing {
		return
	}
	if prev.Scenario == nil || prev.Index < 0 || prev.Index >= len(prev.Scenario.Steps) || prev.Phase == Idle {
		c.idleLocked(prev.Scenario)
		c.publishLocked(protocol.ChangePhase, "", 0)
		return
	}
	l := &c.lesson
	step := &prev.Scenario.Steps[prev.Index]
	l.Scenario, l.Index, l.Phase, l.Demo = prev.Scenario, prev.Index, Active, Demo{}
	l.Attempt = Attempt{Lesson: prev.Scenario.ID, Step: step.ID, Seq: l.Attempt.Seq + 1, StartedAt: c.now()}
	c.obs = observed{nudged: map[string]bool{}}
	c.beginLocked(step, prev.Scenario.Setup.Root)
	c.dirty = true
	c.publishLocked(protocol.ChangePhase, step.ID, 0)
}

// attemptSeq returns the current attempt's number.
func (c *Core) attemptSeq() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lesson.Attempt.Seq
}

// lessonState returns a copy of the lesson state (without Saved).
func (c *Core) lessonState() Lesson {
	c.mu.Lock()
	defer c.mu.Unlock()
	l := c.lesson
	l.Saved = Saved{}
	return l
}

// --- the demo bracket and other non-op methods ----------------------------

// errNotActiveStep answers requests naming a step that is not the active
// one.
func errNotActiveStep() error { return httpx.Conflict("that step is not the active step") }

// errLap answers hint, demo start and skip during a lap.
func errLap() error { return httpx.Conflict("hints, demonstrations and skips are off during the lap") }

// DemoEnd clears the playing flag of the active step stepID (rpc demo
// end); the latch stays.
func (c *Core) DemoEnd(stepID string) error {
	c.mu.Lock()
	a := c.activeLocked()
	ok := a != nil && a.StepID == stepID
	if ok {
		c.lesson.Demo.Playing = false
	}
	c.mu.Unlock()
	if !ok {
		return errNotActiveStep()
	}
	c.trigger()
	return nil
}

// DemoTraffic notes that keys were sent while a demonstration may be
// playing (rpc keys), so it does not end for idleness.
func (c *Core) DemoTraffic() {
	now := c.now()
	c.mu.Lock()
	if c.lesson.Phase == Watching && c.lesson.Demo.Playing {
		c.lesson.Demo.Seen = now
	}
	c.mu.Unlock()
}

// AbandonDemo ends a demonstration whose page went away without rpc demo
// end (its terminal connection closed), as abandonDemoLocked does.
func (c *Core) AbandonDemo() {
	c.mu.Lock()
	ended := c.abandonDemoLocked()
	c.publishLocked(protocol.ChangePhase, c.lesson.Attempt.Step, 0)
	c.mu.Unlock()
	if ended {
		c.trigger()
	}
}

// expireDemoLocked abandons a demonstration idle for DemoIdle.
func (c *Core) expireDemoLocked(now time.Time) {
	if c.lesson.Phase == Watching && c.lesson.Demo.Playing && now.Sub(c.lesson.Demo.Seen) >= DemoIdle {
		c.abandonDemoLocked()
	}
}

// abandonDemoLocked ends a playing demonstration as DemoEnd would; one that
// never completed the step also drops its latch (phase Active again), so
// the learner's own completion counts as done. It reports whether a
// demonstration was playing.
func (c *Core) abandonDemoLocked() bool {
	l := &c.lesson
	if l.Phase != Watching || !l.Demo.Playing {
		return false
	}
	l.Demo.Playing = false
	if !l.Demo.Demonstrated {
		l.Phase = Active
	}
	return true
}

// Watching reports whether the active step has a "Watch it" latch.
func (c *Core) Watching() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lesson.Phase == Watching
}

// Scenario returns the loaded scenario (nil when none).
func (c *Core) Scenario() *protocol.Scenario {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lesson.Scenario
}

// Active returns the active step, or nil when no step is active (none
// loaded, the lesson complete, a fast-forward or an advance under way).
func (c *Core) Active() *protocol.StepRef {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.activeLocked()
}

func (c *Core) activeLocked() *protocol.StepRef {
	l := &c.lesson
	if !l.hasStep() || l.Phase == Rehearsing {
		return nil
	}
	return &protocol.StepRef{LessonID: l.Scenario.ID, StepID: l.Scenario.Steps[l.Index].ID}
}

// ActiveIndex returns the active step index and the step count.
func (c *Core) ActiveIndex() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lesson.Scenario == nil {
		return -1, 0
	}
	return c.lesson.Index, len(c.lesson.Scenario.Steps)
}

// inLapLocked reports whether the loaded lesson runs a lap.
func (c *Core) inLapLocked() bool {
	l := &c.lesson
	return l.Scenario != nil && l.Saved.Lap != nil && l.Saved.Lap.LessonID == l.Scenario.ID
}

// Lap returns the running lap, or nil.
func (c *Core) Lap() *protocol.LapInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.inLapLocked() {
		return nil
	}
	lap := *c.lesson.Saved.Lap
	return &lap
}

// dirtyLocked reports whether the learner ran a command in the active step
// since it started or was last reset, or a load that failed changed the
// workspace under it.
func (c *Core) dirtyLocked() bool {
	return c.dirty || (c.d.Evaluator != nil && c.d.Evaluator.Commands() > 0)
}

// Check evaluates the active step now (rpc check); ok is false when no
// step is active.
func (c *Core) Check(ctx context.Context) (protocol.CheckResult, bool) {
	if c.Active() == nil || c.d.Evaluator == nil {
		return protocol.CheckResult{}, false
	}
	return c.d.Evaluator.Check(ctx)
}
