package proxy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/gateway/auth"
	"github.com/floatshadow/emacs-playground/internal/gateway/sessions"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

type fakeSessions struct {
	mu      sync.Mutex
	byID    map[string]*sessions.Session
	open    map[string]int
	touched int
}

func (f *fakeSessions) Get(_ context.Context, id string) (*sessions.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[id]
	if !ok {
		return nil, sessions.ErrNotFound
	}
	c := *s
	return &c, nil
}
func (f *fakeSessions) Touch(context.Context, string) { f.mu.Lock(); f.touched++; f.mu.Unlock() }
func (f *fakeSessions) Opened(id string)              { f.mu.Lock(); f.open[id]++; f.mu.Unlock() }
func (f *fakeSessions) Closed(id string)              { f.mu.Lock(); f.open[id]--; f.mu.Unlock() }
func (f *fakeSessions) openCount(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.open[id]
}

// fakeSessiond echoes what it received and serves a WebSocket at /events.
func fakeSessiond(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		c, err := httpx.AcceptWebSocket(w, r, httpx.AcceptOptions{TrustProxy: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = httpx.WriteJSONText(r.Context(), c, map[string]string{"t": "hello", "user": r.Header.Get(protocol.HeaderUser)}, time.Second)
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			_ = c.Write(r.Context(), typ, b)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "evil", Value: "1"})
		httpx.WriteJSON(w, 200, map[string]string{
			"path":   r.URL.Path,
			"raw":    r.URL.RawPath,
			"query":  r.URL.RawQuery,
			"user":   r.Header.Get(protocol.HeaderUser),
			"cookie": r.Header.Get("Cookie"),
			"authz":  r.Header.Get("Authorization"),
			"reqid":  r.Header.Get(protocol.HeaderRequestID),
			"method": r.Method,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

type harness struct {
	fs    *fakeSessions
	p     *Proxy
	gw    *httptest.Server
	wsURL string
}

func newHarness(t *testing.T, authorMode bool) *harness {
	t.Helper()
	sd := fakeSessiond(t)
	u, _ := url.Parse(sd.URL)
	fs := &fakeSessions{open: map[string]int{}, byID: map[string]*sessions.Session{
		"s1":     {ID: "s1", UserID: "alice", State: protocol.SessionReady, Endpoint: u},
		"s2":     {ID: "s2", UserID: "bob", State: protocol.SessionReady, Endpoint: u},
		"local":  {ID: "local", UserID: "local", Shared: true, State: protocol.SessionIdle, Endpoint: u},
		"off":    {ID: "off", UserID: "alice", State: protocol.SessionStopped, Endpoint: u},
		"broken": {ID: "broken", UserID: "alice", State: protocol.SessionReady, Endpoint: &url.URL{Scheme: "http", Host: "127.0.0.1:1"}},
	}}
	p := New(Options{Sessions: fs, AuthorMode: authorMode, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	mux := http.NewServeMux()
	mux.Handle("/s/{id}/{rest...}", p)
	// Test auth: the user is the X-Test-User header.
	withUser := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Test-User"); id != "" {
			r = r.WithContext(auth.WithUser(r.Context(), &protocol.User{ID: id}))
		} else if id := r.URL.Query().Get("as"); id != "" {
			r = r.WithContext(auth.WithUser(r.Context(), &protocol.User{ID: id}))
		}
		mux.ServeHTTP(w, r)
	})
	gw := httptest.NewServer(httpx.Chain(withUser, httpx.RequestID))
	t.Cleanup(gw.Close)
	return &harness{fs: fs, p: p, gw: gw, wsURL: "ws" + strings.TrimPrefix(gw.URL, "http")}
}

func (h *harness) do(t *testing.T, method, path, user string) (int, map[string]string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, h.gw.URL+path, strings.NewReader(`{}`))
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	req.Header.Set("Cookie", "pg_session=secret")
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]string
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = map[string]string{"_body": string(b)}
	}
	return resp.StatusCode, m, resp.Header
}

func TestProxyHTTP(t *testing.T) {
	h := newHarness(t, false)
	code, m, hdr := h.do(t, "GET", "/s/s1/preview/docs/a%20b.md?x=1", "alice")
	if code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	if m["path"] != "/preview/docs/a b.md" || m["query"] != "x=1" || m["user"] != "alice" || m["cookie"] != "" || m["authz"] != "" || m["reqid"] == "" {
		t.Fatalf("upstream saw %v", m)
	}
	if hdr.Get("Set-Cookie") != "" {
		t.Fatal("Set-Cookie from sessiond must be dropped")
	}
	if code, m, _ := h.do(t, "POST", "/s/s1/rpc/check", "alice"); code != 200 || m["path"] != "/rpc/check" || m["method"] != "POST" {
		t.Fatalf("rpc %d %v", code, m)
	}
	if code, _, _ := h.do(t, "GET", "/s/s2/status", "alice"); code != 403 {
		t.Fatalf("other user's session: %d", code)
	}
	if code, m, _ := h.do(t, "GET", "/s/local/status", "anyone"); code != 200 || m["user"] != "anyone" {
		t.Fatalf("shared session: %d %v", code, m)
	}
	if code, m, _ := h.do(t, "GET", "/s/off/status", "alice"); code != 503 || !strings.Contains(m["_body"]+m["code"], "session_unavailable") {
		t.Fatalf("stopped session: %d %v", code, m)
	}
	if code, _, _ := h.do(t, "GET", "/s/nope/status", "alice"); code != 404 {
		t.Fatalf("unknown: %d", code)
	}
	if code, _, _ := h.do(t, "GET", "/s/s1/status", ""); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	if code, _, _ := h.do(t, "POST", "/s/s1/rpc/eval", "alice"); code != 403 {
		t.Fatalf("eval without author mode: %d", code)
	}
	if code, _, _ := h.do(t, "POST", "/s/s1/rpc/drop-step", "alice"); code != 403 {
		t.Fatalf("drop-step from browser: %d", code)
	}
	if code, m, _ := h.do(t, "GET", "/s/broken/status", "alice"); code != 503 {
		t.Fatalf("unreachable sessiond: %d %v", code, m)
	}
	if h.fs.touched == 0 {
		t.Fatal("requests must touch the session")
	}
	ha := newHarness(t, true)
	if code, m, _ := ha.do(t, "POST", "/s/s1/rpc/eval", "alice"); code != 200 || m["path"] != "/rpc/eval" {
		t.Fatalf("eval in author mode: %d %v", code, m)
	}
}

func TestProxyWebSocket(t *testing.T) {
	h := newHarness(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func() (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(ctx, h.wsURL+"/s/s1/events?after=3", &websocket.DialOptions{HTTPHeader: http.Header{"X-Test-User": {"alice"}}})
	}
	c, _, err := dial()
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := c.Read(ctx)
	if err != nil || !strings.Contains(string(b), `"user":"alice"`) {
		t.Fatalf("hello %s %v", b, err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	typ, b, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(b) != "\x01\x02\x03" {
		t.Fatalf("echo %v %v %v", typ, b, err)
	}
	if h.fs.openCount("s1") != 1 || h.p.WebSockets("alice") != 1 {
		t.Fatalf("counting: %d %d", h.fs.openCount("s1"), h.p.WebSockets("alice"))
	}
	// Five more are allowed, the seventh is refused.
	conns := []*websocket.Conn{c}
	for i := 0; i < 5; i++ {
		c, _, err := dial()
		if err != nil {
			t.Fatalf("ws %d: %v", i+2, err)
		}
		conns = append(conns, c)
	}
	_, resp, err := dial()
	if err == nil || resp == nil || resp.StatusCode != 429 {
		t.Fatalf("7th websocket: %v %v", err, resp)
	}
	for _, c := range conns {
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
	deadline := time.Now().Add(5 * time.Second)
	for h.fs.openCount("s1") != 0 || h.p.WebSockets("alice") != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("not released: %d %d", h.fs.openCount("s1"), h.p.WebSockets("alice"))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *fakeSessions) set(id string, mod func(s *sessions.Session)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if mod == nil {
		delete(f.byID, id)
		return
	}
	mod(f.byID[id])
}

func (p *Proxy) cachedIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]string, 0, len(p.proxies))
	for id := range p.proxies {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func TestProxyCachePruned(t *testing.T) {
	h := newHarness(t, false)
	for _, id := range []string{"s1", "s2", "local"} {
		user := map[string]string{"s1": "alice", "s2": "bob", "local": "anyone"}[id]
		if code, m, _ := h.do(t, "GET", "/s/"+id+"/status", user); code != 200 {
			t.Fatalf("%s: %d %v", id, code, m)
		}
	}
	if got := h.p.cachedIDs(); !slices.Equal(got, []string{"local", "s1", "s2"}) {
		t.Fatalf("cached %v", got)
	}
	ctx := context.Background()
	h.p.Prune(ctx)
	if got := h.p.cachedIDs(); len(got) != 3 {
		t.Fatalf("live sessions pruned: %v", got)
	}
	// A stopped session and a forgotten one are dropped by Prune.
	h.fs.set("s1", func(s *sessions.Session) { s.State = protocol.SessionStopped })
	h.fs.set("s2", nil)
	h.p.Prune(ctx)
	if got := h.p.cachedIDs(); !slices.Equal(got, []string{"local"}) {
		t.Fatalf("after prune %v", got)
	}
	// A request to a session that is no longer usable drops its entry.
	h.fs.set("local", func(s *sessions.Session) { s.State = protocol.SessionFailed })
	if code, _, _ := h.do(t, "GET", "/s/local/status", "anyone"); code != 503 {
		t.Fatalf("failed session: %d", code)
	}
	if got := h.p.cachedIDs(); len(got) != 0 {
		t.Fatalf("after request to a failed session %v", got)
	}
	// A restarted session gets a fresh proxy.
	h.fs.set("s1", func(s *sessions.Session) { s.State = protocol.SessionReady })
	if code, m, _ := h.do(t, "GET", "/s/s1/status", "alice"); code != 200 {
		t.Fatalf("restarted: %d %v", code, m)
	}
	if got := h.p.cachedIDs(); !slices.Equal(got, []string{"s1"}) {
		t.Fatalf("after restart %v", got)
	}
}
