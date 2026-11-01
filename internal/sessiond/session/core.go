package session

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/progress"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/snapshot"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// Agent is the agent connection the ops call.
type Agent interface {
	Call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error
	Connected() bool
}

// Supervisor is the part of the process supervisor the ops use.
type Supervisor interface {
	// Ready reports whether keys can be sent, with the reason when not.
	Ready() (bool, string)
	// WaitReady waits until no restart is in progress and the terminal
	// frame is open.
	WaitReady(ctx context.Context, timeout time.Duration) bool
	// RestartEmacs restarts the daemon with buffer restore.
	RestartEmacs(ctx context.Context, reason string) error
	// SendKeys types keys or text into a frame.
	SendKeys(ctx context.Context, frame protocol.FrameKind, keys, text string) error
}

// Deps are the components the ops drive.
type Deps struct {
	// Workspace is the absolute workspace path; Root is it opened with
	// os.OpenRoot (fixture extraction).
	Workspace string
	Root      *os.Root
	// CacheDir holds the ETag caches of scenarios and fixtures.
	CacheDir  string
	Fetch     scenario.Fetcher
	Agent     Agent
	Sup       Supervisor
	Snapshots Snapshots
	Modules   *scenario.Modules
	// Evaluator evaluates the current attempt; its Observe hook must call
	// Core.Observe.
	Evaluator *steps.Evaluator
	// Progress is the progress outbox (default: one without a sender).
	// The core calls it under Core.mu; it must not block.
	Progress Progress
	// Generations fetches the gateway's reset generations (the sync op);
	// nil without a gateway.
	Generations func(context.Context) (protocol.ProgressGenerations, error)
	// Persist writes state.json from Persistent; the core calls it without
	// Core.mu after every change.
	Persist func()
	// Git runs git for setup.repos (nil: the git binary); Shell runs
	// setup.run.
	Git   checkpoint.Runner
	Shell steps.ShellRunner
	// Emit publishes an event; it must not block (it runs under Core.mu).
	Emit func(protocol.Event)
	Log  *slog.Logger
	// Now is the clock of attempts, outcomes, laps and demos (default
	// time.Now); Locale selects nudge text (default "en").
	Now    func() time.Time
	Locale string
	// SetupTimeout bounds the agent's setup op (default 10 s).
	SetupTimeout time.Duration
}

// OpKind names a lesson operation.
type OpKind string

// Operation kinds.
const (
	OpLoad       OpKind = "load"
	OpResume     OpKind = "resume"
	OpRestore    OpKind = "restore"
	OpSetModules OpKind = "set-modules"
	OpDrop       OpKind = "drop"
	OpDemoStart  OpKind = "demo-start"
	OpSkip       OpKind = "skip"
	// OpAdvance follows a completion: the next step's checkpoint and
	// activation.
	OpAdvance OpKind = "advance"
	// OpCheckpoint takes a learner's labelled checkpoint (rpc checkpoint).
	OpCheckpoint OpKind = "checkpoint"
	// OpSync applies the gateway's generations (Sync).
	OpSync OpKind = "sync"
	// OpRestartEmacs restarts the daemon (rpc restart-emacs).
	OpRestartEmacs OpKind = "restart-emacs"
	// OpAutosave writes snapshot.json while nothing else is queued.
	OpAutosave OpKind = "autosave"
	// OpReconnect gives an agent that said hello the editor state of a
	// restore made while it was away; OpRecover reopens the editor state
	// in a daemon restarted after a crash.
	OpReconnect OpKind = "reconnect"
	OpRecover   OpKind = "recover"
)

// Deadlines bound each op from the moment it starts running.
const (
	LoadDeadline       = 3 * time.Minute
	ResumeDeadline     = time.Minute
	RestoreDeadline    = 2 * time.Minute
	ModulesDeadline    = 3 * time.Minute
	DropDeadline       = 10 * time.Second
	DemoDeadline       = 30 * time.Second
	SkipDeadline       = 60 * time.Second
	AdvanceDeadline    = time.Minute
	CheckpointDeadline = 30 * time.Second
	EditorDeadline     = 30 * time.Second
)

// RestartTimeout bounds a daemon restart for a module change.
const RestartTimeout = 3 * time.Minute

// Op is one queued lesson operation.
type Op struct {
	Kind     OpKind
	Req      any
	Deadline time.Duration
	// Preempt (drop, advance) runs the op next; Cancels (a drop that
	// applies) also cancels the running op when it concerns the same
	// lesson scope.
	Preempt bool
	Cancels bool

	// early carries a result posted before the op ends (a load's
	// {restarting: true}); done carries the final one. Both are buffered:
	// the worker never waits for a reader.
	early chan any
	done  chan Result
	// ctx and cancel exist while the op runs (under Core.mu).
	ctx    context.Context
	cancel context.CancelFunc
	info   OpInfo
}

// Result is an op's outcome.
type Result struct {
	Value any
	Err   error
}

// OpInfo describes the op in progress (Busy).
type OpInfo struct {
	Kind      OpKind
	LessonID  string
	StepID    string
	StartedAt time.Time
}

// Core owns the lesson state and its orchestration: one worker runs the
// ops one at a time, each to completion and detached from the request that
// asked for it. Between phases an op checks its context, which its
// deadline or a preempting drop cancels. Ops never hold mu across I/O;
// events are emitted under mu, so they keep the order of the state
// changes.
type Core struct {
	d   Deps
	log *slog.Logger

	// runCtx is Run's context, read only by ops (which run in Run's
	// goroutine): phases that must not stop halfway end only with it.
	runCtx context.Context

	mu      sync.Mutex
	queue   []*Op
	running *Op
	// stopped is set once Run returned: later ops fail at once.
	stopped bool
	wake    chan struct{}
	lesson  Lesson
	// pending is the editor state of a restore made while the agent was
	// away (deferEditor). Only ops (the worker) touch it.
	pending *pendingEditor
	// obs is what the current attempt's observations left (Observe).
	obs observed
	// dirty marks the active step dirty after a load that failed changed
	// the workspace (until the next activation).
	dirty bool
	// last is the lesson snapshot published last (publishLocked).
	last protocol.LessonSnapshot
}

// New returns a core; Run executes its ops.
func New(d Deps) *Core {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.SetupTimeout <= 0 {
		d.SetupTimeout = 10 * time.Second
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Locale == "" {
		d.Locale = protocol.DefaultLocale
	}
	if d.Progress == nil {
		d.Progress = progress.New(progress.Snapshot{}, nil, nil, d.Log)
	}
	c := &Core{d: d, log: d.Log, wake: make(chan struct{}, 1),
		lesson: Lesson{Index: -1, Saved: Saved{Lessons: map[string]*LessonState{}}}}
	c.last = c.snapshotLocked()
	return c
}

func (c *Core) now() time.Time { return c.d.Now() }

// emit publishes an event (ops, outside mu).
func (c *Core) emit(ev protocol.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.emitLocked(ev)
}

func (c *Core) emitLocked(ev protocol.Event) {
	if c.d.Emit != nil {
		c.d.Emit(ev)
	}
}

// errStopped answers ops still queued when the core stops.
var errStopped = httpx.SessionUnavailable("the session is stopping")

// Run executes ops in order until ctx ends; the running op's context
// derives from ctx, so stopping cancels it, and queued ops fail.
func (c *Core) Run(ctx context.Context) {
	c.runCtx = ctx
	for {
		op := c.next(ctx)
		if op == nil {
			c.mu.Lock()
			queued := c.queue
			c.queue, c.stopped = nil, true
			c.mu.Unlock()
			for _, op := range queued {
				op.done <- Result{Err: errStopped}
			}
			return
		}
		v, err := c.exec(op.ctx, op)
		op.cancel()
		c.mu.Lock()
		c.running = nil
		c.publishLocked(protocol.ChangeBusy, "", 0)
		c.mu.Unlock()
		op.done <- Result{Value: v, Err: err}
	}
}

// next waits for the next op and makes it the running one, nil once ctx
// ended.
func (c *Core) next(ctx context.Context) *Op {
	for {
		c.mu.Lock()
		if ctx.Err() != nil {
			c.mu.Unlock()
			return nil
		}
		if len(c.queue) > 0 {
			op := c.queue[0]
			c.queue = c.queue[1:]
			op.ctx, op.cancel = context.WithTimeout(ctx, op.Deadline)
			op.info.StartedAt = time.Now()
			c.running = op
			c.publishLocked(protocol.ChangeBusy, "", 0)
			c.mu.Unlock()
			return op
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-c.wake:
		}
	}
}

func (c *Core) exec(ctx context.Context, op *Op) (any, error) {
	switch op.Kind {
	case OpLoad:
		return c.load(ctx, op)
	case OpResume:
		return nil, c.resume(ctx, op.Req.(protocol.StepRef))
	case OpRestore:
		return c.restore(ctx, op)
	case OpDemoStart:
		return c.demoStart(ctx, op.Req.(admitted))
	case OpSkip:
		return c.skip(ctx, op.Req.(admitted))
	case OpAdvance:
		_, err := c.advance(ctx, op.Req.(uint64))
		return nil, err
	case OpSetModules:
		restarting, err := c.setModules(ctx, op)
		return restarting, err
	case OpDrop:
		req := op.Req.(protocol.DropStepRequest)
		c.drop(req.LessonID, req.Generation)
		return nil, nil
	case OpCheckpoint:
		label := op.Req.(string)
		return c.d.Snapshots.Capture(ctx, checkpoint.ManualRef(label), label, nil)
	case OpSync:
		return nil, c.sync(ctx, op.Req.(bool))
	case OpRestartEmacs:
		c.restart("requested")
		return nil, nil
	case OpAutosave:
		if _, err := c.d.Snapshots.SaveEditorState(ctx); err != nil {
			c.log.Debug("autosave of snapshot.json", "err", err)
		}
		return nil, nil
	case OpReconnect:
		c.reconnect(ctx)
		return nil, nil
	case OpRecover:
		c.reopenEditor(ctx)
		return nil, nil
	}
	return nil, errors.New("unknown op " + string(op.Kind))
}

// Submit queues op. A preempting op goes to the front; a cancelling op
// cancels the running op when that one concerns the same lesson (or the
// cancelling op concerns every lesson).
func (c *Core) Submit(op *Op) {
	c.mu.Lock()
	c.submitLocked(op)
	c.mu.Unlock()
}

func (c *Core) submitLocked(op *Op) {
	op.early = make(chan any, 1)
	op.done = make(chan Result, 1)
	op.info.Kind = op.Kind
	if c.stopped {
		op.done <- Result{Err: errStopped}
		return
	}
	if r := c.running; op.Cancels && r != nil && lessonScoped(r.Kind) && (op.info.LessonID == "" || r.info.LessonID == op.info.LessonID) {
		r.cancel()
	}
	if op.Preempt {
		c.queue = append([]*Op{op}, c.queue...)
	} else {
		c.queue = append(c.queue, op)
	}
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// lessonScoped reports whether ops of kind work on a lesson's step, which a
// drop of that lesson makes moot.
func lessonScoped(k OpKind) bool {
	switch k {
	case OpLoad, OpResume, OpRestore, OpDemoStart, OpSkip, OpAdvance:
		return true
	}
	return false
}

// do submits op and waits, while ctx lasts, for its early or final result.
// A caller that goes away changes nothing: the op runs on.
func (c *Core) do(ctx context.Context, op *Op) (any, error) {
	c.Submit(op)
	select {
	case v := <-op.early:
		return v, nil
	case r := <-op.done:
		return r.Value, r.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// post sends op's early result; the request waiting for it returns while
// the op goes on.
func (op *Op) post(v any) {
	select {
	case op.early <- v:
	default:
	}
}

// setTarget records the lesson and step the running op works on (Busy).
func (c *Core) setTarget(op *Op, lessonID, stepID string) {
	c.mu.Lock()
	op.info.LessonID, op.info.StepID = lessonID, stepID
	c.publishLocked(protocol.ChangeBusy, "", 0)
	c.mu.Unlock()
}

// Busy returns the op in progress, or nil.
func (c *Core) Busy() *OpInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil {
		return nil
	}
	info := c.running.info
	return &info
}

// interrupted is the error of an op whose context ended between phases.
func interrupted(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return httpx.Timeout("the operation did not finish in time")
	}
	return httpx.Conflict("the operation was cancelled by a progress reset")
}

// --- public operations ----------------------------------------------------

// Load implements rpc scenario. It returns the op's early result
// ({restarting: true} once a module restart begins) or its final one,
// whichever comes first.
func (c *Core) Load(ctx context.Context, req protocol.ScenarioRequest) (protocol.ScenarioResult, error) {
	if !validLessonID(req.LessonID) {
		return protocol.ScenarioResult{}, httpx.BadRequest("lessonId is not a valid lesson id")
	}
	op := &Op{Kind: OpLoad, Req: req, Deadline: LoadDeadline, info: OpInfo{LessonID: req.LessonID, StepID: req.StepID}}
	v, err := c.do(ctx, op)
	if err != nil {
		return protocol.ScenarioResult{}, err
	}
	return v.(protocol.ScenarioResult), nil
}

// Resume re-activates the step saved in state.json after sessiond, and with
// it Emacs, restarted (startup).
func (c *Core) Resume(ctx context.Context, ref protocol.StepRef) error {
	if !validLessonID(ref.LessonID) {
		return errors.New("invalid saved lesson id " + ref.LessonID)
	}
	_, err := c.do(ctx, &Op{Kind: OpResume, Req: ref, Deadline: ResumeDeadline, info: OpInfo{LessonID: ref.LessonID, StepID: ref.StepID}})
	return err
}

// Restore implements rpc restore.
func (c *Core) Restore(ctx context.Context, req protocol.RestoreRequest) (protocol.RestoreResult, error) {
	v, err := c.do(ctx, &Op{Kind: OpRestore, Req: req, Deadline: RestoreDeadline})
	if err != nil {
		return protocol.RestoreResult{}, err
	}
	return v.(protocol.RestoreResult), nil
}

// DemoStart implements rpc demo start (api.md §4.1): the active step,
// restored first when the learner ran a command in it, gets a "Watch it"
// latch. The op runs only while the attempt it was admitted for is
// current.
func (c *Core) DemoStart(ctx context.Context, stepID string) (protocol.DemoResult, error) {
	req, err := c.admit(stepID)
	if err != nil {
		return protocol.DemoResult{}, err
	}
	v, err := c.do(ctx, &Op{Kind: OpDemoStart, Req: req, Deadline: DemoDeadline, info: OpInfo{LessonID: req.attempt.Lesson, StepID: stepID}})
	if err != nil {
		return protocol.DemoResult{}, err
	}
	return v.(protocol.DemoResult), nil
}

// Skip implements rpc skip (api.md §3.4); like DemoStart it runs only for
// the attempt it was admitted for.
func (c *Core) Skip(ctx context.Context, stepID string) (protocol.SkipResult, error) {
	req, err := c.admit(stepID)
	if err != nil {
		return protocol.SkipResult{}, err
	}
	v, err := c.do(ctx, &Op{Kind: OpSkip, Req: req, Deadline: SkipDeadline, info: OpInfo{LessonID: req.attempt.Lesson, StepID: stepID}})
	if err != nil {
		return protocol.SkipResult{}, err
	}
	return v.(protocol.SkipResult), nil
}

// Checkpoint implements rpc checkpoint: a checkpoint of the workspace and
// the editor labelled by the learner (refs/playground/manual/<slug>). It is
// an op so that it never interleaves with a restore.
func (c *Core) Checkpoint(ctx context.Context, label string) (snapshot.Manifest, error) {
	v, err := c.do(ctx, &Op{Kind: OpCheckpoint, Req: label, Deadline: CheckpointDeadline})
	if err != nil {
		return snapshot.Manifest{}, err
	}
	return v.(snapshot.Manifest), nil
}

// RestartEmacs implements rpc restart-emacs: the restart is queued as an
// op and its progress arrives as emacs events. Every daemon restart sessiond
// asks for runs in an op, so the supervisor's restart hooks always run in
// the worker (AfterRestart).
func (c *Core) RestartEmacs() {
	c.Submit(&Op{Kind: OpRestartEmacs, Deadline: RestartTimeout})
}

// Autosave queues an autosave of snapshot.json when no op runs or waits:
// a capture never interleaves with an op that puts another editor state
// in place, and an op asked for meanwhile waits for it (2 s at most).
func (c *Core) Autosave() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil && len(c.queue) == 0 {
		c.submitLocked(&Op{Kind: OpAutosave, Deadline: EditorDeadline})
	}
}

// SetModules implements rpc modules: it replaces the set exactly and
// answers {restarting} as soon as the daemon restart begins.
func (c *Core) SetModules(ctx context.Context, mods []string) (bool, error) {
	if err := validModules(mods); err != nil {
		return false, err
	}
	v, err := c.do(ctx, &Op{Kind: OpSetModules, Req: mods, Deadline: ModulesDeadline})
	if err != nil {
		return false, err
	}
	return v.(bool), nil
}

// Drop implements rpc drop-step: it forgets a lesson's (with "" every
// lesson's) saved progress and active step for a gateway reset. It runs
// next and, when its generation is newer than the one applied
// (Progress.Newer), cancels a running op on that lesson; a stale drop
// changes nothing and cancels nothing.
func (c *Core) Drop(ctx context.Context, lessonID string, generation int64) error {
	if lessonID != "" && !validLessonID(lessonID) {
		return httpx.BadRequest("lessonId is not a valid lesson id")
	}
	_, err := c.do(ctx, &Op{Kind: OpDrop, Req: protocol.DropStepRequest{LessonID: lessonID, Generation: generation},
		Deadline: DropDeadline, Preempt: true, Cancels: c.d.Progress.Newer(lessonID, generation),
		info: OpInfo{LessonID: lessonID}})
	return err
}
