package protocol

// ErrorCode is a stable, machine-readable error code (api.md §1). New codes
// may be added; clients must treat unknown codes like ErrInternal.
type ErrorCode string

// Stable error codes.
const (
	ErrUnauthenticated    ErrorCode = "unauthenticated"     // 401: no or invalid session cookie, bad bearer token
	ErrForbidden          ErrorCode = "forbidden"           // 403: authenticated but not allowed (other user's session, bad Origin, bad invite)
	ErrNotFound           ErrorCode = "not_found"           // 404
	ErrInvalidRequest     ErrorCode = "invalid_request"     // 400: malformed JSON, missing or invalid fields
	ErrSessionLimit       ErrorCode = "session_limit"       // 429 or 503: sessions.maxSessions reached, per-user WebSocket limit
	ErrSessionUnavailable ErrorCode = "session_unavailable" // 502 or 503: the session container is not running or not healthy
	ErrEmacsUnavailable   ErrorCode = "emacs_unavailable"   // 503: the agent is not connected or Emacs is unresponsive
	ErrTimeout            ErrorCode = "timeout"             // 504: an RPC, agent call or subprocess timed out
	ErrConflict           ErrorCode = "conflict"            // 409: state does not allow the operation (no active step, GUI disabled)
	ErrInternal           ErrorCode = "internal"            // 500
	ErrNotImplemented     ErrorCode = "not_implemented"     // 501: a documented feature that this build does not provide yet (OIDC in slice 1)
	ErrRateLimited        ErrorCode = "rate_limited"        // 429: login attempts, session creation, stuck reports
)

// ProblemContentType is the media type of error responses.
const ProblemContentType = "application/problem+json"

// Problem is the body of every error response (RFC 9457 subset, api.md §1).
type Problem struct {
	// Status repeats the HTTP status code.
	Status int `json:"status"`
	// Code is the stable error code.
	Code ErrorCode `json:"code"`
	// Message is a human-readable sentence safe to show to learners.
	Message string `json:"message"`
	// Details is optional structured context (field errors, retry hints).
	Details any `json:"details,omitempty"`
}

// Error makes a Problem usable as a Go error.
func (p *Problem) Error() string { return string(p.Code) + ": " + p.Message }
