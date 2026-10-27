package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestParseInvites(t *testing.T) {
	data, err := os.ReadFile("../../../deploy/invites.example.txt")
	if err != nil {
		t.Fatal(err)
	}
	specs, err := ParseInvites(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("specs %+v", specs)
	}
	a := specs[0]
	if a.Code != "spring-cohort-7f3a9c" || a.Label != "cohort-A" || a.MaxUses != 40 || a.Until.Format("2006-01-02") != "2027-03-01" {
		t.Fatalf("first %+v", a)
	}
	// Valid through the whole of the until date.
	if a.ExpiresAt() != time.Date(2027, 3, 2, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("expiry %d", a.ExpiresAt())
	}
	if b := specs[1]; b.Label != "workshop" || b.MaxUses != 25 || !b.Until.IsZero() || b.ExpiresAt() != 0 {
		t.Fatalf("second %+v", b)
	}
	ok, err := ParseInvites(strings.NewReader("# c\n\nlong-code-1 Evening class B uses=3\nlong-code-2\n"))
	if err != nil || ok[0].Label != "Evening class B" || ok[1].Label != "" || ok[1].MaxUses != 0 {
		t.Fatalf("%v %+v", err, ok)
	}
	for _, bad := range []string{
		"short",
		"long-code-1 uses=0",
		"long-code-1 uses=x",
		"long-code-1 until=2027-13-01",
		"long-code-1 color=red",
		"uses=3 long-code-1",
		"long-code-1\nlong-code-1",
	} {
		if _, err := ParseInvites(strings.NewReader(bad)); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
	if HashCode(" abc ") != HashCode("abc") || len(HashCode("abc")) != 64 {
		t.Fatal("hash")
	}
}

type fixture struct {
	st   store.Store
	auth Authenticator
	mux  *http.ServeMux
	now  time.Time
}

func newFixture(t *testing.T, mode protocol.AuthMode) *fixture {
	t.Helper()
	st, err := store.OpenJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := NewSigner([]byte("0123456789abcdef0123456789abcdef"))
	f := &fixture{st: st, now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	a, err := New(mode, Options{PublicURL: "https://emacs.example.edu", SessionDays: 30, Signer: signer, Store: st, Now: func() time.Time { return f.now }})
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
	r.RemoteAddr = "192.0.2.1:1234"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, r)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == protocol.SessionCookie {
			return c
		}
	}
	return nil
}

func TestInviteFlow(t *testing.T) {
	f := newFixture(t, protocol.AuthInvite)
	ctx := context.Background()
	if err := f.st.Invites().Sync(ctx, []store.Invite{
		{CodeHash: HashCode("spring-cohort-7f3a9c"), Label: "cohort-A", MaxUses: 1},
		{CodeHash: HashCode("expired-code-1"), ExpiresAt: f.now.UnixMilli()},
	}); err != nil {
		t.Fatal(err)
	}
	if rec := f.do("GET", "/api/v1/me", ""); rec.Code != 401 || !strings.Contains(rec.Body.String(), `"unauthenticated"`) {
		t.Fatalf("anonymous me: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do("POST", PathInviteLogin, `{"code":"wrong-code-123"}`); rec.Code != 403 {
		t.Fatalf("bad code: %d %s", rec.Code, rec.Body)
	}
	if rec := f.do("POST", PathInviteLogin, `{"code":"expired-code-1"}`); rec.Code != 403 || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("expired: %d %s", rec.Code, rec.Body)
	}
	rec := f.do("POST", PathInviteLogin, `{"code":" spring-cohort-7f3a9c "}`)
	if rec.Code != 204 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	c := sessionCookie(rec)
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 30*86400 {
		t.Fatalf("cookie %+v", c)
	}
	rec = f.do("GET", "/api/v1/me", "", c)
	var u protocol.User
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if rec.Code != 200 || u.Kind != protocol.UserInvite || u.DisplayName != "cohort-A" || !strings.HasPrefix(u.ID, "u-") {
		t.Fatalf("me: %d %+v", rec.Code, u)
	}
	// Signed in already: logging in again keeps the user and the use.
	if rec := f.do("POST", PathInviteLogin, `{"code":"spring-cohort-7f3a9c"}`, c); rec.Code != 204 {
		t.Fatalf("relogin: %d %s", rec.Code, rec.Body)
	}
	// The single use is consumed for a new browser.
	if rec := f.do("POST", PathInviteLogin, `{"code":"spring-cohort-7f3a9c"}`); rec.Code != 403 || !strings.Contains(rec.Body.String(), "used up") {
		t.Fatalf("used up: %d %s", rec.Code, rec.Body)
	}
	// Renewal: two days later the cookie is reissued.
	f.now = f.now.Add(48 * time.Hour)
	rec = f.do("GET", "/api/v1/me", "", c)
	if rec.Code != 200 || sessionCookie(rec) == nil {
		t.Fatalf("renewal: %d %v", rec.Code, rec.Result().Cookies())
	}
	// Expired cookie.
	f.now = f.now.Add(31 * 24 * time.Hour)
	if rec := f.do("GET", "/api/v1/me", "", c); rec.Code != 401 {
		t.Fatalf("expired cookie: %d", rec.Code)
	}
	// Logout clears the cookie.
	rec = f.do("POST", PathLogout, "", c)
	if rec.Code != 204 || sessionCookie(rec) == nil || sessionCookie(rec).MaxAge >= 0 {
		t.Fatalf("logout %d %v", rec.Code, rec.Result().Cookies())
	}
	log, _ := f.st.AuditLog(ctx, 10)
	if len(log) == 0 || log[len(log)-1].Action != "login.invite" {
		t.Fatalf("audit %+v", log)
	}
	// OIDC routes are not configured in invite mode.
	if rec := f.do("GET", PathOIDCStart, ""); rec.Code != 404 {
		t.Fatalf("oidc in invite mode: %d", rec.Code)
	}
}

func TestInviteRateLimit(t *testing.T) {
	f := newFixture(t, protocol.AuthInvite)
	var last int
	for i := 0; i < 12; i++ {
		last = f.do("POST", PathInviteLogin, `{"code":"guess-code-`+string(rune('a'+i))+`"}`).Code
	}
	if last != 429 {
		t.Fatalf("11th attempt from one IP: %d", last)
	}
}

func TestNoneAuth(t *testing.T) {
	f := newFixture(t, protocol.AuthNone)
	rec := f.do("GET", "/api/v1/me", "")
	var u protocol.User
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if rec.Code != 200 || u.ID != protocol.LocalUserID || u.Kind != protocol.UserLocal {
		t.Fatalf("%d %+v", rec.Code, u)
	}
	if _, err := f.st.Users().Get(context.Background(), protocol.LocalUserID); err != nil {
		t.Fatalf("local user not stored: %v", err)
	}
	if rec := f.do("POST", PathInviteLogin, `{"code":"whatever-123"}`); rec.Code != 409 {
		t.Fatalf("invite in none mode: %d", rec.Code)
	}
}

func TestOIDCNotImplemented(t *testing.T) {
	f := newFixture(t, protocol.AuthOIDC)
	rec := f.do("GET", PathOIDCStart, "")
	if rec.Code != 501 || !strings.Contains(rec.Body.String(), `"not_implemented"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := f.do("GET", "/api/v1/me", ""); rec.Code != 401 {
		t.Fatalf("me: %d", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.2:5555"
	if ClientIP(r) != "10.0.0.2" {
		t.Fatal(ClientIP(r))
	}
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	if ClientIP(r) != "203.0.113.9" {
		t.Fatal(ClientIP(r))
	}
}
