package steps

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"slices"
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

// Attempt names one activation of a step. The session core numbers
// attempts; Seq is unique per process, so an observation of an older
// attempt is recognised however the steps repeat.
type Attempt struct {
	Lesson, Step string
	Seq          uint64
	StartedAt    time.Time
}

// Stats are the key-stream statistics of an attempt (stuck rules).
type Stats struct {
	// StartedAt is the attempt's start; LastKeyAt the learner's last
	// command (kept across attempts); MiniSince when the open minibuffer
	// opened (zero when closed).
	StartedAt, LastKeyAt, MiniSince time.Time
	// Quits are the times of the attempt's quit commands (the last 64).
	Quits []time.Time
	// UnboundRun counts the same unbound key in a row.
	UnboundRun int
	// Commands counts agent key events since the attempt began.
	Commands int
}

// Observation is one evaluation of an attempt's step.
type Observation struct {
	Attempt  Attempt
	Results  []protocol.CondResult
	Complete bool
	// Forced is set for Check (rpc check, an op polling): its check event
	// goes out even when nothing changed.
	Forced bool
	Stats  Stats
}

// Options configure an Evaluator.
type Options struct {
	// Root is the workspace opened with os.OpenRoot (file conditions).
	Root *os.Root
	// Workspace is the absolute workspace path (shell conditions).
	Workspace string
	Checker   Checker
	Shell     ShellRunner
	Log       *slog.Logger
	// Now, Debounce, DebounceMax and Tick are overridable for tests.
	// DebounceMax defaults to DefaultDebounceMax with the default debounce,
	// else to three times Debounce.
	Now         func() time.Time
	Debounce    time.Duration
	DebounceMax time.Duration
	Tick        time.Duration
	// ShellWait overrides ShellSyncWait.
	ShellWait time.Duration
	// Observe receives every evaluation of the current attempt. It runs on
	// the evaluating goroutine and must not block or call Check.
	Observe func(Observation)
}

// Evaluator evaluates the conditions of one attempt's step and keeps its
// key-stream statistics. It knows nothing of progress, phases or what a
// completion means: it reports Observations. Methods are safe for
// concurrent use.
type Evaluator struct {
	opt Options
	log *slog.Logger

	mu           sync.Mutex
	attempt      Attempt
	step         *protocol.Step // nil: nothing to evaluate
	setupRoot    string
	counts       map[string]int
	lessonCounts map[string]int
	state        *protocol.State
	stateDoc     map[string]any
	lastKeyAt    time.Time
	miniSince    time.Time
	quits        []time.Time
	lastUnbound  string
	unboundRun   int
	commands     int

	shellCache shellCache
	patterns   patternCache
	trigger    chan struct{}
	// evalMu makes evaluations one at a time (Run and Check).
	evalMu sync.Mutex
}

// New returns an evaluator with nothing to evaluate.
func New(opt Options) *Evaluator {
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
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	return &Evaluator{
		opt: opt, log: opt.Log,
		counts: map[string]int{}, lessonCounts: map[string]int{},
		trigger: make(chan struct{}, 1),
	}
}

// Begin starts attempt a of step (nil: nothing to evaluate), whose shell
// conditions default to setupRoot. Step counts, quits, the unbound run and
// cached shell results start afresh; lesson counts start afresh when the
// lesson changes. The learner's last command time and the minibuffer stay.
func (e *Evaluator) Begin(a Attempt, step *protocol.Step, setupRoot string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if a.Lesson != e.attempt.Lesson {
		e.lessonCounts = map[string]int{}
	}
	e.attempt, e.step, e.setupRoot = a, step, setupRoot
	e.counts = map[string]int{}
	e.commands = 0
	e.quits = nil
	e.unboundRun = 0
	e.lastUnbound = ""
	e.shellCache.reset()
}

// Commands returns the number of agent key events since the attempt
// began ("did the learner do anything here?").
func (e *Evaluator) Commands() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.commands
}

// OnKey counts a command (agent key event).
func (e *Evaluator) OnKey(ev *protocol.KeyEvent) {
	now := e.opt.Now()
	e.mu.Lock()
	if ev.Cmd != "" {
		e.counts[ev.Cmd]++
		e.lessonCounts[ev.Cmd]++
	}
	e.commands++
	e.lastKeyAt = now
	// A prefix cancelled with C-g (C-x C-g) is reported as undefined; it is
	// a quit, not an unbound key.
	cancelled := ev.Cmd == protocol.UndefinedCommand && (ev.Keys == "C-g" || strings.HasSuffix(ev.Keys, " C-g"))
	if QuitCommands[ev.Cmd] || cancelled {
		e.quits = append(e.quits, now)
		if len(e.quits) > 64 {
			e.quits = e.quits[len(e.quits)-64:]
		}
	}
	if ev.Cmd == protocol.UndefinedCommand && !cancelled {
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
func (e *Evaluator) OnState(st *protocol.State) {
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
func (e *Evaluator) OnMinibuffer(ev *protocol.MinibufferEvent) {
	now := e.opt.Now()
	e.mu.Lock()
	e.trackMinibufferLocked(ev.Open, now)
	e.mu.Unlock()
	e.Trigger()
}

func (e *Evaluator) trackMinibufferLocked(open bool, now time.Time) {
	switch {
	case open && e.miniSince.IsZero():
		e.miniSince = now
	case !open:
		e.miniSince = time.Time{}
	}
}

// OnFile invalidates cached shell results and schedules an evaluation.
func (e *Evaluator) OnFile(*protocol.FileEvent) {
	e.shellCache.clear()
	e.Trigger()
}

// Invalidate drops cached shell results and schedules an evaluation (a
// nested repository's .git changed: git state is not a file event).
func (e *Evaluator) Invalidate() {
	e.shellCache.clear()
	e.Trigger()
}

// Trigger schedules a debounced evaluation.
func (e *Evaluator) Trigger() {
	select {
	case e.trigger <- struct{}{}:
	default:
	}
}

// Run evaluates on triggers and on every Tick until ctx ends. Triggers are
// debounced on the trailing edge: the evaluation runs Debounce after the
// last trigger of a burst, and at the latest DebounceMax after its first
// (backend.md §3.8).
func (e *Evaluator) Run(ctx context.Context) {
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

// Check evaluates now and returns the result (rpc check, ops waiting for a
// step's conditions). ok is false when there is nothing to evaluate.
func (e *Evaluator) Check(ctx context.Context) (protocol.CheckResult, bool) {
	return e.evaluate(ctx, true)
}

// conditionResults evaluates the step's conditions.
func (e *Evaluator) conditionResults(ec *evalContext, step *protocol.Step) ([]protocol.CondResult, bool) {
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
	return results, complete
}

// evaluate runs one pass over the attempt's conditions and hands the
// result to Observe, unless another attempt began meanwhile.
func (e *Evaluator) evaluate(ctx context.Context, force bool) (protocol.CheckResult, bool) {
	e.evalMu.Lock()
	defer e.evalMu.Unlock()

	e.mu.Lock()
	if e.step == nil {
		e.mu.Unlock()
		return protocol.CheckResult{}, false
	}
	a, step := e.attempt, e.step
	ec := &evalContext{
		ctx:          ctx,
		counts:       maps.Clone(e.counts),
		lessonCounts: maps.Clone(e.lessonCounts),
		state:        e.state,
		stateDoc:     e.stateDoc,
		root:         e.opt.Root,
		workspace:    e.opt.Workspace,
		setupRoot:    e.setupRoot,
		shell:        e.opt.Shell,
		shellCache:   &e.shellCache,
		patterns:     &e.patterns,
		trigger:      e.Trigger,
		shellWait:    e.opt.ShellWait,
		now:          e.opt.Now,
		keyAt:        e.lastKeyAt,
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

	results, complete := e.conditionResults(ec, step)
	if complete && ec.staleShell {
		// A shell result predates the last command: confirm with fresh runs
		// before the step completes (a save is seen before its file event).
		ec.notBefore, ec.staleShell = ec.keyAt, false
		results, complete = e.conditionResults(ec, step)
	}
	out := protocol.CheckResult{LessonID: a.Lesson, StepID: step.ID, Conditions: results, Complete: complete}

	e.mu.Lock()
	if e.attempt.Seq != a.Seq {
		// Another attempt began while we evaluated: nobody wants this.
		e.mu.Unlock()
		return out, true
	}
	obs := Observation{Attempt: a, Results: results, Complete: complete, Forced: force, Stats: Stats{
		StartedAt: a.StartedAt, LastKeyAt: e.lastKeyAt, MiniSince: e.miniSince,
		Quits: slices.Clone(e.quits), UnboundRun: e.unboundRun, Commands: e.commands,
	}}
	e.mu.Unlock()
	if e.opt.Observe != nil {
		e.opt.Observe(obs)
	}
	return out, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
