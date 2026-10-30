package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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
	// OIDC configures auth mode oidc; ClientSecret is the content of
	// OIDC.ClientSecretFile ("" for a public client).
	OIDC         protocol.OIDCConfig
	ClientSecret string
	// HTTPClient talks to the OIDC provider; default a client with a 15 s
	// timeout.
	HTTPClient *http.Client
	// Now is the clock (tests); default time.Now.
	Now func() time.Time
}

// Route paths. PathLogin is the web app's login screen.
const (
	PathLogout       = protocol.APIPrefix + "/auth/logout"
	PathOIDCStart    = protocol.APIPrefix + "/auth/oidc/start"
	PathOIDCCallback = protocol.APIPrefix + "/auth/oidc/callback"
	PathLogin        = "/login"
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
	switch mode {
	case protocol.AuthNone:
		return &noneAuth{o: o}, nil
	case protocol.AuthOIDC:
		return newOIDC(o)
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
	mux.Handle("POST "+PathLogout, httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		httpx.NoContent(w)
		return nil
	}))
	h := httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("OIDC sign-in is not configured")
	})
	mux.Handle("GET "+PathOIDCStart, h)
	mux.Handle("GET "+PathOIDCCallback, h)
}

// ---- session cookies --------------------------------------------------------

// cookies issues and verifies the pg_session cookie and loads its user.
// A cookie lasts sessionDays from the sign-in and is never extended, so
// the provider and the allow-lists are consulted again at least that
// often. It carries the user's session epoch: logout increments it, which
// revokes every cookie of the user.
type cookies struct {
	o        Options
	kind     protocol.UserKind
	secure   bool
	lifetime time.Duration
	// allow, when set, rechecks a stored user on every request and returns
	// why they are refused, or "" (a tightened allow-list applies at once).
	allow func(*store.User) string

	mu   sync.Mutex
	seen map[string]time.Time // user id → last Touch
}

func newCookies(o Options, kind protocol.UserKind) cookies {
	return cookies{
		o:        o,
		kind:     kind,
		secure:   strings.HasPrefix(strings.ToLower(o.PublicURL), "https:"),
		lifetime: time.Duration(o.SessionDays) * 24 * time.Hour,
		seen:     map[string]time.Time{},
	}
}

// cookieSubject is the signed subject of u's cookies: "<id>|<epoch>".
func cookieSubject(u *store.User) string {
	return u.ID + "|" + strconv.FormatInt(u.SessionEpoch, 10)
}

func (a *cookies) setCookie(w http.ResponseWriter, u *store.User) {
	exp := a.o.Now().Add(a.lifetime)
	http.SetCookie(w, &http.Cookie{
		Name:     protocol.SessionCookie,
		Value:    a.o.Signer.Sign(cookieSubject(u), exp),
		Path:     "/",
		Expires:  exp,
		MaxAge:   int(a.lifetime / time.Second),
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *cookies) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: protocol.SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
}

// Cookie refusals besides the Signer's.
var (
	// errWrongKind refuses users of another auth mode (a database kept
	// across a configuration change).
	errWrongKind = errors.New("auth: user signed in with another method")
	errRevoked   = errors.New("auth: session cookie revoked")
)

// userFromCookie verifies the cookie and loads the user.
func (a *cookies) userFromCookie(r *http.Request) (*store.User, error) {
	c, err := r.Cookie(protocol.SessionCookie)
	if err != nil {
		return nil, err
	}
	subject, _, err := a.o.Signer.Verify(c.Value, a.o.Now())
	if err != nil {
		return nil, err
	}
	uid, _, ok := strings.Cut(subject, "|")
	if !ok {
		return nil, ErrBadCookie
	}
	u, err := a.o.Store.Users().Get(r.Context(), uid)
	if err != nil {
		return nil, err
	}
	switch {
	case u.Kind != a.kind:
		return nil, errWrongKind
	case subject != cookieSubject(u):
		return nil, errRevoked
	}
	return u, nil
}

func (a *cookies) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := a.userFromCookie(r)
		if err == nil && a.allow != nil {
			if why := a.allow(u); why != "" {
				err = errors.New(why)
			}
		}
		if err != nil {
			if !errors.Is(err, http.ErrNoCookie) {
				a.clearCookie(w)
			}
			httpx.WriteError(w, r, httpx.Unauthenticated("sign in to continue"))
			return
		}
		a.touch(r.Context(), u.ID, a.o.Now())
		user := &protocol.User{ID: u.ID, DisplayName: u.DisplayName, Kind: u.Kind, Username: u.Username, Email: u.Email, Groups: u.Groups}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

// touch records last_seen_at at most every five minutes per user.
func (a *cookies) touch(ctx context.Context, id string, now time.Time) {
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

// signOut revokes the user's cookies, clears this one and audits the
// logout.
func (a *cookies) signOut(w http.ResponseWriter, r *http.Request) {
	if u, err := a.userFromCookie(r); err == nil {
		if err := a.o.Store.Users().RevokeSessions(r.Context(), u.ID); err != nil {
			a.o.Logger.Warn("auth: revoking session cookies", "err", err)
		}
		_ = a.o.Store.Audit(r.Context(), store.AuditEvent{At: a.o.Now().UnixMilli(), UserID: u.ID, Action: "logout"})
	}
	a.clearCookie(w)
}
