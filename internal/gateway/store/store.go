package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Errors.
var (
	ErrNotFound      = errors.New("store: not found")
	ErrInviteExpired = errors.New("store: invite expired")
	ErrInviteUsedUp  = errors.New("store: invite used up")
)

// User is a learner (users table).
type User struct {
	ID   string            `json:"id"`
	Kind protocol.UserKind `json:"kind"`
	// Subject is the external identity (OIDC issuer plus sub); "" for local
	// and invite users.
	Subject     string `json:"subject,omitempty"`
	DisplayName string `json:"displayName"`
	CreatedAt   int64  `json:"createdAt"`
	LastSeenAt  int64  `json:"lastSeenAt,omitempty"`
}

// Invite is an invite code (invites table). Codes are stored as hashes only.
type Invite struct {
	CodeHash string `json:"codeHash"`
	Label    string `json:"label,omitempty"`
	// MaxUses 0 means unlimited.
	MaxUses int `json:"maxUses,omitempty"`
	Uses    int `json:"uses"`
	// ExpiresAt 0 means never (ms).
	ExpiresAt int64 `json:"expiresAt,omitempty"`
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
	// Put inserts or replaces a user.
	Put(ctx context.Context, u *User) error
	// Touch sets last_seen_at.
	Touch(ctx context.Context, id string, at int64) error
}

// InviteRepo stores invite codes.
type InviteRepo interface {
	// Sync makes the stored invites equal to list (from the invites file):
	// new codes are added, label, max uses and expiry are updated, use
	// counts are kept, and codes no longer listed are removed.
	Sync(ctx context.Context, list []Invite) error
	// Redeem checks the code hash (constant-time), expiry and use count and
	// counts one use. It returns ErrNotFound, ErrInviteExpired or
	// ErrInviteUsedUp.
	Redeem(ctx context.Context, codeHash string, now int64) (*Invite, error)
	List(ctx context.Context) ([]Invite, error)
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
	// DeleteLesson removes a user's records for one lesson.
	DeleteLesson(ctx context.Context, userID, lessonID string) error
	// DeleteAll removes every record of a user ("Start over").
	DeleteAll(ctx context.Context, userID string) error
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
	Invites() InviteRepo
	Audit(ctx context.Context, e AuditEvent) error
	// AuditLog returns the newest entries first (operators, tests).
	AuditLog(ctx context.Context, limit int) ([]AuditEvent, error)
	Close() error
}

// redeemable checks expiry and use count.
func redeemable(inv *Invite, now int64) error {
	if inv.ExpiresAt > 0 && now >= inv.ExpiresAt {
		return ErrInviteExpired
	}
	if inv.MaxUses > 0 && inv.Uses >= inv.MaxUses {
		return ErrInviteUsedUp
	}
	return nil
}
