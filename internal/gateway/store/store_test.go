package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	u := &User{ID: "u1", Kind: protocol.UserOIDC, Subject: "https://idp|1", DisplayName: "cohort-A", CreatedAt: 100}
	if err := s.Users().Put(ctx, u); err != nil {
		t.Fatal(err)
	}
	// Two users without subject must not collide on the unique index.
	if err := s.Users().Put(ctx, &User{ID: "u2", Kind: protocol.UserLocal, CreatedAt: 101}); err != nil {
		t.Fatal(err)
	}
	if err := s.Users().Put(ctx, &User{ID: "u3", Kind: protocol.UserLocal, CreatedAt: 101}); err != nil {
		t.Fatal(err)
	}
	o1 := &User{ID: "o1", Kind: protocol.UserOIDC, Subject: "https://idp|42", DisplayName: "Ada", Username: "ada", Email: "ada@example.edu", Groups: []string{"cs101", "staff"}, CreatedAt: 102}
	if err := s.Users().Put(ctx, o1); err != nil {
		t.Fatal(err)
	}
	o1.Groups[0] = "mutated"
	if got, err := s.Users().BySubject(ctx, "https://idp|42"); err != nil || got.ID != "o1" || got.Username != "ada" ||
		got.Email != "ada@example.edu" || strings.Join(got.Groups, ",") != "cs101,staff" {
		t.Fatalf("by subject: %v %+v", err, got)
	}
	// A login refreshes the claims: groups can shrink to none.
	o1.Groups, o1.Email = nil, "ada@new.example.edu"
	if err := s.Users().Put(ctx, o1); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Users().Get(ctx, "o1"); got.Groups != nil || got.Email != "ada@new.example.edu" {
		t.Fatalf("refreshed %+v", got)
	}
	if _, err := s.Users().BySubject(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty subject: %v", err)
	}
	if err := s.Users().Touch(ctx, "u1", 500); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Users().Get(ctx, "u1"); got.LastSeenAt != 500 || got.DisplayName != "cohort-A" || got.Kind != protocol.UserOIDC {
		t.Fatalf("user %+v", got)
	}
	if err := s.Users().Touch(ctx, "nobody", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("touch missing: %v", err)
	}
	// RevokeSessions increments the epoch, and Put (a login refreshing the
	// user from an older read) never resets it.
	stale, _ := s.Users().Get(ctx, "u1")
	if err := s.Users().RevokeSessions(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Users().Put(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Users().Get(ctx, "u1"); got.SessionEpoch != stale.SessionEpoch+1 {
		t.Fatalf("epoch %d after revoke, was %d", got.SessionEpoch, stale.SessionEpoch)
	}
	if err := s.Users().RevokeSessions(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke missing: %v", err)
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
	if u, err := s.Users().Get(ctx, "o1"); err != nil || u.Email != "ada@new.example.edu" {
		t.Fatalf("persisted claims %v %+v", err, u)
	}
	if reps, _ := s.Stuck().List(ctx, 0); len(reps) != 2 {
		t.Fatalf("persisted stuck %d", len(reps))
	}
}

// TestSQLiteDropsOldSignInCodes: databases of earlier releases lose the
// sign-in code table and gain the claim columns (migration 0003).
func TestSQLiteDropsOldSignInCodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), SQLiteFile)
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	db := s.(*sqlStore).db
	ctx := context.Background()
	// Rewind to the schema before 0003.
	for _, q := range []string{
		`CREATE TABLE invites (code_hash TEXT PRIMARY KEY, label TEXT, max_uses INTEGER, uses INTEGER NOT NULL DEFAULT 0, expires_at INTEGER)`,
		`INSERT INTO invites (code_hash, uses) VALUES ('h', 1)`,
		`ALTER TABLE users DROP COLUMN username`,
		`ALTER TABLE users DROP COLUMN email`,
		`ALTER TABLE users DROP COLUMN groups_json`,
		`ALTER TABLE users DROP COLUMN session_epoch`,
		`INSERT INTO users (id, kind, display_name, created_at) VALUES ('u-old', 'oidc', 'Old', 1)`,
		`DELETE FROM schema_migrations WHERE version >= 3`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	s, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db = s.(*sqlStore).db
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'invites'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sign-in code table still there: %d %v", n, err)
	}
	if u, err := s.Users().Get(ctx, "u-old"); err != nil || u.DisplayName != "Old" || u.Groups != nil {
		t.Fatalf("old user %v %+v", err, u)
	}
}

// TestJSONDropsOldSignInCodes: opening a version 1 local.json rewrites it
// without the removed data.
func TestJSONDropsOldSignInCodes(t *testing.T) {
	dir := t.TempDir()
	old := `{"version":1,"users":{"local":{"id":"local","kind":"local","displayName":"You","createdAt":1}},` +
		`"invites":{"h":{"codeHash":"h","uses":1}},"sessions":{},"progress":{}}`
	if err := os.WriteFile(filepath.Join(dir, JSONFile), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if u, err := s.Users().Get(context.Background(), "local"); err != nil || u.DisplayName != "You" {
		t.Fatalf("user lost: %v %+v", err, u)
	}
	data, _ := os.ReadFile(filepath.Join(dir, JSONFile))
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || raw["invites"] != nil || raw["version"] != float64(jsonVersion) {
		t.Fatalf("not rewritten: %v %s", err, data)
	}
}
