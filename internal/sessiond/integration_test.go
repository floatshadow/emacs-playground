//go:build integration

// Service integration test (backend.md §6) against real Emacs, tmux and an
// agent: /healthz, /status, /events, /pty, rpc keys (expecting the agent's
// key events), checkpoints and restore, the terminal frame respawn after
// C-x C-c, and, with a gateway, a whole lesson: scenario load with a module
// change (daemon restart), keys typed through tmux completing every step,
// and progress reports.
//
//	go test -tags integration ./internal/sessiond/ -run Integration -v
//
// By default the session image is started with testcontainers-go
// (PG_SESSION_IMAGE, default ghcr.io/zsy/emacs-playground/session:dev).
// PG_SESSIOND_URL points the test at an already running sessiond instead.
// PG_TEST_FAKE_GATEWAY (a listen address such as 127.0.0.1:17180) starts an
// in-test gateway serving the lesson 00-orientation/02-first-steps; the
// sessiond under test must reach it as PG_GATEWAY_URL, so run the container
// with --network host and use PG_SESSIOND_URL. PG_TEST_GUI also exercises
// the GUI frame (mode, the /vnc bridge with RFB key tapping, xdotool keys,
// 4002 on switching back). The full run used during development:
//
//	docker run -d --name pg-it --network host --tmpfs /run:mode=1777 \
//	  --tmpfs /tmp:mode=1777 --tmpfs /work:mode=1777,exec \
//	  -e PG_LISTEN=127.0.0.1:17200 -e PG_GATEWAY_URL=http://127.0.0.1:17180 \
//	  -e PG_AUTHOR_MODE=true ghcr.io/zsy/emacs-playground/session:dev
//	PG_SESSIOND_URL=http://127.0.0.1:17200 PG_TEST_FAKE_GATEWAY=127.0.0.1:17180 \
//	  PG_TEST_GUI=1 go test -tags integration ./internal/sessiond/ -run Integration -v
package sessiond

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/zsy/emacs-playground/internal/protocol"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func boolPtr(b bool) *bool { return &b }

type rpcFunc func(op string, body, out any) int

// testGUI switches to the GUI frame, speaks RFB through /vnc (keys typed
// there are tapped), sends keys with xdotool, and switches back.
func testGUI(t *testing.T, ctx context.Context, wsBase string, act, rpc rpcFunc, ev *eventReader) {
	t.Helper()
	var mr protocol.ModeResult
	if act("mode", protocol.ModeRequest{Mode: protocol.FrameGUI}, &mr) != 200 || mr.Mode != protocol.FrameGUI {
		t.Fatalf("mode gui %+v", mr)
	}
	ev.until(`"t":"mode"`, `"mode":"gui"`)
	var st protocol.StatusResult
	if rpc("status", nil, &st) != 200 || !st.Frames.GUI || st.Mode != protocol.FrameGUI {
		t.Fatalf("status in gui mode %+v", st)
	}

	c, resp, err := websocket.Dial(ctx, wsBase+protocol.PathVNC, &websocket.DialOptions{Subprotocols: []string{"binary"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if resp.Header.Get("Sec-WebSocket-Protocol") != "binary" {
		t.Fatalf("subprotocol %q", resp.Header.Get("Sec-WebSocket-Protocol"))
	}
	nc := websocket.NetConn(ctx, c, websocket.MessageBinary)
	_ = nc.SetDeadline(time.Now().Add(30 * time.Second))
	ver := make([]byte, 12)
	if _, err := io.ReadFull(nc, ver); err != nil || !strings.HasPrefix(string(ver), "RFB 003.") {
		t.Fatalf("server version %q %v", ver, err)
	}
	_, _ = nc.Write([]byte("RFB 003.008\n"))
	n := make([]byte, 1)
	if _, err := io.ReadFull(nc, n); err != nil || n[0] == 0 {
		t.Fatalf("security types: %v %v", n, err)
	}
	types := make([]byte, n[0])
	_, _ = io.ReadFull(nc, types)
	if !bytes.Contains(types, []byte{1}) {
		t.Fatalf("Xvnc does not offer security type None: %v", types)
	}
	_, _ = nc.Write([]byte{1})
	res := make([]byte, 4)
	if _, err := io.ReadFull(nc, res); err != nil || !bytes.Equal(res, []byte{0, 0, 0, 0}) {
		t.Fatalf("security result %v %v", res, err)
	}
	_, _ = nc.Write([]byte{1}) // ClientInit, shared
	si := make([]byte, 24)
	if _, err := io.ReadFull(nc, si); err != nil {
		t.Fatal(err)
	}
	t.Logf("GUI framebuffer %dx%d", int(si[0])<<8|int(si[1]), int(si[2])<<8|int(si[3]))
	_ = nc.SetDeadline(time.Time{})
	// Discard the ServerInit name and updates; the copy ends when the
	// bridge closes.
	copyErr := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, nc); copyErr <- err }()

	key := func(down bool, ks uint32) []byte {
		b := []byte{4, 0, 0, 0, byte(ks >> 24), byte(ks >> 16), byte(ks >> 8), byte(ks)}
		if down {
			b[1] = 1
		}
		return b
	}
	ev.mark()
	var msg []byte
	msg = append(msg, key(true, 0xffe3)...) // Control_L
	msg = append(msg, key(true, 'x')...)
	msg = append(msg, key(false, 'x')...)
	msg = append(msg, key(false, 0xffe3)...)
	if _, err := nc.Write(msg); err != nil {
		t.Fatal(err)
	}
	ev.until(`"t":"prefix"`, `"keys":"C-x"`)
	msg = msg[:0]
	msg = append(msg, key(true, 0xffe3)...)
	msg = append(msg, key(true, 'g')...)
	msg = append(msg, key(false, 'g')...)
	msg = append(msg, key(false, 0xffe3)...)
	_, _ = nc.Write(msg)
	ev.until(`"t":"prefix"`, `"keys":""`)
	// The tap reports keys before Emacs reads them. Wait for Emacs to
	// handle the quit: a quit discards pending input, so keys sent before
	// it is processed would be lost. playground-agent.el reports it as an
	// echo "Quit"; a minimal agent may send a key event instead (the
	// first GUI key also produces a <switch-frame> key event; skip that).
	ev.untilFunc("Emacs handling the GUI quit", func(s string) bool {
		return matches(s, []string{`"t":"echo"`, `"text":"Quit"`}) ||
			matches(s, []string{`"t":"key"`, `"frame":"gui"`}) && !strings.Contains(s, "switch-frame")
	})
	time.Sleep(300 * time.Millisecond)

	// xdotool types into the GUI frame.
	if act("keys", protocol.KeysRequest{Keys: "C-x C-f", Frame: protocol.FrameGUI}, nil) != 200 {
		t.Fatal("gui keys")
	}
	ev.until(`"t":"minibuffer"`, `"open":true`)
	if act("keys", protocol.KeysRequest{Keys: "C-g", Frame: protocol.FrameGUI}, nil) != 200 {
		t.Fatal("gui C-g")
	}
	ev.until(`"t":"minibuffer"`, `"open":false`)

	// Back to the terminal: the bridge closes with 4002.
	if act("mode", protocol.ModeRequest{Mode: protocol.FrameTerminal}, &mr) != 200 || mr.Mode != protocol.FrameTerminal {
		t.Fatalf("mode terminal %+v", mr)
	}
	ev.until(`"t":"mode"`, `"mode":"terminal"`)
	select {
	case err := <-copyErr:
		if s := websocket.CloseStatus(err); s != protocol.CloseGUINotActive {
			t.Fatalf("vnc bridge closed with %v, want status %d", err, protocol.CloseGUINotActive)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("vnc bridge stayed open after switching to the terminal")
	}
}

// sessiondURL returns the base URL of the sessiond under test.
func sessiondURL(t *testing.T, ctx context.Context, gatewayURL string) string {
	if u := os.Getenv("PG_SESSIOND_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	image := envOr("PG_SESSION_IMAGE", "ghcr.io/zsy/emacs-playground/session:dev")
	env := map[string]string{"PG_SESSION_ID": "it", "PG_SESSION_TOKEN": "it", "PG_GATEWAY_URL": gatewayURL, "PG_AUTHOR_MODE": "true"}
	ctr, err := testcontainers.Run(ctx, image,
		testcontainers.WithExposedPorts("7000/tcp"),
		testcontainers.WithEnv(env),
		testcontainers.WithTmpfs(map[string]string{"/tmp": "size=512m", "/run": "size=64m,mode=1777", "/work": "size=1g,mode=1777"}),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) { hc.Init = boolPtr(true) }),
		testcontainers.WithWaitStrategy(wait.ForHTTP(protocol.PathHealthz).WithPort("7000/tcp").WithStartupTimeout(3*time.Minute)),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("starting %s: %v", image, err)
	}
	base, err := ctr.PortEndpoint(ctx, "7000/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// eventReader reads /events frames. until matches frames received since the
// last mark, in any order, so independent events (a file event and the step
// it completes) may arrive either way round.
type eventReader struct {
	t    *testing.T
	ctx  context.Context
	c    *websocket.Conn
	log  []string
	from int
}

// mark makes later until calls ignore frames received so far.
func (r *eventReader) mark() { r.from = len(r.log) }

func matches(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

// until returns the first frame since the mark that contains every
// substring, reading more frames as needed.
func (r *eventReader) until(subs ...string) string {
	r.t.Helper()
	return r.untilFunc(strings.Join(subs, " "), func(s string) bool { return matches(s, subs) })
}

// untilFunc returns the first frame since the mark for which ok is true.
func (r *eventReader) untilFunc(what string, ok func(string) bool) string {
	r.t.Helper()
	for _, s := range r.log[r.from:] {
		if ok(s) {
			return s
		}
	}
	// One context for the whole wait: coder/websocket closes the
	// connection when a read context expires.
	rctx, cancel := context.WithTimeout(r.ctx, 60*time.Second)
	defer cancel()
	for {
		_, data, err := r.c.Read(rctx)
		if err != nil {
			tail := r.log
			if len(tail) > 30 {
				tail = tail[len(tail)-30:]
			}
			r.t.Fatalf("no event: %s (%v); last events:\n%s", what, err, strings.Join(tail, "\n"))
		}
		s := string(data)
		r.log = append(r.log, s)
		if ok(s) {
			return s
		}
	}
}

func TestIntegrationSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var gw *fakeGateway
	gatewayURL := os.Getenv("PG_TEST_GATEWAY_URL")
	if addr := os.Getenv("PG_TEST_FAKE_GATEWAY"); addr != "" {
		gw = &fakeGateway{tarball: makeTarball(t, map[string]string{"README.md": "# hello\n", "src/main.rs": "fn main() {}\n"})}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: gw.handler(t)}
		go srv.Serve(ln)
		defer srv.Close()
		gatewayURL = "http://" + addr
	}
	base := sessiondURL(t, ctx, gatewayURL)
	wsBase := strings.Replace(base, "http", "ws", 1)

	rpc := func(op string, body, out any) int {
		t.Helper()
		b, _ := json.Marshal(body)
		resp, err := http.Post(base+"/rpc/"+op, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Logf("rpc %s → %d %s", op, resp.StatusCode, raw)
		}
		if resp.StatusCode == 200 && out != nil {
			var env protocol.RawRPCResponse
			_ = json.Unmarshal(raw, &env)
			if err := json.Unmarshal(env.Result, out); err != nil {
				t.Fatalf("%s: %v %s", op, err, raw)
			}
		}
		return resp.StatusCode
	}
	// Every action marks the event log, so waits only see what followed.
	var ev *eventReader
	act := func(op string, body, out any) int {
		t.Helper()
		if ev != nil {
			ev.mark()
		}
		return rpc(op, body, out)
	}
	keys := func(k string) {
		t.Helper()
		if act("keys", protocol.KeysRequest{Keys: k}, nil) != 200 {
			t.Fatalf("keys %s", k)
		}
	}
	text := func(s string) {
		t.Helper()
		if act("keys", protocol.KeysRequest{Text: s}, nil) != 200 {
			t.Fatalf("text %s", s)
		}
	}

	resp, err := http.Get(base + protocol.PathHealthz)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz %v %v", resp, err)
	}
	var st protocol.StatusResult
	if rpc("status", nil, &st) != 200 || !st.Emacs.Agent || !strings.HasPrefix(st.Emacs.Version, "3") || !st.Frames.Terminal {
		t.Fatalf("status %+v", st)
	}
	t.Logf("emacs %s pid %d, frames %+v, modules %v", st.Emacs.Version, st.Emacs.PID, st.Frames, st.Modules)

	evc, _, err := websocket.Dial(ctx, wsBase+protocol.PathEvents, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer evc.CloseNow()
	ev = &eventReader{t: t, ctx: ctx, c: evc}
	ev.until(`"t":"hello"`)

	// /pty: tmux attaches and draws the Emacs frame.
	pc, _, err := websocket.Dial(ctx, wsBase+protocol.PathPty+"?cols=100&rows=30", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.CloseNow()
	var screen bytes.Buffer
	deadline := time.Now().Add(30 * time.Second)
	for screen.Len() < 500 && time.Now().Before(deadline) {
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
		typ, data, err := pc.Read(rctx)
		rcancel()
		if err != nil {
			break
		}
		if typ == websocket.MessageBinary {
			screen.Write(data)
		}
	}
	if screen.Len() < 500 {
		t.Fatalf("no screen output from tmux: %q", screen.String())
	}
	// Typing C-x into the pty is a tapped prefix; C-g cancels it.
	_ = pc.Write(ctx, websocket.MessageBinary, []byte{0x18})
	ev.until(`"t":"prefix"`, `"keys":"C-x"`)
	// Give Emacs time to read C-x on its own, as with real typing: a C-g
	// in the same read as the prefix is taken as an interrupt by the tty
	// frame and can be lost (Emacs 31.1; it never reaches recent-keys).
	time.Sleep(400 * time.Millisecond)
	_ = pc.Write(ctx, websocket.MessageBinary, []byte{0x07})
	// A quit inside a key sequence is an echo "Quit" (playground-agent.el)
	// or a key event without a command name (a minimal agent).
	ev.untilFunc("Emacs handling the quit", func(s string) bool {
		return strings.Contains(s, `"t":"key"`) || matches(s, []string{`"t":"echo"`, `"text":"Quit"`})
	})

	// Keys through tmux produce agent events: the find-file prompt opens
	// and C-g closes it.
	keys("C-x C-f")
	ev.until(`"t":"minibuffer"`, `"open":true`, `Find file`)
	keys("C-g")
	ev.until(`"t":"minibuffer"`, `"open":false`)

	var evr protocol.EvalResult
	if rpc("eval", protocol.EvalRequest{Form: "(+ 1 2)"}, &evr) != 200 || evr.Value != float64(3) {
		t.Fatalf("eval %+v", evr)
	}
	var dr protocol.DescribeResult
	if rpc("describe", protocol.DescribeRequest{Keys: "C-x C-f"}, &dr) != 200 || dr.Cmd != "find-file" {
		t.Fatalf("describe %+v", dr)
	}

	if gw != nil {
		// A whole lesson. The module set grows, so the daemon restarts.
		var sr protocol.ScenarioResult
		if act("scenario", protocol.ScenarioRequest{LessonID: "00-orientation/02-first-steps"}, &sr) != 200 {
			t.Fatal("scenario")
		}
		t.Logf("scenario %+v", sr)
		if sr.Restarting {
			ev.until(`"t":"emacs"`, `"event":"daemon-restarted"`)
		}
		ev.until(`"t":"step"`, `"stepId":"open"`, `"status":"loaded"`)
		// Step 1: open a file with C-x C-f.
		// Absolute paths: find-file ignores everything before "//", so the
		// test does not depend on the buffer's default directory.
		ws := envOr("PG_TEST_WORKSPACE", "/work")
		keys("C-x C-f")
		ev.until(`"t":"minibuffer"`)
		text(ws + "/hello/README.md")
		keys("RET")
		ev.until(`"t":"step"`, `"stepId":"open"`, `"status":"complete"`)
		ev.until(`"t":"step"`, `"stepId":"write"`, `"status":"loaded"`)
		// Step 2: write notes.txt with "done" and save; step 3's shell
		// condition is already true.
		keys("C-x C-f")
		ev.until(`"t":"minibuffer"`, `"open":true`)
		text(ws + "/hello/notes.txt")
		keys("RET")
		text("all done")
		keys("C-x C-s")
		ev.until(`"t":"file"`, `"path":"hello/notes.txt"`)
		ev.until(`"t":"step"`, `"stepId":"write"`, `"status":"complete"`)
		ev.until(`"t":"step"`, `"stepId":"last"`, `"lessonComplete":true`)
		time.Sleep(time.Second)
		gw.mu.Lock()
		done := 0
		for _, p := range gw.progress {
			if p.Status == protocol.ProgressDone {
				done++
			}
		}
		gw.mu.Unlock()
		if done != 3 {
			t.Fatalf("progress reports: %+v", gw.progress)
		}
		// Restoring the write step's checkpoint removes notes.txt and
		// reverts the buffer.
		var rr protocol.RestoreResult
		if act("restore", protocol.RestoreRequest{Ref: "refs/playground/00-orientation/02-first-steps/write"}, &rr) != 200 || rr.RestoredFiles != 1 {
			t.Fatalf("restore %+v", rr)
		}
		ev.until(`"t":"step"`, `"status":"reset"`)
	}

	// Manual checkpoints.
	var cp protocol.CheckpointResult
	if rpc("checkpoint", protocol.CheckpointRequest{Label: "integration"}, &cp) != 200 {
		t.Fatal("checkpoint")
	}
	var list protocol.CheckpointsResult
	if rpc("checkpoints", nil, &list) != 200 || len(list.Items) == 0 {
		t.Fatal("checkpoints")
	}
	var rr protocol.RestoreResult
	if rpc("restore", protocol.RestoreRequest{Label: "integration"}, &rr) != 200 || rr.RestoredFiles != 0 {
		t.Fatalf("restore %+v", rr)
	}

	// C-x C-c closes the terminal frame; sessiond respawns it.
	keys("C-x C-c")
	ev.until(`"t":"emacs"`, `"event":"frame-closed"`)
	ev.until(`"t":"emacs"`, `"event":"frame-opened"`, `"frame":"terminal"`)
	// Emacs answers keys again in the new frame.
	keys("C-x C-f")
	ev.until(`"t":"minibuffer"`, `"open":true`)
	keys("C-g")
	ev.until(`"t":"minibuffer"`, `"open":false`)

	var rescue protocol.RescueResult
	if rpc("rescue", nil, &rescue) != 200 {
		t.Fatal("rescue")
	}

	// A second pty viewer replaces the first.
	pc2, _, err := websocket.Dial(ctx, wsBase+protocol.PathPty, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pc2.CloseNow()
	for {
		rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
		_, _, err := pc.Read(rctx)
		rcancel()
		if err != nil {
			if websocket.CloseStatus(err) != protocol.CloseReplaced {
				t.Fatalf("first viewer closed with %v", err)
			}
			break
		}
	}

	if os.Getenv("PG_TEST_GUI") != "" {
		testGUI(t, ctx, wsBase, act, rpc, ev)
	}

	// restart-emacs: the daemon comes back.
	if act("restart-emacs", nil, nil) != 200 {
		t.Fatal("restart-emacs")
	}
	ev.until(`"t":"emacs"`, `"event":"daemon-restarted"`)
	ev.until(`"t":"emacs"`, `"event":"frame-opened"`)
}
