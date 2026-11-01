package session

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
)

// FastForwardTimeout bounds the replay of earlier steps' demos when "Go to
// this step" targets a step without a checkpoint.
const FastForwardTimeout = 3 * time.Minute

// RehearsalSettle is how long the fast-forward waits for a replayed step's
// conditions after its demo.
const RehearsalSettle = 10 * time.Second

// activationReserve is the part of a load's deadline the fast-forward
// leaves for the checkpoint and the activation.
const activationReserve = 20 * time.Second

func validLessonID(id string) bool { return gwclient.ValidLessonID(id) }

// load is rpc scenario (backend.md §3.12). Phases, ctx checked between
// them: short-circuit when the lesson and step are already active →
// scenario → fixtures, setup.repos, setup.run → module set (a change
// restarts the daemon after posting {restarting: true}) → agent setup →
// the step's starting state (its checkpoint, else a fast-forward) → the
// step's checkpoint → activation.
func (c *Core) load(ctx context.Context, op *Op) (any, error) {
	req := op.Req.(protocol.ScenarioRequest)
	if res, ok := c.alreadyActive(req); ok {
		return res, nil
	}
	sc, err := scenario.FetchScenario(ctx, c.d.Fetch, c.d.CacheDir, req.LessonID, c.log)
	if err != nil {
		if httpx.AsError(err).Code == protocol.ErrSessionUnavailable {
			c.emit(&protocol.ErrorEvent{Code: protocol.ErrSessionUnavailable, Message: "Could not load the lesson. Try again in a moment."})
		}
		return nil, err
	}
	if len(sc.Steps) == 0 {
		return nil, httpx.Conflict("the lesson has no steps")
	}
	idx := c.firstIncomplete(sc)
	if req.StepID != "" {
		idx = sc.StepIndex(req.StepID)
		if idx < 0 {
			return nil, httpx.BadRequest(fmt.Sprintf("lesson %s has no step %q", sc.ID, req.StepID))
		}
	}
	lap := lapEnd
	if req.Lap {
		// A lap is the whole lesson again from its initial state.
		if idx != 0 && req.StepID != "" {
			return nil, httpx.BadRequest("a lap starts at the first step")
		}
		idx, lap = 0, lapStart
	}
	c.setTarget(op, sc.ID, sc.Steps[idx].ID)
	if ctx.Err() != nil {
		return nil, interrupted(ctx)
	}
	reset := req.Reset || sc.Setup.Reset || req.Lap

	extracted, err := scenario.FetchFixtures(ctx, c.d.Fetch, c.d.CacheDir, c.d.Root, sc, reset, c.log)
	if err != nil {
		c.emit(&protocol.ErrorEvent{Code: protocol.ErrSessionUnavailable, Message: "Could not load the lesson files. Try again in a moment."})
		return nil, err
	}
	if err := scenario.PrepareRepos(ctx, c.d.Git, c.d.Workspace, sc, reset); err != nil {
		c.log.Warn("setup.repos", "lesson", sc.ID, "err", err)
		c.emit(&protocol.ErrorEvent{Code: protocol.ErrInternal, Message: "Could not prepare the lesson's git repository."})
	}
	if err := scenario.RunSetup(ctx, c.d.Shell, c.d.Workspace, sc); err != nil {
		c.log.Warn("setup.run", "lesson", sc.ID, "err", err)
		c.emit(&protocol.ErrorEvent{Code: protocol.ErrInternal, Message: "The lesson's setup script failed; some files may be missing."})
	}
	if ctx.Err() != nil {
		return nil, interrupted(ctx)
	}

	cur := c.d.Modules.Current()
	want := scenario.Union(cur, sc.Modules)
	res := protocol.ScenarioResult{
		Step:  protocol.StepRef{LessonID: sc.ID, StepID: sc.Steps[idx].ID},
		Index: idx, Count: len(sc.Steps), Modules: want,
	}
	if lap == lapStart {
		res.Lap = &protocol.LapInfo{LessonID: sc.ID}
	}
	restarted := !slices.Equal(cur, want)
	if restarted {
		// Once modules.el is written the restart must happen, so this is
		// the last point a cancelled load stops before it.
		if ctx.Err() != nil {
			return nil, interrupted(ctx)
		}
		if err := c.d.Modules.Write(want); err != nil {
			return nil, httpx.Internal("could not write modules.el").Wrap(err)
		}
		// The restart is announced as over once the step is active: the
		// browser holds the learner's keys until then, so none land in the
		// frame before the lesson's setup has run.
		c.emit(&protocol.ModulesEvent{Enabled: want, Restarting: true})
		defer c.emit(&protocol.ModulesEvent{Enabled: want, Restarting: false})
		early := res
		early.Restarting = true
		op.post(early)
		c.restart("module set changed")
		if ctx.Err() != nil {
			return nil, interrupted(ctx)
		}
	}
	prev := c.lessonState()
	if err := c.finish(ctx, sc, idx, reset, reset && extracted > 0, lap); err != nil {
		// A fast-forward cut short leaves the step active before it, not
		// a rehearsal.
		c.endRehearsal(prev)
		if restarted && ctx.Err() == nil {
			// The request has its early result; tell the page.
			c.emit(&protocol.ErrorEvent{Code: protocol.ErrInternal, Message: "Could not restore the step. Try again."})
		}
		return nil, err
	}
	if res.Lap != nil {
		res.Lap = c.Lap()
	}
	return res, nil
}

// restart restarts the daemon within RestartTimeout. Neither the op's
// deadline nor a drop cuts it short, only the core stopping: modules.el is
// already written, and a restart cut short would skip saving the modified
// buffers before the daemon stops (their unsaved edits would be lost) or
// leave the daemon on the old module set.
func (c *Core) restart(reason string) {
	rctx, cancel := context.WithTimeout(c.runCtx, RestartTimeout)
	defer cancel()
	if err := c.d.Sup.RestartEmacs(rctx, reason); err != nil {
		c.log.Error("restart for modules failed", "err", err)
	}
}

// alreadyActive answers a reload of the active lesson without re-running
// setup.
func (c *Core) alreadyActive(req protocol.ScenarioRequest) (protocol.ScenarioResult, bool) {
	if req.Reset || req.Lap {
		return protocol.ScenarioResult{}, false
	}
	sc := c.Scenario()
	act := c.Active()
	if sc == nil || act == nil || sc.ID != req.LessonID {
		return protocol.ScenarioResult{}, false
	}
	if req.StepID != "" && req.StepID != act.StepID {
		return protocol.ScenarioResult{}, false
	}
	if req.StepID != "" && c.Lap() != nil {
		return protocol.ScenarioResult{}, false
	}
	idx, n := c.ActiveIndex()
	return protocol.ScenarioResult{Step: *act, Index: idx, Count: n, Modules: c.d.Modules.Current(), Lap: c.Lap()}, true
}

// finish runs setup, brings the workspace to the step's starting state,
// takes the checkpoint and activates the step.
//
// For the first step the starting state is the lesson's setup. For a later
// step ("Go to this step", "Do this step again", resuming a lesson) it is
// the step's checkpoint, restored after the setup when it exists; else the
// earlier steps' demos are replayed with nothing recorded and the
// checkpoint is taken from the result when every earlier step was replayed
// to completion; otherwise the step activates without a checkpoint. A later
// step's checkpoint is never overwritten with the lesson's initial state
// (api.md §3.4). Nothing activates once ctx has ended.
func (c *Core) finish(ctx context.Context, sc *protocol.Scenario, idx int, reset, forceCheckpoint bool, lap lapMode) error {
	c.setup(ctx, sc, reset)
	if ctx.Err() != nil {
		return interrupted(ctx)
	}
	st := &sc.Steps[idx]
	if idx > 0 {
		ok, err := c.restoreStep(ctx, sc, idx, lap)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		complete := c.fastForward(ctx, sc, idx)
		if ctx.Err() != nil {
			return interrupted(ctx)
		}
		if !complete {
			c.log.Warn("fast-forward incomplete: the step activates without a checkpoint", "lesson", sc.ID, "step", st.ID)
			c.activate(sc, idx, activation{status: protocol.StepLoaded, phase: Active, lap: lap})
			return nil
		}
		forceCheckpoint = true
	}
	if st.TakesCheckpoint() {
		if err := CaptureStep(ctx, c.d.Snapshots, sc.ID, st.ID, forceCheckpoint); err != nil {
			c.log.Warn("checkpoint at scenario load", "err", err)
		}
	}
	if ctx.Err() != nil {
		return interrupted(ctx)
	}
	c.activate(sc, idx, activation{status: protocol.StepLoaded, phase: Active, lap: lap})
	return nil
}

// restoreStep restores step idx's checkpoint and activates the step; it
// reports false when the step has none.
func (c *Core) restoreStep(ctx context.Context, sc *protocol.Scenario, idx int, lap lapMode) (bool, error) {
	ref := checkpoint.StepRef(sc.ID, sc.Steps[idx].ID)
	if !c.d.Snapshots.Exists(ctx, ref) {
		return false, nil
	}
	rep, err := c.d.Snapshots.Restore(ctx, ref)
	defer c.deferEditor(ref, rep)
	if err != nil {
		if noCheckpoint(err) {
			return false, nil
		}
		return false, httpx.Internal("could not restore the step").Wrap(err)
	}
	if ctx.Err() != nil {
		return false, interrupted(ctx)
	}
	c.activate(sc, idx, activation{status: protocol.StepLoaded, phase: Active, lap: lap})
	return true, nil
}

// fastForward replays the demos of the steps before idx into Emacs, each
// activated in phase Rehearsing (conditions are evaluated but nothing is
// recorded or emitted), waiting for each step's conditions before
// the next demo. A step whose demo does not complete it is logged and the
// replay goes on. The replay leaves activationReserve of the op's deadline;
// with less than that left it does not start. It reports whether every
// earlier step was replayed to completion within the limit.
func (c *Core) fastForward(ctx context.Context, sc *protocol.Scenario, idx int) bool {
	limit := FastForwardTimeout
	if dl, ok := ctx.Deadline(); ok {
		limit = min(limit, time.Until(dl)-activationReserve)
	}
	if limit <= 0 {
		c.log.Warn("fast-forward skipped: no time left in the load", "lesson", sc.ID, "to", sc.Steps[idx].ID)
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	start := time.Now()
	complete := true
	for i := 0; i < idx; i++ {
		if ctx.Err() != nil {
			complete = false
			break
		}
		st := &sc.Steps[i]
		c.activate(sc, i, activation{phase: Rehearsing})
		if st.Demo == "" {
			c.log.Warn("fast-forward: the step has no demo", "lesson", sc.ID, "step", st.ID)
			complete = false
			continue
		}
		if err := c.playDemo(ctx, st.Demo); err != nil {
			c.log.Warn("fast-forward: playing the demo", "lesson", sc.ID, "step", st.ID, "err", err)
			complete = false
			continue
		}
		if !c.settle(ctx, c.attemptSeq(), st.ID, RehearsalSettle, 200*time.Millisecond) {
			c.log.Warn("fast-forward: the demo did not complete the step", "lesson", sc.ID, "step", st.ID)
			complete = false
		}
	}
	c.log.Info("fast-forwarded", "lesson", sc.ID, "to", sc.Steps[idx].ID, "complete", complete, "took", time.Since(start).Round(time.Millisecond))
	return complete
}

// setup runs the lesson's setup op in Emacs once the learner's frame is
// open; failures are an error event, not a failed load.
func (c *Core) setup(ctx context.Context, sc *protocol.Scenario, reset bool) {
	s := sc.Setup
	layout := s.Layout
	if strings.HasSuffix(layout, ".eld") {
		// TODO(sessiond, CURRICULUM.md Part B.3): window-state files live in
		// the lesson directory; the bundle does not ship them to sessiond yet.
		c.log.Warn("setup.layout window-state files are not supported yet", "layout", layout)
		layout = ""
	}
	if len(s.Open) == 0 && layout == "" && s.Eval == "" && s.Compile == "" {
		return
	}
	if c.d.Agent == nil {
		return
	}
	// Wait for a restart in progress and for the learner's frame, or the
	// layout would go to the daemon's invisible initial frame.
	if !c.d.Sup.WaitReady(ctx, 15*time.Second) {
		c.log.Warn("setting up the lesson before the terminal frame is open", "lesson", sc.ID)
	}
	args := protocol.AgentSetupArgs{Open: s.Open, Layout: layout, Eval: s.Eval, Compile: s.Compile, Workspace: c.d.Workspace, Reset: reset, Root: s.Root}
	if err := c.d.Agent.Call(ctx, protocol.AgentOpSetup, args, c.d.SetupTimeout, nil); err != nil {
		c.log.Warn("scenario setup in Emacs failed", "lesson", sc.ID, "err", err)
		c.emit(&protocol.ErrorEvent{Code: protocol.ErrEmacsUnavailable, Message: "Emacs could not open the lesson's files; they are in the workspace."})
	}
}
