package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("store: not found")

// User is a learner (users table).
type User struct {
	ID   string            `json:"id"`
	Kind protocol.UserKind `json:"kind"`
	// Subject is the external identity (OIDC issuer + "|" + sub); "" for
	// the local user.
	Subject     string `json:"subject,omitempty"`
	DisplayName string `json:"displayName"`
	// Username, Email and Groups come from the ID token, refreshed at every
	// login.
	Username   string   `json:"username,omitempty"`
	Email      string   `json:"email,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	CreatedAt  int64    `json:"createdAt"`
	LastSeenAt int64    `json:"lastSeenAt,omitempty"`
	// SessionEpoch is signed into the user's session cookies; RevokeSessions
	// increments it, so every earlier cookie stops verifying.
	SessionEpoch int64 `json:"sessionEpoch,omitempty"`
}

// Session is a session registry row (sessions table).
type Session struct {
	ID                string                `json:"id"`
	UserID            string                `json:"userId"`
	State             protocol.SessionState `json:"state"`
	ContainerID       string                `json:"containerId,omitempty"`
	RemoteContainerID string                `json:"remoteContainerId,omitempty"`
	Volume            string                `json:"volume,omitempty"`
	Image             string                `json:"image,omitempty"`
	// TokenHash is the hex SHA-256 of the session's bearer token.
	TokenHash    string `json:"tokenHash"`
	CreatedAt    int64  `json:"createdAt"`
	LastActiveAt int64  `json:"lastActiveAt,omitempty"`
	StoppedAt    int64  `json:"stoppedAt,omitempty"`
	Failure      string `json:"failure,omitempty"`
}

// StepRecord is one row of the progress table.
type StepRecord struct {
	UserID      string                  `json:"-"`
	LessonID    string                  `json:"-"`
	StepID      string                  `json:"-"`
	Status      protocol.ProgressStatus `json:"status"`
	HintsUsed   int                     `json:"hintsUsed"`
	Resets      int                     `json:"resets"`
	CompletedAt int64                   `json:"completedAt,omitempty"`
	UpdatedAt   int64                   `json:"updatedAt"`
}

// LapRecord summarises a user's finished laps of one lesson (laps table).
type LapRecord struct {
	UserID   string `json:"-"`
	LessonID string `json:"-"`
	Count    int    `json:"count"`
	BestMs   int64  `json:"bestMs"`
	LastMs   int64  `json:"lastMs"`
	LastAt   int64  `json:"lastAt"`
	// LastLapID is the id of the last lap counted (empty for laps
	// reported without one).
	LastLapID string `json:"lastLapId,omitempty"`
}

// StuckReport is an "I'm stuck" report (stuck_reports table).
type StuckReport struct {
	ID        int64  `json:"id"`
	UserID    string `json:"userId"`
	LessonID  string `json:"lessonId,omitempty"`
	StepID    string `json:"stepId,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	Note      string `json:"note,omitempty"`
	// Events are the session's last events as raw JSON frames.
	Events json.RawMessage `json:"events"`
}

// AuditEvent is one audit log entry.
type AuditEvent struct {
	At      int64  `json:"at"`
	UserID  string `json:"userId,omitempty"`
	Action  string `json:"action"`
	Details string `json:"details,omitempty"`
}

// UserRepo stores users.
type UserRepo interface {
	Get(ctx context.Context, id string) (*User, error)
	BySubject(ctx context.Context, subject string) (*User, error)
	// Put inserts or replaces a user; an existing user keeps its session
	// epoch.
	Put(ctx context.Context, u *User) error
	// Touch sets last_seen_at.
	Touch(ctx context.Context, id string, at int64) error
	// RevokeSessions increments the user's session epoch (logout).
	RevokeSessions(ctx context.Context, id string) error
}

// SessionRepo stores the session registry.
type SessionRepo interface {
	Get(ctx context.Context, id string) (*Session, error)
	// ByUser returns the user's most recently created session that is not
	// removed.
	ByUser(ctx context.Context, userID string) (*Session, error)
	// Put inserts or replaces a session.
	Put(ctx context.Context, s *Session) error
	List(ctx context.Context) ([]Session, error)
}

// ProgressRepo stores step progress.
type ProgressRepo interface {
	// List returns every step record of a user.
	List(ctx context.Context, userID string) ([]StepRecord, error)
	// Put inserts or replaces one step record.
	Put(ctx context.Context, r StepRecord) error
	// DeleteLesson removes a user's records (laps too) for one lesson.
	DeleteLesson(ctx context.Context, userID, lessonID string, generation int64) error
	// DeleteAll removes every record of a user ("Start over").
	DeleteAll(ctx context.Context, userID string, generation int64) error
	// Generations returns the user and lesson reset generations.
	Generations(ctx context.Context, userID string) (protocol.ProgressGenerations, error)
	// Laps returns a user's lap summaries.
	Laps(ctx context.Context, userID string) ([]LapRecord, error)
	// PutLap inserts or replaces one lap summary.
	PutLap(ctx context.Context, r LapRecord) error
}

// StuckRepo stores stuck reports.
type StuckRepo interface {
	// Add stores a report and returns its id.
	Add(ctx context.Context, r *StuckReport) (int64, error)
	// List returns the newest reports first.
	List(ctx context.Context, limit int) ([]StuckReport, error)
}

// Store is the gateway's persistence (backend.md §2.3).
type Store interface {
	Users() UserRepo
	Sessions() SessionRepo
	Progress() ProgressRepo
	Stuck() StuckRepo
	Audit(ctx context.Context, e AuditEvent) error
	// AuditLog returns the newest entries first (operators, tests).
	AuditLog(ctx context.Context, limit int) ([]AuditEvent, error)
	Close() error
}
