package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Error is an HTTP error with a stable code (backend.md §5). Handlers return
// it; Handle renders it as application/problem+json.
type Error struct {
	Status  int
	Code    protocol.ErrorCode
	Message string
	Details any
	// Err is an optional wrapped cause; it is logged, never sent.
	Err error
}

// Error implements error.
func (e *Error) Error() string {
	if e.Err != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.Err.Error()
	}
	return string(e.Code) + ": " + e.Message
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// Problem converts e to its wire form.
func (e *Error) Problem() protocol.Problem {
	return protocol.Problem{Status: e.Status, Code: e.Code, Message: e.Message, Details: e.Details}
}

// WithDetails returns a copy of e with details set.
func (e *Error) WithDetails(d any) *Error { c := *e; c.Details = d; return &c }

// Wrap returns a copy of e with a cause attached.
func (e *Error) Wrap(err error) *Error { c := *e; c.Err = err; return &c }

// Errorf-style constructors for the stable codes (api.md §1).
func newErr(status int, code protocol.ErrorCode, msg string) *Error {
	return &Error{Status: status, Code: code, Message: msg}
}

// BadRequest is 400 invalid_request.
func BadRequest(msg string) *Error {
	return newErr(http.StatusBadRequest, protocol.ErrInvalidRequest, msg)
}

// Unauthenticated is 401 unauthenticated.
func Unauthenticated(msg string) *Error {
	return newErr(http.StatusUnauthorized, protocol.ErrUnauthenticated, msg)
}

// Forbidden is 403 forbidden.
func Forbidden(msg string) *Error { return newErr(http.StatusForbidden, protocol.ErrForbidden, msg) }

// NotFound is 404 not_found.
func NotFound(msg string) *Error { return newErr(http.StatusNotFound, protocol.ErrNotFound, msg) }

// Conflict is 409 conflict.
func Conflict(msg string) *Error { return newErr(http.StatusConflict, protocol.ErrConflict, msg) }

// SessionLimit is 429 session_limit.
func SessionLimit(msg string) *Error {
	return newErr(http.StatusTooManyRequests, protocol.ErrSessionLimit, msg)
}

// RateLimited is 429 rate_limited.
func RateLimited(msg string) *Error {
	return newErr(http.StatusTooManyRequests, protocol.ErrRateLimited, msg)
}

// SessionUnavailable is 503 session_unavailable.
func SessionUnavailable(msg string) *Error {
	return newErr(http.StatusServiceUnavailable, protocol.ErrSessionUnavailable, msg)
}

// EmacsUnavailable is 503 emacs_unavailable.
func EmacsUnavailable(msg string) *Error {
	return newErr(http.StatusServiceUnavailable, protocol.ErrEmacsUnavailable, msg)
}

// Timeout is 504 timeout.
func Timeout(msg string) *Error { return newErr(http.StatusGatewayTimeout, protocol.ErrTimeout, msg) }

// Internal is 500 internal.
func Internal(msg string) *Error {
	return newErr(http.StatusInternalServerError, protocol.ErrInternal, msg)
}

// NotImplemented is 501 not_implemented.
func NotImplemented(msg string) *Error {
	return newErr(http.StatusNotImplemented, protocol.ErrNotImplemented, msg)
}

// AsError converts any error to *Error: *Error and *protocol.Problem pass
// through, context deadline errors become Timeout, everything else becomes
// Internal with the cause attached (and logged, not sent).
func AsError(err error) *Error {
	var he *Error
	if errors.As(err, &he) {
		return he
	}
	var p *protocol.Problem
	if errors.As(err, &p) {
		return &Error{Status: p.Status, Code: p.Code, Message: p.Message, Details: p.Details}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Timeout("the operation timed out").Wrap(err)
	}
	return Internal("internal error").Wrap(err)
}

// WriteError renders err as problem+json and logs server errors.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	he := AsError(err)
	if he.Status >= 500 {
		Logger(r.Context()).Error("request failed", "status", he.Status, "code", he.Code, "err", he.Error())
	}
	p := he.Problem()
	w.Header().Set("Content-Type", protocol.ProblemContentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(he.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// HandlerFunc is an http.HandlerFunc that returns an error.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts h to http.Handler; a returned error is rendered with
// WriteError (unless the handler already wrote a response, which is the
// handler's bug and only logged).
func Handle(h HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, err)
		}
	})
}

// DecodeProblem reads a problem+json body from a non-2xx response (for
// clients such as sessiond calling the gateway, or the gateway calling
// sessiond). It never fails: unknown bodies become an internal error.
func DecodeProblem(resp *http.Response) *Error {
	var p protocol.Problem
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&p); err != nil || p.Code == "" {
		return &Error{Status: resp.StatusCode, Code: protocol.ErrInternal, Message: http.StatusText(resp.StatusCode)}
	}
	if p.Status == 0 {
		p.Status = resp.StatusCode
	}
	return &Error{Status: p.Status, Code: p.Code, Message: p.Message, Details: p.Details}
}

// logger returns the request logger or the default.
func logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
