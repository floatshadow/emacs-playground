package progress

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
