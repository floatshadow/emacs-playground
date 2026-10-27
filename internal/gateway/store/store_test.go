package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestJSONStore(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	testStore(t, s)
	// Data survives a reopen, and the file is private and readable JSON.
	s2, err := OpenJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	checkPersisted(t, s2)
	st, err := os.Stat(filepath.Join(dir, JSONFile))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("local.json: %v %v", err, st.Mode())
	}
	data, _ := os.ReadFile(filepath.Join(dir, JSONFile))
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || raw["progress"] == nil {
		t.Fatalf("local.json is not the documented shape: %v", err)
	}
}

func TestSQLiteStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db", SQLiteFile)
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	testStore(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopening applies no migration twice and keeps the data.
	s2, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	checkPersisted(t, s2)
}

func testStore(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()

	// Users.
	if _, err := s.Users().Get(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	u := &User{ID: "u1", Kind: protocol.UserInvite, DisplayName: "cohort-A", CreatedAt: 100}
	if err := s.Users().Put(ctx, u); err != nil {
		t.Fatal(err)
	}
	// Two users without subject must not collide on the unique index.
	if err := s.Users().Put(ctx, &User{ID: "u2", Kind: protocol.UserInvite, CreatedAt: 101}); err != nil {
		t.Fatal(err)
	}
	if err := s.Users().Put(ctx, &User{ID: "o1", Kind: protocol.UserOIDC, Subject: "https://idp|42", DisplayName: "Ada", CreatedAt: 102}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Users().BySubject(ctx, "https://idp|42"); err != nil || got.ID != "o1" {
		t.Fatalf("by subject: %v %+v", err, got)
	}
	if _, err := s.Users().BySubject(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty subject: %v", err)
	}
	if err := s.Users().Touch(ctx, "u1", 500); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Users().Get(ctx, "u1"); got.LastSeenAt != 500 || got.DisplayName != "cohort-A" || got.Kind != protocol.UserInvite {
		t.Fatalf("user %+v", got)
	}
	if err := s.Users().Touch(ctx, "nobody", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("touch missing: %v", err)
	}

	// Invites: sync, redeem, use counting, expiry, removal.
	invs := []Invite{
		{CodeHash: "h1", Label: "cohort-A", MaxUses: 2},
		{CodeHash: "h2", Label: "old", ExpiresAt: 1000},
		{CodeHash: "h3"},
	}
	if err := s.Invites().Sync(ctx, invs); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		inv, err := s.Invites().Redeem(ctx, "h1", 10)
		if err != nil || inv.Uses != i+1 || inv.Label != "cohort-A" {
			t.Fatalf("redeem %d: %v %+v", i, err, inv)
		}
	}
	if _, err := s.Invites().Redeem(ctx, "h1", 10); !errors.Is(err, ErrInviteUsedUp) {
		t.Fatalf("used up: %v", err)
	}
	if _, err := s.Invites().Redeem(ctx, "h2", 1000); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired: %v", err)
	}
	if _, err := s.Invites().Redeem(ctx, "h2", 999); err != nil {
		t.Fatalf("not yet expired: %v", err)
	}
	if _, err := s.Invites().Redeem(ctx, "nope", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	// Re-sync keeps use counts, updates limits and drops removed codes.
	if err := s.Invites().Sync(ctx, []Invite{{CodeHash: "h1", Label: "cohort-A", MaxUses: 3}, {CodeHash: "h3"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Invites().Redeem(ctx, "h2", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed code still works: %v", err)
	}
	if inv, err := s.Invites().Redeem(ctx, "h1", 10); err != nil || inv.Uses != 3 {
		t.Fatalf("raised limit: %v %+v", err, inv)
	}
	list, _ := s.Invites().List(ctx)
	if len(list) != 2 {
		t.Fatalf("invites %+v", list)
	}

	// Sessions.
	sess := &Session{ID: "s1", UserID: "u1", State: protocol.SessionReady, ContainerID: "c1", Volume: "pg-ws-u1", Image: "img:1", TokenHash: "th", CreatedAt: 200, LastActiveAt: 250}
	if err := s.Sessions().Put(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := s.Sessions().Put(ctx, &Session{ID: "s0", UserID: "u1", State: protocol.SessionRemoved, TokenHash: "x", CreatedAt: 300}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Sessions().ByUser(ctx, "u1"); err != nil || got.ID != "s1" || got.Volume != "pg-ws-u1" || got.LastActiveAt != 250 {
		t.Fatalf("by user: %v %+v", err, got)
	}
	sess.State, sess.StoppedAt, sess.Failure = protocol.SessionFailed, 400, "boom"
	if err := s.Sessions().Put(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Sessions().Get(ctx, "s1"); got.State != protocol.SessionFailed || got.Failure != "boom" || got.StoppedAt != 400 {
		t.Fatalf("updated session %+v", got)
	}
	if _, err := s.Sessions().ByUser(ctx, "u2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no session: %v", err)
	}
	if all, _ := s.Sessions().List(ctx); len(all) != 2 {
		t.Fatalf("sessions %+v", all)
	}

	// Progress.
	recs := []StepRecord{
		{UserID: "u1", LessonID: "00-a/01-b", StepID: "open", Status: protocol.ProgressDone, HintsUsed: 1, CompletedAt: 600, UpdatedAt: 600},
		{UserID: "u1", LessonID: "00-a/01-b", StepID: "save", Status: protocol.ProgressInProgress, UpdatedAt: 601},
		{UserID: "u1", LessonID: "00-a/02-c", StepID: "x", Status: protocol.ProgressDone, Resets: 2, CompletedAt: 602, UpdatedAt: 602},
		{UserID: "u2", LessonID: "00-a/01-b", StepID: "open", Status: protocol.ProgressInProgress, UpdatedAt: 603},
	}
	for _, r := range recs {
		if err := s.Progress().Put(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	recs[1].Status, recs[1].CompletedAt, recs[1].UpdatedAt = protocol.ProgressDone, 700, 700
	if err := s.Progress().Put(ctx, recs[1]); err != nil {
		t.Fatal(err)
	}
	got, err := s.Progress().List(ctx, "u1")
	if err != nil || len(got) != 3 {
		t.Fatalf("progress %v %+v", err, got)
	}
	if got[1].StepID != "save" || got[1].Status != protocol.ProgressDone || got[1].CompletedAt != 700 || got[2].Resets != 2 || got[0].HintsUsed != 1 {
		t.Fatalf("progress rows %+v", got)
	}
	if err := s.Progress().DeleteLesson(ctx, "u1", "00-a/02-c"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Progress().List(ctx, "u1"); len(got) != 2 {
		t.Fatalf("after delete %+v", got)
	}
	if got, _ := s.Progress().List(ctx, "u2"); len(got) != 1 {
		t.Fatalf("other user touched %+v", got)
	}

	// Stuck reports and audit.
	id1, err := s.Stuck().Add(ctx, &StuckReport{UserID: "u1", LessonID: "00-a/01-b", StepID: "open", CreatedAt: 800, Note: "help", Events: json.RawMessage(`[{"t":"key"}]`)})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Stuck().Add(ctx, &StuckReport{UserID: "u1", CreatedAt: 801})
	if err != nil || id2 <= id1 {
		t.Fatalf("ids %d %d %v", id1, id2, err)
	}
	reps, _ := s.Stuck().List(ctx, 10)
	if len(reps) != 2 || reps[0].ID != id2 || string(reps[1].Events) != `[{"t":"key"}]` || reps[1].Note != "help" || string(reps[0].Events) != "[]" {
		t.Fatalf("stuck %+v", reps)
	}
	for i, a := range []string{"login", "session.create", "session.stop"} {
		if err := s.Audit(ctx, AuditEvent{At: int64(900 + i), UserID: "u1", Action: a, Details: "d"}); err != nil {
			t.Fatal(err)
		}
	}
	log, _ := s.AuditLog(ctx, 2)
	if len(log) != 2 || log[0].Action != "session.stop" || log[0].UserID != "u1" {
		t.Fatalf("audit %+v", log)
	}

	// Concurrent writers do not lose updates or fail with busy errors.
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.Progress().Put(ctx, StepRecord{UserID: "u2", LessonID: "00-a/09-z", StepID: string(rune('a' + i)), Status: protocol.ProgressDone, UpdatedAt: 1})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.Progress().List(ctx, "u2"); len(got) != 21 {
		t.Fatalf("concurrent puts: %d rows", len(got))
	}
}

func checkPersisted(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	if u, err := s.Users().Get(ctx, "u1"); err != nil || u.LastSeenAt != 500 {
		t.Fatalf("persisted user %v %+v", err, u)
	}
	if got, _ := s.Progress().List(ctx, "u1"); len(got) != 2 {
		t.Fatalf("persisted progress %+v", got)
	}
	if inv, err := s.Invites().Redeem(ctx, "h1", 10); !errors.Is(err, ErrInviteUsedUp) {
		t.Fatalf("persisted invite uses: %v %+v", err, inv)
	}
	if reps, _ := s.Stuck().List(ctx, 0); len(reps) != 2 {
		t.Fatalf("persisted stuck %d", len(reps))
	}
}
