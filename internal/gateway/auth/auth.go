package auth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/gateway/limit"
	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Authenticator authenticates learners (backend.md §2.3).
type Authenticator interface {
	// Middleware sets the user on the request context or answers 401.
	Middleware(next http.Handler) http.Handler
	// Routes registers login, callback and logout routes.
	Routes(mux *http.ServeMux)
	// Mode is the configured auth mode.
	Mode() protocol.AuthMode
}

type ctxKey struct{}

// WithUser stores the authenticated user in ctx.
func WithUser(ctx context.Context, u *protocol.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (*protocol.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*protocol.User)
	return u, ok && u != nil
}

// Options configure New.
type Options struct {
	PublicURL   string
	SessionDays int
	Signer      *Signer
	Store       store.Store
	Logger      *slog.Logger
	// Now is the clock (tests); default time.Now.
	Now func() time.Time
}

// Route paths under /api/v1.
const (
	PathInviteLogin  = protocol.APIPrefix + "/auth/invite"
	PathLogout       = protocol.APIPrefix + "/auth/logout"
	PathOIDCStart    = protocol.APIPrefix + "/auth/oidc/start"
	PathOIDCCallback = protocol.APIPrefix + "/auth/oidc/callback"
)

// New returns the authenticator for mode.
func New(mode protocol.AuthMode, o Options) (Authenticator, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.SessionDays <= 0 {
		o.SessionDays = 30
	}
	base := &cookieAuth{
		o:        o,
		secure:   strings.HasPrefix(strings.ToLower(o.PublicURL), "https:"),
		lifetime: time.Duration(o.SessionDays) * 24 * time.Hour,
		ipLimit:  limit.New(10, time.Minute),
		codeLim:  limit.New(20, time.Minute),
		seen:     map[string]time.Time{},
	}
	switch mode {
	case protocol.AuthNone:
		return &noneAuth{o: o}, nil
	case protocol.AuthInvite:
		if o.Signer == nil {
			return nil, errors.New("auth: invite mode needs a cookie signer")
		}
		return &inviteAuth{cookieAuth: base}, nil
	case protocol.AuthOIDC:
		if o.Signer == nil {
			return nil, errors.New("auth: oidc mode needs a cookie signer")
		}
		return &oidcAuth{cookieAuth: base}, nil
	}
	return nil, errors.New("auth: unknown mode " + string(mode))
}

// ---- none ------------------------------------------------------------------

// noneAuth is auth mode none: a single local user, local mode only.
type noneAuth struct {
	o    Options
	once sync.Once
	user *protocol.User
}

func (a *noneAuth) Mode() protocol.AuthMode { return protocol.AuthNone }

func (a *noneAuth) localUser(ctx context.Context) *protocol.User {
	a.once.Do(func() {
		u := &protocol.User{ID: protocol.LocalUserID, DisplayName: "You", Kind: protocol.UserLocal}
		if a.o.Store != nil {
			if rec, err := a.o.Store.Users().Get(ctx, protocol.LocalUserID); err == nil {
				if rec.DisplayName != "" {
					u.DisplayName = rec.DisplayName
				}
			} else {
				now := a.o.Now().UnixMilli()
				if err := a.o.Store.Users().Put(ctx, &store.User{ID: u.ID, Kind: u.Kind, DisplayName: u.DisplayName, CreatedAt: now, LastSeenAt: now}); err != nil {
					a.o.Logger.Warn("auth: storing the local user", "err", err)
				}
			}
		}
		a.user = u
	})
	return a.user
}

func (a *noneAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), a.localUser(r.Context()))))
	})
}

func (a *noneAuth) Routes(mux *http.ServeMux) {
	mux.Handle("POST "+PathInviteLogin, httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.Conflict("invite codes are not used: this playground has no login")
	}))
	mux.Handle("POST "+PathLogout, httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		httpx.NoContent(w)
		return nil
	}))
	oidcNotConfigured(mux)
}

// ---- cookies (invite, oidc) -------------------------------------------------

type cookieAuth struct {
	o        Options
	secure   bool
	lifetime time.Duration
	ipLimit  *limit.Limiter
	codeLim  *limit.Limiter

	mu   sync.Mutex
	seen map[string]time.Time // user id → last Touch
}

func (a *cookieAuth) setCookie(w http.ResponseWriter, userID string) {
	exp := a.o.Now().Add(a.lifetime)
	http.SetCookie(w, &http.Cookie{
		Name:     protocol.SessionCookie,
		Value:    a.o.Signer.Sign(userID, exp),
		Path:     "/",
		Expires:  exp,
		MaxAge:   int(a.lifetime / time.Second),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *cookieAuth) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: protocol.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
}

// userFromCookie verifies the cookie and loads the user.
func (a *cookieAuth) userFromCookie(r *http.Request) (*store.User, time.Time, error) {
	c, err := r.Cookie(protocol.SessionCookie)
	if err != nil {
		return nil, time.Time{}, err
	}
	uid, exp, err := a.o.Signer.Verify(c.Value, a.o.Now())
	if err != nil {
		return nil, time.Time{}, err
	}
	u, err := a.o.Store.Users().Get(r.Context(), uid)
	if err != nil {
		return nil, time.Time{}, err
	}
	return u, exp, nil
}

func (a *cookieAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, exp, err := a.userFromCookie(r)
		if err != nil {
			if !errors.Is(err, http.ErrNoCookie) {
				a.clearCookie(w)
			}
			httpx.WriteError(w, r, httpx.Unauthenticated("sign in to continue"))
			return
		}
		now := a.o.Now()
		// Renew on use, at most once a day.
		if exp.Sub(now) < a.lifetime-24*time.Hour {
			a.setCookie(w, u.ID)
		}
		a.touch(r.Context(), u.ID, now)
		user := &protocol.User{ID: u.ID, DisplayName: u.DisplayName, Kind: u.Kind}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

// touch records last_seen_at at most every five minutes per user.
func (a *cookieAuth) touch(ctx context.Context, id string, now time.Time) {
	a.mu.Lock()
	last := a.seen[id]
	if now.Sub(last) < 5*time.Minute {
		a.mu.Unlock()
		return
	}
	a.seen[id] = now
	a.mu.Unlock()
	if err := a.o.Store.Users().Touch(ctx, id, now.UnixMilli()); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.o.Logger.Warn("auth: touch", "err", err)
	}
}

func (a *cookieAuth) logout(w http.ResponseWriter, r *http.Request) error {
	if u, _, err := a.userFromCookie(r); err == nil {
		_ = a.o.Store.Audit(r.Context(), store.AuditEvent{At: a.o.Now().UnixMilli(), UserID: u.ID, Action: "logout"})
	}
	a.clearCookie(w)
	httpx.NoContent(w)
	return nil
}

// ClientIP is the address used for per-IP limits: the last X-Forwarded-For
// hop (added by Caddy, the only proxy in front of the gateway) or the
// connection's remote address.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- invite -----------------------------------------------------------------

type inviteAuth struct{ *cookieAuth }

func (a *inviteAuth) Mode() protocol.AuthMode { return protocol.AuthInvite }

func (a *inviteAuth) Routes(mux *http.ServeMux) {
	mux.Handle("POST "+PathInviteLogin, httpx.Handle(a.login))
	mux.Handle("POST "+PathLogout, httpx.Handle(a.logout))
	oidcNotConfigured(mux)
}

// login implements POST /api/v1/auth/invite {code} (api.md §2.1).
func (a *inviteAuth) login(w http.ResponseWriter, r *http.Request) error {
	var req protocol.InviteLoginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return httpx.BadRequest("enter your invite code")
	}
	if !a.ipLimit.Allow(ClientIP(r)) {
		return httpx.RateLimited("too many attempts; wait a minute and try again")
	}
	hash := HashCode(code)
	if !a.codeLim.Allow(hash) {
		return httpx.RateLimited("too many attempts with this code; wait a minute and try again")
	}
	// Already signed in: keep the user, do not consume another use.
	if u, _, err := a.userFromCookie(r); err == nil {
		a.setCookie(w, u.ID)
		httpx.NoContent(w)
		return nil
	}
	now := a.o.Now()
	inv, err := a.o.Store.Invites().Redeem(r.Context(), hash, now.UnixMilli())
	switch {
	case errors.Is(err, store.ErrNotFound):
		return httpx.Forbidden("this invite code is not valid")
	case errors.Is(err, store.ErrInviteExpired):
		return httpx.Forbidden("this invite code has expired")
	case errors.Is(err, store.ErrInviteUsedUp):
		return httpx.Forbidden("this invite code has been used up")
	case err != nil:
		return err
	}
	name := inv.Label
	if name == "" {
		name = "Learner"
	}
	u := &store.User{ID: "u-" + httpx.NewID(9), Kind: protocol.UserInvite, DisplayName: name, CreatedAt: now.UnixMilli(), LastSeenAt: now.UnixMilli()}
	if err := a.o.Store.Users().Put(r.Context(), u); err != nil {
		return err
	}
	_ = a.o.Store.Audit(r.Context(), store.AuditEvent{At: now.UnixMilli(), UserID: u.ID, Action: "login.invite", Details: "label=" + inv.Label})
	a.setCookie(w, u.ID)
	httpx.NoContent(w)
	return nil
}

// ---- oidc -------------------------------------------------------------------

// oidcAuth verifies cookies like invite mode; the login flow itself is not
// in slice 1.
//
// TODO(gateway, backend.md §2.6): GET /auth/oidc/start and /callback with
// github.com/coreos/go-oidc/v3 and golang.org/x/oauth2 (PKCE, issuer
// verification, allowedEmailDomains); user id = issuer + "|" + sub, display
// name from name or email; then setCookie as for invites.
type oidcAuth struct{ *cookieAuth }

func (a *oidcAuth) Mode() protocol.AuthMode { return protocol.AuthOIDC }

func (a *oidcAuth) Routes(mux *http.ServeMux) {
	notImpl := httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotImplemented("sign-in with your organisation's account is not available in this build")
	})
	mux.Handle("GET "+PathOIDCStart, notImpl)
	mux.Handle("GET "+PathOIDCCallback, notImpl)
	mux.Handle("POST "+PathInviteLogin, httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.Conflict("this playground signs in with OIDC, not invite codes")
	}))
	mux.Handle("POST "+PathLogout, httpx.Handle(a.logout))
}

func oidcNotConfigured(mux *http.ServeMux) {
	h := httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("OIDC sign-in is not configured")
	})
	mux.Handle("GET "+PathOIDCStart, h)
	mux.Handle("GET "+PathOIDCCallback, h)
}
