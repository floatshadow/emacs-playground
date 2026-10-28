package sessiond

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/klauspost/compress/zstd"

	"github.com/floatshadow/emacs-playground/internal/config"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
)

const testLesson = `{
  "id": "00-orientation/02-first-steps",
  "title": "First steps",
  "modules": ["base", "agent", "editing"],
  "setup": {"workspace": "fixtures/hello", "root": "hello", "open": ["hello/README.md"]},
  "steps": [
    {"id": "open", "goal": "Open a file", "conditions": [{"text": "find-file ran", "event": {"command": "find-file"}}]},
    {"id": "write", "goal": "Write notes", "conditions": [{"file": {"path": "hello/notes.txt", "contains": "done"}}],
     "stuck": [{"when": {"step_seconds": 1}, "say": "Need help? Write done into notes.txt."}]},
    {"id": "last", "goal": "Keep the README", "conditions": [{"shell": {"run": "test -f README.md"}}]}
  ]
}`

type fakeGateway struct {
	mu       sync.Mutex
	progress []protocol.ProgressReport
	stuck    []protocol.InternalStuckReport
	beats    int
	tarball  []byte
	auth     []string
}

func (g *fakeGateway) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/curriculum/scenarios/{id...}", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		g.mu.Unlock()
		if r.PathValue("id") != "00-orientation/02-first-steps" {
			w.Header().Set("Content-Type", protocol.ProblemContentType)
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"status":404,"code":"not_found","message":"no such lesson"}`))
			return
		}
		w.Header().Set("ETag", `"s1"`)
		_, _ = w.Write([]byte(testLesson))
	})
	mux.HandleFunc("GET /internal/curriculum/fixtures/{id...}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", protocol.FixturesContentType)
		w.Header().Set("ETag", `"f1"`)
		_, _ = w.Write(g.tarball)
	})
	mux.HandleFunc("POST /internal/sessions/{id}/{what}", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		switch r.PathValue("what") {
		case "progress":
			var p protocol.ProgressReport
			_ = json.NewDecoder(r.Body).Decode(&p)
			g.progress = append(g.progress, p)
		case "stuck":
			var s protocol.InternalStuckReport
			_ = json.NewDecoder(r.Body).Decode(&s)
			g.stuck = append(g.stuck, s)
		case "heartbeat":
			g.beats++
		}
		w.WriteHeader(204)
	})
	return mux
}

func makeTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, _ := zstd.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

type testServer struct {
	t      *testing.T
	srv    *Server
	base   string
	cancel context.CancelFunc
	done   chan error
	ws     string
	gw     *fakeGateway
}

func startServer(t *testing.T, ws, run string, gw *fakeGateway, gwURL string, mutate ...func(*Options)) *testServer {
	t.Helper()
	cfg, err := config.LoadSessiond([]string{
		"PG_WORKSPACE=" + ws, "PG_RUN_DIR=" + run, "PG_AGENT_SOCKET=" + filepath.Join(run, "agent.sock"),
		"PG_GATEWAY_URL=" + gwURL, "PG_SESSION_ID=s1", "PG_SESSION_TOKEN=tok",
		"PG_PRISTINE_CONFIG=" + filepath.Join(ws, "..", "pristine"), "PG_VERSIONS_FILE=" + filepath.Join(ws, "..", "versions.env"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	opt := Options{Fake: true, Listener: ln, Ready: ready, Log: log, HelloTimeout: 10 * time.Second}
	for _, m := range mutate {
		m(&opt)
	}
	s := New(cfg, opt)
	ctx, cancel := context.WithCancel(context.Background())
	ts := &testServer{t: t, srv: s, base: "http://" + ln.Addr().String(), cancel: cancel, done: make(chan error, 1), ws: ws, gw: gw}
	go func() { ts.done <- s.Run(ctx) }()
	select {
	case <-ready:
	case err := <-ts.done:
		t.Fatalf("server exited: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("server did not start")
	}
	return ts
}

func (ts *testServer) stop() {
	ts.cancel()
	select {
	case err := <-ts.done:
		if err != nil {
			ts.t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		ts.t.Fatal("server did not stop")
	}
}

func (ts *testServer) rpc(op string, body any, out any, hdr ...string) int {
	ts.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.base+"/rpc/"+op, bytes.NewReader(b))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 200 && out != nil {
		var env protocol.RawRPCResponse
		if err := json.Unmarshal(raw, &env); err != nil || !env.OK {
			ts.t.Fatalf("%s: bad envelope %s", op, raw)
		}
		if err := json.Unmarshal(env.Result, out); err != nil {
			ts.t.Fatalf("%s: %v %s", op, err, raw)
		}
	}
	if resp.StatusCode != 200 && testing.Verbose() {
		ts.t.Logf("%s → %d %s", op, resp.StatusCode, raw)
	}
	return resp.StatusCode
}

func (ts *testServer) get(path string) (int, []byte) {
	resp, err := http.Get(ts.base + path)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// eventStream collects /events frames.
type eventStream struct {
	t   *testing.T
	c   *websocket.Conn
	mu  sync.Mutex
	evs []protocol.Event
	err error
	end chan struct{}
}

func (ts *testServer) events(after string) *eventStream {
	ts.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	ts.t.Cleanup(cancel)
	u := strings.Replace(ts.base, "http", "ws", 1) + "/events" + after
	c, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		ts.t.Fatal(err)
	}
	es := &eventStream{t: ts.t, c: c, end: make(chan struct{})}
	go func() {
		defer close(es.end)
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				es.mu.Lock()
				es.err = err
				es.mu.Unlock()
				return
			}
			ev, derr := protocol.DecodeEvent(data)
			if derr != nil {
				continue
			}
			es.mu.Lock()
			es.evs = append(es.evs, ev)
			es.mu.Unlock()
		}
	}()
	return es
}

func describe(ev protocol.Event) string {
	switch e := ev.(type) {
	case *protocol.StepEvent:
		s := "step:" + e.StepID + ":" + string(e.Status)
		if e.LessonComplete {
			s += ":lesson-complete"
		}
		return s
	case *protocol.EmacsEvent:
		return "emacs:" + string(e.Event)
	case *protocol.ModulesEvent:
		return fmt.Sprintf("modules:%v", e.Restarting)
	case *protocol.CheckEvent:
		return fmt.Sprintf("check:%s:%v", e.StepID, e.Complete)
	case *protocol.NudgeEvent:
		return "nudge:" + e.ID
	case *protocol.ModeEvent:
		return "mode:" + string(e.Mode)
	case *protocol.FileEvent:
		return "file:" + e.Path + ":" + string(e.Event)
	}
	return string(ev.Type())
}

func (es *eventStream) wait(want string) protocol.Event {
	es.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		es.mu.Lock()
		for _, ev := range es.evs {
			if describe(ev) == want {
				es.mu.Unlock()
				return ev
			}
		}
		es.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	es.mu.Lock()
	defer es.mu.Unlock()
	var got []string
	for _, ev := range es.evs {
		got = append(got, describe(ev))
	}
	es.t.Fatalf("event %s never arrived; got %v", want, got)
	return nil
}

func (es *eventStream) first() protocol.Event {
	es.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		es.mu.Lock()
		if len(es.evs) > 0 {
			ev := es.evs[0]
			es.mu.Unlock()
			return ev
		}
		es.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	es.t.Fatal("no first event")
	return nil
}

func TestServerFakeMode(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	ws := filepath.Join(base, "work")
	run, err := os.MkdirTemp("", "pgrun")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(run)
	// A pristine config to copy and a versions file.
	_ = os.MkdirAll(filepath.Join(base, "pristine", "modules"), 0o755)
	_ = os.WriteFile(filepath.Join(base, "pristine", "init.el"), []byte(";; init\n"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "versions.env"), []byte("# pins\nEMACS_VERSION=31.1\nTMUX_VERSION=3.5a\n"), 0o644)

	gw := &fakeGateway{tarball: makeTarball(t, map[string]string{"README.md": "# hello\n", "src/main.rs": "fn main() {}\n"})}
	gwSrv := httptest.NewServer(gw.handler(t))
	defer gwSrv.Close()

	// Buffers with unsaved changes, as the agent reports them while
	// unsavedBuffers is set.
	var unsavedBuffers atomic.Bool
	fakeAgent := func(op protocol.AgentOp, args json.RawMessage) (any, error) {
		if op == protocol.AgentOpEval && unsavedBuffers.Load() && strings.Contains(string(args), "buffer-modified-p b") {
			return protocol.AgentEvalResult{Value: []string{"hello/notes.txt", "hello/README.md"}}, nil
		}
		return agent.DefaultFakeHandler(op, args)
	}
	ts := startServer(t, ws, run, gw, gwSrv.URL, func(o *Options) { o.FakeAgent = fakeAgent })
	stopped := false
	defer func() {
		if !stopped {
			ts.stop()
		}
	}()

	// Workspace initialised.
	if b, _ := os.ReadFile(filepath.Join(ws, "emacs.d", "init.el")); string(b) != ";; init\n" {
		t.Fatal("pristine config not copied")
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "emacs.d", "modules.el")); !strings.Contains(string(b), "'(base agent)") {
		t.Fatalf("modules.el %q", b)
	}
	if _, err := os.Stat(filepath.Join(ws, ".git")); err != nil {
		t.Fatal("workspace not a git repository")
	}
	// The remote box key pair, where the local box reads authorized_keys;
	// fake mode leaves ~/.ssh alone, so no alias and no box.
	for _, f := range []string{"id_ed25519", "id_ed25519.pub", "authorized_keys"} {
		if _, err := os.Stat(filepath.Join(ws, ".playground", "ssh", f)); err != nil {
			t.Fatalf("box key file %s: %v", f, err)
		}
	}

	// Health and status.
	if code, body := ts.get("/healthz"); code != 200 || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("healthz %d %s", code, body)
	}
	var st protocol.StatusResult
	if ts.rpc("status", nil, &st) != 200 || st.Emacs.Version != "fake" || !st.Emacs.Agent || st.Mode != protocol.FrameTerminal ||
		st.Versions["EMACS_VERSION"] != "31.1" || st.SessionID != "s1" || st.RemoteBox || st.Lap != nil {
		t.Fatalf("status %+v", st)
	}
	if code, body := ts.get("/status"); code != 200 || !strings.Contains(string(body), `"sessionId":"s1"`) {
		t.Fatalf("GET /status %d %s", code, body)
	}

	es := ts.events("")
	hello, ok := es.first().(*protocol.HelloEvent)
	if !ok || hello.Protocol != 1 || hello.SessionID != "s1" || hello.Step != nil || strings.Join(hello.Modules, " ") != "base agent" {
		t.Fatalf("hello %+v", es.first())
	}

	// Load the lesson: the module set grows, so Emacs restarts first.
	var sr protocol.ScenarioResult
	if code := ts.rpc("scenario", protocol.ScenarioRequest{LessonID: "00-orientation/02-first-steps"}, &sr); code != 200 {
		t.Fatalf("scenario %d", code)
	}
	if !sr.Restarting || sr.Step.StepID != "open" || sr.Count != 3 || strings.Join(sr.Modules, " ") != "base agent editing" {
		t.Fatalf("scenario result %+v", sr)
	}
	es.wait("modules:true")
	es.wait("emacs:restarting")
	es.wait("emacs:daemon-restarted")
	es.wait("modules:false")
	es.wait("checkpoint")
	es.wait("step:open:loaded")
	if b, _ := os.ReadFile(filepath.Join(ws, "hello", "src", "main.rs")); string(b) != "fn main() {}\n" {
		t.Fatal("fixtures not extracted")
	}
	gw.mu.Lock()
	if len(gw.auth) == 0 || gw.auth[0] != "Bearer tok" {
		t.Fatalf("auth %v", gw.auth)
	}
	gw.mu.Unlock()
	if code := ts.rpc("scenario", protocol.ScenarioRequest{LessonID: "99-nope/01-x"}, nil); code != 404 {
		t.Fatalf("missing lesson %d", code)
	}

	// Step 1: an agent key event completes it.
	var cr protocol.CheckResult
	if ts.rpc("check", nil, &cr) != 200 || cr.Complete || cr.Conditions[0].ID != "open/1" {
		t.Fatalf("check %+v", cr)
	}
	ts.srv.OnEvent(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file", Buf: "README.md", Frame: protocol.FrameTerminal})
	es.wait("key")
	es.wait("step:open:complete")
	es.wait("step:write:loaded")

	// Step 2: writing the file completes it (file event → evaluation); the
	// stuck rule fires after a second.
	if ts.rpc("hint", protocol.HintRequest{StepID: "write", Level: 1}, nil) != 200 {
		t.Fatal("hint")
	}
	es.wait("nudge:write/stuck/1")
	if err := os.WriteFile(filepath.Join(ws, "hello", "notes.txt"), []byte("all done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	es.wait("file:hello/notes.txt:created")
	es.wait("step:write:complete")
	es.wait("step:last:complete:lesson-complete")

	// Progress reached the gateway: in-progress, hints, done.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		gw.mu.Lock()
		n := len(gw.progress)
		gw.mu.Unlock()
		if n >= 6 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	gw.mu.Lock()
	var summary []string
	for _, p := range gw.progress {
		s := p.StepID + ":" + string(p.Status)
		if p.HintsUsed != nil {
			s += fmt.Sprintf(":h%d", *p.HintsUsed)
		}
		summary = append(summary, s)
	}
	gw.mu.Unlock()
	joined := strings.Join(summary, " ")
	for _, want := range []string{"open:in-progress", "open:done", "write:in-progress:h1", "write:done:h1", "last:done"} {
		if !strings.Contains(joined, want) {
			t.Errorf("progress lacks %s: %s", want, joined)
		}
	}

	// Manual checkpoints, listing and restore.
	var cp protocol.CheckpointResult
	if ts.rpc("checkpoint", protocol.CheckpointRequest{Label: "Before experiments"}, &cp) != 200 || cp.Ref != "refs/playground/manual/before-experiments" {
		t.Fatalf("checkpoint %+v", cp)
	}
	_ = os.WriteFile(filepath.Join(ws, "hello", "notes.txt"), []byte("scribbles"), 0o644)
	var list protocol.CheckpointsResult
	if ts.rpc("checkpoints", nil, &list) != 200 || len(list.Items) < 3 || list.Items[0].Label != "Before experiments" ||
		strings.Join(list.Items[0].Files, ",") != "hello/notes.txt" {
		t.Fatalf("checkpoints %+v", list)
	}
	// The count covers the files restored on disk and the buffers whose
	// unsaved changes the restore discarded, each file once.
	var rr protocol.RestoreResult
	unsavedBuffers.Store(true)
	if ts.rpc("restore", protocol.RestoreRequest{Label: "Before experiments"}, &rr) != 200 || rr.RestoredFiles != 2 {
		t.Fatalf("restore %+v", rr)
	}
	unsavedBuffers.Store(false)
	if b, _ := os.ReadFile(filepath.Join(ws, "hello", "notes.txt")); string(b) != "all done\n" {
		t.Fatalf("notes after restore %q", b)
	}
	// Restoring a step checkpoint resets that step.
	if ts.rpc("restore", protocol.RestoreRequest{Ref: "refs/playground/00-orientation/02-first-steps/write"}, &rr) != 200 {
		t.Fatal("restore step")
	}
	es.wait("step:write:reset")
	if _, err := os.Stat(filepath.Join(ws, "hello", "notes.txt")); err == nil {
		t.Fatal("notes.txt should be gone after restoring the write step's checkpoint")
	}
	if ts.rpc("restore", protocol.RestoreRequest{Ref: "refs/heads/main"}, nil) != 400 {
		t.Fatal("foreign ref accepted")
	}
	if ts.rpc("restore", protocol.RestoreRequest{Label: "nope"}, nil) != 404 {
		t.Fatal("unknown label")
	}

	// Preview.
	if code, body := ts.get("/preview/hello/README.md"); code != 200 || string(body) != "# hello\n" {
		t.Fatalf("preview %d %s", code, body)
	}
	if code, body := ts.get("/preview/hello/"); code != 200 || !strings.Contains(string(body), `"name":"src"`) {
		t.Fatalf("listing %d %s", code, body)
	}
	if code, _ := ts.get("/preview/.playground/state.json"); code != 404 {
		t.Fatal(".playground served")
	}

	// Keys, eval, describe, mode, stuck, unknown ops.
	if ts.rpc("keys", protocol.KeysRequest{Keys: "C-x C-f"}, nil) != 200 {
		t.Fatal("keys")
	}
	if ts.rpc("keys", protocol.KeysRequest{Keys: "C-x <bogus key>"}, nil) != 400 {
		t.Fatal("bad keys")
	}
	if ts.rpc("keys", protocol.KeysRequest{Keys: "a", Text: "b"}, nil) != 400 {
		t.Fatal("keys and text")
	}
	if ts.rpc("eval", protocol.EvalRequest{Form: "(+ 1 2)"}, nil) != 403 {
		t.Fatal("eval outside author mode")
	}
	// open: workspace-relative files only, no author mode needed.
	if ts.rpc("open", protocol.OpenRequest{Path: "hello/README.md"}, &protocol.OpenResult{}) != 200 {
		t.Fatal("open a workspace file")
	}
	for path, want := range map[string]int{"../etc/passwd": 400, "": 400, "hello": 400, "hello/nope.txt": 404} {
		if got := ts.rpc("open", protocol.OpenRequest{Path: path}, nil); got != want {
			t.Fatalf("open %q: %d, want %d", path, got, want)
		}
	}
	var dr protocol.DescribeResult
	if ts.rpc("describe", protocol.DescribeRequest{Keys: "C-x C-f"}, &dr) != 200 {
		t.Fatal("describe")
	}
	var rescue protocol.RescueResult
	if ts.rpc("rescue", nil, &rescue) != 200 || rescue.Did == nil {
		t.Fatalf("rescue %+v", rescue)
	}
	var mr protocol.ModeResult
	if ts.rpc("mode", protocol.ModeRequest{Mode: protocol.FrameGUI}, &mr) != 200 || mr.Mode != protocol.FrameGUI {
		t.Fatalf("mode gui %+v", mr)
	}
	es.wait("mode:gui")
	if ts.rpc("mode", protocol.ModeRequest{Mode: protocol.FrameTerminal}, &mr) != 200 {
		t.Fatal("mode terminal")
	}
	es.wait("mode:terminal")
	if ts.rpc("mode", protocol.ModeRequest{Mode: "vr"}, nil) != 400 {
		t.Fatal("bad mode")
	}
	var stuck protocol.StuckRPCResult
	// The browser through the gateway proxy: forwarding headers are set.
	if ts.rpc("stuck", protocol.StuckRPCRequest{Note: "lost"}, &stuck, protocol.HeaderUser, "u1", "X-Forwarded-For", "10.0.0.1") != 200 || len(stuck.Events) == 0 {
		t.Fatalf("stuck %d events", len(stuck.Events))
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		gw.mu.Lock()
		n := len(gw.stuck)
		gw.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	gw.mu.Lock()
	if len(gw.stuck) != 1 || gw.stuck[0].Note != "lost" || gw.stuck[0].LessonID != "00-orientation/02-first-steps" {
		t.Fatalf("stuck reports %+v", gw.stuck)
	}
	gw.mu.Unlock()
	// The gateway itself asking (POST /api/v1/stuck): it sets the user but
	// no forwarding headers, and stores the events itself.
	ts.rpc("stuck", protocol.StuckRPCRequest{Note: "gateway"}, &stuck, protocol.HeaderUser, "u1")
	time.Sleep(100 * time.Millisecond)
	gw.mu.Lock()
	if len(gw.stuck) != 1 {
		t.Fatal("gateway-initiated stuck call filed a report")
	}
	gw.mu.Unlock()
	if ts.rpc("bogus", nil, nil) != 404 {
		t.Fatal("unknown op")
	}

	// Replay: reconnect after seq 1 gets hello(replayed) and the rest.
	es2 := ts.events("?after=1")
	if h := es2.first().(*protocol.HelloEvent); !h.Replayed || h.Step == nil || h.Step.StepID != "write" {
		t.Fatalf("replay hello %+v", h)
	}

	// The pty attaches bash in fake mode.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pc, _, err := websocket.Dial(ctx, strings.Replace(ts.base, "http", "ws", 1)+"/pty?cols=90&rows=20", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = pc.Write(ctx, websocket.MessageBinary, []byte("echo pty-$((40+2))\r"))
	var out bytes.Buffer
	for !strings.Contains(out.String(), "pty-42") {
		typ, data, err := pc.Read(ctx)
		if err != nil {
			t.Fatalf("pty read: %v (%q)", err, out.String())
		}
		if typ == websocket.MessageBinary {
			out.Write(data)
		}
	}
	// C-x through the pty is a tapped prefix.
	_ = pc.Write(ctx, websocket.MessageBinary, []byte{0x18})
	es.wait("prefix")
	_ = pc.Write(ctx, websocket.MessageBinary, []byte{0x07})

	// Modules rpc and restart-emacs.
	var mres protocol.ModulesResult
	if ts.rpc("modules", protocol.ModulesRequest{Enabled: []string{"base", "agent", "editing", "files"}}, &mres) != 200 || !mres.Restarting {
		t.Fatalf("modules %+v", mres)
	}
	if ts.rpc("modules", protocol.ModulesRequest{Enabled: []string{"base"}}, nil) != 400 {
		t.Fatal("modules without agent")
	}
	if ts.rpc("restart-emacs", nil, nil) != 200 {
		t.Fatal("restart-emacs")
	}

	// drop-step forgets the lesson.
	if ts.rpc("drop-step", protocol.DropStepRequest{LessonID: "00-orientation/02-first-steps"}, nil) != 200 {
		t.Fatal("drop-step")
	}
	if ts.rpc("check", nil, nil) != 409 {
		t.Fatal("check after drop should conflict")
	}
	// Load again and leave it active for the restart test below.
	if ts.rpc("scenario", protocol.ScenarioRequest{LessonID: "00-orientation/02-first-steps", StepID: "write"}, &sr) != 200 {
		t.Fatal("reload")
	}
	es.wait("step:write:loaded")

	// Graceful shutdown: stopping event, 4003 on WebSockets.
	stopped = true
	ts.stop()
	select {
	case <-es.end:
	case <-time.After(10 * time.Second):
		t.Fatal("events socket not closed")
	}
	es.mu.Lock()
	if websocket.CloseStatus(es.err) != protocol.CloseSessionStopping {
		t.Errorf("events close: %v", es.err)
	}
	es.mu.Unlock()
	es.wait("emacs:stopping")
	for {
		if _, _, err := pc.Read(ctx); err != nil {
			if websocket.CloseStatus(err) != protocol.CloseSessionStopping {
				t.Errorf("pty close: %v", err)
			}
			break
		}
	}

	// state.json survives; a new sessiond resumes the step.
	b, err := os.ReadFile(filepath.Join(ws, ".playground", "state.json"))
	if err != nil || !strings.Contains(string(b), `"stepId": "write"`) {
		t.Fatalf("state.json %s %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".playground", "events.log")); err != nil {
		t.Fatal("events.log missing")
	}
	// The learner's buffers and windows at shutdown come back in the new
	// Emacs before the step is active again.
	snap := `{"files":[{"path":"hello/README.md","point":7,"visible":true}],"layout":"single"}`
	if err := os.WriteFile(filepath.Join(ws, ".playground", "snapshot.json"), []byte(snap), 0o644); err != nil {
		t.Fatal(err)
	}
	ts2 := startServer(t, ws, run, gw, gwSrv.URL)
	defer ts2.stop()
	// The resume may happen before we connect: replay from the start.
	es3 := ts2.events("?after=0")
	es3.wait("step:write:loaded")
	ts2.srv.fakeMu.Lock()
	fake := ts2.srv.fake
	ts2.srv.fakeMu.Unlock()
	var reopened bool
	for _, c := range fake.Calls() {
		reopened = reopened || (c.Op == protocol.AgentOpOpen && strings.Contains(string(c.Args), `"point":7`))
	}
	if !reopened {
		t.Fatalf("the snapshot was not re-opened: %+v", fake.Calls())
	}
	if h := es3.first().(*protocol.HelloEvent); strings.Join(h.Modules, " ") != "base agent editing files" {
		t.Fatalf("modules after restart %v", h.Modules)
	}
}

func TestHealthzBeforeReady(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.healthz(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "starting") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestSplitStepRef(t *testing.T) {
	cases := map[string][2]string{
		"refs/playground/02-buffers-and-files/03-opening-files/open": {"02-buffers-and-files/03-opening-files", "open"},
		"refs/playground/projects/p6-polyglot/deploy":                {"projects/p6-polyglot", "deploy"},
	}
	for ref, want := range cases {
		l, s, ok := splitStepRef(ref)
		if !ok || l != want[0] || s != want[1] {
			t.Errorf("%s → %s %s %v", ref, l, s, ok)
		}
	}
	if _, _, ok := splitStepRef("refs/playground/manual/x"); ok {
		t.Error("manual ref is not a step ref")
	}
}

func TestElispString(t *testing.T) {
	for in, want := range map[string]string{
		"/work/a.md":     `"/work/a.md"`,
		`/work/q"uote`:   `"/work/q\"uote"`,
		`/work/back\sl`:  `"/work/back\\sl"`,
		"/work/ünï cödé": `"/work/ünï cödé"`,
	} {
		if got := elispString(in); got != want {
			t.Errorf("elispString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestProxyAuth(t *testing.T) {
	var seen []string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get(protocol.HeaderProxySecret))
		w.WriteHeader(200)
	})
	h := ProxyAuth("s3cret")(next)
	for _, c := range []struct {
		method, path, secret string
		want                 int
	}{
		{"GET", "/healthz", "", 200},
		{"HEAD", "/healthz", "", 200},
		{"GET", "/status", "", 401},
		{"GET", "/status", "wrong", 401},
		{"GET", "/status", "s3cret", 200},
		{"POST", "/rpc/keys", "", 401},
		{"POST", "/rpc/drop-step", "s3cre", 401},
		{"POST", "/rpc/keys", "s3cret", 200},
		{"GET", "/pty", "", 401},
		{"GET", "/preview/hello/README.md", "", 401},
		{"POST", "/healthz", "", 401},
	} {
		req := httptest.NewRequest(c.method, c.path, nil)
		if c.secret != "" {
			req.Header.Set(protocol.HeaderProxySecret, c.secret)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s %s secret %q: %d, want %d", c.method, c.path, c.secret, rec.Code, c.want)
		}
		if c.want == 401 && !strings.Contains(rec.Body.String(), "unauthenticated") {
			t.Errorf("%s %s: body %s", c.method, c.path, rec.Body)
		}
	}
	// The secret is not passed on to the handlers.
	for _, s := range seen {
		if strings.HasSuffix(s, "s3cret") {
			t.Errorf("handler saw the secret: %s", s)
		}
	}
	// No secret configured: everything passes (local mode).
	rec := httptest.NewRecorder()
	ProxyAuth("")(next).ServeHTTP(rec, httptest.NewRequest("POST", "/rpc/keys", nil))
	if rec.Code != 200 {
		t.Fatalf("without a secret: %d", rec.Code)
	}
}
