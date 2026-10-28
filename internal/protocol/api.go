package protocol

import "encoding/json"

// Gateway REST API (api.md §2) under APIPrefix, and the internal API that
// sessiond calls (api.md §2.6).

// Cookie and auth constants.
const (
	// SessionCookie is the gateway's auth cookie (backend.md §2.6).
	SessionCookie = "pg_session"
	// LocalUserID is the single user of auth mode none.
	LocalUserID = "local"
)

// UserKind says how a user authenticated.
type UserKind string

// User kinds.
const (
	UserLocal  UserKind = "local"
	UserInvite UserKind = "invite"
	UserOIDC   UserKind = "oidc"
)

// User is the authenticated learner.
type User struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"displayName"`
	Kind        UserKind `json:"kind"`
}

// MeFeatures tells the browser which optional features are available.
type MeFeatures struct {
	GUI          bool `json:"gui"`
	RemoteBox    bool `json:"remoteBox"`
	StuckReports bool `json:"stuckReports"`
	AuthorMode   bool `json:"authorMode"`
}

// Me is GET /api/v1/me. Unauthenticated requests get 401 with
// ErrUnauthenticated; the browser then shows /login using AuthMode from
// GET /api/v1/auth/config.
type Me struct {
	User     User       `json:"user"`
	Mode     DeployMode `json:"mode"`
	AuthMode AuthMode   `json:"authMode"`
	Features MeFeatures `json:"features"`
	// Version is the gateway build version.
	Version string `json:"version,omitempty"`
}

// AuthConfigInfo is GET /api/v1/auth/config (unauthenticated): what the
// login screen should offer.
type AuthConfigInfo struct {
	Mode     DeployMode `json:"mode"`
	AuthMode AuthMode   `json:"authMode"`
	// OIDCStartURL is the path of GET /auth/oidc/start when AuthMode is oidc.
	OIDCStartURL string `json:"oidcStartUrl,omitempty"`
}

// InviteLoginRequest is POST /api/v1/auth/invite.
type InviteLoginRequest struct {
	Code string `json:"code"`
}

// SessionState is the gateway's session lifecycle state (architecture.md
// §5.1).
type SessionState string

// Session states.
const (
	SessionCreating SessionState = "creating"
	SessionStarting SessionState = "starting"
	SessionReady    SessionState = "ready"
	SessionIdle     SessionState = "idle"
	SessionStopping SessionState = "stopping"
	SessionStopped  SessionState = "stopped"
	SessionFailed   SessionState = "failed"
	SessionRemoved  SessionState = "removed"
)

// SessionInfo is the body of POST /api/v1/sessions (201) and
// GET /api/v1/sessions/{id}. Emacs, Mode and Step come from sessiond's
// /status when the session is running.
type SessionInfo struct {
	ID           string       `json:"id"`
	State        SessionState `json:"state"`
	CreatedAt    int64        `json:"createdAt"`
	LastActiveAt int64        `json:"lastActiveAt,omitempty"`
	Image        string       `json:"image,omitempty"`
	// GUI is true when the GUI frame is available for this session.
	GUI bool `json:"gui"`
	// Failure is the reason when State is failed.
	Failure string     `json:"failure,omitempty"`
	Emacs   *EmacsInfo `json:"emacs,omitempty"`
	Mode    FrameKind  `json:"mode,omitempty"`
	Step    *StepRef   `json:"step,omitempty"`
	// UpdateAvailable is true when the session runs an older image than the
	// gateway's configured one ("restart to update" chip).
	UpdateAvailable bool `json:"updateAvailable,omitempty"`
}

// ProgressStatus is the status of a lesson or step in progress records.
type ProgressStatus string

// Progress statuses, lowest first (api.md §2.4). A step's stored status
// only rises in this order; only a reset clears it.
const (
	ProgressNotStarted ProgressStatus = "not-started"
	ProgressInProgress ProgressStatus = "in-progress"
	// ProgressDemonstrated: the step completed while a "Watch it"
	// demonstration was latched.
	ProgressDemonstrated ProgressStatus = "demonstrated"
	// ProgressSkipped: the learner passed the step with "Skip this step".
	ProgressSkipped ProgressStatus = "skipped"
	ProgressDone    ProgressStatus = "done"
)

// ProgressRank orders step statuses for "only rises": not-started 0,
// in-progress 1, demonstrated 2, skipped 3, done 4; unknown statuses -1.
func ProgressRank(s ProgressStatus) int {
	switch s {
	case ProgressNotStarted, "":
		return 0
	case ProgressInProgress:
		return 1
	case ProgressDemonstrated:
		return 2
	case ProgressSkipped:
		return 3
	case ProgressDone:
		return 4
	}
	return -1
}

// StepProgress is the progress of one step.
type StepProgress struct {
	Status      ProgressStatus `json:"status"`
	HintsUsed   int            `json:"hintsUsed"`
	Resets      int            `json:"resets"`
	CompletedAt int64          `json:"completedAt,omitempty"`
}

// ProgressCounts counts a lesson's steps by status.
type ProgressCounts struct {
	Done         int `json:"done"`
	Demonstrated int `json:"demonstrated"`
	Skipped      int `json:"skipped"`
	// Total is the number of steps in the bundle's scenario (0 when the
	// lesson is not in the loaded bundle).
	Total int `json:"total"`
}

// LessonProgress is the progress of one lesson. Status is done when every
// step of the lesson's scenario is done, in-progress when any step is above
// not-started, else not-started.
type LessonProgress struct {
	Status ProgressStatus          `json:"status"`
	Counts ProgressCounts          `json:"counts"`
	Steps  map[string]StepProgress `json:"steps"`
	// Laps summarises finished "again, without hints" laps (projects).
	Laps *LapStats `json:"laps,omitempty"`
}

// LapStats summarises a lesson's finished laps.
type LapStats struct {
	Count  int   `json:"count"`
	BestMs int64 `json:"bestMs"`
	LastMs int64 `json:"lastMs"`
	// LastAt is when the last lap finished (ms).
	LastAt int64 `json:"lastAt"`
}

// Progress is GET /api/v1/progress.
type Progress struct {
	Lessons   map[string]LessonProgress `json:"lessons"`
	UpdatedAt int64                     `json:"updatedAt"`
}

// ProgressResetRequest is POST /api/v1/progress/reset. An empty LessonID
// (or no body at all) clears every lesson of the user ("Start over").
type ProgressResetRequest struct {
	LessonID string `json:"lessonId,omitempty"`
}

// StuckRequest is POST /api/v1/stuck (the "I'm stuck" button).
type StuckRequest struct {
	SessionID string `json:"sessionId"`
	LessonID  string `json:"lessonId,omitempty"`
	StepID    string `json:"stepId,omitempty"`
	Note      string `json:"note"`
}

// Created is a 201 body carrying a new resource id.
type Created struct {
	ID string `json:"id"`
}

// ProgressReport is POST /internal/sessions/{id}/progress from sessiond.
// HintsUsed and Resets, when present, are the step's absolute totals since
// the step's progress was last reset (not increments).
type ProgressReport struct {
	LessonID  string         `json:"lessonId"`
	StepID    string         `json:"stepId"`
	Status    ProgressStatus `json:"status"`
	HintsUsed *int           `json:"hintsUsed,omitempty"`
	Resets    *int           `json:"resets,omitempty"`
	// At is when the change happened (ms); the gateway uses it as
	// completedAt for done.
	At int64 `json:"at,omitempty"`
	// LapMs, on the done report of a lap's last step, records a finished
	// lap of this many milliseconds.
	LapMs int64 `json:"lapMs,omitempty"`
}

// InternalStuckReport is POST /internal/sessions/{id}/stuck from sessiond.
type InternalStuckReport struct {
	LessonID string            `json:"lessonId,omitempty"`
	StepID   string            `json:"stepId,omitempty"`
	Note     string            `json:"note"`
	Events   []json.RawMessage `json:"events"`
}

// Heartbeat is POST /internal/sessions/{id}/heartbeat, every 30 s.
type Heartbeat struct {
	ActiveWebSockets int `json:"activeWebSockets"`
}

// Internal API paths (sessiond → gateway), relative to PG_GATEWAY_URL.
// Lesson ids contain a slash and are placed in the path literally.
const (
	InternalScenarioPath  = "/internal/curriculum/scenarios/" // + lessonId
	InternalFixturesPath  = "/internal/curriculum/fixtures/"  // + lessonId
	InternalSessionsPath  = "/internal/sessions/"             // + id + "/progress" | "/stuck" | "/heartbeat"
	FixturesContentType   = "application/zstd"
	InternalAuthorization = "Authorization" // "Bearer <PG_SESSION_TOKEN>"
)
