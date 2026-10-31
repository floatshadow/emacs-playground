package progress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

type steps map[string][]string

func (s steps) StepIDs(id string) ([]string, bool) { v, ok := s[id]; return v, ok }
func (s steps) Loaded() (bool, string)             { return s != nil, "test" }

func ip(n int) *int { return &n }

func TestProgress(t *testing.T) {
	st, err := store.OpenJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1_000_000)
	s := &Service{Store: st, Steps: steps{"00-a/01-b": {"open", "save"}}, Now: func() time.Time { return now }}
	ctx := context.Background()

	if err := s.Apply(ctx, "u1", protocol.ProgressReport{LessonID: "00-a/01-b", StepID: "open", Status: protocol.ProgressInProgress, HintsUsed: ip(1)}); err != nil {
		t.Fatal(err)
	}
	p, _ := s.Get(ctx, "u1")
	if lp := p.Lessons["00-a/01-b"]; lp.Status != protocol.ProgressInProgress || lp.Steps["open"].HintsUsed != 1 || p.UpdatedAt != now.UnixMilli() {
		t.Fatalf("in progress %+v", p)
	}
	// Absolute totals, not increments.
	_ = s.Apply(ctx, "u1", protocol.ProgressReport{LessonID: "00-a/01-b", StepID: "open", Status: protocol.ProgressInProgress, HintsUsed: ip(2), Resets: ip(1)})
	if err := s.Apply(ctx, "u1", protocol.ProgressReport{LessonID: "00-a/01-b", StepID: "open", Status: protocol.ProgressDone, At: 42}); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Get(ctx, "u1")
	o := p.Lessons["00-a/01-b"].Steps["open"]
	if o.Status != protocol.ProgressDone || o.HintsUsed != 2 || o.Resets != 1 || o.CompletedAt != 42 {
		t.Fatalf("done %+v", o)
	}
	// A later in-progress report does not undo done.
	_ = s.Apply(ctx, "u1", protocol.ProgressReport{LessonID: "00-a/01-b", StepID: "open", Status: protocol.ProgressInProgress, HintsUsed: ip(3)})
	p, _ = s.Get(ctx, "u1")
	if o := p.Lessons["00-a/01-b"].Steps["open"]; o.Status != protocol.ProgressDone || o.HintsUsed != 3 {
		t.Fatalf("downgraded %+v", o)
	}
	if p.Lessons["00-a/01-b"].Status != protocol.ProgressInProgress {
		t.Fatal("lesson must not be done with a step left")
	}
	_ = s.Apply(ctx, "u1", protocol.ProgressReport{LessonID: "00-a/01-b", StepID: "save", Status: protocol.ProgressDone})
	p, _ = s.Get(ctx, "u1")
	if p.Lessons["00-a/01-b"].Status != protocol.ProgressDone || p.Lessons["00-a/01-b"].Steps["save"].CompletedAt != now.UnixMilli() {
		t.Fatalf("lesson done %+v", p.Lessons["00-a/01-b"])
	}
	// Validation.
	var he *httpx.Error
	for _, bad := range []protocol.ProgressReport{
		{LessonID: "00-a/01-b", StepID: "nope", Status: protocol.ProgressDone},
		{LessonID: "00-a/01-b", Status: protocol.ProgressDone},
		{LessonID: "00-a/01-b", StepID: "open", Status: "finished"},
		{LessonID: "00-a/01-b", StepID: "open", Status: protocol.ProgressDone, HintsUsed: ip(-1)},
		// A lesson the loaded bundle does not have: rows only for real
		// lessons, so a session cannot grow the store with made-up ids.
		{LessonID: "09-x/01-y", StepID: "a", Status: protocol.ProgressDone},
		{LessonID: string(make([]byte, 300)), StepID: "a", Status: protocol.ProgressDone},
	} {
		if err := s.Apply(ctx, "u1", bad); !errors.As(err, &he) || he.Status != 400 {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	// Without a bundle nothing can be checked: unknown lessons are recorded.
	unloaded := &Service{Store: st, Steps: steps(nil), Now: s.Now}
	if err := unloaded.Apply(ctx, "u1", protocol.ProgressReport{LessonID: "09-x/01-y", StepID: "a", Status: protocol.ProgressDone}); err != nil {
		t.Fatal(err)
	}
	// Reset clears one lesson only.
	if _, err := s.Reset(ctx, "u1", "00-a/01-b"); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Get(ctx, "u1")
	if _, ok := p.Lessons["00-a/01-b"]; ok || len(p.Lessons) != 1 {
		t.Fatalf("after reset %+v", p)
	}
	// Other users are separate; empty progress is an empty map.
	p, _ = s.Get(ctx, "u2")
	b, _ := json.Marshal(p)
	if string(b) != `{"lessons":{},"updatedAt":0}` {
		t.Fatalf("empty %s", b)
	}
}

func TestStuckDedupe(t *testing.T) {
	st, _ := store.OpenJSON(t.TempDir())
	now := time.Unix(1000, 0)
	s := NewStuck(st)
	s.Now = func() time.Time { return now }
	ctx := context.Background()
	ev := []json.RawMessage{json.RawMessage(`{"t":"key"}`)}
	id1, err := s.File(ctx, "u1", "l/a", "open", "help", ev)
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := s.File(ctx, "u1", "l/a", "open", "help", nil)
	if id1 != id2 {
		t.Fatal("duplicate within 30 s not merged")
	}
	now = now.Add(time.Minute)
	id3, _ := s.File(ctx, "u1", "l/a", "open", "help", ev)
	if id3 == id1 {
		t.Fatal("later report merged")
	}
	many := make([]json.RawMessage, 300)
	for i := range many {
		many[i] = json.RawMessage(`1`)
	}
	_, _ = s.File(ctx, "u2", "", "", "x", many)
	reps, _ := st.Stuck().List(ctx, 1)
	var got []json.RawMessage
	_ = json.Unmarshal(reps[0].Events, &got)
	if len(got) != MaxStuckEvents {
		t.Fatalf("events kept %d", len(got))
	}
	// Big events: only the newest that fit in MaxStuckEventBytes; the note
	// is cut on a rune boundary.
	big := make([]json.RawMessage, 50)
	for i := range big {
		big[i] = json.RawMessage(`"` + strings.Repeat("x", 20<<10) + `"`)
	}
	big[49] = json.RawMessage(`"last"`)
	_, _ = s.File(ctx, "u3", "", "", strings.Repeat("é", 3000), big)
	reps, _ = st.Stuck().List(ctx, 1)
	got = nil
	_ = json.Unmarshal(reps[0].Events, &got)
	if len(reps[0].Events) > MaxStuckEventBytes+len(got)+2 || len(got) == 0 || string(got[len(got)-1]) != `"last"` {
		t.Fatalf("big events: %d kept, %d bytes", len(got), len(reps[0].Events))
	}
	if n := len(reps[0].Note); n > MaxStuckNote || !utf8.ValidString(reps[0].Note) {
		t.Fatalf("note %d bytes, valid %v", n, utf8.ValidString(reps[0].Note))
	}
	for i := 0; i < 10; i++ {
		s.Allow("u9")
	}
	if s.Allow("u9") {
		t.Fatal("rate limit")
	}
}

// gatedStuck holds every Add until gate is closed and counts the adds.
type gatedStuck struct {
	store.StuckRepo
	gate    chan struct{}
	entered chan struct{}
	fail    bool
	mu      sync.Mutex
	adds    int
}

func (g *gatedStuck) Add(ctx context.Context, r *store.StuckReport) (int64, error) {
	g.mu.Lock()
	g.adds++
	fail := g.fail
	g.mu.Unlock()
	g.entered <- struct{}{}
	<-g.gate
	if fail {
		return 0, errors.New("disk full")
	}
	return g.StuckRepo.Add(ctx, r)
}

type gatedStore struct {
	store.Store
	stuck *gatedStuck
}

func (g gatedStore) Stuck() store.StuckRepo { return g.stuck }

func TestStuckDedupeConcurrent(t *testing.T) {
	base, _ := store.OpenJSON(t.TempDir())
	g := &gatedStuck{StuckRepo: base.Stuck(), gate: make(chan struct{}), entered: make(chan struct{}, 8)}
	s := NewStuck(gatedStore{base, g})
	ctx := context.Background()

	// Identical reports filed while the first is being written share its id.
	const n = 5
	ids, errs := make([]int64, n), make([]error, n)
	var wg sync.WaitGroup
	note := "help"
	file := func(i int) {
		defer wg.Done()
		ids[i], errs[i] = s.File(ctx, "u1", "l/a", "open", note, nil)
	}
	wg.Add(1)
	go file(0)
	<-g.entered
	for i := 1; i < n; i++ {
		wg.Add(1)
		go file(i)
	}
	// The others must be waiting on the first write, not writing.
	time.Sleep(50 * time.Millisecond)
	close(g.gate)
	wg.Wait()
	for i := range n {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("report %d: id %d (first %d), err %v", i, ids[i], ids[0], errs[i])
		}
	}
	if g.adds != 1 {
		t.Fatalf("%d reports stored, want 1", g.adds)
	}

	// A failed write is shared with the reports waiting on it and then
	// forgotten, so the next report is written.
	g.gate, g.fail, note = make(chan struct{}), true, "other"
	wg.Add(2)
	go file(0)
	<-g.entered
	go file(1)
	time.Sleep(50 * time.Millisecond)
	close(g.gate)
	wg.Wait()
	if errs[0] == nil || errs[1] == nil {
		t.Fatalf("errors %v, %v; want both failed", errs[0], errs[1])
	}
	g.fail = false
	if _, err := s.File(ctx, "u1", "l/a", "open", "other", nil); err != nil {
		t.Fatal(err)
	}
	if g.adds != 3 {
		t.Fatalf("%d adds, want 3 (a failed report is written again)", g.adds)
	}
}

func TestProgressRetryAndReset(t *testing.T) {
	for _, kind := range []string{"json", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			open := func() store.Store {
				var st store.Store
				var err error
				if kind == "json" {
					st, err = store.OpenJSON(dir)
				} else {
					st, err = store.OpenSQLite(filepath.Join(dir, "test.db"))
				}
				if err != nil {
					t.Fatal(err)
				}
				return st
			}
			st := open()
			now := time.UnixMilli(200)
			s := &Service{Store: st, Steps: steps{"a/b": {"one"}, "a/c": {"one"}}, Now: func() time.Time { return now }}
			ctx := context.Background()
			rep := protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 100, LapMs: 50}
			for range 2 {
				if err := s.Apply(ctx, "u", rep); err != nil {
					t.Fatal(err)
				}
			}
			laps, _ := st.Progress().Laps(ctx, "u")
			if len(laps) != 1 || laps[0].Count != 1 {
				t.Fatalf("duplicate lap %+v", laps)
			}
			rep.At = 150
			if err := s.Apply(ctx, "u", rep); err != nil {
				t.Fatal(err)
			}
			laps, _ = st.Progress().Laps(ctx, "u")
			if len(laps) != 1 || laps[0].Count != 2 {
				t.Fatalf("second lap %+v", laps)
			}
			for _, lessonID := range []string{"a/b", ""} {
				generation, err := s.Reset(ctx, "u", lessonID)
				if err != nil {
					t.Fatal(err)
				}
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				st = open()
				s.Store = st
				for _, at := range []int64{0, 150, 9_000_000_000_000} {
					rep.At = at
					if err := s.Apply(ctx, "u", rep); err != nil {
						t.Fatal(err)
					}
				}
				records, _ := st.Progress().List(ctx, "u")
				laps, _ = st.Progress().Laps(ctx, "u")
				if len(records) != 0 || len(laps) != 0 {
					t.Fatalf("stale report survived reset: %+v %+v", records, laps)
				}
				rep.Generation = generation
				rep.At = 1
				if err := s.Apply(ctx, "u", rep); err != nil {
					t.Fatal(err)
				}
				records, _ = st.Progress().List(ctx, "u")
				if len(records) != 1 {
					t.Fatalf("new report lost: %+v", records)
				}
				// Without a production timestamp, completion uses receipt time.
				generation, err = s.Reset(ctx, "u", lessonID)
				if err != nil {
					t.Fatal(err)
				}
				rep.Generation = generation
				rep.At = 0
				if err := s.Apply(ctx, "u", rep); err != nil {
					t.Fatal(err)
				}
				records, _ = st.Progress().List(ctx, "u")
				if len(records) != 1 || records[0].CompletedAt != now.UnixMilli() {
					t.Fatalf("report without a timestamp: %+v", records)
				}
			}
			st.Close()
		})
	}
}

func TestProgressAbsoluteTotals(t *testing.T) {
	st, err := store.OpenJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Service{Store: st}
	for _, n := range []int{5, 1, 0} {
		if err := s.Apply(context.Background(), "u", protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressInProgress, HintsUsed: &n, Resets: &n}); err != nil {
			t.Fatal(err)
		}
		p, err := s.Get(context.Background(), "u")
		if err != nil {
			t.Fatal(err)
		}
		if step := p.Lessons["a/b"].Steps["one"]; step.HintsUsed != n || step.Resets != n {
			t.Fatalf("totals %+v, want %d", step, n)
		}
	}
}

func TestProgressLocksPerUser(t *testing.T) {
	st, err := store.OpenJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Service{Store: st}
	unlock := s.lockUser("one")
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	done := make(chan error, 1)
	go func() {
		done <- s.Apply(context.Background(), "two", protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("another user is blocked")
	}
	unlock()
	unlock = nil
	if len(s.users) != 0 {
		t.Fatal("idle user locks retained")
	}
}

func TestGatewayGenerationRecovery(t *testing.T) {
	for _, kind := range []string{"json", "sqlite"} {
		for _, scope := range []string{"", "a/b"} {
			t.Run(kind+"/"+scope, func(t *testing.T) {
				var st store.Store
				var err error
				if kind == "json" {
					st, err = store.OpenJSON(t.TempDir())
				} else {
					st, err = store.OpenSQLite(filepath.Join(t.TempDir(), "progress.db"))
				}
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				var logs bytes.Buffer
				now := time.UnixMilli(1_800_000_000_000)
				s := &Service{Store: st, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Now: func() time.Time { return now }}
				ctx := context.Background()
				rep := protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, Generation: 18}
				if err := s.Apply(ctx, "u", rep); err != nil {
					t.Fatal(err)
				}
				p, err := s.Get(ctx, "u")
				if err != nil || p.Lessons["a/b"].Steps["one"].Status != protocol.ProgressDone {
					t.Fatalf("progress %+v: %v", p, err)
				}
				generations, err := st.Progress().Generations(ctx, "u")
				if err != nil || generations.ForLesson("a/b") != 0 {
					t.Fatalf("gateway generations %+v: %v", generations, err)
				}
				generation, err := s.Reset(ctx, "u", scope)
				if err != nil {
					t.Fatal(err)
				}
				if generation != now.UnixMilli() || generation <= rep.Generation {
					t.Fatalf("reset generation %d does not supersede session generation %d", generation, rep.Generation)
				}
				rep.Generation = 0
				if err := s.Apply(ctx, "u", rep); err != nil {
					t.Fatal(err)
				}
				p, err = s.Get(ctx, "u")
				if err != nil || len(p.Lessons) != 0 {
					t.Fatalf("stale progress %+v: %v", p, err)
				}
				var entry map[string]any
				if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
					t.Fatal(err)
				}
				for key, want := range map[string]any{"level": "WARN", "user_id": "u", "lesson": "a/b", "step": "one", "report_generation": float64(0), "gateway_generation": float64(generation)} {
					if entry[key] != want {
						t.Errorf("log %s=%v, want %v", key, entry[key], want)
					}
				}
				rep.Generation = generation
				if err := s.Apply(ctx, "u", rep); err != nil {
					t.Fatal(err)
				}
				p, err = s.Get(ctx, "u")
				if err != nil || p.Lessons["a/b"].Steps["one"].Status != protocol.ProgressDone {
					t.Fatalf("completion after reset %+v: %v", p, err)
				}
			})
		}
	}
}

func TestResetGenerationClock(t *testing.T) {
	st, err := store.OpenJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Service{Store: st}
	for _, tc := range []struct {
		name, scope string
		now, want   int64
	}{
		{"first reset", "a/b", 1000, 1000},
		{"same millisecond", "a/c", 1000, 1001},
		{"clock behind", "a/b", 900, 1002},
		{"clock ahead", "", 2000, 2000},
		{"lesson after user reset", "a/b", 2000, 2001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.Now = func() time.Time { return time.UnixMilli(tc.now) }
			generation, err := s.Reset(context.Background(), "u", tc.scope)
			if err != nil || generation != tc.want {
				t.Fatalf("generation %d, want %d: %v", generation, tc.want, err)
			}
		})
	}
}
