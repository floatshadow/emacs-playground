package httpx

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/zsy/emacs-playground/internal/protocol"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so that the first listed is the outermost.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type requestIDKey struct{}
type loggerKey struct{}

// RequestID takes X-Request-Id from the request when it looks sane, else
// generates one; stores it in the context and echoes it in the response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(protocol.HeaderRequestID)
		if id == "" || len(id) > 64 || strings.ContainsAny(id, " \t\r\n") {
			id = NewID(8)
		}
		w.Header().Set(protocol.HeaderRequestID, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the request id stored by RequestID, or "".
func RequestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(requestIDKey{}).(string)
	return s
}

// NewID returns n random bytes hex-encoded.
func NewID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WithLogger stores a logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// Logger returns the request-scoped logger (with request_id and any
// attributes added by later middleware), or slog.Default().
func Logger(ctx context.Context) *slog.Logger { return logger(ctx) }

// Logging logs one line per request at info (debug for /healthz and
// /metrics) with method, path, status, bytes, duration and request_id, and
// stores a request-scoped logger in the context. WebSocket upgrades are
// logged when they end, with status 101.
func Logging(base *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			l := base.With("request_id", RequestIDFrom(r.Context()))
			rec := &StatusRecorder{ResponseWriter: w, Status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(WithLogger(r.Context(), l)))
			level := slog.LevelInfo
			if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" || strings.HasSuffix(r.URL.Path, "/healthz") {
				level = slog.LevelDebug
			}
			l.Log(r.Context(), level, "http",
				"method", r.Method, "path", r.URL.Path, "status", rec.Status,
				"bytes", rec.Bytes, "duration_ms", time.Since(start).Milliseconds())
		})
	}
}

// Recover turns panics into 500 responses and logs the stack.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				Logger(r.Context()).Error("panic", "value", fmt.Sprint(v), "stack", string(debug.Stack()))
				WriteError(w, r, Internal("internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// StatusRecorder records the status and size of a response. It forwards
// Flush and Hijack and supports http.ResponseController through Unwrap, so
// WebSocket upgrades and ReverseProxy streaming work through it.
type StatusRecorder struct {
	http.ResponseWriter
	Status      int
	Bytes       int64
	wroteHeader bool
}

// WriteHeader records the status.
func (s *StatusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.Status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

// Write counts bytes.
func (s *StatusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	n, err := s.ResponseWriter.Write(b)
	s.Bytes += int64(n)
	return n, err
}

// Flush implements http.Flusher.
func (s *StatusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker (WebSocket upgrades).
func (s *StatusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("httpx: response writer does not support hijacking")
	}
	s.Status = http.StatusSwitchingProtocols
	s.wroteHeader = true
	return h.Hijack()
}

// Unwrap supports http.ResponseController.
func (s *StatusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// SameOrigin reports whether the request's Origin header equals the origin
// (scheme://host[:port]) of publicURL. A missing Origin is not same-origin.
func SameOrigin(r *http.Request, publicURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	want, err := url.Parse(publicURL)
	if err != nil {
		return false
	}
	got, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(got.Scheme, want.Scheme) && strings.EqualFold(got.Host, want.Host)
}

// IsWebSocketUpgrade reports whether r asks for a WebSocket upgrade.
func IsWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// IsStateChanging reports whether r must pass the Origin check: any method
// other than GET, HEAD and OPTIONS, and every WebSocket upgrade.
func IsStateChanging(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return IsWebSocketUpgrade(r)
	}
	return true
}

// OriginCheck rejects state-changing requests and WebSocket upgrades whose
// Origin does not match publicURL with 403 forbidden (deployment.md §9.3).
// Requests for which skip returns true are not checked (for example the
// /internal API, authenticated by bearer token).
func OriginCheck(publicURL string, skip func(*http.Request) bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsStateChanging(r) && (skip == nil || !skip(r)) && !SameOrigin(r, publicURL) {
				WriteError(w, r, Forbidden("cross-origin request refused"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BearerToken returns the token of an "Authorization: Bearer <token>"
// header, or "".
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// NewServer returns an http.Server with the timeouts from backend.md §5
// (read header 10 s, idle 120 s; no write timeout because of WebSockets and
// streaming).
func NewServer(addr string, h http.Handler, l *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(l.Handler(), slog.LevelWarn),
	}
}
