package steps

import (
	"context"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Timing (backend.md §3.8).
const (
	DefaultDebounce = 150 * time.Millisecond
	DefaultTick     = 2 * time.Second
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
	Done   map[string]int64 `json:"done,omitempty"`
	Hints  map[string]int   `json:"hints,omitempty"`
	Resets map[string]int   `json:"resets,omitempty"`
}

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
	// Now, Debounce and Tick are overridable for tests.
	Now      func() time.Time
	Debounce time.Duration
	Tick     time.Duration
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
		c := &LessonState{Done: map[string]int64{}, Hints: map[string]int{}, Resets: map[string]int{}}
		for k, v := range ls.Done {
			c.Done[k] = v
		}
		for k, v := range ls.Hints {
			c.Hints[k] = v
		}
		for k, v := range ls.Resets {
			c.Resets[k] = v
		}
		out.Lessons[id] = c
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
	if ls.Hints == nil {
		ls.Hints = map[string]int{}
	}
	if ls.Resets == nil {
		ls.Resets = map[string]int{}
	}
	return ls
}

// FirstIncomplete returns the index of the first step of sc not yet done
// according to saved progress, or 0 when every step is done.
func (e *Engine) FirstIncomplete(sc *protocol.Scenario) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	ls := e.saved.Lessons[sc.ID]
	for i := range sc.Steps {
		if ls == nil || ls.Done[sc.Steps[i].ID] == 0 {
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
	if e.sc == nil || e.idx < 0 || e.idx >= len(e.sc.Steps) {
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

// startStepLocked resets per-step state and returns the step event.
func (e *Engine) startStepLocked(idx int, status protocol.StepStatus) []protocol.Event {
	e.idx = idx
	e.gen++
	e.startedAt = e.opt.Now()
	e.counts = map[string]int{}
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
	if e.reported[key] || (ls != nil && ls.Done[a.StepID] != 0) {
		return nil
	}
	e.reported[key] = true
	return []protocol.ProgressReport{{LessonID: a.LessonID, StepID: a.StepID, Status: protocol.ProgressInProgress, At: e.opt.Now().UnixMilli()}}
}

// Drop forgets the active step when it belongs to lessonID, and the lesson's
// saved progress (the gateway reset it).
func (e *Engine) Drop(lessonID string) bool {
	e.mu.Lock()
	dropped := false
	if e.sc != nil && e.sc.ID == lessonID {
		e.sc = nil
		e.idx = -1
		e.gen++
		e.saved.Active = nil
		dropped = true
	}
	delete(e.saved.Lessons, lessonID)
	for k := range e.reported {
		if strings.HasPrefix(k, lessonID+"\x00") {
			delete(e.reported, k)
		}
	}
	saved := e.savedLocked()
	e.mu.Unlock()
	e.persist(saved)
	return dropped
}

// ResetTo restarts step stepID of the active lesson after its checkpoint was
// restored: counters clear, the reset count increments and is reported, and
// step {status: reset} is emitted. It returns false when the step is not
// part of the active lesson.
func (e *Engine) ResetTo(lessonID, stepID string) bool {
	e.mu.Lock()
	if e.sc == nil || e.sc.ID != lessonID {
		e.mu.Unlock()
		return false
	}
	idx := e.sc.StepIndex(stepID)
	if idx < 0 {
		e.mu.Unlock()
		return false
	}
	ls := e.lesson(lessonID)
	ls.Resets[stepID]++
	delete(ls.Done, stepID)
	evs := e.startStepLocked(idx, protocol.StepReset)
	resets, hints := ls.Resets[stepID], ls.Hints[stepID]
	rep := protocol.ProgressReport{LessonID: lessonID, StepID: stepID, Status: protocol.ProgressInProgress, Resets: &resets, At: e.opt.Now().UnixMilli()}
	if hints > 0 {
		rep.HintsUsed = &hints
	}
	saved := e.savedLocked()
	e.mu.Unlock()
	e.emit(evs...)
	e.report(rep)
	e.persist(saved)
	e.Trigger()
	return true
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
	status := protocol.ProgressInProgress
	if ls.Done[stepID] != 0 {
		status = protocol.ProgressDone
	}
	hints := level
	rep := protocol.ProgressReport{LessonID: lessonID, StepID: stepID, Status: status, HintsUsed: &hints, At: e.opt.Now().UnixMilli()}
	if r := ls.Resets[stepID]; r > 0 {
		rep.Resets = &r
	}
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

// Run evaluates on triggers (at most once per Debounce) and on every Tick
// until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	tick := time.NewTicker(e.opt.Tick)
	defer tick.Stop()
	var timer *time.Timer
	var timerC <-chan time.Time
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
			if timerC == nil {
				if timer == nil {
					timer = time.NewTimer(e.opt.Debounce)
				} else {
					timer.Reset(e.opt.Debounce)
				}
				timerC = timer.C
			}
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
		counts:       copyCounts(e.counts),
		lessonCounts: copyCounts(e.lessonCounts),
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
		if r.t != protocol.True {
			cr.Detail = truncate(r.detail, MaxShellOutput)
		}
		results[i] = cr
		if r.t != protocol.True {
			complete = false
		}
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
	if !complete {
		evs = append(evs, e.stuckLocked(step, now)...)
		e.mu.Unlock()
		e.emit(evs...)
		return out, true
	}

	// Complete: record, advance.
	ls := e.lesson(sc.ID)
	ls.Done[step.ID] = now.UnixMilli()
	next := idx + 1
	last := next >= len(sc.Steps)
	evs = append(evs, &protocol.StepEvent{LessonID: sc.ID, StepID: step.ID, Status: protocol.StepComplete, Index: idx, Count: len(sc.Steps), LessonComplete: last})
	rep := protocol.ProgressReport{LessonID: sc.ID, StepID: step.ID, Status: protocol.ProgressDone, At: now.UnixMilli()}
	if h := ls.Hints[step.ID]; h > 0 {
		rep.HintsUsed = &h
	}
	if r := ls.Resets[step.ID]; r > 0 {
		rep.Resets = &r
	}
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
		return out, true
	}
	if nextStep.TakesCheckpoint() && e.opt.Hooks.Checkpoint != nil {
		e.opt.Hooks.Checkpoint(ctx, sc.ID, nextStep.ID)
	}
	e.mu.Lock()
	if e.sc != sc {
		e.mu.Unlock()
		return out, true
	}
	loaded := e.startStepLocked(next, protocol.StepLoaded)
	reports := e.inProgressLocked()
	saved = e.savedLocked()
	e.mu.Unlock()
	e.emit(loaded...)
	e.report(reports...)
	e.persist(saved)
	e.Trigger()
	return out, true
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

func copyCounts(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
