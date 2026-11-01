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
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
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
	Engine    *steps.Engine
	// DropProgress applies a gateway reset to saved progress and queued
	// reports in one step, serialised with generation syncs.
	DropProgress func(lessonID string, generation int64)
	// Git runs git for setup.repos (nil: the git binary); Shell runs
	// setup.run.
	Git   checkpoint.Runner
	Shell steps.ShellRunner
	Emit  func(protocol.Event)
	Log   *slog.Logger
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
	// OpRestoreIfDirty puts the active step back to its checkpoint when
	// the learner ran a command in it (demo start and skip).
	// TODO(sessiond, backend.md §3.12): folded into the demo-start and
	// skip ops.
	OpRestoreIfDirty OpKind = "restore-if-dirty"
)

// Deadlines bound each op from the moment it starts running.
const (
	LoadDeadline    = 3 * time.Minute
	ResumeDeadline  = time.Minute
	RestoreDeadline = 2 * time.Minute
	ModulesDeadline = 3 * time.Minute
	DropDeadline    = 10 * time.Second
)

// RestartTimeout bounds a daemon restart for a module change.
const RestartTimeout = 3 * time.Minute

// Op is one queued lesson operation.
type Op struct {
	Kind     OpKind
	Req      any
	Deadline time.Duration
	// Preempt (drop) runs the op next; Cancels (a drop that applies) also
	// cancels the running op when it concerns the same lesson scope.
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

// Core owns lesson orchestration: one worker runs the ops one at a time,
// each to completion and detached from the request that asked for it.
// Between phases an op checks its context, which its deadline or a
// preempting drop cancels. Ops never hold mu across I/O.
type Core struct {
	d   Deps
	log *slog.Logger

	// runCtx is Run's context, read only by ops (which run in Run's
	// goroutine): phases that must not stop halfway end only with it.
	runCtx context.Context

	mu      sync.Mutex
	queue   []*Op
	running *Op
	wake    chan struct{}
}

// New returns a core; Run executes its ops.
func New(d Deps) *Core {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.SetupTimeout <= 0 {
		d.SetupTimeout = 10 * time.Second
	}
	return &Core{d: d, log: d.Log, wake: make(chan struct{}, 1)}
}

func (c *Core) emit(ev protocol.Event) {
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
			c.queue = nil
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
	case OpRestoreIfDirty:
		return c.restartIfDirty(ctx, op.Req.(protocol.StepRef)), nil
	case OpSetModules:
		restarting, err := c.setModules(ctx, op)
		return restarting, err
	case OpDrop:
		req := op.Req.(protocol.DropStepRequest)
		c.drop(req.LessonID, req.Generation)
		return nil, nil
	}
	return nil, errors.New("unknown op " + string(op.Kind))
}

// Submit queues op. A preempting op goes to the front; a cancelling op
// cancels the running op when that one concerns the same lesson (or the
// cancelling op concerns every lesson).
func (c *Core) Submit(op *Op) {
	op.early = make(chan any, 1)
	op.done = make(chan Result, 1)
	op.info.Kind = op.Kind
	c.mu.Lock()
	if r := c.running; op.Cancels && r != nil && lessonScoped(r.Kind) && (op.info.LessonID == "" || r.info.LessonID == op.info.LessonID) {
		r.cancel()
	}
	if op.Preempt {
		c.queue = append([]*Op{op}, c.queue...)
	} else {
		c.queue = append(c.queue, op)
	}
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// lessonScoped reports whether ops of kind work on a lesson's step, which a
// drop of that lesson makes moot.
func lessonScoped(k OpKind) bool { return k != OpSetModules && k != OpDrop }

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

// RestoreIfDirty puts the active step act back to its checkpoint, without
// counting a reset, when the learner ran a command in it since it started
// or was last reset (demo start and skip, api.md §3.4). It reports whether
// it restored.
func (c *Core) RestoreIfDirty(ctx context.Context, act protocol.StepRef) (bool, error) {
	v, err := c.do(ctx, &Op{Kind: OpRestoreIfDirty, Req: act, Deadline: RestoreDeadline, info: OpInfo{LessonID: act.LessonID, StepID: act.StepID}})
	if err != nil {
		return false, err
	}
	return v.(bool), nil
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
// next and, when its generation is newer than the one applied (the
// predicate DropGeneration applies), cancels a running op on that lesson;
// a stale drop changes nothing and cancels nothing.
func (c *Core) Drop(ctx context.Context, lessonID string, generation int64) error {
	if lessonID != "" && !validLessonID(lessonID) {
		return httpx.BadRequest("lessonId is not a valid lesson id")
	}
	_, err := c.do(ctx, &Op{Kind: OpDrop, Req: protocol.DropStepRequest{LessonID: lessonID, Generation: generation},
		Deadline: DropDeadline, Preempt: true, Cancels: c.d.Engine.GenerationNewer(lessonID, generation),
		info: OpInfo{LessonID: lessonID}})
	return err
}
