package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/gateway/auth/oidctest"
	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestSignerRoundTrip(t *testing.T) {
	s, err := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	for _, uid := range []string{"local", "u-abc", "https://accounts.example.com|1234.5"} {
		v := s.Sign(uid, now.Add(time.Hour))
		if strings.Count(v, ".") != 2 {
			t.Fatalf("cookie %q is not three dot-separated parts", v)
		}
		got, exp, err := s.Verify(v, now)
		if err != nil || got != uid || !exp.Equal(now.Add(time.Hour)) {
			t.Fatalf("verify %q: %v %q %v", uid, err, got, exp)
		}
	}
	v := s.Sign("u1", now.Add(time.Hour))
	if _, _, err := s.Verify(v, now.Add(2*time.Hour)); !errors.Is(err, ErrCookieExpired) {
		t.Fatalf("expired: %v", err)
	}
	parts := strings.Split(v, ".")
	forged := b64.EncodeToString([]byte("admin")) + "." + parts[1] + "." + parts[2]
	if _, _, err := s.Verify(forged, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("forged user: %v", err)
	}
	extended := parts[0] + ".9999999999." + parts[2]
	if _, _, err := s.Verify(extended, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("extended expiry: %v", err)
	}
	other, _ := NewSigner([]byte("another-secret-of-enough-length"))
	if _, _, err := other.Verify(v, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("other key: %v", err)
	}
	for _, bad := range []string{"", "a.b", "a.b.c.d", "!!.1.x", parts[0] + ".x." + parts[2]} {
		if _, _, err := s.Verify(bad, now); err == nil {
			t.Fatalf("%q verified", bad)
		}
	}
	if _, err := NewSigner([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestLoadOrCreateSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cookie.secret")
	k1, err := LoadOrCreateSecret(path)
	if err != nil || len(k1) < 32 {
		t.Fatalf("%v %d", err, len(k1))
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	k2, err := LoadOrCreateSecret(path)
	if err != nil || string(k1) != string(k2) {
		t.Fatal("secret not stable")
	}
	short := filepath.Join(t.TempDir(), "short")
	_ = os.WriteFile(short, []byte("abc\n"), 0o600)
	if _, err := LoadOrCreateSecret(short); err == nil {
		t.Fatal("short secret accepted")
	}
}

const testPublicURL = "https://emacs.example.edu"

type fixture struct {
	t    *testing.T
	st   store.Store
	auth Authenticator
	mux  *http.ServeMux
	idp  *oidctest.Provider
	now  time.Time
}

// newFixture starts auth in mode against a fake provider; mutate adjusts
// the options (auth.oidc configuration) before New.
func newFixture(t *testing.T, mode protocol.AuthMode, mutate func(o *Options)) *fixture {
	t.Helper()
	st, err := store.OpenJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	f := &fixture{t: t, st: st, now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	f.idp = oidctest.New("emacs-playground", "s3cret")
	t.Cleanup(f.idp.Server.Close)
	f.idp.Now = func() time.Time { return f.now }
	f.idp.SetClaims(map[string]any{"sub": "42", "email": "ada@example.edu", "email_verified": true, "name": "Ada Lovelace",
		"preferred_username": "ada", "groups": []string{"cs101", "staff"}})
	o := Options{
		PublicURL: testPublicURL, SessionDays: 30, Signer: signer, Store: st, Now: func() time.Time { return f.now },
		OIDC: protocol.OIDCConfig{
			Issuer: f.idp.Issuer, ClientID: "emacs-playground", Scopes: []string{"openid", "email", "profile"},
			UsernameClaim: "preferred_username", GroupsClaim: "groups", ProviderLogout: true,
		},
		ClientSecret: "s3cret",
	}
	if mutate != nil {
		mutate(&o)
	}
	a, err := New(mode, o)
	if err != nil {
		t.Fatal(err)
	}
	f.auth = a
	f.mux = http.NewServeMux()
	a.Routes(f.mux)
	f.mux.Handle("GET /api/v1/me", a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		_ = json.NewEncoder(w).Encode(u)
	})))
	return f
}

func (f *fixture) do(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, r)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	return cookieNamed(rec, protocol.SessionCookie)
}

// begin runs GET /auth/oidc/start and the provider's authorization step;
// it returns the callback path with query and the flow cookie.
func (f *fixture) begin(next string) (string, *http.Cookie) {
	f.t.Helper()
	rec := f.do("GET", PathOIDCStart+"?next="+url.QueryEscape(next), "")
	if rec.Code != http.StatusFound {
		f.t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	flow := cookieNamed(rec, FlowCookie)
	if flow == nil {
		f.t.Fatal("no flow cookie")
	}
	back, err := f.idp.Authorize(rec.Header().Get("Location"))
	if err != nil {
		f.t.Fatal(err)
	}
	u, _ := url.Parse(back)
	if u.Scheme+"://"+u.Host+u.Path != testPublicURL+PathOIDCCallback {
		f.t.Fatalf("redirect uri %s", back)
	}
	return u.Path + "?" + u.RawQuery, flow
}

// login signs in and returns the session cookie.
func (f *fixture) login() *http.Cookie {
	f.t.Helper()
	cb, flow := f.begin("/")
	rec := f.do("GET", cb, "", flow)
	c := sessionCookie(rec)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" || c == nil {
		f.t.Fatalf("callback: %d %s %v", rec.Code, rec.Header().Get("Location"), rec.Result().Cookies())
	}
	return c
}

// refused runs a sign-in and checks it lands on /login?error=want without
// a session cookie.
func (f *fixture) refused(want protocol.LoginError, callback func(cb string, flow *http.Cookie) *httptest.ResponseRecorder) {
	f.t.Helper()
	cb, flow := f.begin("/")
	var rec *httptest.ResponseRecorder
	if callback != nil {
		rec = callback(cb, flow)
	} else {
		rec = f.do("GET", cb, "", flow)
	}
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/login?error="+string(want) || sessionCookie(rec) != nil {
		f.t.Fatalf("want /login?error=%s, got %d %s %v", want, rec.Code, rec.Header().Get("Location"), rec.Result().Cookies())
	}
}

func me(t *testing.T, f *fixture, c *http.Cookie) protocol.User {
	t.Helper()
	rec := f.do("GET", "/api/v1/me", "", c)
	var u protocol.User
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if rec.Code != 200 {
		t.Fatalf("me: %d %s", rec.Code, rec.Body)
	}
	return u
}

func TestOIDCFlow(t *testing.T) {
	f := newFixture(t, protocol.AuthOIDC, nil)
	ctx := context.Background()
	if rec := f.do("GET", "/api/v1/me", ""); rec.Code != 401 || !strings.Contains(rec.Body.String(), `"unauthenticated"`) {
		t.Fatalf("anonymous me: %d %s", rec.Code, rec.Body)
	}

	// Start: PKCE S256, state, nonce and the configured scopes, with the
	// flow bound to a short-lived signed cookie.
	rec := f.do("GET", PathOIDCStart+"?next="+url.QueryEscape("/lessons/00-a/01-b"), "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	q := loc.Query()
	if rec.Code != 302 || loc.Host != strings.TrimPrefix(f.idp.Issuer, "http://") || loc.Path != "/authorize" ||
		q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) < 43 || q.Get("state") == "" || q.Get("nonce") == "" ||
		q.Get("scope") != "openid email profile" || q.Get("client_id") != "emacs-playground" ||
		q.Get("redirect_uri") != testPublicURL+"/api/v1/auth/oidc/callback" {
		t.Fatalf("start: %d %s", rec.Code, loc)
	}
	flow := cookieNamed(rec, FlowCookie)
	if flow == nil || !flow.HttpOnly || !flow.Secure || flow.SameSite != http.SameSiteLaxMode || flow.MaxAge != 600 || flow.Path != "/api/v1/auth/oidc/" {
		t.Fatalf("flow cookie %+v", flow)
	}
	back, err := f.idp.Authorize(loc.String())
	if err != nil {
		t.Fatal(err)
	}
	bu, _ := url.Parse(back)
	rec = f.do("GET", bu.Path+"?"+bu.RawQuery, "", flow)
	c := sessionCookie(rec)
	if rec.Code != 302 || rec.Header().Get("Location") != "/lessons/00-a/01-b" || c == nil {
		t.Fatalf("callback: %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 30*86400 {
		t.Fatalf("cookie %+v", c)
	}
	if fc := cookieNamed(rec, FlowCookie); fc == nil || fc.MaxAge >= 0 {
		t.Fatalf("flow cookie not cleared: %+v", fc)
	}
	// The code is single use, and the flow cookie is gone: a replay fails.
	if rec := f.do("GET", bu.Path+"?"+bu.RawQuery, ""); rec.Header().Get("Location") != "/login?error=state" {
		t.Fatalf("replay: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	subject := f.idp.Issuer + "|42"
	u := me(t, f, c)
	if u.Kind != protocol.UserOIDC || u.ID != UserID(subject) || u.DisplayName != "Ada Lovelace" || u.Username != "ada" ||
		u.Email != "ada@example.edu" || strings.Join(u.Groups, ",") != "cs101,staff" {
		t.Fatalf("me: %+v", u)
	}
	rec0, err := f.st.Users().BySubject(ctx, subject)
	if err != nil || rec0.ID != u.ID {
		t.Fatalf("stored %v %+v", err, rec0)
	}

	// The next login refreshes email, name and groups on the same user.
	f.now = f.now.Add(time.Hour)
	f.idp.SetClaims(map[string]any{"sub": "42", "email": "ada@cs.example.edu", "groups": []string{"cs102"}})
	c2 := f.login()
	u2 := me(t, f, c2)
	if u2.ID != u.ID || u2.Email != "ada@cs.example.edu" || u2.Username != "ada@cs.example.edu" || u2.DisplayName != "ada@cs.example.edu" ||
		strings.Join(u2.Groups, ",") != "cs102" {
		t.Fatalf("refreshed: %+v", u2)
	}
	if rec1, _ := f.st.Users().Get(ctx, u.ID); rec1.CreatedAt != rec0.CreatedAt {
		t.Fatalf("created at changed: %d %d", rec1.CreatedAt, rec0.CreatedAt)
	}

	// The cookie is never extended: still valid just before sessionDays
	// after the sign-in, refused from then on.
	if rec := f.do("GET", "/api/v1/me", "", c); rec.Code != 200 || sessionCookie(rec) != nil {
		t.Fatalf("use reissued the cookie: %d %v", rec.Code, rec.Result().Cookies())
	}
	f.now = f.now.Add(30*24*time.Hour - 2*time.Hour)
	if rec := f.do("GET", "/api/v1/me", "", c); rec.Code != 200 {
		t.Fatalf("cookie before its expiry: %d", rec.Code)
	}
	f.now = f.now.Add(time.Hour)
	if rec := f.do("GET", "/api/v1/me", "", c); rec.Code != 401 {
		t.Fatalf("cookie past sessionDays: %d", rec.Code)
	}
	// Logout clears the cookie and revokes every cookie of the user (c2
	// from the other sign-in too); no end_session_endpoint: 204.
	rec = f.do("POST", PathLogout, "", c2)
	if rec.Code != 204 || sessionCookie(rec) == nil || sessionCookie(rec).MaxAge >= 0 {
		t.Fatalf("logout %d %v", rec.Code, rec.Result().Cookies())
	}
	if rec := f.do("GET", "/api/v1/me", "", c2); rec.Code != 401 || sessionCookie(rec) == nil {
		t.Fatalf("revoked cookie: %d", rec.Code)
	}
	// A new sign-in works, and its cookie survives the old one's revocation.
	c3 := f.login()
	if u3 := me(t, f, c3); u3.ID != u.ID {
		t.Fatalf("sign-in after logout %+v", u3)
	}
	if rec := f.do("GET", "/api/v1/me", "", c2); rec.Code != 401 {
		t.Fatalf("revoked cookie valid again: %d", rec.Code)
	}
	log, _ := f.st.AuditLog(ctx, 0)
	actions := []string{}
	for _, e := range log {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "login.oidc,logout,login.oidc,login.oidc" {
		t.Fatalf("audit %v", actions)
	}
}

func TestOIDCRefusals(t *testing.T) {
	f := newFixture(t, protocol.AuthOIDC, nil)
	tamper := func(k string, v any) func(map[string]any) { return func(c map[string]any) { c[k] = v } }

	t.Run("bad state", func(t *testing.T) {
		f.refused(protocol.LoginErrorState, func(cb string, flow *http.Cookie) *httptest.ResponseRecorder {
			u, _ := url.Parse(cb)
			q := u.Query()
			q.Set("state", "forged")
			return f.do("GET", u.Path+"?"+q.Encode(), "", flow)
		})
	})
	t.Run("no flow cookie", func(t *testing.T) {
		f.refused(protocol.LoginErrorState, func(cb string, _ *http.Cookie) *httptest.ResponseRecorder { return f.do("GET", cb, "") })
	})
	t.Run("forged flow cookie", func(t *testing.T) {
		f.refused(protocol.LoginErrorState, func(cb string, flow *http.Cookie) *httptest.ResponseRecorder {
			// A session cookie's signature does not verify as a flow.
			signer, _ := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
			forged := *flow
			payload, exp, _ := f.auth.(*oidcAuth).flows.Verify(flow.Value, f.now)
			forged.Value = signer.Sign(payload, exp)
			return f.do("GET", cb, "", &forged)
		})
	})
	t.Run("flow expired", func(t *testing.T) {
		f.refused(protocol.LoginErrorState, func(cb string, flow *http.Cookie) *httptest.ResponseRecorder {
			f.now = f.now.Add(11 * time.Minute)
			return f.do("GET", cb, "", flow)
		})
	})
	t.Run("provider denies", func(t *testing.T) {
		f.idp.Deny = true
		defer func() { f.idp.Deny = false }()
		f.refused(protocol.LoginErrorProvider, nil)
	})
	for name, tm := range map[string]func(map[string]any){
		"nonce mismatch": tamper("nonce", "replayed"),
		"no nonce":       func(c map[string]any) { delete(c, "nonce") },
		"wrong issuer":   tamper("iss", "https://evil.example"),
		"wrong audience": tamper("aud", "another-client"),
		"expired token":  func(c map[string]any) { c["exp"] = f.now.Add(-time.Minute).Unix() },
	} {
		t.Run(name, func(t *testing.T) {
			f.idp.SetTamper(tm)
			defer f.idp.SetTamper(nil)
			f.refused(protocol.LoginErrorToken, nil)
		})
	}
	if f.idp.Tokens == 0 {
		t.Fatal("token requests were not made")
	}
	t.Run("wrong client secret", func(t *testing.T) {
		g := newFixture(t, protocol.AuthOIDC, func(o *Options) { o.ClientSecret = "wrong" })
		g.refused(protocol.LoginErrorToken, nil)
	})
	if users, _ := f.st.Users().BySubject(context.Background(), f.idp.Issuer+"|42"); users != nil {
		t.Fatalf("a refused login stored a user: %+v", users)
	}
}

func TestOIDCAllowLists(t *testing.T) {
	f := newFixture(t, protocol.AuthOIDC, func(o *Options) {
		o.OIDC.AllowedEmailDomains = []string{"example.edu"}
		o.OIDC.AllowedGroups = []string{"cs101", "staff"}
	})
	base := func() map[string]any {
		return map[string]any{"sub": "7", "email": "bob@example.edu", "email_verified": true, "groups": []string{"cs101"}}
	}
	cases := []struct {
		name string
		edit func(map[string]any)
		ok   bool
	}{
		{"allowed", func(map[string]any) {}, true},
		{"domain case-insensitive", func(c map[string]any) { c["email"] = "Bob@EXAMPLE.edu" }, true},
		{"verified as string", func(c map[string]any) { c["email_verified"] = "true" }, true},
		{"email_verified missing", func(c map[string]any) { delete(c, "email_verified") }, false},
		{"other domain", func(c map[string]any) { c["email"] = "bob@evil.example" }, false},
		{"subdomain", func(c map[string]any) { c["email"] = "bob@mail.example.edu" }, false},
		{"missing email", func(c map[string]any) { delete(c, "email") }, false},
		{"unverified email", func(c map[string]any) { c["email_verified"] = false }, false},
		{"unverified email as string", func(c map[string]any) { c["email_verified"] = "false" }, false},
		{"other group", func(c map[string]any) { c["groups"] = []string{"cs102"} }, false},
		{"no groups claim", func(c map[string]any) { delete(c, "groups") }, false},
		{"second allowed group", func(c map[string]any) { c["groups"] = []string{"x", "staff"} }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.edit(c)
			f.idp.SetClaims(c)
			if tc.ok {
				f.login()
			} else {
				f.refused(protocol.LoginErrorForbidden, nil)
			}
		})
	}
	log, _ := f.st.AuditLog(context.Background(), 1)
	if len(log) != 1 || log[0].Action != "login.oidc" {
		t.Fatalf("audit %+v", log)
	}

	// The allow-lists are rechecked against the stored user on every
	// request: a tightened list takes effect without a new sign-in.
	f.idp.SetClaims(base())
	c := f.login()
	if rec := f.do("GET", "/api/v1/me", "", c); rec.Code != 200 {
		t.Fatalf("before: %d", rec.Code)
	}
	f.auth.(*oidcAuth).cfg.AllowedGroups = []string{"staff"}
	if rec := f.do("GET", "/api/v1/me", "", c); rec.Code != 401 {
		t.Fatalf("after the group was dropped: %d", rec.Code)
	}
}

func TestOIDCClaimNamesAndPublicClient(t *testing.T) {
	f := newFixture(t, protocol.AuthOIDC, func(o *Options) {
		o.ClientSecret = ""
		o.OIDC.UsernameClaim = "upn"
		o.OIDC.GroupsClaim = "roles"
		o.OIDC.Scopes = []string{"email", "groups"}
	})
	f.idp.ClientSecret = "" // public client: PKCE only
	f.idp.SetClaims(map[string]any{"sub": "abc", "upn": "carol@corp", "roles": "teachers", "groups": []string{"ignored"}})
	rec := f.do("GET", PathOIDCStart, "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Query().Get("scope") != "openid email groups" {
		t.Fatalf("scope %q", loc.Query().Get("scope"))
	}
	u := me(t, f, f.login())
	if u.Username != "carol@corp" || u.DisplayName != "carol@corp" || strings.Join(u.Groups, ",") != "teachers" || u.Email != "" {
		t.Fatalf("me %+v", u)
	}
}

func TestOIDCProviderLogout(t *testing.T) {
	f := newFixture(t, protocol.AuthOIDC, nil)
	f.idp.EndSession = true
	c := f.login()
	rec := f.do("POST", PathLogout, "", c)
	var res protocol.LogoutResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	u, _ := url.Parse(res.RedirectURL)
	if rec.Code != 200 || sessionCookie(rec).MaxAge >= 0 || u.Path != "/logout" || u.Query().Get("realm") != "main" ||
		u.Query().Get("client_id") != "emacs-playground" || u.Query().Get("post_logout_redirect_uri") != testPublicURL+"/login" {
		t.Fatalf("logout %d %s", rec.Code, rec.Body)
	}
	g := newFixture(t, protocol.AuthOIDC, func(o *Options) { o.OIDC.ProviderLogout = false })
	g.idp.EndSession = true
	if rec := g.do("POST", PathLogout, "", g.login()); rec.Code != 204 {
		t.Fatalf("provider logout off: %d", rec.Code)
	}
}

func TestOIDCDiscoveryFailures(t *testing.T) {
	// The issuer must match the discovery document exactly.
	f := newFixture(t, protocol.AuthOIDC, func(o *Options) { o.OIDC.Issuer += "/" })
	if err := f.auth.(*oidcAuth).Discover(context.Background()); err == nil {
		t.Fatal("issuer mismatch accepted")
	}
	if rec := f.do("GET", PathOIDCStart, ""); rec.Code != 302 || rec.Header().Get("Location") != "/login?error=provider" {
		t.Fatalf("start: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	// An unreachable provider is retried on the next sign-in.
	g := newFixture(t, protocol.AuthOIDC, nil)
	g.idp.Server.Close()
	if rec := g.do("GET", PathOIDCStart, ""); rec.Header().Get("Location") != "/login?error=provider" {
		t.Fatalf("down: %s", rec.Header().Get("Location"))
	}
	if g.auth.(*oidcAuth).prov != nil {
		t.Fatal("failed discovery cached")
	}
}

// TestOIDCDiscoveryShared: concurrent sign-ins share one discovery, and a
// failure is answered from cache until discoveryBackoff has passed.
func TestOIDCDiscoveryShared(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer down.Close()
	f := newFixture(t, protocol.AuthOIDC, func(o *Options) { o.OIDC.Issuer = down.URL })
	var wg sync.WaitGroup
	locs := make(chan string, 8)
	for range 8 {
		wg.Go(func() { locs <- f.do("GET", PathOIDCStart, "").Header().Get("Location") })
	}
	for hits.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let the others queue
	close(release)
	wg.Wait()
	close(locs)
	for l := range locs {
		if l != "/login?error=provider" {
			t.Fatalf("start while down: %s", l)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("%d discoveries for 8 concurrent sign-ins", n)
	}
	f.do("GET", PathOIDCStart, "")
	if n := hits.Load(); n != 1 {
		t.Fatalf("retried within the backoff: %d", n)
	}
	f.now = f.now.Add(discoveryBackoff)
	f.do("GET", PathOIDCStart, "")
	if n := hits.Load(); n != 2 {
		t.Fatalf("not retried after the backoff: %d", n)
	}
}

func TestOIDCRefusesOtherUserKinds(t *testing.T) {
	f := newFixture(t, protocol.AuthOIDC, nil)
	_ = f.st.Users().Put(context.Background(), &store.User{ID: "local", Kind: protocol.UserLocal, CreatedAt: 1})
	signer, _ := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	c := &http.Cookie{Name: protocol.SessionCookie, Value: signer.Sign("local|0", f.now.Add(time.Hour))}
	if rec := f.do("GET", "/api/v1/me", "", c); rec.Code != 401 {
		t.Fatalf("local user in oidc mode: %d", rec.Code)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "/",
		"/lessons/00-a/01-b?x=1": "/lessons/00-a/01-b?x=1",
		"//evil.example/":        "/",
		"/\\evil.example":        "/",
		"https://evil.example/":  "/",
		"lessons":                "/",
		"/login?error=state":     "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoneAuth(t *testing.T) {
	f := newFixture(t, protocol.AuthNone, nil)
	rec := f.do("GET", "/api/v1/me", "")
	var u protocol.User
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if rec.Code != 200 || u.ID != protocol.LocalUserID || u.Kind != protocol.UserLocal {
		t.Fatalf("%d %+v", rec.Code, u)
	}
	if _, err := f.st.Users().Get(context.Background(), protocol.LocalUserID); err != nil {
		t.Fatalf("local user not stored: %v", err)
	}
	if rec := f.do("GET", PathOIDCStart, ""); rec.Code != 404 {
		t.Fatalf("oidc in none mode: %d", rec.Code)
	}
	if rec := f.do("POST", PathLogout, ""); rec.Code != 204 {
		t.Fatalf("logout: %d", rec.Code)
	}
}

func TestNewOIDCNeedsConfig(t *testing.T) {
	signer, _ := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	if _, err := New(protocol.AuthOIDC, Options{Signer: signer}); err == nil {
		t.Fatal("oidc without issuer accepted")
	}
	if _, err := New(protocol.AuthOIDC, Options{OIDC: protocol.OIDCConfig{Issuer: "https://idp", ClientID: "x"}}); err == nil {
		t.Fatal("oidc without signer accepted")
	}
	if _, err := New("password", Options{}); err == nil {
		t.Fatal("unknown mode accepted")
	}
}
