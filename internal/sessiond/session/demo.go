package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
)

// skipSettle is how long the skip op waits for the step's conditions after
// its demo (file events, compilation output).
const skipSettle = 8 * time.Second

// admitted is a demo start or skip of stepID admitted while attempt was
// current. A load or advance queued before it replaces the attempt, and a
// step of another lesson can have the same id, so the op runs only while
// that attempt is still the current one.
type admitted struct {
	stepID  string
	attempt Attempt
}

// admit checks that stepID is the active step and no lap runs (demo start
// and skip, before they queue) and names the current attempt.
func (c *Core) admit(stepID string) (admitted, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireActiveLocked(stepID); err != nil {
		return admitted{}, err
	}
	return admitted{stepID: stepID, attempt: c.lesson.Attempt}, nil
}

// begin starts a demo start or skip op: its attempt must still be current
// and its step active. It returns the scenario and the step's index.
func (c *Core) begin(req admitted) (*protocol.Scenario, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lesson.Attempt.Seq != req.attempt.Seq || c.lesson.Attempt.Lesson != req.attempt.Lesson {
		return nil, 0, errNotActiveStep()
	}
	if err := c.requireActiveLocked(req.stepID); err != nil {
		return nil, 0, err
	}
	return c.lesson.Scenario, c.lesson.Index, nil
}

func (c *Core) requireActiveLocked(stepID string) error {
	a := c.activeLocked()
	switch {
	case a == nil || stepID == "" || a.StepID != stepID || c.lesson.Phase == Skipping:
		return errNotActiveStep()
	case c.inLapLocked():
		return errLap()
	}
	return nil
}

// enter puts the active step stepID into phase p (Watching with its demo
// playing, or Skipping) after admit's checks, in the same hold of
// mu, so a completion Observe records first is never overwritten (the
// step is then Advancing, not active). It returns the attempt's seq.
func (c *Core) enter(stepID string, p Phase) (uint64, error) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireActiveLocked(stepID); err != nil {
		return 0, err
	}
	l := &c.lesson
	switch p {
	case Watching:
		l.Phase = Watching
		l.Demo.Playing, l.Demo.Seen = true, now
	case Skipping:
		l.Phase, l.Demo = Skipping, Demo{}
	}
	return l.Attempt.Seq, nil
}

// restoreIfDirty puts the active step back to its checkpoint, without
// counting a reset, when the learner ran a command in it since it started
// or was last reset (demo start and skip, api.md §3.4); the step restarts
// as reset. It reports whether it restored; a missing or failed restore
// leaves the step as it is.
func (c *Core) restoreIfDirty(ctx context.Context, sc *protocol.Scenario, idx int) (bool, error) {
	c.mu.Lock()
	dirty := c.dirtyLocked()
	c.mu.Unlock()
	if !dirty {
		return false, nil
	}
	stepID := sc.Steps[idx].ID
	ref := checkpoint.StepRef(sc.ID, stepID)
	rep, err := c.d.Snapshots.Restore(ctx, ref)
	defer c.deferEditor(ref, rep)
	if err != nil {
		if !noCheckpoint(err) {
			c.log.Warn("restoring the step before its demo", "lesson", sc.ID, "step", stepID, "err", err)
		}
		return false, nil
	}
	if ctx.Err() != nil {
		return false, interrupted(ctx)
	}
	return c.resetTo(sc.ID, stepID, false), nil
}

// demoStart is rpc demo start (api.md §4.1): the active step, restored
// first when dirty, gets a "Watch it" latch with its demo playing. A
// second start keeps a latch that already demonstrated.
func (c *Core) demoStart(ctx context.Context, req admitted) (protocol.DemoResult, error) {
	stepID := req.stepID
	sc, idx, err := c.begin(req)
	if err != nil {
		return protocol.DemoResult{}, err
	}
	restored, err := c.restoreIfDirty(ctx, sc, idx)
	if err != nil {
		return protocol.DemoResult{}, err
	}
	if _, err := c.enter(stepID, Watching); err != nil {
		return protocol.DemoResult{}, err
	}
	return protocol.DemoResult{Restored: restored}, nil
}

// skip is rpc skip ("Skip this step", api.md §3.4): the active step,
// restored first when dirty, has its demo played into Emacs in phase
// Skipping (completions are left to this op), its conditions polled for
// skipSettle, then it is recorded skipped and the next step activates. A
// drop that cancels the op leaves the step to the drop; a deadline still
// records the skip.
func (c *Core) skip(ctx context.Context, req admitted) (protocol.SkipResult, error) {
	stepID := req.stepID
	sc, idx, err := c.begin(req)
	if err != nil {
		return protocol.SkipResult{}, err
	}
	if _, err := c.restoreIfDirty(ctx, sc, idx); err != nil {
		return protocol.SkipResult{}, err
	}
	seq, err := c.enter(stepID, Skipping)
	if err != nil {
		return protocol.SkipResult{}, err
	}

	completed := false
	if demo := sc.Steps[idx].Demo; demo != "" {
		if err := c.playDemo(ctx, demo); err != nil {
			c.log.Warn("skip: playing the demo", "lesson", sc.ID, "step", stepID, "err", err)
		}
		if ctx.Err() == nil {
			completed = c.settle(ctx, seq, stepID, skipSettle, 250*time.Millisecond)
			if !completed {
				c.log.Warn("skip: the step's demo did not complete it (curriculum defect)", "lesson", sc.ID, "step", stepID)
			}
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		c.mu.Lock()
		if c.lesson.Attempt.Seq == seq && c.lesson.Phase == Skipping {
			c.lesson.Phase = Active
		}
		c.mu.Unlock()
		return protocol.SkipResult{}, interrupted(ctx)
	}

	now := c.now()
	c.mu.Lock()
	l := &c.lesson
	if l.Attempt.Seq != seq || l.Phase != Skipping {
		c.mu.Unlock()
		return protocol.SkipResult{}, errNotActiveStep()
	}
	l.Saved.lesson(sc.ID).Skipped[stepID] = now.UnixMilli()
	c.recordLocked(sc.ID, stepID, protocol.ProgressSkipped, now)
	last := idx+1 >= len(sc.Steps)
	c.emitLocked(&protocol.StepEvent{LessonID: sc.ID, StepID: stepID, Status: protocol.StepSkipped, Index: idx, Count: len(sc.Steps), LessonComplete: last})
	c.finishLocked(last, false)
	c.mu.Unlock()
	c.persist()
	next, err := c.advance(ctx, seq)
	return protocol.SkipResult{Next: next, DemoCompleted: completed}, err
}

// settle polls the evaluator for up to d until attempt seq's step stepID
// completes; it reports whether it did.
func (c *Core) settle(ctx context.Context, seq uint64, stepID string, d, every time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		res, ok := c.d.Evaluator.Check(ctx)
		c.mu.Lock()
		current := c.lesson.Attempt.Seq == seq
		c.mu.Unlock()
		if !ok || !current || res.StepID != stepID {
			return false
		}
		if res.Complete {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(every):
		}
	}
}

// playDemo plays a demo string into the frame the learner sees, at
// playctl's pacing (protocol.PlayDemo).
func (c *Core) playDemo(ctx context.Context, demo string) error {
	return protocol.PlayDemo(ctx, demo, 1, func(ctx context.Context, req protocol.KeysRequest) error {
		if ok, reason := c.d.Sup.Ready(); !ok {
			return fmt.Errorf("%s", reason)
		}
		return c.d.Sup.SendKeys(ctx, "", req.Keys, req.Text)
	})
}
