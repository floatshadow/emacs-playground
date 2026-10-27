package progress

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// TestStatusesRiseAndCount covers the demonstrated and skipped statuses,
// "only rises", the per-lesson counts and "Start over" (api.md §2.4).
func TestStatusesRiseAndCount(t *testing.T) {
	for _, open := range []func(string) (store.Store, error){
		func(d string) (store.Store, error) { return store.OpenJSON(d) },
		func(d string) (store.Store, error) { return store.OpenSQLite(filepath.Join(d, "p.db")) },
	} {
		st, err := open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		now := time.UnixMilli(5_000)
		lesson := "00-a/01-b"
		s := &Service{Store: st, Steps: steps{lesson: {"one", "two", "three"}, "00-a/02-c": {"x"}}, Now: func() time.Time { return now }}
		ctx := context.Background()
		apply := func(step string, status protocol.ProgressStatus) {
			t.Helper()
			if err := s.Apply(ctx, "u1", protocol.ProgressReport{LessonID: lesson, StepID: step, Status: status}); err != nil {
				t.Fatal(err)
			}
		}
		status := func(step string) protocol.ProgressStatus {
			p, _ := s.Get(ctx, "u1")
			return p.Lessons[lesson].Steps[step].Status
		}
		apply("one", protocol.ProgressInProgress)
		apply("one", protocol.ProgressDemonstrated)
		if got := status("one"); got != protocol.ProgressDemonstrated {
			t.Fatalf("demonstrated: %s", got)
		}
		apply("one", protocol.ProgressInProgress) // never lowers
		if got := status("one"); got != protocol.ProgressDemonstrated {
			t.Fatalf("lowered to %s", got)
		}
		apply("one", protocol.ProgressSkipped)
		apply("one", protocol.ProgressDemonstrated)
		if got := status("one"); got != protocol.ProgressSkipped {
			t.Fatalf("skipped: %s", got)
		}
		apply("one", protocol.ProgressDone)
		apply("one", protocol.ProgressSkipped)
		p, _ := s.Get(ctx, "u1")
		if o := p.Lessons[lesson].Steps["one"]; o.Status != protocol.ProgressDone || o.CompletedAt != now.UnixMilli() {
			t.Fatalf("done %+v", o)
		}
		apply("two", protocol.ProgressSkipped)
		apply("three", protocol.ProgressDemonstrated)
		p, _ = s.Get(ctx, "u1")
		lp := p.Lessons[lesson]
		if lp.Status != protocol.ProgressInProgress || lp.Counts != (protocol.ProgressCounts{Done: 1, Skipped: 1, Demonstrated: 1, Total: 3}) {
			t.Fatalf("a lesson with skipped and demonstrated steps is not done: %+v", lp)
		}
		if lp.Steps["two"].CompletedAt != 0 {
			t.Fatal("completedAt is for done only")
		}
		apply("two", protocol.ProgressDone)
		apply("three", protocol.ProgressDone)
		p, _ = s.Get(ctx, "u1")
		if lp := p.Lessons[lesson]; lp.Status != protocol.ProgressDone || lp.Counts.Done != 3 {
			t.Fatalf("all done %+v", lp)
		}
		_ = s.Apply(ctx, "u1", protocol.ProgressReport{LessonID: "00-a/02-c", StepID: "x", Status: protocol.ProgressInProgress})
		_ = s.Apply(ctx, "u2", protocol.ProgressReport{LessonID: "00-a/02-c", StepID: "x", Status: protocol.ProgressDone})

		// Start over: every lesson of the user, no one else's.
		if err := s.Reset(ctx, "u1", ""); err != nil {
			t.Fatal(err)
		}
		if p, _ := s.Get(ctx, "u1"); len(p.Lessons) != 0 {
			t.Fatalf("after start over %+v", p)
		}
		if p, _ := s.Get(ctx, "u2"); p.Lessons["00-a/02-c"].Status != protocol.ProgressDone {
			t.Fatalf("another user's progress went: %+v", p)
		}
		log, _ := st.AuditLog(ctx, 1)
		if len(log) != 1 || log[0].Action != "progress.reset" || log[0].Details != "lesson=*" {
			t.Fatalf("audit %+v", log)
		}
	}
}
