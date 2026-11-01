package session

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// TODO(sessiond, design.md §7): the progress outbox replaces the
// once-per-process in-progress map, dropGeneration and SyncGenerations.

// generationNewer reports whether dropGeneration(lessonID, generation)
// would apply: generation zero, or newer than the one applied.
func (c *Core) generationNewer(lessonID string, generation int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return generation == 0 || generation > c.lesson.Saved.Generations.ForLesson(lessonID)
}

// dropGeneration is the drop op's change to the lesson state, so it runs
// only in the op worker: it applies a gateway reset once, retaining any
// newer lesson reset; generation zero is an unconditional local drop. It
// forgets the saved progress, the lap and the active step of lessonID
// (with "" of every lesson); a forgotten step emits step {status:
// dropped}. It reports whether a step was forgotten.
func (c *Core) dropGeneration(lessonID string, generation int64) bool {
	c.mu.Lock()
	l := &c.lesson
	current := l.Saved.Generations.ForLesson(lessonID)
	if generation > 0 && generation <= current {
		c.mu.Unlock()
		return false
	}
	drop := func(id string) bool {
		return (lessonID == "" || id == lessonID) && (generation == 0 || l.Saved.Generations.ForLesson(id) < generation)
	}
	dropped := false
	if l.Scenario != nil && drop(l.Scenario.ID) {
		// A rehearsed step counts as active here: the page waiting for a
		// load the drop cancelled learns the lesson's step went.
		if l.hasStep() {
			dropped = true
			c.emitLocked(&protocol.StepEvent{LessonID: l.Scenario.ID, StepID: l.Scenario.Steps[l.Index].ID, Status: protocol.StepDropped,
				Index: l.Index, Count: len(l.Scenario.Steps)})
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
	for k := range c.reported {
		id, _, _ := strings.Cut(k, "\x00")
		if drop(id) {
			delete(c.reported, k)
		}
	}
	if generation > 0 {
		if l.Saved.Generations == nil {
			l.Saved.Generations = protocol.ProgressGenerations{}
		}
		l.Saved.Generations[lessonID] = generation
	}
	saved := c.savedLocked()
	c.mu.Unlock()
	c.persist(saved)
	return dropped
}

// SyncGenerations applies the gateway's snapshot of reset generations: a
// drop op (Drop) for every generation newer than the one applied, "" first,
// then the snapshot is adopted exactly, including lower generations. A
// drop that does not finish while ctx lasts still runs; the snapshot is
// then not adopted (the next sync adopts it) and the error is returned.
// The caller serialises syncs with rpc drop-step, so a reset the gateway
// sends while a sync runs applies after the adoption.
func (c *Core) SyncGenerations(ctx context.Context, generations protocol.ProgressGenerations) error {
	ids := slices.Sorted(maps.Keys(generations))
	for _, id := range ids {
		if g := generations[id]; g > 0 && c.generationNewer(id, g) {
			if err := c.Drop(ctx, id, g); err != nil {
				return err
			}
		}
	}
	c.mu.Lock()
	if maps.Equal(c.lesson.Saved.Generations, generations) {
		c.mu.Unlock()
		return nil
	}
	c.lesson.Saved.Generations = maps.Clone(generations)
	saved := c.savedLocked()
	c.mu.Unlock()
	c.persist(saved)
	return nil
}

// inProgressLocked is the in-progress report of the active step, once per
// process and step, and never for a step already finished or demonstrated.
func (c *Core) inProgressLocked() []protocol.ProgressReport {
	a := c.activeLocked()
	if a == nil {
		return nil
	}
	key := a.LessonID + "\x00" + a.StepID
	ls := c.lesson.Saved.Lessons[a.LessonID]
	if c.reported[key] || ls.finished(a.StepID) || (ls != nil && ls.Demonstrated[a.StepID] != 0) {
		return nil
	}
	c.reported[key] = true
	return []protocol.ProgressReport{{LessonID: a.LessonID, StepID: a.StepID, Status: protocol.ProgressInProgress,
		At: c.now().UnixMilli(), Generation: c.lesson.Saved.Generations.ForLesson(a.LessonID)}}
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
	rep := c.progressLocked(lessonID, stepID, statusOf(ls, stepID), c.now())
	saved := c.savedLocked()
	c.mu.Unlock()
	c.report(rep)
	c.persist(saved)
	return nil
}
