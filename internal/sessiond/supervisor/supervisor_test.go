package supervisor

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
)

func TestTmuxKeys(t *testing.T) {
	cases := map[string]string{
		"C-x C-f":         "C-x C-f",
		"M-x":             "M-x",
		"C-M-%":           "C-M-%",
		"RET TAB SPC ESC": "Enter Tab Space Escape",
		"DEL":             "BSpace",
		"<f3> <f12>":      "F3 F12",
		"C-<left> S-<up>": "C-Left S-Up",
		"<prior> <next>":  "PPage NPage",
		"<deletechar>":    "DC",
		"C-;":             `C-\;`,
		";":               `\;`,
		"a b":             "a b",
		"C-x 4 f":         "C-x 4 f",
		"<backtab>":       "BTab",
		"C-S-s":           "C-S-s",
		"é":               "é",
	}
	for in, want := range cases {
		got, err := TmuxKeys(in)
		if err != nil || strings.Join(got, " ") != want {
			t.Errorf("TmuxKeys(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"s-a", "<f20>", "<mouse-1>", ""} {
		if _, err := TmuxKeys(bad); err == nil {
			t.Errorf("TmuxKeys(%q) should fail", bad)
		}
	}
}

func TestXdotoolKeys(t *testing.T) {
	cases := map[string]string{
		"C-x C-f":         "ctrl+x ctrl+f",
		"M-%":             "alt+percent",
		"RET TAB DEL ESC": "Return Tab BackSpace Escape",
		"SPC":             "space",
		"<f3> C-<left>":   "F3 ctrl+Left",
		"s-a H-b":         "super+a hyper+b",
		"C-;":             "ctrl+semicolon",
		"é":               "U00E9",
		"<kp-1>":          "KP_1",
	}
	for in, want := range cases {
		got, err := XdotoolKeys(in)
		if err != nil || strings.Join(got, " ") != want {
			t.Errorf("XdotoolKeys(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestChildRestartPolicyAndGiveUp(t *testing.T) {
	var mu sync.Mutex
	var exits []string
	starts := 0
	c := &Child{
		Name: "flaky",
		New:  func() *exec.Cmd { return exec.Command("sh", "-c", "exit 3") },
		Policy: Policy{Backoff: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			MaxRestarts: 3, Window: time.Minute},
		OnStart: func(int) { mu.Lock(); starts++; mu.Unlock() },
		OnExit: func(err error, restarting, gaveUp bool) {
			mu.Lock()
			exits = append(exits, errString(err)+"|"+map[bool]string{true: "restart", false: "stop"}[restarting]+map[bool]string{true: "|gaveup", false: ""}[gaveUp])
			mu.Unlock()
		},
	}
	c.Start()
	deadline := time.Now().Add(5 * time.Second)
	for !c.GaveUp() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !c.GaveUp() || c.Wanted() {
		t.Fatal("did not give up")
	}
	mu.Lock()
	if starts != 4 || len(exits) != 4 || exits[3] != "exit status 3|stop|gaveup" || exits[0] != "exit status 3|restart" {
		t.Fatalf("starts=%d exits=%v", starts, exits)
	}
	mu.Unlock()
	// Start resets the give-up state.
	c.Start()
	if c.GaveUp() {
		t.Fatal("Start should reset give-up")
	}
	c.Stop(time.Second)
}

func TestChildStopKillsGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	c := &Child{Name: "tree", New: func() *exec.Cmd {
		// A parent that ignores TERM with a child sleeping: the group kill
		// takes both down.
		return exec.Command("bash", "-c", "trap '' TERM; sleep 300 & echo $! > "+pidFile+"; wait")
	}}
	c.Start()
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(pidFile)
		if err == nil && len(strings.TrimSpace(string(b))) > 0 {
			_, _ = fmtSscan(strings.TrimSpace(string(b)), &childPID)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 || !c.Running() {
		t.Fatal("child did not start")
	}
	start := time.Now()
	c.Stop(200 * time.Millisecond)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("stop took %v", time.Since(start))
	}
	time.Sleep(50 * time.Millisecond)
	if err := syscall.Kill(childPID, 0); err == nil {
		t.Fatal("grandchild survived the group kill")
	}
	if c.Running() {
		t.Fatal("still running")
	}
}

// fakeTmux records calls; Die simulates the pane-died hook.
type fakeTmux struct {
	mu       sync.Mutex
	dead     bool
	died     chan struct{}
	respawns int
	sent     []string
	starts   int
}

func newFakeTmux() *fakeTmux { return &fakeTmux{died: make(chan struct{}, 1)} }

func (f *fakeTmux) Start(context.Context) error { f.mu.Lock(); f.starts++; f.mu.Unlock(); return nil }
func (f *fakeTmux) Alive(context.Context) bool  { return true }
func (f *fakeTmux) PaneDead(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dead
}
func (f *fakeTmux) RespawnPane(context.Context) error {
	f.mu.Lock()
	f.dead = false
	f.respawns++
	f.mu.Unlock()
	return nil
}
func (f *fakeTmux) WaitPaneDied(ctx context.Context) error {
	select {
	case <-f.died:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *fakeTmux) SendKeys(_ context.Context, literal bool, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := ""
	if literal {
		prefix = "-l "
	}
	f.sent = append(f.sent, prefix+strings.Join(keys, " "))
	return nil
}
func (f *fakeTmux) Kill(context.Context) error { return nil }
func (f *fakeTmux) respawnCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.respawns
}
func (f *fakeTmux) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}
func (f *fakeTmux) die() {
	f.mu.Lock()
	f.dead = true
	f.mu.Unlock()
	f.died <- struct{}{}
}

type events struct {
	mu  sync.Mutex
	evs []protocol.Event
}

func (e *events) emit(ev protocol.Event) { e.mu.Lock(); e.evs = append(e.evs, ev); e.mu.Unlock() }

func (e *events) emacs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.evs {
		switch x := ev.(type) {
		case *protocol.EmacsEvent:
			s := string(x.Event)
			if x.Frame != "" {
				s += ":" + string(x.Frame)
			}
			out = append(out, s)
		case *protocol.ModeEvent:
			out = append(out, "mode:"+string(x.Mode))
		case *protocol.ErrorEvent:
			out = append(out, "error")
		}
	}
	return out
}

func (e *events) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range e.emacs() {
			if s == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event %s never came; got %v", want, e.emacs())
}

// harness wires a supervisor to bash children, a real agent socket and the
// in-process fake agent, which connects when the fake daemon starts.
type harness struct {
	sup   *Supervisor
	srv   *agent.Server
	tmux  *fakeTmux
	evs   *events
	mu    sync.Mutex
	fake  *agent.Fake
	stop  context.CancelFunc
	calls []string
	dir   string
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("", "pgsup")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	h := &harness{tmux: newFakeTmux(), evs: &events{}, dir: dir}
	srv, err := agent.Listen(filepath.Join(dir, "agent.sock"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.srv = srv
	t.Cleanup(func() { srv.Close() })
	opt := Options{
		Commands:     FakeCommands("bash"),
		Tmux:         h.tmux,
		Agent:        srv,
		Emit:         h.evs.emit,
		GUIEnabled:   true,
		SnapshotFile: filepath.Join(dir, "snapshot.json"),
		HelloTimeout: 3 * time.Second,
		PingInterval: 50 * time.Millisecond,
		RespawnDelay: 10 * time.Millisecond,
		FrameWait:    300 * time.Millisecond,
		DaemonPolicy: &Policy{Backoff: []time.Duration{50 * time.Millisecond}, MaxRestarts: 3, Window: time.Minute},
	}
	opt.OnDaemonStart = func(pid int) {
		ctx, cancel := context.WithCancel(context.Background())
		fake := &agent.Fake{Path: srv.Path(), PingInterval: 50 * time.Millisecond, Hello: protocol.AgentHelloMsg{PID: pid, Emacs: "31.1"}}
		fake.Handle = func(op protocol.AgentOp, args json.RawMessage) (any, error) {
			h.mu.Lock()
			h.calls = append(h.calls, string(op))
			h.mu.Unlock()
			if op == protocol.AgentOpSnapshot {
				return protocol.WindowSpec{Files: []protocol.FileSpec{{Path: "hello/src/main.rs", Point: 42, Visible: true}}}, nil
			}
			return agent.DefaultFakeHandler(op, args)
		}
		h.mu.Lock()
		h.fake, h.stop = fake, cancel
		h.mu.Unlock()
		go fake.Run(ctx)
	}
	opt.OnDaemonExit = func() {
		h.mu.Lock()
		if h.stop != nil {
			h.stop()
		}
		h.mu.Unlock()
	}
	if mutate != nil {
		mutate(&opt)
	}
	srv.SetHandler(supHandler{h})
	h.sup = New(opt)
	return h
}

// supHandler forwards agent callbacks to the supervisor.
type supHandler struct{ h *harness }

func (s supHandler) OnHello(m *protocol.AgentHelloMsg) { s.h.sup.OnHello(m) }
func (s supHandler) OnEvent(protocol.Event)            {}
func (s supHandler) OnFrame(f *protocol.AgentFrameMsg) { s.h.sup.OnFrame(f) }
func (s supHandler) OnDisconnect(error)                {}

func (h *harness) agentCalls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

func TestSupervisorLifecycle(t *testing.T) {
	h := newHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if ok, _ := h.sup.Ready(); ok {
		t.Fatal("ready before start")
	}
	if err := h.sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer h.sup.Stop()
	if ok, reason := h.sup.Ready(); !ok {
		t.Fatalf("not ready: %s", reason)
	}
	st := h.sup.Status()
	if st.Version != "31.1" || st.PID == 0 || !st.Agent || !st.Responsive {
		t.Fatalf("status %+v", st)
	}
	if h.tmux.startCount() != 1 {
		t.Fatalf("tmux starts %d", h.tmux.startCount())
	}

	// Heartbeat: a silent agent becomes unresponsive, then responsive.
	h.mu.Lock()
	fake := h.fake
	h.mu.Unlock()
	fake.SetSilent(true)
	h.evs.waitFor(t, "unresponsive")
	if !h.sup.Unresponsive() || h.sup.Status().Responsive {
		t.Fatal("should be unresponsive")
	}
	fake.SetSilent(false)
	h.evs.waitFor(t, "responsive")

	// Terminal frame: the pane dies (C-x C-c) and is respawned.
	h.sup.OnFrame(&protocol.AgentFrameMsg{Event: protocol.FrameOpened, Kind: protocol.FrameTerminal})
	h.sup.OnKey(&protocol.KeyEvent{Keys: "C-x C-c", Cmd: "save-buffers-kill-terminal"})
	h.tmux.die()
	h.evs.waitFor(t, "frame-closed:terminal")
	deadline := time.Now().Add(5 * time.Second)
	for h.tmux.respawnCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if h.tmux.respawnCount() != 1 {
		t.Fatalf("respawns %d", h.tmux.respawnCount())
	}
	// No agent frame event: the supervisor reports frame-opened itself.
	h.evs.waitFor(t, "frame-opened:terminal")
	h.evs.mu.Lock()
	for _, ev := range h.evs.evs {
		if e, ok := ev.(*protocol.EmacsEvent); ok && e.Event == protocol.EmacsFrameClosed && e.Reason != "C-x C-c" {
			t.Errorf("frame-closed reason %q", e.Reason)
		}
	}
	h.evs.mu.Unlock()

	// Keys go through tmux.
	if err := h.sup.SendKeys(ctx, "", "C-x C-f", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.sup.SendKeys(ctx, protocol.FrameTerminal, "", "hello world"); err != nil {
		t.Fatal(err)
	}
	if err := h.sup.SendKeys(ctx, "", "C-x <mouse-1>", ""); err == nil {
		t.Fatal("bad keys should fail")
	}
	h.tmux.mu.Lock()
	if strings.Join(h.tmux.sent, "|") != "C-x C-f|-l hello world" {
		t.Fatalf("sent %q", h.tmux.sent)
	}
	h.tmux.mu.Unlock()

	// A snapshot is saved for restart restore.
	if _, err := h.sup.Snapshot(ctx); err != nil {
		t.Fatal(err)
	}
	if spec := h.sup.LastSnapshot(); spec == nil || spec.Files[0].Point != 42 {
		t.Fatalf("snapshot %+v", spec)
	}

	// Crash: the daemon dies, restarts with backoff, buffers are restored.
	oldPID := h.sup.Status().PID
	_ = syscall.Kill(oldPID, syscall.SIGKILL)
	h.evs.waitFor(t, "restarting")
	h.evs.waitFor(t, "daemon-restarted")
	if h.sup.Status().PID == oldPID {
		t.Fatal("daemon pid unchanged")
	}
	found := false
	for _, c := range h.agentCalls() {
		found = found || c == string(protocol.AgentOpOpen)
	}
	if !found {
		t.Fatalf("no open call after the crash: %v", h.agentCalls())
	}

	// Requested restart.
	before := len(h.evs.emacs())
	callsBefore := len(h.agentCalls())
	if err := h.sup.RestartEmacs(ctx, "module set changed"); err != nil {
		t.Fatal(err)
	}
	got := h.evs.emacs()[before:]
	if got[0] != "restarting" || got[len(got)-1] != "daemon-restarted" {
		t.Fatalf("restart events %v", got)
	}
	// Snapshot, then the modified buffers are saved, then the new daemon
	// re-opens the snapshot.
	calls := strings.Join(h.agentCalls()[callsBefore:], " ")
	if !strings.HasPrefix(calls, "snapshot eval") || !strings.Contains(calls, "open") {
		t.Fatalf("agent calls around the restart: %s", calls)
	}
}

func TestSupervisorGUIMode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var modes []protocol.FrameKind
	h := newHarness(t, func(o *Options) {
		o.VNCAddr = ln.Addr().String()
		o.OnModeChange = func(m protocol.FrameKind) { modes = append(modes, m) }
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer h.sup.Stop()
	go func() {
		// The agent reports the GUI frame shortly after emacsclient -c.
		time.Sleep(100 * time.Millisecond)
		h.sup.OnFrame(&protocol.AgentFrameMsg{Event: protocol.FrameOpened, Kind: protocol.FrameGUI})
	}()
	m, err := h.sup.SetMode(ctx, protocol.FrameGUI)
	if err != nil || m != protocol.FrameGUI || h.sup.Mode() != protocol.FrameGUI {
		t.Fatalf("gui: %v %v", m, err)
	}
	if !h.sup.Frames().GUI || !h.sup.xvnc.Running() || !h.sup.gui.Running() {
		t.Fatal("gui children not running")
	}
	if err := h.sup.SendKeys(ctx, protocol.FrameGUI, "C-x C-f", ""); err != nil {
		t.Fatalf("xdotool: %v", err)
	}
	h.mu.Lock()
	evalsBefore := strings.Count(strings.Join(h.calls, ","), "eval")
	h.mu.Unlock()
	if _, err := h.sup.SetMode(ctx, protocol.FrameTerminal); err != nil {
		t.Fatal(err)
	}
	if h.sup.Frames().GUI || h.sup.xvnc.Running() || h.sup.gui.Running() {
		t.Fatal("gui children still running")
	}
	if len(modes) != 2 || modes[1] != protocol.FrameTerminal {
		t.Fatalf("mode callbacks %v", modes)
	}
	// The GUI frame is deleted inside Emacs (an eval) before Xvnc stops,
	// and an intentional switch emits no frame-closed event.
	h.mu.Lock()
	evalsAfter := strings.Count(strings.Join(h.calls, ","), "eval")
	h.mu.Unlock()
	if evalsAfter != evalsBefore+1 {
		t.Fatalf("agent calls %v: want one eval deleting the GUI frame", h.calls)
	}
	want := []string{"frame-opened:gui", "mode:gui", "mode:terminal"}
	got := h.evs.emacs()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events %v", got)
	}
	if err := h.sup.SendKeys(ctx, protocol.FrameGUI, "C-g", ""); err == nil {
		t.Fatal("gui keys while in terminal mode should fail")
	}
}

func TestSupervisorGUIDisabledAndHelloTimeout(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.GUIEnabled = false
		o.HelloTimeout = 200 * time.Millisecond
		o.OnDaemonStart = nil // no agent will ever connect
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := h.sup.Start(ctx)
	if err == nil {
		t.Fatal("expected a hello timeout")
	}
	defer h.sup.Stop()
	ok, reason := h.sup.Ready()
	if ok || !strings.Contains(reason, "did not start") {
		t.Fatalf("ready=%v reason=%q", ok, reason)
	}
	got := strings.Join(h.evs.emacs(), ",")
	if got != "unresponsive,error" {
		t.Fatalf("events %s", got)
	}
	if _, err := h.sup.SetMode(ctx, protocol.FrameGUI); err != ErrGUIDisabled {
		t.Fatalf("gui disabled: %v", err)
	}
}

func TestSupervisorGivesUpOnCrashLoop(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		calls := 0
		o.Commands.Daemon = func() *exec.Cmd {
			calls++
			if calls == 1 {
				return exec.Command("bash", "-c", "exec sleep 300")
			}
			return exec.Command("bash", "-c", "exit 1")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer h.sup.Stop()
	_ = syscall.Kill(h.sup.Status().PID, syscall.SIGKILL)
	h.evs.waitFor(t, "error")
	if ok, reason := h.sup.Ready(); ok || reason != "Emacs keeps exiting" {
		t.Fatalf("ready=%v %q", ok, reason)
	}
	n := 0
	for _, e := range h.evs.emacs() {
		if e == "restarting" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("restarting events %d (%v)", n, h.evs.emacs())
	}
}

// TestSupervisorBusyEmacs: a missing heartbeat sends the debug signal to
// the daemon process only (its subprocesses survive), and Rescue's C-g
// offers a restart when Emacs is still busy RescueGrace later.
func TestSupervisorBusyEmacs(t *testing.T) {
	var dir string
	h := newHarness(t, func(o *Options) {
		dir = filepath.Dir(o.SnapshotFile)
		o.BusySignal = syscall.SIGUSR2
		o.RescueGrace = 200 * time.Millisecond
		o.Commands.Daemon = func() *exec.Cmd {
			script := `sleep 1000 & echo $! > "$D/child"
trap 'echo usr2 >> "$D/signals"' USR2
while :; do sleep 0.05; done`
			c := exec.Command("bash", "-c", script)
			c.Env = append(os.Environ(), "D="+dir)
			return c
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer h.sup.Stop()

	h.mu.Lock()
	fake := h.fake
	h.mu.Unlock()
	fake.SetSilent(true)
	h.evs.waitFor(t, "unresponsive")
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(dir, "signals"))
		if strings.TrimSpace(string(b)) == "usr2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon got no SIGUSR2 (signals file %q)", b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, err := os.ReadFile(filepath.Join(dir, "child"))
	if err != nil {
		t.Fatal(err)
	}
	var child int
	fmtSscan(strings.TrimSpace(string(b)), &child)
	if child == 0 || syscall.Kill(child, 0) != nil {
		t.Fatalf("the daemon's subprocess %d did not survive the signal", child)
	}

	// Rescue while still busy: C-g through tmux, then the restart offer.
	if err := h.sup.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	h.tmux.mu.Lock()
	sent := strings.Join(h.tmux.sent, "|")
	h.tmux.mu.Unlock()
	if sent != "C-g" {
		t.Fatalf("tmux sent %q", sent)
	}
	h.evs.waitFor(t, "error")
	h.evs.mu.Lock()
	var msg string
	for _, ev := range h.evs.evs {
		if e, ok := ev.(*protocol.ErrorEvent); ok {
			msg = e.Message
		}
	}
	h.evs.mu.Unlock()
	if msg != StillBusyMessage {
		t.Fatalf("error event %q", msg)
	}

	// Once Emacs answers again, Rescue offers nothing.
	fake.SetSilent(false)
	h.evs.waitFor(t, "responsive")
	errorsBefore := strings.Count(strings.Join(h.evs.emacs(), ","), "error")
	if err := h.sup.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if n := strings.Count(strings.Join(h.evs.emacs(), ","), "error"); n != errorsBefore {
		t.Fatalf("restart offered although Emacs is responsive: %v", h.evs.emacs())
	}
}

func fmtSscan(s string, v *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*v = n
	return 1, nil
}

func TestSupervisorWaitReady(t *testing.T) {
	h := newHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.sup.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer h.sup.Stop()
	h.sup.mu.Lock()
	h.sup.frames.Terminal = false
	h.sup.mu.Unlock()
	if h.sup.WaitReady(ctx, 100*time.Millisecond) {
		t.Fatal("ready without a terminal frame")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.sup.OnFrame(&protocol.AgentFrameMsg{Event: protocol.FrameOpened, Kind: protocol.FrameTerminal})
	}()
	if !h.sup.WaitReady(ctx, 3*time.Second) {
		t.Fatal("not ready after the terminal frame opened")
	}
}
