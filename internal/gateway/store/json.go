package store

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// JSONFile is the local-mode store file name under the data directory.
const JSONFile = "local.json"

// Retention limits of the JSON store (local mode keeps a short history).
const (
	jsonMaxStuck = 50
	jsonMaxAudit = 500
)

// jsonData is the on-disk shape of local.json.
type jsonData struct {
	Version  int                 `json:"version"`
	Users    map[string]*User    `json:"users"`
	Invites  map[string]*Invite  `json:"invites,omitempty"`
	Sessions map[string]*Session `json:"sessions"`
	// Progress is user → lesson → step → record.
	Progress    map[string]map[string]map[string]*StepRecord `json:"progress"`
	Stuck       []StuckReport                                `json:"stuck,omitempty"`
	NextStuckID int64                                        `json:"nextStuckId,omitempty"`
	Audit       []AuditEvent                                 `json:"audit,omitempty"`
}

// jsonStore keeps everything in memory and rewrites the file on change.
type jsonStore struct {
	mu   sync.Mutex
	path string
	d    jsonData
}

// OpenJSON opens (or creates) <dir>/local.json.
func OpenJSON(dir string) (Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &jsonStore{path: filepath.Join(dir, JSONFile)}
	data, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, &s.d); err != nil {
			return nil, fmt.Errorf("store: %s: %w", s.path, err)
		}
	}
	s.d.Version = 1
	if s.d.Users == nil {
		s.d.Users = map[string]*User{}
	}
	if s.d.Invites == nil {
		s.d.Invites = map[string]*Invite{}
	}
	if s.d.Sessions == nil {
		s.d.Sessions = map[string]*Session{}
	}
	if s.d.Progress == nil {
		s.d.Progress = map[string]map[string]map[string]*StepRecord{}
	}
	return s, nil
}

// save writes the file atomically. Callers hold mu.
func (s *jsonStore) save() error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&s.d); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".local.json-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

func (s *jsonStore) Users() UserRepo        { return jsonUsers{s} }
func (s *jsonStore) Sessions() SessionRepo  { return jsonSessions{s} }
func (s *jsonStore) Progress() ProgressRepo { return jsonProgress{s} }
func (s *jsonStore) Stuck() StuckRepo       { return jsonStuck{s} }
func (s *jsonStore) Invites() InviteRepo    { return jsonInvites{s} }
func (s *jsonStore) Close() error           { return nil }

func (s *jsonStore) Audit(_ context.Context, e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Audit = append(s.d.Audit, e)
	if n := len(s.d.Audit); n > jsonMaxAudit {
		s.d.Audit = append([]AuditEvent(nil), s.d.Audit[n-jsonMaxAudit:]...)
	}
	return s.save()
}

func (s *jsonStore) AuditLog(_ context.Context, limit int) ([]AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AuditEvent
	for i := len(s.d.Audit) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		out = append(out, s.d.Audit[i])
	}
	return out, nil
}

type jsonUsers struct{ s *jsonStore }

func (r jsonUsers) Get(_ context.Context, id string) (*User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if u, ok := r.s.d.Users[id]; ok {
		c := *u
		return &c, nil
	}
	return nil, ErrNotFound
}

func (r jsonUsers) BySubject(_ context.Context, subject string) (*User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, u := range r.s.d.Users {
		if subject != "" && u.Subject == subject {
			c := *u
			return &c, nil
		}
	}
	return nil, ErrNotFound
}

func (r jsonUsers) Put(_ context.Context, u *User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	c := *u
	r.s.d.Users[u.ID] = &c
	return r.s.save()
}

func (r jsonUsers) Touch(_ context.Context, id string, at int64) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	u, ok := r.s.d.Users[id]
	if !ok {
		return ErrNotFound
	}
	u.LastSeenAt = at
	return r.s.save()
}

type jsonInvites struct{ s *jsonStore }

func (r jsonInvites) Sync(_ context.Context, list []Invite) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	keep := map[string]bool{}
	for _, inv := range list {
		keep[inv.CodeHash] = true
		if cur, ok := r.s.d.Invites[inv.CodeHash]; ok {
			cur.Label, cur.MaxUses, cur.ExpiresAt = inv.Label, inv.MaxUses, inv.ExpiresAt
			continue
		}
		c := inv
		c.Uses = 0
		r.s.d.Invites[inv.CodeHash] = &c
	}
	for h := range r.s.d.Invites {
		if !keep[h] {
			delete(r.s.d.Invites, h)
		}
	}
	return r.s.save()
}

func (r jsonInvites) Redeem(_ context.Context, codeHash string, now int64) (*Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var found *Invite
	// Compare against every stored hash in constant time per entry.
	for h, inv := range r.s.d.Invites {
		if subtle.ConstantTimeCompare([]byte(h), []byte(codeHash)) == 1 {
			found = inv
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	if err := redeemable(found, now); err != nil {
		return nil, err
	}
	found.Uses++
	c := *found
	return &c, r.s.save()
}

func (r jsonInvites) List(_ context.Context) ([]Invite, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := make([]Invite, 0, len(r.s.d.Invites))
	for _, inv := range r.s.d.Invites {
		out = append(out, *inv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CodeHash < out[j].CodeHash })
	return out, nil
}

type jsonSessions struct{ s *jsonStore }

func (r jsonSessions) Get(_ context.Context, id string) (*Session, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if v, ok := r.s.d.Sessions[id]; ok {
		c := *v
		return &c, nil
	}
	return nil, ErrNotFound
}

func (r jsonSessions) ByUser(_ context.Context, userID string) (*Session, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var best *Session
	for _, v := range r.s.d.Sessions {
		if v.UserID != userID || v.State == protocol.SessionRemoved {
			continue
		}
		if best == nil || v.CreatedAt > best.CreatedAt || (v.CreatedAt == best.CreatedAt && v.ID > best.ID) {
			best = v
		}
	}
	if best == nil {
		return nil, ErrNotFound
	}
	c := *best
	return &c, nil
}

func (r jsonSessions) Put(_ context.Context, v *Session) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	c := *v
	r.s.d.Sessions[v.ID] = &c
	return r.s.save()
}

func (r jsonSessions) List(_ context.Context) ([]Session, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := make([]Session, 0, len(r.s.d.Sessions))
	for _, v := range r.s.d.Sessions {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type jsonProgress struct{ s *jsonStore }

func (r jsonProgress) List(_ context.Context, userID string) ([]StepRecord, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out []StepRecord
	for lesson, steps := range r.s.d.Progress[userID] {
		for step, rec := range steps {
			c := *rec
			c.UserID, c.LessonID, c.StepID = userID, lesson, step
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LessonID != out[j].LessonID {
			return out[i].LessonID < out[j].LessonID
		}
		return out[i].StepID < out[j].StepID
	})
	return out, nil
}

func (r jsonProgress) Put(_ context.Context, rec StepRecord) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	lessons := r.s.d.Progress[rec.UserID]
	if lessons == nil {
		lessons = map[string]map[string]*StepRecord{}
		r.s.d.Progress[rec.UserID] = lessons
	}
	steps := lessons[rec.LessonID]
	if steps == nil {
		steps = map[string]*StepRecord{}
		lessons[rec.LessonID] = steps
	}
	c := rec
	steps[rec.StepID] = &c
	return r.s.save()
}

func (r jsonProgress) DeleteLesson(_ context.Context, userID, lessonID string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if lessons := r.s.d.Progress[userID]; lessons != nil {
		delete(lessons, lessonID)
	}
	return r.s.save()
}

func (r jsonProgress) DeleteAll(_ context.Context, userID string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	delete(r.s.d.Progress, userID)
	return r.s.save()
}

type jsonStuck struct{ s *jsonStore }

func (r jsonStuck) Add(_ context.Context, rep *StuckReport) (int64, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.d.NextStuckID++
	c := *rep
	c.ID = r.s.d.NextStuckID
	if len(c.Events) == 0 {
		c.Events = json.RawMessage("[]")
	}
	r.s.d.Stuck = append(r.s.d.Stuck, c)
	if n := len(r.s.d.Stuck); n > jsonMaxStuck {
		r.s.d.Stuck = append([]StuckReport(nil), r.s.d.Stuck[n-jsonMaxStuck:]...)
	}
	return c.ID, r.s.save()
}

func (r jsonStuck) List(_ context.Context, limit int) ([]StuckReport, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out []StuckReport
	for i := len(r.s.d.Stuck) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		out = append(out, r.s.d.Stuck[i])
	}
	return out, nil
}
