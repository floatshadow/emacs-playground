package gateway

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/config"
	source "github.com/floatshadow/emacs-playground/internal/curriculum"
	"github.com/floatshadow/emacs-playground/internal/docker"
	"github.com/floatshadow/emacs-playground/internal/docker/dockertest"
	"github.com/floatshadow/emacs-playground/internal/gateway/sessions"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

const publicURL = "http://localhost:8080"

// sessiondFake records RPCs and serves /healthz, /status, /rpc/* and an
// echoing /events WebSocket.
type sessiondFake struct {
	mu   sync.Mutex
	rpcs []string
	srv  *httptest.Server
	// secret, when set, is required in X-Playground-Proxy like sessiond
	// with PG_PROXY_SECRET (all but /healthz).
	secret string
	// denied counts requests refused for a missing or wrong secret.
	denied int
}

func (f *sessiondFake) requireSecret(s string) {
	f.mu.Lock()
	f.secret = s
	f.mu.Unlock()
}

func newSessiondFake(t *testing.T) *sessiondFake {
	f := &sessiondFake{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, 200, protocol.HealthStatus{OK: true})
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, 200, protocol.StatusResult{
			SessionID: "local", Emacs: protocol.EmacsStatus{Version: "31.1", UptimeMs: 1234, Agent: true, Responsive: true},
			Mode: protocol.FrameTerminal, Step: &protocol.StepRef{LessonID: "00-orientation/01-first", StepID: "open"},
			Modules: []string{"base", "agent"},
		})
	})
	mux.HandleFunc("POST /rpc/{op}", func(w http.ResponseWriter, r *http.Request) {
		op := r.PathValue("op")
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.rpcs = append(f.rpcs, op+" "+string(bytes.TrimSpace(body))+" user="+r.Header.Get(protocol.HeaderUser))
		f.mu.Unlock()
		switch op {
		case "stuck":
			httpx.WriteOK(w, protocol.StuckRPCResult{Events: []json.RawMessage{json.RawMessage(`{"t":"key","seq":1}`)}})
		case "eval":
			httpx.WriteOK(w, protocol.EvalResult{Value: "ok"})
		default:
			httpx.WriteOK(w, nil)
		}
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		c, err := httpx.AcceptWebSocket(w, r, httpx.AcceptOptions{TrustProxy: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = httpx.WriteJSONText(r.Context(), c, map[string]any{"t": "hello", "protocol": 1}, time.Second)
		_, _, _ = c.Read(r.Context())
	})
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		secret := f.secret
		bad := secret != "" && r.URL.Path != "/healthz" && r.Header.Get(protocol.HeaderProxySecret) != secret
		if bad {
			f.denied++
		}
		f.mu.Unlock()
		if bad {
			httpx.WriteError(w, r, httpx.Unauthenticated("no proxy secret"))
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *sessiondFake) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.rpcs...)
}

func bundle(t *testing.T) string {
	t.Helper()
	src, diags := source.Load("../curriculum/testdata/good")
	if diags.HasErrors() {
		t.Fatal(diags.Err())
	}
	out := t.TempDir()
	if err := source.Build(src, out, source.BuildOptions{Version: "e2e"}); err != nil {
		t.Fatal(err)
	}
	return out
}

var webFS = fstest.MapFS{
	"index.html":                          {Data: []byte("<!doctype html><title>Emacs Playground</title><h1>app</h1>")},
	"assets/index-abc123.js":              {Data: []byte("console.log(1)")},
	"favicon.svg":                         {Data: []byte("<svg/>")},
	"fonts/IosevkaNerdFont-Regular.woff2": {Data: []byte("wOF2")},
}

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, hc: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *client) do(method, path string, body any, hdr ...string) (*http.Response, []byte) {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	if method != "GET" {
		req.Header.Set("Origin", publicURL)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			req.Header.Del(hdr[i])
		} else {
			req.Header.Set(hdr[i], hdr[i+1])
		}
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func (c *client) json(method, path string, body, out any, want int, hdr ...string) http.Header {
	c.t.Helper()
	resp, b := c.do(method, path, body, hdr...)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, want, b)
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("%s %s: %v: %s", method, path, err, b)
		}
	}
	return resp.Header
}

func (c *client) cookies(base string) []string {
	u, _ := url.Parse(base)
	var out []string
	for _, ck := range c.hc.Jar.Cookies(u) {
		out = append(out, ck.Name+"="+ck.Value)
	}
	return []string{strings.Join(out, "; ")}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSpace(b)
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func localConfig(t *testing.T, sessionURL string) *protocol.Config {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Local.SessionURL = sessionURL
	cfg.Curriculum.Dir = bundle(t)
	cfg.Listen = "127.0.0.1:0"
	config.Resolve(&cfg)
	if err := config.Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	return &cfg
}

func TestLocalModeEndToEnd(t *testing.T) {
	sd := newSessiondFake(t)
	cfg := localConfig(t, sd.srv.URL)
	app, err := Build(context.Background(), cfg, Options{Web: webFS, Fonts: FontsDir("", webFS), Logger: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	gw := httptest.NewServer(app.Handler())
	defer gw.Close()
	c := newClient(t, gw.URL)

	var hs protocol.HealthStatus
	c.json("GET", "/healthz", nil, &hs, 200)
	if !hs.OK {
		t.Fatal("healthz")
	}
	var ac protocol.AuthConfigInfo
	c.json("GET", "/api/v1/auth/config", nil, &ac, 200)
	if ac.Mode != protocol.ModeLocal || ac.AuthMode != protocol.AuthNone {
		t.Fatalf("auth config %+v", ac)
	}
	var me protocol.Me
	c.json("GET", "/api/v1/me", nil, &me, 200)
	if me.User.ID != protocol.LocalUserID || me.User.Kind != protocol.UserLocal || !me.Features.GUI || !me.Features.StuckReports || me.Features.AuthorMode || me.Mode != protocol.ModeLocal {
		t.Fatalf("me %+v", me)
	}

	// Sessions: Origin is required for state-changing requests.
	c.json("POST", "/api/v1/sessions", nil, nil, 403, "Origin", "")
	c.json("POST", "/api/v1/sessions", nil, nil, 403, "Origin", "http://evil.example")
	var info protocol.SessionInfo
	c.json("POST", "/api/v1/sessions", nil, &info, 201)
	if info.ID != "local" || info.State != protocol.SessionReady || !info.GUI {
		t.Fatalf("session %+v", info)
	}
	c.json("GET", "/api/v1/sessions/local", nil, &info, 200)
	if info.Emacs == nil || info.Emacs.Version != "31.1" || info.Mode != protocol.FrameTerminal || info.Step == nil || info.Step.StepID != "open" {
		t.Fatalf("session status %+v", info)
	}
	c.json("GET", "/api/v1/sessions/nope", nil, nil, 404)
	c.json("DELETE", "/api/v1/sessions/local", nil, nil, 409)
	var rpc protocol.RawRPCResponse
	c.json("POST", "/api/v1/sessions/local/restart-emacs", nil, &rpc, 200)
	if !rpc.OK {
		t.Fatalf("restart %+v", rpc)
	}

	// Curriculum.
	var idx protocol.CurriculumIndex
	c.json("GET", "/api/v1/curriculum", nil, &idx, 200)
	if idx.Version != "e2e" || len(idx.Parts) != 3 {
		t.Fatalf("index %+v", idx)
	}
	var lb protocol.LessonBundle
	c.json("GET", "/api/v1/curriculum/lessons/00-orientation/01-first", nil, &lb, 200)
	if lb.Title != "Opening files" {
		t.Fatalf("lesson %+v", lb.Title)
	}
	c.json("GET", "/api/v1/curriculum/lessons/01-moving-and-editing/01-motion", nil, nil, 404)
	if resp, _ := c.do("GET", "/api/v1/curriculum/assets/00-orientation/01-first/screen.svg", nil); resp.StatusCode != 200 {
		t.Fatalf("asset %d", resp.StatusCode)
	}

	// Internal API needs the session token.
	c.json("GET", "/internal/curriculum/scenarios/00-orientation/01-first", nil, nil, 401)
	c.json("GET", "/internal/curriculum/scenarios/00-orientation/01-first", nil, nil, 401, "Authorization", "Bearer wrong")
	var sc protocol.Scenario
	c.json("GET", "/internal/curriculum/scenarios/00-orientation/01-first", nil, &sc, 200, "Authorization", "Bearer local")
	if sc.ID != "00-orientation/01-first" {
		t.Fatalf("scenario %+v", sc.ID)
	}
	resp, b := c.do("GET", "/internal/curriculum/fixtures/00-orientation/01-first", nil, "Authorization", "Bearer local")
	if resp.StatusCode != 200 || resp.Header.Get("ETag") == "" || len(b) == 0 {
		t.Fatalf("fixtures %d", resp.StatusCode)
	}
	if resp, _ := c.do("GET", "/internal/curriculum/fixtures/00-orientation/01-first", nil, "Authorization", "Bearer local", "If-None-Match", resp.Header.Get("ETag")); resp.StatusCode != 304 {
		t.Fatalf("fixtures etag %d", resp.StatusCode)
	}

	// Progress from sessiond (no Origin needed on /internal).
	two := 2
	c.json("POST", "/internal/sessions/local/progress", protocol.ProgressReport{LessonID: "00-orientation/01-first", StepID: "open", Status: protocol.ProgressDone, HintsUsed: &two}, nil, 204, "Authorization", "Bearer local", "Origin", "")
	c.json("POST", "/internal/sessions/other/progress", protocol.ProgressReport{LessonID: "00-orientation/01-first", StepID: "open", Status: protocol.ProgressDone}, nil, 403, "Authorization", "Bearer local")
	c.json("POST", "/internal/sessions/local/progress", protocol.ProgressReport{LessonID: "00-orientation/01-first", StepID: "bogus", Status: protocol.ProgressDone}, nil, 400, "Authorization", "Bearer local")
	c.json("POST", "/internal/sessions/local/heartbeat", protocol.Heartbeat{ActiveWebSockets: 2}, nil, 204, "Authorization", "Bearer local")
	var prog protocol.Progress
	c.json("GET", "/api/v1/progress", nil, &prog, 200)
	if st := prog.Lessons["00-orientation/01-first"]; st.Status != protocol.ProgressInProgress || st.Steps["open"].HintsUsed != 2 || st.Steps["open"].Status != protocol.ProgressDone {
		t.Fatalf("progress %+v", prog)
	}
	for _, step := range []string{"save", "layout"} {
		c.json("POST", "/internal/sessions/local/progress", protocol.ProgressReport{LessonID: "00-orientation/01-first", StepID: step, Status: protocol.ProgressDone}, nil, 204, "Authorization", "Bearer local")
	}
	c.json("GET", "/api/v1/progress", nil, &prog, 200)
	if prog.Lessons["00-orientation/01-first"].Status != protocol.ProgressDone {
		t.Fatalf("lesson done %+v", prog)
	}
	c.json("POST", "/api/v1/progress/reset", protocol.ProgressResetRequest{LessonID: "00-orientation/01-first"}, nil, 204)
	prog = protocol.Progress{}
	c.json("GET", "/api/v1/progress", nil, &prog, 200)
	if len(prog.Lessons) != 0 {
		t.Fatalf("after reset %+v", prog)
	}
	// Start over: a reset with no body clears every lesson and tells
	// sessiond drop-step {}.
	c.json("POST", "/internal/sessions/local/progress", protocol.ProgressReport{LessonID: "00-orientation/01-first", StepID: "open", Status: protocol.ProgressSkipped}, nil, 204, "Authorization", "Bearer local")
	c.json("GET", "/api/v1/progress", nil, &prog, 200)
	if lp := prog.Lessons["00-orientation/01-first"]; lp.Steps["open"].Status != protocol.ProgressSkipped || lp.Counts.Skipped != 1 || lp.Counts.Total != 3 {
		t.Fatalf("skipped %+v", lp)
	}
	c.json("POST", "/api/v1/progress/reset", nil, nil, 204)
	prog = protocol.Progress{}
	c.json("GET", "/api/v1/progress", nil, &prog, 200)
	if len(prog.Lessons) != 0 {
		t.Fatalf("after start over %+v", prog)
	}

	// Stuck: the gateway asks sessiond for events; sessiond's own filing
	// of the same report merges.
	var created protocol.Created
	c.json("POST", "/api/v1/stuck", protocol.StuckRequest{SessionID: "local", LessonID: "00-orientation/01-first", StepID: "open", Note: "lost"}, &created, 201)
	var created2 protocol.Created
	c.json("POST", "/internal/sessions/local/stuck", protocol.InternalStuckReport{LessonID: "00-orientation/01-first", StepID: "open", Note: "lost", Events: []json.RawMessage{json.RawMessage(`1`)}}, &created2, 201, "Authorization", "Bearer local")
	if created.ID == "" || created.ID != created2.ID {
		t.Fatalf("stuck ids %q %q", created.ID, created2.ID)
	}
	reps, _ := app.Store.Stuck().List(context.Background(), 0)
	if len(reps) != 1 || !strings.Contains(string(reps[0].Events), `"seq":1`) || reps[0].UserID != "local" {
		t.Fatalf("stuck reports %+v", reps)
	}
	calls := strings.Join(sd.calls(), "\n")
	for _, want := range []string{"restart-emacs {} user=local", `drop-step {"lessonId":"00-orientation/01-first"} user=local`, `drop-step {} user=local`, `stuck {"note":"lost"} user=local`} {
		if !strings.Contains(calls, want) {
			t.Errorf("sessiond did not get %q; got:\n%s", want, calls)
		}
	}

	// /internal stuck reports count against the same hourly limit as
	// POST /api/v1/stuck (two used above); a changed note is no way round it.
	limited := 0
	for i := 0; i < 12 && limited == 0; i++ {
		resp, _ := c.do("POST", "/internal/sessions/local/stuck", protocol.InternalStuckReport{LessonID: "00-orientation/01-first", StepID: "open", Note: fmt.Sprintf("note %d", i)}, "Authorization", "Bearer local")
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = i + 1
		} else if resp.StatusCode != 201 {
			t.Fatalf("internal stuck %d: %d", i, resp.StatusCode)
		}
	}
	if limited != 9 {
		t.Fatalf("internal stuck reports limited after %d calls, want 9", limited)
	}
	c.json("POST", "/api/v1/stuck", protocol.StuckRequest{SessionID: "local", LessonID: "00-orientation/01-first", StepID: "open", Note: "again"}, nil, 429)
	// Progress for a lesson the bundle does not have is refused.
	c.json("POST", "/internal/sessions/local/progress", protocol.ProgressReport{LessonID: "99-made-up/01-x", StepID: "a", Status: protocol.ProgressDone}, nil, 400, "Authorization", "Bearer local")

	// Proxy.
	var st protocol.StatusResult
	c.json("GET", "/s/local/status", nil, &st, 200)
	if st.Emacs.Version != "31.1" {
		t.Fatalf("proxied status %+v", st)
	}
	c.json("POST", "/s/local/rpc/check", protocol.Empty{}, nil, 200)
	c.json("POST", "/s/local/rpc/check", protocol.Empty{}, nil, 403, "Origin", "http://evil.example")
	c.json("POST", "/s/local/rpc/eval", protocol.EvalRequest{Form: "(+ 1 2)"}, nil, 403)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(gw.URL, "http") + "/s/local/events"
	if _, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://evil.example"}}}); err == nil || resp.StatusCode != 403 {
		t.Fatalf("cross-origin websocket: %v", err)
	}
	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {publicURL}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, msg, err := ws.Read(ctx); err != nil || !strings.Contains(string(msg), `"hello"`) {
		t.Fatalf("events hello %s %v", msg, err)
	}
	if s, _ := app.Sessions.Get(ctx, "local"); s.ActiveWebSockets != 1 {
		t.Fatalf("websocket not counted: %d", s.ActiveWebSockets)
	}
	ws.Close(websocket.StatusNormalClosure, "")

	// Static app, SPA fallback, fonts.
	resp, b = c.do("GET", "/", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "<h1>app</h1>") || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("index %d %s", resp.StatusCode, resp.Header)
	}
	resp, b = c.do("GET", "/lessons/00-orientation/01-first", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "<h1>app</h1>") {
		t.Fatalf("spa fallback %d", resp.StatusCode)
	}
	resp, _ = c.do("GET", "/assets/index-abc123.js", nil)
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset %d %s", resp.StatusCode, resp.Header)
	}
	if resp, _ := c.do("GET", "/assets/missing.js", nil); resp.StatusCode != 404 {
		t.Fatalf("missing asset %d", resp.StatusCode)
	}
	resp, _ = c.do("GET", "/fonts/IosevkaNerdFont-Regular.woff2", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "font/woff2" {
		t.Fatalf("font %d %s", resp.StatusCode, resp.Header)
	}
	c.json("GET", "/fonts/../index.html", nil, nil, 307)
	c.json("GET", "/api/v1/nope", nil, nil, 404)
	c.json("GET", "/internal/nope", nil, nil, 404)
	// Data lives in local.json.
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "local.json")); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceholderWithoutBundle(t *testing.T) {
	sd := newSessiondFake(t)
	cfg := localConfig(t, sd.srv.URL)
	cfg.Curriculum.Dir = filepath.Join(t.TempDir(), "missing")
	app, err := Build(context.Background(), cfg, Options{Logger: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	gw := httptest.NewServer(app.Handler())
	defer gw.Close()
	c := newClient(t, gw.URL)
	resp, b := c.do("GET", "/some/route", nil)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "built without the web app") {
		t.Fatalf("placeholder %d %s", resp.StatusCode, b)
	}
	var hs protocol.HealthStatus
	c.json("GET", "/healthz", nil, &hs, 200)
	if hs.Reason == "" {
		t.Fatal("healthz should mention the missing bundle")
	}
	c.json("GET", "/api/v1/curriculum", nil, nil, 503)
	c.json("GET", "/fonts/x.woff2", nil, nil, 404)
}

func TestServerModeEndToEnd(t *testing.T) {
	sd := newSessiondFake(t)
	sdURL, _ := url.Parse(sd.srv.URL)
	dataDir := t.TempDir()
	invites := filepath.Join(dataDir, "invites.txt")
	if err := os.WriteFile(invites, []byte("spring-cohort-7f3a9c cohort-A uses=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Mode = protocol.ModeServer
	cfg.PublicURL = publicURL
	cfg.DataDir = dataDir
	cfg.Auth.Mode = protocol.AuthInvite
	cfg.Auth.Invites.File = invites
	cfg.Curriculum.Dir = bundle(t)
	config.Resolve(&cfg)
	if err := config.Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	fake := dockertest.New()
	backend := &sessions.DockerBackend{Client: fake, Version: "test", EndpointFor: func(sessions.Handle) *url.URL { return sdURL }}
	app, err := Build(context.Background(), &cfg, Options{Backend: backend, Logger: quietLog()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	gw := httptest.NewServer(app.Handler())
	defer gw.Close()

	alice := newClient(t, gw.URL)
	alice.json("GET", "/api/v1/me", nil, nil, 401)
	alice.json("POST", "/api/v1/sessions", nil, nil, 401)
	var ac protocol.AuthConfigInfo
	alice.json("GET", "/api/v1/auth/config", nil, &ac, 200)
	if ac.AuthMode != protocol.AuthInvite || ac.Mode != protocol.ModeServer {
		t.Fatalf("auth config %+v", ac)
	}
	alice.json("POST", "/api/v1/auth/invite", protocol.InviteLoginRequest{Code: "wrong-code-xyz"}, nil, 403)
	alice.json("POST", "/api/v1/auth/invite", protocol.InviteLoginRequest{Code: "spring-cohort-7f3a9c"}, nil, 204)
	var me protocol.Me
	alice.json("GET", "/api/v1/me", nil, &me, 200)
	if me.User.Kind != protocol.UserInvite || me.User.DisplayName != "cohort-A" {
		t.Fatalf("me %+v", me)
	}
	var info protocol.SessionInfo
	alice.json("POST", "/api/v1/sessions", nil, &info, 201)
	if info.State != protocol.SessionReady || info.ID == "local" {
		t.Fatalf("session %+v", info)
	}
	var live, boxes []dockertest.FakeContainer
	for _, c := range fake.Live() {
		switch c.Spec.Labels[docker.LabelRole] {
		case docker.RoleSession:
			live = append(live, c)
		case docker.RoleRemoteBox:
			boxes = append(boxes, c)
		}
	}
	if len(live) != 1 || live[0].Spec.Labels[docker.LabelUser] != me.User.ID || !live[0].Spec.ReadOnlyRootfs {
		t.Fatalf("container %+v", live)
	}
	// The session's remote box authorises the key sessiond gets.
	if !me.Features.RemoteBox || len(boxes) != 1 || live[0].Spec.Env["PG_BOX_HOST"] != boxes[0].Spec.Name {
		t.Fatalf("remote box %+v, me %+v", boxes, me.Features)
	}
	if k, err := protocol.ParseBoxKey([]byte(live[0].Spec.Env["PG_BOX_KEY"])); err != nil ||
		protocol.AuthorizedKey(k.Public().(ed25519.PublicKey), "playground-"+info.ID) != boxes[0].Spec.Env["AUTHORIZED_KEYS"] {
		t.Fatalf("box key mismatch: %v", err)
	}
	token := live[0].Spec.Env["PG_SESSION_TOKEN"]
	if live[0].Spec.Env["PG_GATEWAY_URL"] != "http://gateway:8080" {
		t.Fatalf("gateway url %v", live[0].Spec.Env)
	}
	// sessiond only answers requests carrying its proxy secret, which the
	// gateway derives per session and sends on the proxy, RPCs and status.
	secret := live[0].Spec.Env["PG_PROXY_SECRET"]
	if len(secret) != 64 || secret != sessions.ProxySecret(mustRead(t, filepath.Join(dataDir, ProxyKeyFile)), info.ID) {
		t.Fatalf("proxy secret %q", secret)
	}
	sd.requireSecret(secret)
	var st protocol.StatusResult
	alice.json("GET", "/s/"+info.ID+"/status", nil, &st, 200)
	alice.json("GET", "/s/"+info.ID+"/status", nil, &st, 200, protocol.HeaderProxySecret, "forged")
	var gi protocol.SessionInfo
	alice.json("GET", "/api/v1/sessions/"+info.ID, nil, &gi, 200)
	if gi.Emacs == nil || gi.Emacs.Version != "31.1" {
		t.Fatalf("session status through the secret %+v", gi)
	}
	alice.json("POST", "/api/v1/sessions/"+info.ID+"/restart-emacs", nil, nil, 200)
	{
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(gw.URL, "http")+"/s/"+info.ID+"/events",
			&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {publicURL}, "Cookie": alice.cookies(gw.URL)}})
		if err != nil {
			t.Fatalf("events through the proxy: %v", err)
		}
		if _, msg, err := ws.Read(ctx); err != nil || !strings.Contains(string(msg), `"hello"`) {
			t.Fatalf("events hello %s %v", msg, err)
		}
		ws.Close(websocket.StatusNormalClosure, "")
		cancel()
	}
	if resp, err := http.Get(sd.srv.URL + "/status"); err != nil || resp.StatusCode != 401 {
		t.Fatalf("direct request without the secret: %v %v", resp, err)
	}
	sd.mu.Lock()
	denied := sd.denied
	sd.mu.Unlock()
	if denied != 1 {
		t.Fatalf("sessiond refused %d gateway requests", denied-1)
	}

	bob := newClient(t, gw.URL)
	bob.json("POST", "/api/v1/auth/invite", protocol.InviteLoginRequest{Code: "spring-cohort-7f3a9c"}, nil, 204)
	bob.json("GET", "/s/"+info.ID+"/status", nil, nil, 403)
	bob.json("GET", "/api/v1/sessions/"+info.ID, nil, nil, 403)
	// The code had two uses.
	carol := newClient(t, gw.URL)
	carol.json("POST", "/api/v1/auth/invite", protocol.InviteLoginRequest{Code: "spring-cohort-7f3a9c"}, nil, 403)

	// sessiond reports with its own token; progress lands on alice.
	auth := "Bearer " + token
	alice.json("POST", "/internal/sessions/"+info.ID+"/progress", protocol.ProgressReport{LessonID: "00-orientation/02-second", StepID: "cancel", Status: protocol.ProgressDone}, nil, 204, "Authorization", auth)
	var prog protocol.Progress
	alice.json("GET", "/api/v1/progress", nil, &prog, 200)
	if prog.Lessons["00-orientation/02-second"].Status != protocol.ProgressDone {
		t.Fatalf("alice progress %+v", prog)
	}
	prog = protocol.Progress{}
	bob.json("GET", "/api/v1/progress", nil, &prog, 200)
	if len(prog.Lessons) != 0 {
		t.Fatalf("bob progress %+v", prog)
	}
	alice.json("GET", "/internal/curriculum/scenarios/00-orientation/02-second", nil, nil, 200, "Authorization", auth)
	// Through the public reverse proxy /internal does not exist.
	alice.json("GET", "/internal/curriculum/scenarios/00-orientation/02-second", nil, nil, 404, "Authorization", auth, "X-Forwarded-For", "203.0.113.7")

	// Stop keeps the volume; the token dies with the container.
	alice.json("DELETE", "/api/v1/sessions/"+info.ID, nil, nil, 204)
	alice.json("GET", "/s/"+info.ID+"/status", nil, nil, 503)
	alice.json("GET", "/internal/curriculum/scenarios/00-orientation/02-second", nil, nil, 200, "Authorization", auth)
	alice.json("POST", "/api/v1/sessions", nil, &info, 201)
	live = live[:0]
	boxes = boxes[:0]
	for _, c := range fake.Live() {
		if c.Spec.Labels[docker.LabelRole] == docker.RoleSession {
			live = append(live, c)
		} else {
			boxes = append(boxes, c)
		}
	}
	if len(live) != 1 || len(boxes) != 1 {
		t.Fatalf("recreated %+v", fake.Live())
	}
	if got := live[0].Spec.Env["PG_PROXY_SECRET"]; got != secret {
		t.Fatalf("re-created session has another proxy secret: %q", got)
	}
	alice.json("GET", "/internal/curriculum/scenarios/00-orientation/02-second", nil, nil, 401, "Authorization", auth)
	if _, err := os.Stat(filepath.Join(dataDir, "playground.db")); err != nil {
		t.Fatal("server mode must use SQLite")
	}
}
