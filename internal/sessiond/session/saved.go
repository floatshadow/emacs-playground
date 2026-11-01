package session

import (
	"maps"
	"slices"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Saved is the lesson state kept in /work/.playground/state.json (version
// 1). The core is its only writer.
type Saved struct {
	Generations protocol.ProgressGenerations `json:"generations,omitempty"`
	// Revision orders snapshots within this process, independently of disk state.
	Revision uint64 `json:"-"`
	// Active is the active step, nil when none.
	Active *protocol.StepRef `json:"active,omitempty"`
	// Lessons is per-lesson progress known to this session.
	Lessons map[string]*LessonState `json:"lessons,omitempty"`
	// Lap is the running "again, without hints" lap.
	Lap *protocol.LapInfo `json:"lap,omitempty"`
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

// lesson returns the lesson's state, created with every map allocated.
func (s *Saved) lesson(id string) *LessonState {
	if s.Lessons == nil {
		s.Lessons = map[string]*LessonState{}
	}
	ls := s.Lessons[id]
	if ls == nil {
		ls = &LessonState{}
		s.Lessons[id] = ls
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

// clone returns a deep copy.
func (s *Saved) clone() Saved {
	out := Saved{Revision: s.Revision, Generations: maps.Clone(s.Generations), Lessons: map[string]*LessonState{}}
	if s.Active != nil {
		a := *s.Active
		out.Active = &a
	}
	if s.Lap != nil {
		l := *s.Lap
		out.Lap = &l
	}
	for id, ls := range s.Lessons {
		out.Lessons[id] = &LessonState{
			Done: maps.Clone(ls.Done), Skipped: maps.Clone(ls.Skipped), Demonstrated: maps.Clone(ls.Demonstrated),
			Hints: maps.Clone(ls.Hints), Resets: maps.Clone(ls.Resets),
		}
	}
	return out
}

// RestoreSaved loads saved progress (state.json) without activating a step.
func (c *Core) RestoreSaved(s Saved) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.Lessons == nil {
		s.Lessons = map[string]*LessonState{}
	}
	s.Revision = c.lesson.Saved.Revision
	c.lesson.Saved = s
}

// Saved returns a copy of the persistent state.
func (c *Core) Saved() Saved {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.savedLocked()
}

// savedLocked returns a copy of the persistent state with the next
// revision, for persist.
func (c *Core) savedLocked() Saved {
	c.lesson.Saved.Revision++
	return c.lesson.Saved.clone()
}

func (c *Core) persist(s Saved) {
	if c.d.Persist != nil {
		c.d.Persist(s)
	}
}

func (c *Core) report(rs ...protocol.ProgressReport) {
	if c.d.Report == nil {
		return
	}
	for _, r := range rs {
		c.d.Report(r)
	}
}

// progressLocked is a report of the step's saved totals with status and
// the lesson's generation.
func (c *Core) progressLocked(lessonID, stepID string, status protocol.ProgressStatus, at time.Time) protocol.ProgressReport {
	r := c.lesson.Saved.lesson(lessonID).progress(lessonID, stepID, status, at)
	r.Generation = c.lesson.Saved.Generations.ForLesson(lessonID)
	return r
}

// firstIncomplete returns the index of the first step of sc that is
// neither done nor skipped according to saved progress, or 0 when every
// step is. A demonstrated step is resumed so the learner practises it
// (api.md §2.4).
func (c *Core) firstIncomplete(sc *protocol.Scenario) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	ls := c.lesson.Saved.Lessons[sc.ID]
	for i := range sc.Steps {
		if !ls.finished(sc.Steps[i].ID) {
			return i
		}
	}
	return 0
}

// ProgressReports reconciles completed, skipped and demonstrated steps using
// their original timestamps. Lap increments are sent only at completion.
func (c *Core) ProgressReports() []protocol.ProgressReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	var reports []protocol.ProgressReport
	for _, lessonID := range slices.Sorted(maps.Keys(c.lesson.Saved.Lessons)) {
		ls := c.lesson.Saved.Lessons[lessonID]
		times := map[string]int64{}
		for _, entries := range []map[string]int64{ls.Demonstrated, ls.Skipped, ls.Done} {
			for id, at := range entries {
				times[id] = at
			}
		}
		for _, stepID := range slices.Sorted(maps.Keys(times)) {
			reports = append(reports, c.progressLocked(lessonID, stepID, statusOf(ls, stepID), time.UnixMilli(times[stepID])))
		}
	}
	return reports
}
