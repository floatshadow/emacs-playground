package steps

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Timing (backend.md §3.8). An evaluation runs DefaultDebounce after the
// last trigger of a burst (a key event and the state that follows it a few
// milliseconds later), but never later than DefaultDebounceMax after the
// burst's first trigger, so a stream of events cannot postpone it.
const (
	DefaultDebounce    = 40 * time.Millisecond
	DefaultDebounceMax = 120 * time.Millisecond
	DefaultTick        = 2 * time.Second
)

// QuitCommands count towards quits_in_window.
var QuitCommands = map[string]bool{
	"keyboard-quit": true, "abort-minibuffers": true, "abort-recursive-edit": true,
	"minibuffer-keyboard-quit": true, "keyboard-escape-quit": true,
	"isearch-abort": true, "isearch-cancel": true,
}

// Hooks connect the engine to the rest of sessiond. Every hook is optional.
type Hooks struct {
	// Emit publishes an event (check, step, nudge).
	Emit func(protocol.Event)
	// Report sends progress to the gateway; it must not block (queue it).
	Report func(protocol.ProgressReport)
	// Checkpoint takes the checkpoint for a step that is starting. It runs
	// on the evaluation goroutine before the step's loaded event.
	Checkpoint func(ctx context.Context, lessonID, stepID string)
	// Persist is called after the saved state changed.
	Persist func(Saved)
}

// Saved is the engine state kept in /work/.playground/state.json.
type Saved struct {
	// Active is the active step, nil when none.
	Active *protocol.StepRef `json:"active,omitempty"`
	// Lessons is per-lesson progress known to this session.
	Lessons map[string]*LessonState `json:"lessons,omitempty"`
}

// LessonState is the progress of one lesson.
type LessonState struct {
	// Done maps step id to completion time (ms).
	Done map[string]int64 `json:"done,omitempty"`
	// Skipped maps step id to the time the learner skipped it (ms).
	Skipped map[string]int64 `json:"skipped,omitempty"`
	// Demonstrated maps step id to the time a "Watch it" demonstration
	// completed it (ms).
	Demonstrated map[string]int64 `json:"demonstrated,omitempty"`
	Hints        map[string]int   `json:"hints,omitempty"`
	Resets       map[string]int   `json:"resets,omitempty"`
}

// finished reports whether resuming the lesson passes over stepID: done and
// skipped steps are finished, a demonstrated one is practised (api.md §2.4).
func (ls *LessonState) finished(stepID string) bool {
	return ls != nil && (ls.Done[stepID] != 0 || ls.Skipped[stepID] != 0)
}

// progress is a report of stepID with status, carrying the step's hint and
// reset totals when there are any.
func (ls *LessonState) progress(lessonID, stepID string, status protocol.ProgressStatus, at time.Time) protocol.ProgressReport {
	rep := protocol.ProgressReport{LessonID: lessonID, StepID: stepID, Status: status, At: at.UnixMilli()}
	if h := ls.Hints[stepID]; h > 0 {
		rep.HintsUsed = &h
	}
	if r := ls.Resets[stepID]; r > 0 {
		rep.Resets = &r
	}
	return rep
}

// hold says what the engine does when the active step's conditions hold.
type hold int

const (
	// holdNone: record the completion (done, or demonstrated under the
	// demo latch) and advance.
	holdNone hold = iota
	// holdSkip: a skip is replaying the step's demo; nothing is recorded
	// until FinishSkip, but check events flow.
	holdSkip
	// holdQuiet: a "Go to this step" fast-forward replays earlier steps;
	// nothing is recorded or emitted.
	holdQuiet
)

// Options configure an Engine.
type Options struct {
	// Root is the workspace opened with os.OpenRoot (file conditions).
	Root *os.Root
	// Workspace is the absolute workspace path (shell conditions).
	Workspace string
	Checker   Checker
	Shell     ShellRunner
	Hooks     Hooks
	Log       *slog.Logger
	// Locale selects nudge text from localised strings (default "en").
	Locale string
	// Now, Debounce, DebounceMax and Tick are overridable for tests.
	// DebounceMax defaults to DefaultDebounceMax with the default debounce,
	// else to three times Debounce.
	Now         func() time.Time
	Debounce    time.Duration
	DebounceMax time.Duration
	Tick        time.Duration
}

// Engine evaluates the active step. Methods are safe for concurrent use.
type Engine struct {
	opt Options
	log *slog.Logger

	mu           sync.Mutex
	sc           *protocol.Scenario
	idx          int // active step index; -1 when none; len(Steps) after the last
	gen          uint64
	startedAt    time.Time
	counts       map[string]int
	lessonCounts map[string]int
	state        *protocol.State
	stateDoc     map[string]any
	lastKeyAt    time.Time
	miniSince    time.Time
	quits        []time.Time
	lastUnbound  string
	unboundRun   int
	lastResults  []protocol.CondResult
	lastComplete bool
	nudged       map[string]bool
	reported     map[string]bool
	saved        Saved
	// commands counts agent key events since the step started or was last
	// reset ("did the learner do anything here?").
	commands int
	// demoLatch is set by StartDemo: a completion is then recorded as
	// demonstrated, not done, and the step stays active. demoPlaying is
	// true between StartDemo and EndDemo (stuck rules wait).
	// demonstrated is set once the demonstrated event went out for the
	// latch. All three clear when the step (re)starts.
	demoLatch    bool
	demoPlaying  bool
	demonstrated bool
	hold         hold

	shellCache shellCache
	patterns   patternCache
	trigger    chan struct{}
	evalMu     sync.Mutex
}

// New returns an engine with no active scenario.
func New(opt Options) *Engine {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Debounce <= 0 {
		opt.Debounce = DefaultDebounce
		if opt.DebounceMax <= 0 {
			opt.DebounceMax = DefaultDebounceMax
		}
	}
	if opt.DebounceMax < opt.Debounce {
		opt.DebounceMax = 3 * opt.Debounce
	}
	if opt.Tick <= 0 {
		opt.Tick = DefaultTick
	}
	if opt.Locale == "" {
		opt.Locale = protocol.DefaultLocale
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	return &Engine{
		opt: opt, log: opt.Log, idx: -1,
		counts: map[string]int{}, lessonCounts: map[string]int{},
		nudged: map[string]bool{}, reported: map[string]bool{},
		saved:   Saved{Lessons: map[string]*LessonState{}},
		trigger: make(chan struct{}, 1),
	}
}

// Restore loads saved progress (from state.json) without activating a step.
func (e *Engine) Restore(s Saved) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s.Lessons == nil {
		s.Lessons = map[string]*LessonState{}
	}
	e.saved = s
}

// Saved returns a copy of the persistent state.
func (e *Engine) Saved() Saved {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.savedLocked()
}

func (e *Engine) savedLocked() Saved {
	out := Saved{Lessons: map[string]*LessonState{}}
	if e.saved.Active != nil {
		a := *e.saved.Active
		out.Active = &a
	}
	for id, ls := range e.saved.Lessons {
		out.Lessons[id] = &LessonState{
			Done: maps.Clone(ls.Done), Skipped: maps.Clone(ls.Skipped), Demonstrated: maps.Clone(ls.Demonstrated),
			Hints: maps.Clone(ls.Hints), Resets: maps.Clone(ls.Resets),
		}
	}
	return out
}

func (e *Engine) lesson(id string) *LessonState {
	ls := e.saved.Lessons[id]
	if ls == nil {
		ls = &LessonState{}
		e.saved.Lessons[id] = ls
	}
	if ls.Done == nil {
		ls.Done = map[string]int64{}
	}
	if ls.Skipped == nil {
		ls.Skipped = map[string]int64{}
	}
	if ls.Demonstrated == nil {
		ls.Demonstrated = map[string]int64{}
	}
	if ls.Hints == nil {
		ls.Hints = map[string]int{}
	}
	if ls.Resets == nil {
		ls.Resets = map[string]int{}
	}
	return ls
}

// FirstIncomplete returns the index of the first step of sc that is neither
// done nor skipped according to saved progress, or 0 when every step is.
// A demonstrated step is resumed so the learner practises it (api.md §2.4).
func (e *Engine) FirstIncomplete(sc *protocol.Scenario) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	ls := e.saved.Lessons[sc.ID]
	for i := range sc.Steps {
		if !ls.finished(sc.Steps[i].ID) {
			return i
		}
	}
	return 0
}

// Scenario returns the loaded scenario (nil when none).
func (e *Engine) Scenario() *protocol.Scenario {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sc
}

// Active returns the active step, or nil when no step is active (no
// scenario, or the lesson is complete).
func (e *Engine) Active() *protocol.StepRef {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.activeLocked()
}

func (e *Engine) activeLocked() *protocol.StepRef {
	if e.sc == nil || e.idx < 0 || e.idx >= len(e.sc.Steps) || e.hold == holdQuiet {
		return nil
	}
	return &protocol.StepRef{LessonID: e.sc.ID, StepID: e.sc.Steps[e.idx].ID}
}

// ActiveIndex returns the active step index and the step count.
func (e *Engine) ActiveIndex() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sc == nil {
		return -1, 0
	}
	return e.idx, len(e.sc.Steps)
}

// Activate makes step idx of sc active (the scenario loader has already
// taken its checkpoint), emits step loaded, and schedules an evaluation.
func (e *Engine) Activate(sc *protocol.Scenario, idx int) protocol.StepRef {
	sc.AssignIDs()
	e.mu.Lock()
	sameLesson := e.sc != nil && e.sc.ID == sc.ID
	e.sc = sc
	if !sameLesson {
		e.lessonCounts = map[string]int{}
	}
	if idx < 0 || idx >= len(sc.Steps) {
		idx = 0
	}
	e.hold = holdNone
	evs := e.startStepLocked(idx, protocol.StepLoaded)
	ref := *e.activeLocked()
	saved := e.savedLocked()
	reports := e.inProgressLocked()
	e.mu.Unlock()
	e.emit(evs...)
	e.report(reports...)
	e.persist(saved)
	e.Trigger()
	return ref
}

// Rehearse makes step idx of sc active without recording or emitting
// anything: completions are not recorded, check, step and nudge events
// are not emitted, progress is neither reported nor persisted. The scenario
// loader uses it to replay the steps before the one "Go to this step" asked
// for, evaluating each step's conditions after its demo (Check); Activate
// ends it. Command counts restart for the step as with Activate.
func (e *Engine) Rehearse(sc *protocol.Scenario, idx int) {
	sc.AssignIDs()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sc == nil || e.sc.ID != sc.ID {
		e.lessonCounts = map[string]int{}
	}
	e.sc = sc
	e.hold = holdQuiet
	e.startStepLocked(idx, protocol.StepLoaded)
}

// startStepLocked resets per-step state and returns the step event.
func (e *Engine) startStepLocked(idx int, status protocol.StepStatus) []protocol.Event {
	e.idx = idx
	e.gen++
	e.startedAt = e.opt.Now()
	e.counts = map[string]int{}
	e.commands = 0
	e.demoLatch, e.demoPlaying, e.demonstrated = false, false, false
	e.lastResults = nil
	e.lastComplete = false
	e.nudged = map[string]bool{}
	e.quits = nil
	e.unboundRun = 0
	e.lastUnbound = ""
	e.shellCache.clear()
	if e.sc == nil || idx >= len(e.sc.Steps) {
		return nil
	}
	if e.hold == holdQuiet {
		return nil
	}
	e.saved.Active = &protocol.StepRef{LessonID: e.sc.ID, StepID: e.sc.Steps[idx].ID}
	return []protocol.Event{&protocol.StepEvent{
		LessonID: e.sc.ID, StepID: e.sc.Steps[idx].ID, Status: status,
		Index: idx, Count: len(e.sc.Steps),
	}}
}

// inProgressLocked reports a step as started once per session.
func (e *Engine) inProgressLocked() []protocol.ProgressReport {
	a := e.activeLocked()
	if a == nil {
		return nil
	}
	key := a.LessonID + "\x00" + a.StepID
	ls := e.saved.Lessons[a.LessonID]
	if e.reported[key] || ls.finished(a.StepID) || (ls != nil && ls.Demonstrated[a.StepID] != 0) {
		return nil
	}
	e.reported[key] = true
	return []protocol.ProgressReport{{LessonID: a.LessonID, StepID: a.StepID, Status: protocol.ProgressInProgress, At: e.opt.Now().UnixMilli()}}
}

// statusOf is the saved status of a step, for reports that carry it along
// (hints): done, skipped, demonstrated or in-progress.
func statusOf(ls *LessonState, stepID string) protocol.ProgressStatus {
	switch {
	case ls == nil:
		return protocol.ProgressInProgress
	case ls.Done[stepID] != 0:
		return protocol.ProgressDone
	case ls.Skipped[stepID] != 0:
		return protocol.ProgressSkipped
	case ls.Demonstrated[stepID] != 0:
		return protocol.ProgressDemonstrated
	}
	return protocol.ProgressInProgress
}

// Drop forgets the active step when it belongs to lessonID, and the lesson's
// saved progress (the gateway reset it). With an empty lessonID it forgets
// every lesson's progress and the active step whatever its lesson ("Start
// over"). A forgotten active step emits step {status: dropped}. It reports
// whether an active step was forgotten.
func (e *Engine) Drop(lessonID string) bool {
	e.mu.Lock()
	var evs []protocol.Event
	if e.sc != nil && (lessonID == "" || e.sc.ID == lessonID) {
		if a := e.activeLocked(); a != nil {
			evs = append(evs, &protocol.StepEvent{LessonID: a.LessonID, StepID: a.StepID, Status: protocol.StepDropped,
				Index: e.idx, Count: len(e.sc.Steps)})
		}
		e.sc = nil
		e.idx = -1
		e.gen++
		e.hold = holdNone
		e.demoLatch, e.demoPlaying, e.demonstrated = false, false, false
		e.saved.Active = nil
	}
	if lessonID == "" {
		e.saved.Lessons = map[string]*LessonState{}
		e.reported = map[string]bool{}
	} else {
		delete(e.saved.Lessons, lessonID)
		for k := range e.reported {
			if strings.HasPrefix(k, lessonID+"\x00") {
				delete(e.reported, k)
			}
		}
	}
	saved := e.savedLocked()
	e.mu.Unlock()
	e.emit(evs...)
	e.persist(saved)
	return len(evs) > 0
}

// ResetTo restarts step stepID of the active lesson after its checkpoint was
// restored ("Reset this step", "Do it yourself"): counters clear, the demo
// latch clears, the reset count increments and is reported, and step
// {status: reset} is emitted. It returns false when the step is not part of
// the active lesson.
func (e *Engine) ResetTo(lessonID, stepID string) bool {
	return e.restart(lessonID, stepID, true)
}

// Restart is ResetTo without counting a reset: "Watch it" and "Skip this
// step" put a step back to its checkpoint before playing its demo (api.md
// §3.4).
func (e *Engine) Restart(lessonID, stepID string) bool {
	return e.restart(lessonID, stepID, false)
}

func (e *Engine) restart(lessonID, stepID string, count bool) bool {
	e.mu.Lock()
	if e.sc == nil || e.sc.ID != lessonID || e.hold == holdQuiet {
		e.mu.Unlock()
		return false
	}
	idx := e.sc.StepIndex(stepID)
	if idx < 0 {
		e.mu.Unlock()
		return false
	}
	ls := e.lesson(lessonID)
	if count {
		ls.Resets[stepID]++
	}
	delete(ls.Done, stepID)
	delete(ls.Skipped, stepID)
	delete(ls.Demonstrated, stepID)
	evs := e.startStepLocked(idx, protocol.StepReset)
	rep := ls.progress(lessonID, stepID, protocol.ProgressInProgress, e.opt.Now())
	saved := e.savedLocked()
	e.mu.Unlock()
	e.emit(evs...)
	if count {
		e.report(rep)
	}
	e.persist(saved)
	e.Trigger()
	return true
}

// Dirty reports whether the learner ran a command in the active step since
// it started or was last reset (demo start and skip restore the checkpoint
// first when it is).
func (e *Engine) Dirty() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.commands > 0
}

// StartDemo sets the demo latch of the active step stepID (rpc demo start,
// api.md §4.1): until the step restarts, a completion is recorded as
// demonstrated and the step stays active. It returns false when stepID is
// not the active step.
func (e *Engine) StartDemo(stepID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.activeLocked()
	if a == nil || a.StepID != stepID || e.hold != holdNone {
		return false
	}
	e.demoLatch, e.demoPlaying = true, true
	return true
}

// EndDemo clears the playing flag (rpc demo end); the latch stays. It
// returns false when stepID is not the active step.
func (e *Engine) EndDemo(stepID string) bool {
	e.mu.Lock()
	a := e.activeLocked()
	ok := a != nil && a.StepID == stepID
	if ok {
		e.demoPlaying = false
	}
	e.mu.Unlock()
	if ok {
		e.Trigger()
	}
	return ok
}

// Latched reports whether the active step's demo latch is set.
func (e *Engine) Latched() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.demoLatch
}

// BeginSkip holds completions of the active step stepID while a skip replays
// its demo (rpc skip). It returns false when stepID is not the active step
// or another skip runs.
func (e *Engine) BeginSkip(stepID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.activeLocked()
	if a == nil || a.StepID != stepID || e.hold != holdNone {
		return false
	}
	e.hold = holdSkip
	e.demoLatch, e.demoPlaying, e.demonstrated = false, false, false
	return true
}

// AbortSkip releases a hold taken by BeginSkip without skipping.
func (e *Engine) AbortSkip() {
	e.mu.Lock()
	if e.hold == holdSkip {
		e.hold = holdNone
	}
	e.mu.Unlock()
	e.Trigger()
}

// FinishSkip records the active step stepID as skipped, emits step
// {status: skipped}, takes the next step's checkpoint and activates it
// (step loaded), like a completion. It returns the next step's id (nil
// after the last step) and false when stepID is no longer the active step.
func (e *Engine) FinishSkip(ctx context.Context, stepID string) (*string, bool) {
	e.mu.Lock()
	if e.hold == holdSkip {
		e.hold = holdNone
	}
	a := e.activeLocked()
	if a == nil || a.StepID != stepID {
		e.mu.Unlock()
		return nil, false
	}
	sc, idx := e.sc, e.idx
	now := e.opt.Now()
	ls := e.lesson(sc.ID)
	ls.Skipped[stepID] = now.UnixMilli()
	rep := ls.progress(sc.ID, stepID, protocol.ProgressSkipped, now)
	last := idx+1 >= len(sc.Steps)
	ev := &protocol.StepEvent{LessonID: sc.ID, StepID: stepID, Status: protocol.StepSkipped, Index: idx, Count: len(sc.Steps), LessonComplete: last}
	next := e.advanceLocked(ctx, sc, idx, []protocol.Event{ev}, rep)
	if next == nil {
		return nil, true
	}
	id := next.ID
	return &id, true
}

// Hint records that hint level was shown for stepID of the active lesson.
// HintsUsed is the highest level shown (an absolute total).
func (e *Engine) Hint(stepID string, level int) bool {
	e.mu.Lock()
	if e.sc == nil || e.sc.StepIndex(stepID) < 0 || level <= 0 {
		e.mu.Unlock()
		return false
	}
	lessonID := e.sc.ID
	ls := e.lesson(lessonID)
	if level <= ls.Hints[stepID] {
		e.mu.Unlock()
		return true
	}
	ls.Hints[stepID] = level
	rep := ls.progress(lessonID, stepID, statusOf(ls, stepID), e.opt.Now())
	saved := e.savedLocked()
	e.mu.Unlock()
	e.report(rep)
	e.persist(saved)
	return true
}

// OnKey counts a command (agent key event).
func (e *Engine) OnKey(ev *protocol.KeyEvent) {
	now := e.opt.Now()
	e.mu.Lock()
	if ev.Cmd != "" {
		e.counts[ev.Cmd]++
		e.lessonCounts[ev.Cmd]++
	}
	e.commands++
	e.lastKeyAt = now
	if QuitCommands[ev.Cmd] {
		e.quits = append(e.quits, now)
		if len(e.quits) > 64 {
			e.quits = e.quits[len(e.quits)-64:]
		}
	}
	if ev.Cmd == protocol.UndefinedCommand {
		if ev.Keys == e.lastUnbound {
			e.unboundRun++
		} else {
			e.lastUnbound, e.unboundRun = ev.Keys, 1
		}
	} else {
		e.lastUnbound, e.unboundRun = "", 0
	}
	e.mu.Unlock()
	e.Trigger()
}

// OnState records the latest state.
func (e *Engine) OnState(st *protocol.State) {
	doc := protocol.StateDoc(st)
	now := e.opt.Now()
	e.mu.Lock()
	e.state = st
	e.stateDoc = doc
	e.trackMinibufferLocked(st.Minibuffer != nil, now)
	e.mu.Unlock()
	e.Trigger()
}

// OnMinibuffer tracks how long a minibuffer has been open.
func (e *Engine) OnMinibuffer(ev *protocol.MinibufferEvent) {
	now := e.opt.Now()
	e.mu.Lock()
	e.trackMinibufferLocked(ev.Open, now)
	e.mu.Unlock()
	e.Trigger()
}

func (e *Engine) trackMinibufferLocked(open bool, now time.Time) {
	switch {
	case open && e.miniSince.IsZero():
		e.miniSince = now
	case !open:
		e.miniSince = time.Time{}
	}
}

// OnFile invalidates cached shell results and schedules an evaluation.
func (e *Engine) OnFile(*protocol.FileEvent) {
	e.shellCache.clear()
	e.Trigger()
}

// Trigger schedules a debounced evaluation.
func (e *Engine) Trigger() {
	select {
	case e.trigger <- struct{}{}:
	default:
	}
}

// Run evaluates on triggers and on every Tick until ctx ends. Triggers are
// debounced on the trailing edge: the evaluation runs Debounce after the
// last trigger of a burst, and at the latest DebounceMax after its first
// (backend.md §3.8).
func (e *Engine) Run(ctx context.Context) {
	tick := time.NewTicker(e.opt.Tick)
	defer tick.Stop()
	var timer *time.Timer
	var timerC <-chan time.Time
	var first time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.trigger:
			now := time.Now()
			if timerC == nil {
				first = now
			}
			wait := e.opt.Debounce
			if left := first.Add(e.opt.DebounceMax).Sub(now); left < wait {
				wait = max(left, 0)
			}
			if timer == nil {
				timer = time.NewTimer(wait)
			} else {
				timer.Stop()
				timer.Reset(wait)
			}
			timerC = timer.C
		case <-timerC:
			timerC = nil
			e.evaluate(ctx, false)
		case <-tick.C:
			e.evaluate(ctx, false)
		}
	}
}

// Check evaluates now and returns the result (rpc check). ok is false when
// no step is active.
func (e *Engine) Check(ctx context.Context) (protocol.CheckResult, bool) {
	return e.evaluate(ctx, true)
}

// evaluate runs one pass: conditions, check event, completion, stuck rules.
func (e *Engine) evaluate(ctx context.Context, force bool) (protocol.CheckResult, bool) {
	e.evalMu.Lock()
	defer e.evalMu.Unlock()

	e.mu.Lock()
	if e.sc == nil || e.idx < 0 || e.idx >= len(e.sc.Steps) {
		e.mu.Unlock()
		return protocol.CheckResult{}, false
	}
	sc, idx, gen := e.sc, e.idx, e.gen
	step := &sc.Steps[idx]
	ec := &evalContext{
		ctx:          ctx,
		counts:       maps.Clone(e.counts),
		lessonCounts: maps.Clone(e.lessonCounts),
		state:        e.state,
		stateDoc:     e.stateDoc,
		root:         e.opt.Root,
		workspace:    e.opt.Workspace,
		setupRoot:    sc.Setup.Root,
		shell:        e.opt.Shell,
		shellCache:   &e.shellCache,
		patterns:     &e.patterns,
	}
	e.mu.Unlock()

	// Elisp forms of the step, batched into one agent call.
	var forms []string
	seen := map[string]bool{}
	for i := range step.Conditions {
		forms = collectElisp(&step.Conditions[i], forms, seen)
	}
	if len(forms) > 0 {
		ec.elisp = map[string]protocol.Truth{}
		if e.opt.Checker == nil {
			ec.elispErr = "Emacs is not connected"
		} else {
			cctx, cancel := context.WithTimeout(ctx, ElispTimeout+time.Second)
			res, err := e.opt.Checker.Check(cctx, forms, ElispTimeout)
			cancel()
			if err != nil {
				ec.elispErr = err.Error()
			} else {
				for i, f := range forms {
					if i < len(res) {
						ec.elisp[f] = res[i]
					}
				}
			}
		}
	}

	results := make([]protocol.CondResult, len(step.Conditions))
	complete := len(step.Conditions) > 0
	for i := range step.Conditions {
		c := &step.Conditions[i]
		r := ec.eval(c)
		id := c.ID
		if id == "" {
			id = protocol.ConditionID(step.ID, i)
		}
		cr := protocol.CondResult{ID: id, OK: r.t == protocol.True, Unknown: r.t == protocol.Unknown}
		if !cr.OK {
			cr.Detail = truncate(r.detail, MaxShellOutput)
			complete = false
		}
		results[i] = cr
	}
	out := protocol.CheckResult{LessonID: sc.ID, StepID: step.ID, Conditions: results, Complete: complete}

	now := e.opt.Now()
	e.mu.Lock()
	if e.gen != gen {
		// The step changed while we evaluated: discard.
		e.mu.Unlock()
		return out, true
	}
	var evs []protocol.Event
	if force || !reflect.DeepEqual(results, e.lastResults) || complete != e.lastComplete {
		e.lastResults = results
		e.lastComplete = complete
		evs = append(evs, &protocol.CheckEvent{LessonID: sc.ID, StepID: step.ID, Conditions: results, Complete: complete})
	}
	switch {
	case e.hold == holdQuiet:
		// A fast-forward rehearses the step: nothing goes out.
		e.mu.Unlock()
		return out, true
	case !complete:
		if !e.demoPlaying && e.hold == holdNone {
			evs = append(evs, e.stuckLocked(step, now)...)
		}
		e.mu.Unlock()
		e.emit(evs...)
		return out, true
	case e.hold == holdSkip:
		// A skip is replaying the demo; FinishSkip records the step.
		e.mu.Unlock()
		e.emit(evs...)
		return out, true
	case e.demoLatch:
		// "Watch it": the demonstration completed the step. Record it once
		// as demonstrated; the step stays active for the learner.
		if e.demonstrated {
			e.mu.Unlock()
			e.emit(evs...)
			return out, true
		}
		e.demonstrated = true
		ls := e.lesson(sc.ID)
		ls.Demonstrated[step.ID] = now.UnixMilli()
		evs = append(evs, &protocol.StepEvent{LessonID: sc.ID, StepID: step.ID, Status: protocol.StepDemonstrated, Index: idx, Count: len(sc.Steps)})
		rep := ls.progress(sc.ID, step.ID, protocol.ProgressDemonstrated, now)
		saved := e.savedLocked()
		e.mu.Unlock()
		e.emit(evs...)
		e.report(rep)
		e.persist(saved)
		return out, true
	}

	// Complete: record, advance.
	ls := e.lesson(sc.ID)
	ls.Done[step.ID] = now.UnixMilli()
	last := idx+1 >= len(sc.Steps)
	evs = append(evs, &protocol.StepEvent{LessonID: sc.ID, StepID: step.ID, Status: protocol.StepComplete, Index: idx, Count: len(sc.Steps), LessonComplete: last})
	rep := ls.progress(sc.ID, step.ID, protocol.ProgressDone, now)
	e.advanceLocked(ctx, sc, idx, evs, rep)
	return out, true
}

// advanceLocked finishes step idx of sc (completed or skipped): it emits
// evs, reports rep, takes the next step's checkpoint and activates it
// (step loaded). After the last step no step is active. It is called with
// e.mu held and returns with it released; the result is the step now
// active (nil after the last step, or when the scenario changed meanwhile).
func (e *Engine) advanceLocked(ctx context.Context, sc *protocol.Scenario, idx int, evs []protocol.Event, rep protocol.ProgressReport) *protocol.Step {
	next := idx + 1
	last := next >= len(sc.Steps)
	var nextStep *protocol.Step
	if last {
		e.idx = next
		e.gen++
		e.saved.Active = nil
	} else {
		nextStep = &sc.Steps[next]
		e.gen++ // block concurrent evaluations of the old step
		e.idx = -1
	}
	saved := e.savedLocked()
	e.mu.Unlock()
	e.emit(evs...)
	e.report(rep)
	e.persist(saved)
	if last {
		return nil
	}
	if nextStep.TakesCheckpoint() && e.opt.Hooks.Checkpoint != nil {
		e.opt.Hooks.Checkpoint(ctx, sc.ID, nextStep.ID)
	}
	e.mu.Lock()
	if e.sc != sc || e.idx != -1 {
		e.mu.Unlock()
		return nil
	}
	loaded := e.startStepLocked(next, protocol.StepLoaded)
	reports := e.inProgressLocked()
	saved = e.savedLocked()
	e.mu.Unlock()
	e.emit(loaded...)
	e.report(reports...)
	e.persist(saved)
	e.Trigger()
	return nextStep
}

// stuckLocked evaluates the step's stuck rules (edge-triggered: a rule
// fires when it becomes true and re-arms when it becomes false).
func (e *Engine) stuckLocked(step *protocol.Step, now time.Time) []protocol.Event {
	var evs []protocol.Event
	for i := range step.Stuck {
		r := &step.Stuck[i]
		id := r.ID
		if id == "" {
			id = protocol.StuckRuleID(step.ID, i)
		}
		holds := e.ruleHoldsLocked(&r.When, now)
		if holds && !e.nudged[id] {
			e.nudged[id] = true
			body := r.Say.Get(e.opt.Locale)
			title := r.Title.Get(e.opt.Locale)
			if title == "" {
				title = firstSentence(body)
			}
			kind := r.Kind
			if kind == "" {
				kind = protocol.NudgeCalm
			}
			evs = append(evs, &protocol.NudgeEvent{ID: id, Kind: kind, Title: title, Body: body})
		} else if !holds {
			e.nudged[id] = false
		}
	}
	return evs
}

func (e *Engine) ruleHoldsLocked(w *protocol.StuckWhen, now time.Time) bool {
	set := false
	if w.MinibufferSeconds > 0 {
		set = true
		if e.miniSince.IsZero() || now.Sub(e.miniSince) < secs(w.MinibufferSeconds) {
			return false
		}
	}
	if w.IdleSeconds > 0 {
		set = true
		last := e.startedAt
		if e.lastKeyAt.After(last) {
			last = e.lastKeyAt
		}
		if now.Sub(last) < secs(w.IdleSeconds) {
			return false
		}
	}
	if w.StepSeconds > 0 {
		set = true
		if now.Sub(e.startedAt) < secs(w.StepSeconds) {
			return false
		}
	}
	if q := w.QuitsInWindow; q != nil && q.Count > 0 {
		set = true
		n := 0
		for _, t := range e.quits {
			if now.Sub(t) <= secs(q.Seconds) {
				n++
			}
		}
		if n < q.Count {
			return false
		}
	}
	if w.UnboundRepeats > 0 {
		set = true
		if e.unboundRun < w.UnboundRepeats {
			return false
		}
	}
	return set
}

func secs(n int) time.Duration { return time.Duration(n) * time.Second }

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if (r == '.' || r == '?' || r == '!') && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\n') {
			return s[:i+1]
		}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (e *Engine) emit(evs ...protocol.Event) {
	if e.opt.Hooks.Emit == nil {
		return
	}
	for _, ev := range evs {
		e.opt.Hooks.Emit(ev)
	}
}

func (e *Engine) report(rs ...protocol.ProgressReport) {
	if e.opt.Hooks.Report == nil {
		return
	}
	for _, r := range rs {
		e.opt.Hooks.Report(r)
	}
}

func (e *Engine) persist(s Saved) {
	if e.opt.Hooks.Persist != nil {
		e.opt.Hooks.Persist(s)
	}
}
