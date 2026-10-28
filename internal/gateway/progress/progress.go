package progress

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/floatshadow/emacs-playground/internal/gateway/limit"
	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Steps resolves a lesson's step ids (the curriculum bundle).
type Steps interface {
	// StepIDs returns the steps of a ready lesson; false for lessons the
	// bundle does not have.
	StepIDs(lessonID string) ([]string, bool)
	// Loaded reports whether a bundle is loaded (and its version); without
	// one, reports cannot be checked against it.
	Loaded() (bool, string)
}

// maxIDLen bounds lesson and step ids in reports.
const maxIDLen = 200

// Service records and aggregates progress.
type Service struct {
	Store store.Store
	Steps Steps
	Now   func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Apply records a progress report for userID.
func (s *Service) Apply(ctx context.Context, userID string, rep protocol.ProgressReport) error {
	if rep.LessonID == "" || rep.StepID == "" {
		return httpx.BadRequest("lessonId and stepId are required")
	}
	if len(rep.LessonID) > maxIDLen || len(rep.StepID) > maxIDLen {
		return httpx.BadRequest("lessonId and stepId must be shorter than 200 bytes")
	}
	switch rep.Status {
	case protocol.ProgressInProgress, protocol.ProgressDone, protocol.ProgressNotStarted,
		protocol.ProgressDemonstrated, protocol.ProgressSkipped:
	default:
		return httpx.BadRequest("status must be in-progress, demonstrated, skipped, done or not-started")
	}
	if s.Steps != nil {
		steps, ok := s.Steps.StepIDs(rep.LessonID)
		if ok && !slices.Contains(steps, rep.StepID) {
			return httpx.BadRequest("step " + rep.StepID + " is not in lesson " + rep.LessonID)
		}
		// Every row is a lesson and step of the bundle: a session cannot
		// grow the store with made-up ids.
		if loaded, _ := s.Steps.Loaded(); !ok && loaded {
			return httpx.BadRequest("lesson " + rep.LessonID + " is not in the curriculum")
		}
	}
	if (rep.HintsUsed != nil && *rep.HintsUsed < 0) || (rep.Resets != nil && *rep.Resets < 0) {
		return httpx.BadRequest("hintsUsed and resets must not be negative")
	}
	now := s.now().UnixMilli()
	recs, err := s.Store.Progress().List(ctx, userID)
	if err != nil {
		return err
	}
	rec := store.StepRecord{UserID: userID, LessonID: rep.LessonID, StepID: rep.StepID, Status: protocol.ProgressNotStarted}
	for _, r := range recs {
		if r.LessonID == rep.LessonID && r.StepID == rep.StepID {
			rec = r
		}
	}
	// A step's status only rises (not-started < in-progress < demonstrated
	// < skipped < done); only a reset clears it (api.md §2.4).
	if protocol.ProgressRank(rep.Status) > protocol.ProgressRank(rec.Status) {
		rec.Status = rep.Status
		if rep.Status == protocol.ProgressDone {
			rec.CompletedAt = rep.At
			if rec.CompletedAt == 0 {
				rec.CompletedAt = now
			}
		}
	}
	if rep.HintsUsed != nil {
		rec.HintsUsed = *rep.HintsUsed
	}
	if rep.Resets != nil {
		rec.Resets = *rep.Resets
	}
	rec.UpdatedAt = now
	if err := s.Store.Progress().Put(ctx, rec); err != nil {
		return err
	}
	if rep.LapMs > 0 {
		return s.addLap(ctx, userID, rep, now)
	}
	return nil
}

// addLap records a finished lap: only on the done report of the lesson's
// last step (the engine sends it there).
func (s *Service) addLap(ctx context.Context, userID string, rep protocol.ProgressReport, now int64) error {
	if rep.Status != protocol.ProgressDone {
		return httpx.BadRequest("lapMs comes with a done report")
	}
	if s.Steps != nil {
		if steps, ok := s.Steps.StepIDs(rep.LessonID); ok && (len(steps) == 0 || steps[len(steps)-1] != rep.StepID) {
			return httpx.BadRequest("lapMs comes with the last step")
		}
	}
	laps, err := s.Store.Progress().Laps(ctx, userID)
	if err != nil {
		return err
	}
	lr := store.LapRecord{UserID: userID, LessonID: rep.LessonID}
	for _, l := range laps {
		if l.LessonID == rep.LessonID {
			lr = l
		}
	}
	lr.Count++
	lr.LastMs = rep.LapMs
	if lr.BestMs == 0 || rep.LapMs < lr.BestMs {
		lr.BestMs = rep.LapMs
	}
	lr.LastAt = rep.At
	if lr.LastAt == 0 {
		lr.LastAt = now
	}
	return s.Store.Progress().PutLap(ctx, lr)
}

// Get aggregates a user's progress.
func (s *Service) Get(ctx context.Context, userID string) (protocol.Progress, error) {
	out := protocol.Progress{Lessons: map[string]protocol.LessonProgress{}}
	recs, err := s.Store.Progress().List(ctx, userID)
	if err != nil {
		return out, err
	}
	for _, r := range recs {
		lp, ok := out.Lessons[r.LessonID]
		if !ok {
			lp = protocol.LessonProgress{Status: protocol.ProgressNotStarted, Steps: map[string]protocol.StepProgress{}}
		}
		lp.Steps[r.StepID] = protocol.StepProgress{Status: r.Status, HintsUsed: r.HintsUsed, Resets: r.Resets, CompletedAt: r.CompletedAt}
		if protocol.ProgressRank(r.Status) > 0 {
			lp.Status = protocol.ProgressInProgress
		}
		out.Lessons[r.LessonID] = lp
		if r.UpdatedAt > out.UpdatedAt {
			out.UpdatedAt = r.UpdatedAt
		}
	}
	laps, err := s.Store.Progress().Laps(ctx, userID)
	if err != nil {
		return out, err
	}
	for _, l := range laps {
		lp, ok := out.Lessons[l.LessonID]
		if !ok {
			lp = protocol.LessonProgress{Status: protocol.ProgressNotStarted, Steps: map[string]protocol.StepProgress{}}
		}
		lp.Laps = &protocol.LapStats{Count: l.Count, BestMs: l.BestMs, LastMs: l.LastMs, LastAt: l.LastAt}
		out.Lessons[l.LessonID] = lp
	}
	for id, lp := range out.Lessons {
		var steps []string
		known := false
		if s.Steps != nil {
			steps, known = s.Steps.StepIDs(id)
		}
		if !known {
			// Not in the bundle: count what was recorded.
			for st := range lp.Steps {
				steps = append(steps, st)
			}
		} else {
			lp.Counts.Total = len(steps)
		}
		for _, st := range steps {
			switch lp.Steps[st].Status {
			case protocol.ProgressDone:
				lp.Counts.Done++
			case protocol.ProgressSkipped:
				lp.Counts.Skipped++
			case protocol.ProgressDemonstrated:
				lp.Counts.Demonstrated++
			}
		}
		// A lesson is done only when every step of the bundle's scenario
		// is done (skipped and demonstrated steps are not).
		if known && len(steps) > 0 && lp.Counts.Done == len(steps) {
			lp.Status = protocol.ProgressDone
		}
		out.Lessons[id] = lp
	}
	return out, nil
}

// Reset clears a lesson's progress for userID; an empty lessonID clears
// every lesson of the user ("Start over").
func (s *Service) Reset(ctx context.Context, userID, lessonID string) error {
	details := "lesson=" + lessonID
	if lessonID == "" {
		if err := s.Store.Progress().DeleteAll(ctx, userID); err != nil {
			return err
		}
		details = "lesson=*"
	} else {
		if len(lessonID) > maxIDLen {
			return httpx.BadRequest("lessonId must be shorter than 200 bytes")
		}
		if err := s.Store.Progress().DeleteLesson(ctx, userID, lessonID); err != nil {
			return err
		}
	}
	_ = s.Store.Audit(ctx, store.AuditEvent{At: s.now().UnixMilli(), UserID: userID, Action: "progress.reset", Details: details})
	return nil
}

// Bounds on what one stuck report stores: the last 200 events (api.md
// §2.5), at most MaxStuckEventBytes of them (the newest that fit), and a
// note of MaxStuckNote bytes.
const (
	MaxStuckEvents     = 200
	MaxStuckEventBytes = 512 << 10
	MaxStuckNote       = 4000
)

// Stuck files stuck reports with deduplication and a per-user rate limit.
type Stuck struct {
	Store store.Store
	// Limit is per user; default 10 per hour.
	Limit *limit.Limiter
	Now   func() time.Time

	mu sync.Mutex
	// recent holds one entry per report key filed within 30 s, entered
	// before the store write, so identical reports filed at the same time
	// wait for the first one's write and share its id.
	recent map[string]*recentReport
}

// recentReport is a report filed at at, pending while its write runs;
// id and err are set before done is closed.
type recentReport struct {
	at      time.Time
	pending bool
	done    chan struct{}
	id      int64
	err     error
}

// NewStuck returns a stuck report filer.
func NewStuck(st store.Store) *Stuck {
	return &Stuck{Store: st, Limit: limit.New(10, time.Hour), recent: map[string]*recentReport{}}
}

// Allow applies the per-user rate limit.
func (s *Stuck) Allow(userID string) bool { return s.Limit.Allow(userID) }

// File stores a report, merging duplicates within 30 seconds.
// TODO(gateway, deployment.md §9.4): with features.telemetryUpload, also
// send the report to a configured URL (off by default; no key yet).
func (s *Stuck) File(ctx context.Context, userID, lessonID, stepID, note string, events []json.RawMessage) (int64, error) {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	key := strings.Join([]string{userID, lessonID, stepID, note}, "\x00")
	s.mu.Lock()
	for k, r := range s.recent {
		if !r.pending && now.Sub(r.at) > 30*time.Second {
			delete(s.recent, k)
		}
	}
	if r, ok := s.recent[key]; ok {
		s.mu.Unlock()
		select {
		case <-r.done:
			return r.id, r.err
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	r := &recentReport{at: now, pending: true, done: make(chan struct{})}
	s.recent[key] = r
	s.mu.Unlock()

	// Identical reports wait on this write, so it outlives the request.
	id, err := s.add(context.WithoutCancel(ctx), userID, lessonID, stepID, note, events, now)
	s.mu.Lock()
	r.id, r.err, r.pending = id, err, false
	if err != nil {
		// A failed write is not remembered: the next report tries again.
		delete(s.recent, key)
	}
	s.mu.Unlock()
	close(r.done)
	return id, err
}

// add bounds and stores one report.
func (s *Stuck) add(ctx context.Context, userID, lessonID, stepID, note string, events []json.RawMessage, now time.Time) (int64, error) {
	events = capEvents(events)
	raw, err := json.Marshal(events)
	if err != nil {
		return 0, httpx.BadRequest("events must be JSON")
	}
	note = truncate(note, MaxStuckNote)
	lessonID, stepID = truncate(lessonID, maxIDLen), truncate(stepID, maxIDLen)
	return s.Store.Stuck().Add(ctx, &store.StuckReport{UserID: userID, LessonID: lessonID, StepID: stepID, CreatedAt: now.UnixMilli(), Note: note, Events: raw})
}

// capEvents keeps the newest events within MaxStuckEvents and
// MaxStuckEventBytes.
func capEvents(events []json.RawMessage) []json.RawMessage {
	if len(events) > MaxStuckEvents {
		events = events[len(events)-MaxStuckEvents:]
	}
	size, first := 0, len(events)
	for first > 0 && size+len(events[first-1]) <= MaxStuckEventBytes {
		first--
		size += len(events[first])
	}
	if events = events[first:]; events == nil {
		events = []json.RawMessage{}
	}
	return events
}

// truncate cuts s to at most n bytes on a UTF-8 boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
