package session

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/progress"
)

// Progress is the progress outbox (progress.Outbox). The core changes it
// (Record, Drop, Sync) and reads it for state.json (Snapshot) only under
// Core.mu, so every change to the outbox's generations and entries is one
// change with the saved progress it belongs to; Newer only decides whether
// a drop preempts.
type Progress interface {
	Record(protocol.ProgressReport)
	Drop(scope string, generation int64) (protocol.ProgressGenerations, bool)
	Newer(scope string, generation int64) bool
	Sync(protocol.ProgressGenerations)
	Snapshot() progress.Snapshot
}

// SyncTimeout bounds the fetch of the gateway's generations in the sync
// op.
const SyncTimeout = 5 * time.Second

// Persistent returns the saved lesson state and the outbox's state read in
// one hold of mu: every change to either (outcomes, drops, syncs) happens
// under mu, so the pair is always one the session was in. state.json is
// written from it (Deps.Persist), outside mu.
func (c *Core) Persistent() (Saved, progress.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lesson.Saved.clone(), c.d.Progress.Snapshot()
}

// Sync implements the progress sync before every load and at startup: it
// applies the gateway resets this session missed while it was unavailable
// (whole-user first), then adopts the gateway's generations exactly. With
// reconcile (startup) every saved outcome is queued again afterwards: a
// gateway that lost its store, or a state.json without an outbox, gets
// them back. It is an op, so no drop comes between the gateway's answer
// and its adoption; a gateway that cannot answer leaves the generations
// as they are.
func (c *Core) Sync(ctx context.Context, reconcile bool) error {
	_, err := c.do(ctx, &Op{Kind: OpSync, Req: reconcile, Deadline: SyncTimeout + time.Second})
	return err
}

func (c *Core) sync(ctx context.Context, reconcile bool) error {
	var err error
	if c.d.Generations != nil {
		gctx, cancel := context.WithTimeout(ctx, SyncTimeout)
		var gens protocol.ProgressGenerations
		gens, err = c.d.Generations(gctx)
		cancel()
		if err == nil {
			for _, id := range slices.Sorted(maps.Keys(gens)) {
				if g := gens[id]; g > 0 {
					c.drop(id, g)
				}
			}
			c.mu.Lock()
			c.d.Progress.Sync(gens)
			c.mu.Unlock()
		}
	}
	if reconcile {
		c.mu.Lock()
		for _, r := range c.outcomesLocked() {
			c.d.Progress.Record(r)
		}
		c.mu.Unlock()
	}
	c.persist()
	return err
}

// drop is the drop op (rpc drop-step, and the sync op for a reset the
// session missed): it applies a gateway reset once, keeping any newer
// reset; generation zero is an unconditional local drop. The outbox purges
// the scope's entries and raises the generation, and the saved progress,
// the lap and the active step of lessonID (with "" of every lesson) are
// forgotten, and a lesson event {change: dropped} goes out. Both happen in
// one hold of mu, so an outcome recorded before the drop is purged and an
// observation after it belongs to a past attempt. It reports whether a
// step was forgotten.
func (c *Core) drop(lessonID string, generation int64) bool {
	c.mu.Lock()
	l := &c.lesson
	gens, ok := c.d.Progress.Drop(lessonID, generation)
	if !ok {
		c.mu.Unlock()
		return false
	}
	drop := func(id string) bool {
		return (lessonID == "" || id == lessonID) && (generation == 0 || gens.ForLesson(id) < generation)
	}
	dropped, stepID := false, ""
	if l.Scenario != nil && drop(l.Scenario.ID) {
		// A rehearsed step counts as active here: the page waiting for a
		// load the drop cancelled learns the lesson's step went.
		if l.hasStep() {
			dropped, stepID = true, l.Scenario.Steps[l.Index].ID
		}
		c.idleLocked(nil)
		l.Saved.Active = nil
		l.Saved.Lap = nil
	}
	if l.Saved.Active != nil && drop(l.Saved.Active.LessonID) {
		l.Saved.Active = nil
	}
	if l.Saved.Lap != nil && drop(l.Saved.Lap.LessonID) {
		l.Saved.Lap = nil
	}
	for id := range l.Saved.Lessons {
		if drop(id) {
			delete(l.Saved.Lessons, id)
		}
	}
	c.publishLocked(protocol.ChangeDropped, stepID, 0)
	c.mu.Unlock()
	c.persist()
	return dropped
}

// Hint records that hint level was shown for stepID of the loaded lesson
// (rpc hint). HintsUsed is the highest level shown (an absolute total).
func (c *Core) Hint(stepID string, level int) error {
	c.mu.Lock()
	l := &c.lesson
	switch {
	case c.inLapLocked():
		c.mu.Unlock()
		return errLap()
	case l.Scenario == nil || l.Scenario.StepIndex(stepID) < 0 || level <= 0:
		c.mu.Unlock()
		return httpx.Conflict("that step is not part of the active lesson")
	}
	lessonID := l.Scenario.ID
	ls := l.Saved.lesson(lessonID)
	if level <= ls.Hints[stepID] {
		c.mu.Unlock()
		return nil
	}
	ls.Hints[stepID] = level
	c.recordLocked(lessonID, stepID, statusOf(ls, stepID), c.now())
	c.mu.Unlock()
	c.persist()
	return nil
}
