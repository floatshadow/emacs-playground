package progress

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestLaps(t *testing.T) {
	dir := t.TempDir()
	js, err := store.OpenJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	sq, err := store.OpenSQLite(filepath.Join(dir, "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sq.Close()
	for name, st := range map[string]store.Store{"json": js, "sqlite": sq} {
		t.Run(name, func(t *testing.T) {
			now := time.UnixMilli(5_000_000)
			id := "projects/p1-hello-cargo"
			s := &Service{Store: st, Steps: steps{id: {"scaffold", "commit"}}, Now: func() time.Time { return now }}
			ctx := context.Background()
			var generation int64
			lap := func(ms int64, at int64) error {
				return s.Apply(ctx, "u", protocol.ProgressReport{LessonID: id, StepID: "commit", Status: protocol.ProgressDone, LapMs: ms, At: at, Generation: generation})
			}
			if err := lap(90_000, 10); err != nil {
				t.Fatal(err)
			}
			if err := lap(60_000, 20); err != nil {
				t.Fatal(err)
			}
			if err := lap(75_000, 30); err != nil {
				t.Fatal(err)
			}
			p, _ := s.Get(ctx, "u")
			l := p.Lessons[id].Laps
			if l == nil || *l != (protocol.LapStats{Count: 3, BestMs: 60_000, LastMs: 75_000, LastAt: 30}) {
				t.Fatalf("laps %+v", l)
			}
			// Only with the last step's done report.
			err := s.Apply(ctx, "u", protocol.ProgressReport{LessonID: id, StepID: "scaffold", Status: protocol.ProgressDone, LapMs: 1})
			if httpx.AsError(err).Status != 400 {
				t.Fatalf("lap on another step: %v", err)
			}
			// A lesson reset clears its laps.
			generation, err = s.Reset(ctx, "u", id)
			if err != nil {
				t.Fatal(err)
			}
			p, _ = s.Get(ctx, "u")
			if p.Lessons[id].Laps != nil {
				t.Fatalf("laps after reset %+v", p.Lessons[id])
			}
			if err := lap(1000, 40); err != nil {
				t.Fatal(err)
			}
			p, _ = s.Get(ctx, "u")
			if l := p.Lessons[id].Laps; l == nil || l.Count != 1 {
				t.Fatalf("new lap after reset %+v", l)
			}
			_, _ = s.Reset(ctx, "u", "")
			if p, _ = s.Get(ctx, "u"); len(p.Lessons) != 0 {
				t.Fatalf("after start over %+v", p.Lessons)
			}
		})
	}
}

// A lap reported twice counts once: by its id, or for reports without one
// (an older sessiond) by (at, ms).
func TestLapDedupe(t *testing.T) {
	dir := t.TempDir()
	js, err := store.OpenJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	sq, err := store.OpenSQLite(filepath.Join(dir, "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sq.Close()
	for name, st := range map[string]store.Store{"json": js, "sqlite": sq} {
		t.Run(name, func(t *testing.T) {
			id := "projects/p1-hello-cargo"
			s := &Service{Store: st, Steps: steps{id: {"scaffold", "commit"}}}
			ctx := context.Background()
			lap := func(ms, at int64, lapID string) {
				t.Helper()
				if err := s.Apply(ctx, "u", protocol.ProgressReport{LessonID: id, StepID: "commit", Status: protocol.ProgressDone,
					LapMs: ms, At: at, LapID: lapID}); err != nil {
					t.Fatal(err)
				}
			}
			count := func() int {
				t.Helper()
				p, _ := s.Get(ctx, "u")
				return p.Lessons[id].Laps.Count
			}
			lap(90_000, 10, protocol.LapID(id, 1))
			lap(90_000, 10, protocol.LapID(id, 1))
			// The same lap resent after a restart keeps its id, whatever
			// else differs.
			lap(90_000, 11, protocol.LapID(id, 1))
			if n := count(); n != 1 {
				t.Fatalf("one lap by id counted %d times", n)
			}
			lap(80_000, 20, protocol.LapID(id, 15))
			if n := count(); n != 2 {
				t.Fatalf("a second lap: %d", n)
			}
			lap(70_000, 30, "")
			lap(70_000, 30, "")
			if n := count(); n != 3 {
				t.Fatalf("a lap without an id counted by (at, ms): %d", n)
			}
			lap(70_000, 31, "")
			if n := count(); n != 4 {
				t.Fatalf("another lap without an id: %d", n)
			}
			for _, bad := range []string{"other/lesson@5", id + "@", id + "@5x", id + "@" + strings.Repeat("9", 20)} {
				err := s.Apply(ctx, "u", protocol.ProgressReport{LessonID: id, StepID: "commit", Status: protocol.ProgressDone, LapMs: 1, LapID: bad})
				if httpx.AsError(err).Status != 400 {
					t.Fatalf("lapId %q: %v", bad, err)
				}
			}
			p, _ := s.Get(ctx, "u")
			if l := p.Lessons[id].Laps; l.BestMs != 70_000 || l.LastAt != 31 {
				t.Fatalf("laps %+v", l)
			}
		})
	}
}
