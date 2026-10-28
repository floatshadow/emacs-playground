package progress

import (
	"context"
	"path/filepath"
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
			lap := func(ms int64, at int64) error {
				return s.Apply(ctx, "u", protocol.ProgressReport{LessonID: id, StepID: "commit", Status: protocol.ProgressDone, LapMs: ms, At: at})
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
			if err := s.Reset(ctx, "u", id); err != nil {
				t.Fatal(err)
			}
			p, _ = s.Get(ctx, "u")
			if p.Lessons[id].Laps != nil {
				t.Fatalf("laps after reset %+v", p.Lessons[id])
			}
			_ = lap(1000, 40)
			_ = s.Reset(ctx, "u", "")
			if p, _ = s.Get(ctx, "u"); len(p.Lessons) != 0 {
				t.Fatalf("after start over %+v", p.Lessons)
			}
		})
	}
}
