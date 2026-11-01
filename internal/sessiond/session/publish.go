package session

import (
	"reflect"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// busyKind reports whether ops of kind k show in the snapshot as busy: the
// ops that change the lesson or the editor under the learner's keys.
func busyKind(k OpKind) bool {
	switch k {
	case OpLoad, OpResume, OpRestore, OpDemoStart, OpSkip, OpSetModules, OpRestartEmacs, OpRecover, OpReconnect:
		return true
	}
	return false
}

// Snapshot returns the lesson snapshot (hello, status).
func (c *Core) Snapshot() protocol.LessonSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

// snapshotLocked is the lesson state as the browser sees it (backend.md
// §3.8), with the rev of the last snapshot published. Active and Skipping
// are active; Advancing is active on the finished step, or after the last
// step as Idle; Idle is complete when every step of the loaded lesson is
// done or skipped. A rehearsal is never visible: it shows the last
// snapshot published.
func (c *Core) snapshotLocked() protocol.LessonSnapshot {
	l := &c.lesson
	if l.Phase == Rehearsing {
		return c.last
	}
	s := protocol.LessonSnapshot{Rev: c.last.Rev, Phase: protocol.PhaseIdle, Outcomes: map[string]protocol.ProgressStatus{}}
	if sc := l.Scenario; sc != nil {
		s.LessonID = sc.ID
		ls := l.Saved.Lessons[sc.ID]
		finished := 0
		for _, st := range sc.Steps {
			if status := statusOf(ls, st.ID); status != protocol.ProgressInProgress {
				s.Outcomes[st.ID] = status
			}
			if ls.finished(st.ID) {
				finished++
			}
		}
		idle := l.Phase == Idle || l.Phase == Advancing && l.Index+1 >= len(sc.Steps)
		switch {
		case idle && finished == len(sc.Steps):
			s.Phase = protocol.PhaseComplete
		case !idle && l.Index >= 0:
			s.Step = &protocol.StepPos{ID: sc.Steps[l.Index].ID, Index: l.Index, Count: len(sc.Steps)}
			s.Attempt = l.Attempt.Seq
			s.Phase = protocol.PhaseActive
			if l.Phase == Watching {
				s.Phase = protocol.PhaseWatching
			}
		}
		if c.inLapLocked() {
			lap := *l.Saved.Lap
			s.Lap = &lap
		}
	}
	if r := c.running; r != nil && busyKind(r.Kind) {
		s.Busy = &protocol.BusyInfo{Op: string(r.Kind), LessonID: r.info.LessonID, StepID: r.info.StepID, StartedAt: r.info.StartedAt.UnixMilli()}
	}
	return s
}

// publishLocked publishes the snapshot as a lesson event when it differs
// from the last one published, rev aside; complete, skipped, demonstrated
// and dropped are news in themselves and always publish. Every critical
// section that changes the lesson, its saved outcomes or the running op
// ends with it, so the last snapshot published is always the current one.
// A rehearsal publishes nothing.
func (c *Core) publishLocked(change protocol.LessonChange, stepID string, lapMs int64) {
	if c.lesson.Phase == Rehearsing {
		return
	}
	s := c.snapshotLocked()
	switch change {
	case protocol.ChangeComplete, protocol.ChangeSkipped, protocol.ChangeDemonstrated, protocol.ChangeDropped:
	default:
		if reflect.DeepEqual(s, c.last) {
			return
		}
	}
	s.Rev++
	c.last = s
	c.emitLocked(&protocol.LessonEvent{Change: change, StepID: stepID, LapMs: lapMs, Lesson: s})
}
