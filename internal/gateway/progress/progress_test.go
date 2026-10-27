package progress

import (
	"context"
	"encoding/json"
	"errors"
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
	if err := s.Reset(ctx, "u1", "00-a/01-b"); err != nil {
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
