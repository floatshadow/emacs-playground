package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// FlowCookie carries the state, nonce and PKCE verifier of one sign-in
// from GET /auth/oidc/start to the callback.
const FlowCookie = "pg_oidc"

// flowLifetime bounds how long a sign-in at the provider may take.
const flowLifetime = 10 * time.Minute

// flow is the signed content of FlowCookie.
type flow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	// Next is the same-origin path to return to.
	Next string `json:"r,omitempty"`
}

// oidcAuth is auth mode oidc: the authorization code flow with PKCE
// against the provider discovered from auth.oidc.issuer.
type oidcAuth struct {
	cookies
	cfg         protocol.OIDCConfig
	secret      string
	redirectURL string
	flows       *Signer
	client      *http.Client

	pmu  sync.Mutex
	prov *provider
	disc *discovery // the running or last failed discovery
}

// discoveryBackoff is how long a failed discovery is answered from cache
// before the next attempt.
const discoveryBackoff = 15 * time.Second

// discovery is one attempt; done is closed when p or err is set.
type discovery struct {
	done chan struct{}
	at   time.Time
	p    *provider
	err  error
}

// provider is the discovered provider, cached after the first success.
type provider struct {
	verifier   *oidc.IDTokenVerifier
	oauth      oauth2.Config
	endSession string
}

func newOIDC(o Options) (*oidcAuth, error) {
	if o.Signer == nil {
		return nil, errors.New("auth: oidc mode needs a cookie signer")
	}
	if o.OIDC.Issuer == "" || o.OIDC.ClientID == "" {
		return nil, errors.New("auth: oidc mode needs auth.oidc.issuer and auth.oidc.clientId")
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	a := &oidcAuth{
		cookies:     newCookies(o, protocol.UserOIDC),
		cfg:         o.OIDC,
		secret:      o.ClientSecret,
		redirectURL: strings.TrimRight(o.PublicURL, "/") + PathOIDCCallback,
		flows:       o.Signer.derive("oidc flow"),
		client:      o.HTTPClient,
	}
	// The stored email was verified at sign-in (refuse).
	verified := true
	a.allow = func(u *store.User) string {
		return a.refuse(identity{email: u.Email, emailVerified: &verified, groups: u.Groups})
	}
	return a, nil
}

func (a *oidcAuth) Mode() protocol.AuthMode { return protocol.AuthOIDC }

func (a *oidcAuth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PathOIDCStart, a.start)
	mux.HandleFunc("GET "+PathOIDCCallback, a.callback)
	mux.Handle("POST "+PathLogout, httpx.Handle(a.logout))
}

// Discover fetches the provider's discovery document now (the gateway
// calls it at start to report a misconfiguration early); a failure is
// retried by a later sign-in after discoveryBackoff.
func (a *oidcAuth) Discover(ctx context.Context) error {
	_, err := a.provider(ctx)
	return err
}

// provider returns the discovered provider. Concurrent callers share one
// discovery, which runs without the lock, and a failure is answered from
// cache for discoveryBackoff, so an unreachable provider never queues
// requests behind one another.
func (a *oidcAuth) provider(ctx context.Context) (*provider, error) {
	a.pmu.Lock()
	if a.prov != nil {
		a.pmu.Unlock()
		return a.prov, nil
	}
	d := a.disc
	switch {
	case d == nil:
	case d.at.IsZero(): // running
		a.pmu.Unlock()
		select {
		case <-d.done:
			return d.p, d.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case a.o.Now().Sub(d.at) < discoveryBackoff:
		a.pmu.Unlock()
		return nil, d.err
	}
	d = &discovery{done: make(chan struct{})}
	a.disc = d
	a.pmu.Unlock()

	// Not the caller's context: other callers wait for this result.
	p, err := a.discover()
	a.pmu.Lock()
	d.p, d.err, d.at = p, err, a.o.Now()
	if err == nil {
		a.prov = p
	}
	a.pmu.Unlock()
	close(d.done)
	return p, err
}

func (a *oidcAuth) discover() (*provider, error) {
	ctx, cancel := context.WithTimeout(oidc.ClientContext(context.Background(), a.client), 15*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, a.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: oidc discovery for %s: %w", a.cfg.Issuer, err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&meta)
	scopes := []string{oidc.ScopeOpenID}
	for _, s := range a.cfg.Scopes {
		if !slices.Contains(scopes, s) {
			scopes = append(scopes, s)
		}
	}
	ep := p.Endpoint()
	if a.secret == "" {
		// Public client: client_id in the body, no Basic auth.
		ep.AuthStyle = oauth2.AuthStyleInParams
	}
	return &provider{
		// The key set fetches JWKS with our client; its context is never
		// cancelled.
		verifier: p.VerifierContext(oidc.ClientContext(context.Background(), a.client),
			&oidc.Config{ClientID: a.cfg.ClientID, Now: a.o.Now}),
		oauth: oauth2.Config{
			ClientID: a.cfg.ClientID, ClientSecret: a.secret, Endpoint: ep,
			RedirectURL: a.redirectURL, Scopes: scopes,
		},
		endSession: meta.EndSession,
	}, nil
}

func randomString() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return b64.EncodeToString(b)
}

// safeNext returns next when it is a same-origin path, else "/".
func safeNext(next string) string {
	if next == "" || len(next) > 2048 || !strings.HasPrefix(next, "/") ||
		strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") || strings.HasPrefix(next, PathLogin) {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return next
}

func (a *oidcAuth) flowCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: FlowCookie, Value: value, Path: protocol.APIPrefix + "/auth/oidc/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.secure,
		// Lax: the provider's redirect back is a top-level GET.
		SameSite: http.SameSiteLaxMode,
	}
}

// start implements GET /api/v1/auth/oidc/start[?next=/path]: a redirect to
// the provider's authorization endpoint.
func (a *oidcAuth) start(w http.ResponseWriter, r *http.Request) {
	p, err := a.provider(r.Context())
	if err != nil {
		a.fail(w, r, protocol.LoginErrorProvider, "discovery", err)
		return
	}
	f := flow{State: randomString(), Nonce: randomString(), Verifier: oauth2.GenerateVerifier(), Next: safeNext(r.URL.Query().Get("next"))}
	payload, _ := json.Marshal(f)
	http.SetCookie(w, a.flowCookie(a.flows.Sign(string(payload), a.o.Now().Add(flowLifetime)), int(flowLifetime/time.Second)))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.oauth.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
}

// fail logs a failed sign-in and sends the browser to the login screen
// with the reason (protocol.LoginError).
func (a *oidcAuth) fail(w http.ResponseWriter, r *http.Request, code protocol.LoginError, what string, err error) {
	httpx.Logger(r.Context()).Warn("auth: oidc sign-in failed", "reason", code, "step", what, "err", err)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, PathLogin+"?error="+url.QueryEscape(string(code)), http.StatusFound)
}

// identity is what a login takes from the ID token.
type identity struct {
	subject, displayName, username, email string
	emailVerified                         *bool
	groups                                []string
}

// callback implements GET /api/v1/auth/oidc/callback: checks the flow
// cookie, exchanges the code with the PKCE verifier, verifies the ID token
// (signature, issuer, audience, expiry, nonce), applies the allow-lists,
// stores the user and sets the session cookie.
func (a *oidcAuth) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, cerr := r.Cookie(FlowCookie)
	http.SetCookie(w, a.flowCookie("", -1))
	if e := q.Get("error"); e != "" {
		a.fail(w, r, protocol.LoginErrorProvider, "authorize", fmt.Errorf("%s: %s", e, q.Get("error_description")))
		return
	}
	if cerr != nil {
		a.fail(w, r, protocol.LoginErrorState, "flow cookie", cerr)
		return
	}
	payload, _, err := a.flows.Verify(c.Value, a.o.Now())
	var f flow
	if err == nil {
		err = json.Unmarshal([]byte(payload), &f)
	}
	if err != nil {
		a.fail(w, r, protocol.LoginErrorState, "flow cookie", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(f.State)) != 1 {
		a.fail(w, r, protocol.LoginErrorState, "state", errors.New("state does not match"))
		return
	}
	p, err := a.provider(r.Context())
	if err != nil {
		a.fail(w, r, protocol.LoginErrorProvider, "discovery", err)
		return
	}
	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, a.client)
	tok, err := p.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		a.fail(w, r, protocol.LoginErrorToken, "code exchange", err)
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		a.fail(w, r, protocol.LoginErrorToken, "code exchange", errors.New("no id_token in the token response"))
		return
	}
	idt, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		a.fail(w, r, protocol.LoginErrorToken, "id token", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(f.Nonce)) != 1 {
		a.fail(w, r, protocol.LoginErrorToken, "id token", errors.New("nonce does not match"))
		return
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		a.fail(w, r, protocol.LoginErrorToken, "id token claims", err)
		return
	}
	id := a.identity(idt.Issuer+"|"+idt.Subject, claims)
	if why := a.refuse(id); why != "" {
		_ = a.o.Store.Audit(r.Context(), store.AuditEvent{At: a.o.Now().UnixMilli(), Action: "login.refused",
			Details: fmt.Sprintf("subject=%s email=%s reason=%s", id.subject, id.email, why)})
		a.fail(w, r, protocol.LoginErrorForbidden, "allow-list", errors.New(why))
		return
	}
	u, err := a.upsert(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = a.o.Store.Audit(r.Context(), store.AuditEvent{At: a.o.Now().UnixMilli(), UserID: u.ID, Action: "login.oidc",
		Details: fmt.Sprintf("email=%s groups=%d", u.Email, len(u.Groups))})
	a.setCookie(w, u)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, f.Next, http.StatusFound)
}

func (a *oidcAuth) identity(subject string, claims map[string]any) identity {
	str := func(k string) string { s, _ := claims[k].(string); return strings.TrimSpace(s) }
	id := identity{subject: subject, email: str("email"), username: str(a.cfg.UsernameClaim)}
	switch v := claims["email_verified"].(type) {
	case bool:
		id.emailVerified = &v
	case string: // some providers send "true"/"false"
		b := strings.EqualFold(v, "true")
		id.emailVerified = &b
	}
	if id.username == "" {
		id.username = id.email
	}
	id.displayName = str("name")
	if id.displayName == "" {
		id.displayName = id.username
	}
	if id.displayName == "" {
		id.displayName = "Learner"
	}
	switch g := claims[a.cfg.GroupsClaim].(type) {
	case []any:
		for _, v := range g {
			if s, ok := v.(string); ok && s != "" {
				id.groups = append(id.groups, s)
			}
		}
	case string:
		if g != "" {
			id.groups = []string{g}
		}
	}
	return id
}

// refuse applies auth.oidc.allowedEmailDomains (which needs email_verified
// true) and allowedGroups; it returns why the identity is refused, or "".
func (a *oidcAuth) refuse(id identity) string {
	if len(a.cfg.AllowedEmailDomains) > 0 {
		at := strings.LastIndexByte(id.email, '@')
		switch {
		case at < 0:
			return "no email in the ID token"
		case id.emailVerified == nil || !*id.emailVerified:
			// Without email_verified true anyone could claim an address
			// in the domain.
			return "email not verified"
		}
		domain := id.email[at+1:]
		if !slices.ContainsFunc(a.cfg.AllowedEmailDomains, func(d string) bool { return strings.EqualFold(d, domain) }) {
			return "email domain " + domain + " not allowed"
		}
	}
	if len(a.cfg.AllowedGroups) > 0 && !slices.ContainsFunc(id.groups, func(g string) bool { return slices.Contains(a.cfg.AllowedGroups, g) }) {
		return "not in an allowed group"
	}
	return ""
}

// UserID is the stored id of an OIDC subject (issuer + "|" + sub): stable,
// short and safe in volume names.
func UserID(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return "u-" + hex.EncodeToString(sum[:12])
}

// upsert creates or refreshes the user of id.
func (a *oidcAuth) upsert(ctx context.Context, id identity) (*store.User, error) {
	now := a.o.Now().UnixMilli()
	u, err := a.o.Store.Users().BySubject(ctx, id.subject)
	switch {
	case errors.Is(err, store.ErrNotFound):
		u = &store.User{ID: UserID(id.subject), Kind: protocol.UserOIDC, Subject: id.subject, CreatedAt: now}
	case err != nil:
		return nil, err
	}
	u.Kind = protocol.UserOIDC
	u.DisplayName, u.Username, u.Email, u.Groups = id.displayName, id.username, id.email, id.groups
	u.LastSeenAt = now
	if err := a.o.Store.Users().Put(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// logout implements POST /api/v1/auth/logout: clears the cookie and, when
// auth.oidc.providerLogout is on and the provider has an
// end_session_endpoint, answers where to sign out there too.
func (a *oidcAuth) logout(w http.ResponseWriter, r *http.Request) error {
	a.signOut(w, r)
	if a.cfg.ProviderLogout {
		if p, err := a.provider(r.Context()); err == nil && p.endSession != "" {
			if u, err := url.Parse(p.endSession); err == nil {
				v := u.Query()
				v.Set("client_id", a.cfg.ClientID)
				v.Set("post_logout_redirect_uri", strings.TrimRight(a.o.PublicURL, "/")+PathLogin)
				u.RawQuery = v.Encode()
				httpx.WriteJSON(w, http.StatusOK, protocol.LogoutResult{RedirectURL: u.String()})
				return nil
			}
		}
	}
	httpx.NoContent(w)
	return nil
}
