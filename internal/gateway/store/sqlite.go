package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// SQLiteFile is the server-mode database file name under the data directory.
const SQLiteFile = "playground.db"

//go:embed migrations/*.sql
var migrations embed.FS

type sqlStore struct{ db *sql.DB }

// OpenSQLite opens (or creates) the database at path, applies pending
// migrations and returns the store. One connection serialises writers;
// WAL lets readers proceed, busy_timeout absorbs short contention.
func OpenSQLite(path string) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &sqlStore{db: db}, nil
}

// migrate applies migrations/NNNN_name.sql files not yet recorded in
// schema_migrations, each in its own transaction.
func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("store: migrations table: %w", err)
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		num, _, ok := strings.Cut(name, "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			return fmt.Errorf("store: migration %s: name must be NNNN_description.sql", name)
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, v).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := fs.ReadFile(migrations, "migrations/"+name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`, v, name, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqlStore) Users() UserRepo        { return sqlUsers{s.db} }
func (s *sqlStore) Sessions() SessionRepo  { return sqlSessions{s.db} }
func (s *sqlStore) Progress() ProgressRepo { return sqlProgress{s.db} }
func (s *sqlStore) Stuck() StuckRepo       { return sqlStuck{s.db} }
func (s *sqlStore) Invites() InviteRepo    { return sqlInvites{s.db} }
func (s *sqlStore) Close() error           { return s.db.Close() }

func (s *sqlStore) Audit(ctx context.Context, e AuditEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit (at, user_id, action, details) VALUES (?, ?, ?, ?)`,
		e.At, nullStr(e.UserID), e.Action, nullStr(e.Details))
	return err
}

func (s *sqlStore) AuditLog(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := s.db.QueryContext(ctx, `SELECT at, user_id, action, details FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var uid, det sql.NullString
		if err := rows.Scan(&e.At, &uid, &e.Action, &det); err != nil {
			return nil, err
		}
		e.UserID, e.Details = uid.String, det.String
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }
func nullInt(n int64) sql.NullInt64   { return sql.NullInt64{Int64: n, Valid: n != 0} }

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type sqlUsers struct{ db *sql.DB }

const userCols = `id, kind, subject, display_name, created_at, last_seen_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var subj, name sql.NullString
	var seen sql.NullInt64
	var kind string
	if err := row.Scan(&u.ID, &kind, &subj, &name, &u.CreatedAt, &seen); err != nil {
		return nil, notFound(err)
	}
	u.Kind = userKind(kind)
	u.Subject, u.DisplayName, u.LastSeenAt = subj.String, name.String, seen.Int64
	return &u, nil
}

func (r sqlUsers) Get(ctx context.Context, id string) (*User, error) {
	return scanUser(r.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (r sqlUsers) BySubject(ctx context.Context, subject string) (*User, error) {
	if subject == "" {
		return nil, ErrNotFound
	}
	return scanUser(r.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE subject = ?`, subject))
}

func (r sqlUsers) Put(ctx context.Context, u *User) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO users (`+userCols+`) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET kind = excluded.kind, subject = excluded.subject,
  display_name = excluded.display_name, created_at = excluded.created_at, last_seen_at = excluded.last_seen_at`,
		u.ID, string(u.Kind), nullStr(u.Subject), nullStr(u.DisplayName), u.CreatedAt, nullInt(u.LastSeenAt))
	return err
}

func (r sqlUsers) Touch(ctx context.Context, id string, at int64) error {
	res, err := r.db.ExecContext(ctx, `UPDATE users SET last_seen_at = ? WHERE id = ?`, at, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type sqlInvites struct{ db *sql.DB }

func (r sqlInvites) Sync(ctx context.Context, list []Invite) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	keep := map[string]bool{}
	for _, inv := range list {
		keep[inv.CodeHash] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO invites (code_hash, label, max_uses, uses, expires_at) VALUES (?, ?, ?, 0, ?)
ON CONFLICT(code_hash) DO UPDATE SET label = excluded.label, max_uses = excluded.max_uses, expires_at = excluded.expires_at`,
			inv.CodeHash, nullStr(inv.Label), nullInt(int64(inv.MaxUses)), nullInt(inv.ExpiresAt)); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT code_hash FROM invites`)
	if err != nil {
		return err
	}
	var drop []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return err
		}
		if !keep[h] {
			drop = append(drop, h)
		}
	}
	rows.Close()
	for _, h := range drop {
		if _, err := tx.ExecContext(ctx, `DELETE FROM invites WHERE code_hash = ?`, h); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	var inv Invite
	var label sql.NullString
	var maxUses, exp sql.NullInt64
	if err := row.Scan(&inv.CodeHash, &label, &maxUses, &inv.Uses, &exp); err != nil {
		return nil, notFound(err)
	}
	inv.Label, inv.MaxUses, inv.ExpiresAt = label.String, int(maxUses.Int64), exp.Int64
	return &inv, nil
}

func (r sqlInvites) Redeem(ctx context.Context, codeHash string, now int64) (*Invite, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	inv, err := scanInvite(tx.QueryRowContext(ctx, `SELECT code_hash, label, max_uses, uses, expires_at FROM invites WHERE code_hash = ?`, codeHash))
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(inv.CodeHash), []byte(codeHash)) != 1 {
		return nil, ErrNotFound
	}
	if err := redeemable(inv, now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invites SET uses = uses + 1 WHERE code_hash = ?`, codeHash); err != nil {
		return nil, err
	}
	inv.Uses++
	return inv, tx.Commit()
}

func (r sqlInvites) List(ctx context.Context) ([]Invite, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT code_hash, label, max_uses, uses, expires_at FROM invites ORDER BY code_hash`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

type sqlSessions struct{ db *sql.DB }

const sessionCols = `id, user_id, state, container_id, remote_container_id, volume, image, token_hash, created_at, last_active_at, stopped_at, failure`

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	var s Session
	var state string
	var cid, rcid, vol, img, fail sql.NullString
	var last, stopped sql.NullInt64
	if err := row.Scan(&s.ID, &s.UserID, &state, &cid, &rcid, &vol, &img, &s.TokenHash, &s.CreatedAt, &last, &stopped, &fail); err != nil {
		return nil, notFound(err)
	}
	s.State = sessionState(state)
	s.ContainerID, s.RemoteContainerID, s.Volume, s.Image, s.Failure = cid.String, rcid.String, vol.String, img.String, fail.String
	s.LastActiveAt, s.StoppedAt = last.Int64, stopped.Int64
	return &s, nil
}

func (r sqlSessions) Get(ctx context.Context, id string) (*Session, error) {
	return scanSession(r.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
}

func (r sqlSessions) ByUser(ctx context.Context, userID string) (*Session, error) {
	return scanSession(r.db.QueryRowContext(ctx, `SELECT `+sessionCols+` FROM sessions WHERE user_id = ? AND state != 'removed' ORDER BY created_at DESC, id DESC LIMIT 1`, userID))
}

func (r sqlSessions) Put(ctx context.Context, s *Session) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET user_id = excluded.user_id, state = excluded.state, container_id = excluded.container_id,
  remote_container_id = excluded.remote_container_id, volume = excluded.volume, image = excluded.image,
  token_hash = excluded.token_hash, created_at = excluded.created_at, last_active_at = excluded.last_active_at,
  stopped_at = excluded.stopped_at, failure = excluded.failure`,
		s.ID, s.UserID, string(s.State), nullStr(s.ContainerID), nullStr(s.RemoteContainerID), nullStr(s.Volume), nullStr(s.Image),
		s.TokenHash, s.CreatedAt, nullInt(s.LastActiveAt), nullInt(s.StoppedAt), nullStr(s.Failure))
	return err
}

func (r sqlSessions) List(ctx context.Context) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+sessionCols+` FROM sessions ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

type sqlProgress struct{ db *sql.DB }

func (r sqlProgress) List(ctx context.Context, userID string) ([]StepRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT lesson_id, step_id, status, hints_used, resets, completed_at, updated_at
FROM progress WHERE user_id = ? ORDER BY lesson_id, step_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StepRecord
	for rows.Next() {
		rec := StepRecord{UserID: userID}
		var status string
		var done sql.NullInt64
		if err := rows.Scan(&rec.LessonID, &rec.StepID, &status, &rec.HintsUsed, &rec.Resets, &done, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		rec.Status = progressStatus(status)
		rec.CompletedAt = done.Int64
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r sqlProgress) Put(ctx context.Context, rec StepRecord) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO progress (user_id, lesson_id, step_id, status, hints_used, resets, completed_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, lesson_id, step_id) DO UPDATE SET status = excluded.status, hints_used = excluded.hints_used,
  resets = excluded.resets, completed_at = excluded.completed_at, updated_at = excluded.updated_at`,
		rec.UserID, rec.LessonID, rec.StepID, string(rec.Status), rec.HintsUsed, rec.Resets, nullInt(rec.CompletedAt), rec.UpdatedAt)
	return err
}

func (r sqlProgress) DeleteLesson(ctx context.Context, userID, lessonID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM progress WHERE user_id = ? AND lesson_id = ?`, userID, lessonID)
	return err
}

func (r sqlProgress) DeleteAll(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM progress WHERE user_id = ?`, userID)
	return err
}

type sqlStuck struct{ db *sql.DB }

func (r sqlStuck) Add(ctx context.Context, rep *StuckReport) (int64, error) {
	events := rep.Events
	if len(events) == 0 {
		events = json.RawMessage("[]")
	}
	res, err := r.db.ExecContext(ctx, `INSERT INTO stuck_reports (user_id, lesson_id, step_id, created_at, note, events_json) VALUES (?, ?, ?, ?, ?, ?)`,
		rep.UserID, nullStr(rep.LessonID), nullStr(rep.StepID), rep.CreatedAt, nullStr(rep.Note), string(events))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (r sqlStuck) List(ctx context.Context, limit int) ([]StuckReport, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, user_id, lesson_id, step_id, created_at, note, events_json FROM stuck_reports ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StuckReport
	for rows.Next() {
		var rep StuckReport
		var lesson, step, note sql.NullString
		var events string
		if err := rows.Scan(&rep.ID, &rep.UserID, &lesson, &step, &rep.CreatedAt, &note, &events); err != nil {
			return nil, err
		}
		rep.LessonID, rep.StepID, rep.Note = lesson.String, step.String, note.String
		rep.Events = json.RawMessage(events)
		out = append(out, rep)
	}
	return out, rows.Err()
}

func userKind(s string) protocol.UserKind             { return protocol.UserKind(s) }
func sessionState(s string) protocol.SessionState     { return protocol.SessionState(s) }
func progressStatus(s string) protocol.ProgressStatus { return protocol.ProgressStatus(s) }
