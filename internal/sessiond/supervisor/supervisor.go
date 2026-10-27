package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/zsy/emacs-playground/internal/protocol"
)

// Timing (backend.md §3.2, §3.3, emacs.md §7).
const (
	HelloTimeout     = 60 * time.Second
	PingInterval     = 2 * time.Second
	MissedPings      = 5
	SnapshotInterval = 30 * time.Second
	GUIStartTimeout  = 5 * time.Second
	StopGrace        = 15 * time.Second
	RestartGrace     = 5 * time.Second
	// RescueGrace: if Emacs is still unresponsive this long after Rescue
	// interrupted it, a daemon restart is offered (backend.md §3.3).
	RescueGrace = 10 * time.Second
)

// StillBusyMessage is the error event text that offers a restart.
const StillBusyMessage = "Emacs is still busy after Rescue. Restart Emacs to recover; your files are safe."

// ErrGUIDisabled is returned by SetMode when the GUI frame is disabled.
var ErrGUIDisabled = errors.New("the GUI frame is disabled for this session")

// Agent is what the supervisor needs from the agent connection.
type Agent interface {
	HelloCount() uint64
	WaitHello(ctx context.Context, after uint64) (*protocol.AgentHelloMsg, error)
	LastPing() time.Time
	Connected() bool
	Call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error
}

// Commands build the supervised processes. RealCommands uses the session
// image binaries; tests and -fake mode substitute bash.
type Commands struct {
	Daemon   func() *exec.Cmd
	Xvnc     func() *exec.Cmd
	GUIFrame func() *exec.Cmd
	// Xdotool returns a command running xdotool with args on the GUI
	// display.
	Xdotool func(args ...string) *exec.Cmd
}

// Options configure a Supervisor.
type Options struct {
	Commands Commands
	Tmux     Tmux
	Agent    Agent
	// Emit publishes emacs, mode and error events.
	Emit func(protocol.Event)
	Log  *slog.Logger
	// GUIEnabled allows SetMode(gui).
	GUIEnabled bool
	// VNCAddr is where Xvnc listens (127.0.0.1:5901).
	VNCAddr string
	// SnapshotFile stores the last buffer and window spec (restart
	// restore), e.g. /work/.playground/snapshot.json.
	SnapshotFile string
	// OnModeChange is called once the new mode is recorded, before the GUI
	// frame and Xvnc are stopped (sessiond closes VNC bridges there) and
	// before the mode event is emitted. It must not block.
	OnModeChange func(protocol.FrameKind)
	// OnDaemonStart and OnDaemonExit observe the daemon process (-fake mode
	// starts its in-process agent here).
	OnDaemonStart func(pid int)
	OnDaemonExit  func()
	// BusySignal is sent to the daemon process (not its group) when the
	// heartbeat goes missing: SIGUSR2 makes Emacs enter the debugger
	// (debug-on-event), which breaks most busy loops (architecture.md §6).
	// Zero sends nothing (tests and -fake mode, where the daemon is bash).
	BusySignal syscall.Signal
	// Overridable timing for tests.
	RescueGrace      time.Duration
	HelloTimeout     time.Duration
	PingInterval     time.Duration
	SnapshotInterval time.Duration
	DaemonPolicy     *Policy
	RespawnDelay     time.Duration
	GUIStartTimeout  time.Duration
	// FrameWait bounds how long a restart waits for the new terminal
	// frame before it reports daemon-restarted (default 10 s).
	FrameWait time.Duration
}

// Supervisor keeps Emacs, tmux and the GUI display alive (backend.md §3.3,
// architecture.md §5.2).
type Supervisor struct {
	opt Options
	log *slog.Logger

	daemon *Child
	xvnc   *Child
	gui    *Child

	mu            sync.Mutex
	mode          protocol.FrameKind
	frames        protocol.Frames
	ready         bool // the daemon's agent said hello since it last started
	everReady     bool
	failed        string
	unresponsive  bool
	intentional   bool // a restart or stop is in progress
	crashed       bool // the daemon exited unexpectedly; restore on next hello
	crashReason   string
	daemonStarts  int
	version       string
	agentPID      int
	lastRespawn   time.Time
	respawnStreak int
	lastCloseCmd  string
	lastCloseAt   time.Time
	termSeen      chan struct{}
	guiSeen       chan struct{}

	restarting bool // RestartEmacs is running (under mu)

	restartMu sync.Mutex
	modeMu    sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// New builds a supervisor; Start runs it.
func New(opt Options) *Supervisor {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.Tmux == nil {
		opt.Tmux = NoTmux{}
	}
	if opt.HelloTimeout <= 0 {
		opt.HelloTimeout = HelloTimeout
	}
	if opt.PingInterval <= 0 {
		opt.PingInterval = PingInterval
	}
	if opt.SnapshotInterval <= 0 {
		opt.SnapshotInterval = SnapshotInterval
	}
	if opt.FrameWait <= 0 {
		opt.FrameWait = 10 * time.Second
	}
	if opt.RespawnDelay <= 0 {
		opt.RespawnDelay = 100 * time.Millisecond
	}
	if opt.GUIStartTimeout <= 0 {
		opt.GUIStartTimeout = GUIStartTimeout
	}
	if opt.RescueGrace <= 0 {
		opt.RescueGrace = RescueGrace
	}
	policy := DaemonPolicy
	if opt.DaemonPolicy != nil {
		policy = *opt.DaemonPolicy
	}
	s := &Supervisor{opt: opt, log: opt.Log, mode: protocol.FrameTerminal,
		termSeen: make(chan struct{}), guiSeen: make(chan struct{})}
	s.daemon = &Child{Name: "emacs", New: opt.Commands.Daemon, Policy: policy, Log: opt.Log,
		OnStart: s.onDaemonStart, OnExit: s.onDaemonExit}
	if opt.Commands.Xvnc != nil {
		s.xvnc = &Child{Name: "xvnc", New: opt.Commands.Xvnc, Policy: WhileActivePolicy, Log: opt.Log}
	}
	if opt.Commands.GUIFrame != nil {
		s.gui = &Child{Name: "gui-frame", New: opt.Commands.GUIFrame, Policy: WhileActivePolicy, Log: opt.Log}
	}
	return s
}

func (s *Supervisor) emit(evs ...protocol.Event) {
	if s.opt.Emit == nil {
		return
	}
	for _, ev := range evs {
		s.opt.Emit(ev)
	}
}

// Start runs the startup sequence's Emacs part (backend.md §3.2 steps 3 and
// 4): start the daemon and wait for the agent's hello (restarting once
// after HelloTimeout), then start tmux and the terminal frame, and the
// background loops. It returns an error when Emacs never said hello; the
// supervisor keeps running so /rpc/restart-emacs can try again.
func (s *Supervisor) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	before := s.opt.Agent.HelloCount()
	s.daemon.Start()
	_, err := s.waitHello(before)
	if err != nil {
		s.log.Warn("emacs did not say hello; restarting it once", "timeout", s.opt.HelloTimeout)
		s.setIntentional(true)
		before = s.opt.Agent.HelloCount()
		s.daemon.Stop(RestartGrace)
		s.setIntentional(false)
		s.daemon.Start()
		_, err = s.waitHello(before)
	}
	if err != nil {
		s.mu.Lock()
		s.failed = "Emacs did not start within " + s.opt.HelloTimeout.String()
		s.mu.Unlock()
		s.emit(&protocol.EmacsEvent{Event: protocol.EmacsUnresponsive, Reason: "Emacs did not start"},
			&protocol.ErrorEvent{Code: protocol.ErrEmacsUnavailable, Message: "Emacs did not start. Try restarting it from the session page."})
	}
	if terr := s.opt.Tmux.Start(s.ctx); terr != nil {
		s.log.Error("tmux start failed", "err", terr)
		if err == nil {
			err = terr
		}
	}
	s.wg.Add(3)
	go s.frameWatcher()
	go s.heartbeat()
	go s.snapshotLoop()
	return err
}

func (s *Supervisor) waitHello(before uint64) (*protocol.AgentHelloMsg, error) {
	ctx, cancel := context.WithTimeout(s.ctx, s.opt.HelloTimeout)
	defer cancel()
	return s.opt.Agent.WaitHello(ctx, before)
}

func (s *Supervisor) setIntentional(v bool) {
	s.mu.Lock()
	s.intentional = v
	s.mu.Unlock()
}

// Ready reports whether Emacs has said hello at least once and has not
// failed for good. reason explains a false.
func (s *Supervisor) Ready() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.failed != "":
		return false, s.failed
	case !s.everReady:
		return false, "Emacs is starting"
	}
	return true, ""
}

// Mode returns the frame mode.
func (s *Supervisor) Mode() protocol.FrameKind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// Frames returns which frames exist.
func (s *Supervisor) Frames() protocol.Frames {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames
}

// Status describes Emacs for /status and hello.
func (s *Supervisor) Status() protocol.EmacsStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := protocol.EmacsStatus{Version: s.version, PID: s.daemon.PID(), Agent: s.opt.Agent.Connected(), Responsive: !s.unresponsive && s.ready}
	if t := s.daemon.StartedAt(); !t.IsZero() && st.PID != 0 {
		st.UptimeMs = time.Since(t).Milliseconds()
	}
	return st
}

func (s *Supervisor) onDaemonStart(pid int) {
	s.mu.Lock()
	s.daemonStarts++
	s.ready = false
	s.mu.Unlock()
	if s.opt.OnDaemonStart != nil {
		s.opt.OnDaemonStart(pid)
	}
}

func (s *Supervisor) onDaemonExit(err error, restarting, gaveUp bool) {
	if s.opt.OnDaemonExit != nil {
		s.opt.OnDaemonExit()
	}
	s.mu.Lock()
	s.ready = false
	s.frames = protocol.Frames{}
	intentional := s.intentional
	if !intentional {
		s.crashed = true
		s.crashReason = errString(err)
	}
	if gaveUp {
		s.failed = "Emacs keeps exiting"
	}
	s.mu.Unlock()
	if intentional {
		return
	}
	if gaveUp {
		s.emit(&protocol.EmacsEvent{Event: protocol.EmacsUnresponsive, Reason: "Emacs keeps exiting"},
			&protocol.ErrorEvent{Code: protocol.ErrEmacsUnavailable, Message: "Emacs keeps exiting. Restart it from the session page; your files are safe."})
		return
	}
	s.emit(&protocol.EmacsEvent{Event: protocol.EmacsRestarting, Reason: errString(err)})
}

// OnHello is called by the agent handler for every hello.
func (s *Supervisor) OnHello(h *protocol.AgentHelloMsg) {
	// The socket is read independently of the process: a daemon that said
	// hello and exited at once may be heard after its exit was handled
	// (and after the restart policy gave up). Its hello proves nothing.
	if s.daemon.PID() == 0 || s.daemon.GaveUp() {
		s.log.Info("ignoring the hello of an exited daemon", "pid", h.PID)
		return
	}
	s.mu.Lock()
	s.ready = true
	s.everReady = true
	s.version = h.Emacs
	s.agentPID = h.PID
	wasUnresponsive := s.unresponsive
	s.unresponsive = false
	crashed := s.crashed
	reason := s.crashReason
	s.crashed = false
	intentional := s.intentional
	if !intentional {
		s.failed = ""
	}
	s.mu.Unlock()
	if wasUnresponsive {
		s.emit(&protocol.EmacsEvent{Event: protocol.EmacsResponsive})
	}
	if crashed && !intentional && s.ctx != nil {
		// Unexpected exit and automatic restart: restore buffers.
		go s.afterRestart(reason)
	} else if s.ctx != nil && !intentional {
		go s.ensureTerminalFrame()
	}
}

// OnFrame is called for agent frame messages.
func (s *Supervisor) OnFrame(f *protocol.AgentFrameMsg) {
	s.setFrame(f.Kind, f.Event == protocol.FrameOpened, "")
}

// OnKey watches for the commands that close a frame, so frame-closed can
// say why.
func (s *Supervisor) OnKey(ev *protocol.KeyEvent) {
	switch ev.Cmd {
	case "save-buffers-kill-terminal", "save-buffers-kill-emacs", "delete-frame":
		s.mu.Lock()
		s.lastCloseCmd = ev.Keys
		if s.lastCloseCmd == "" {
			s.lastCloseCmd = ev.Cmd
		}
		s.lastCloseAt = time.Now()
		s.mu.Unlock()
	}
}

// setFrame records a frame opening or closing and emits the event once.
func (s *Supervisor) setFrame(kind protocol.FrameKind, open bool, reason string) {
	s.mu.Lock()
	var cur *bool
	switch kind {
	case protocol.FrameGUI:
		cur = &s.frames.GUI
	default:
		kind = protocol.FrameTerminal
		cur = &s.frames.Terminal
	}
	if *cur == open {
		s.mu.Unlock()
		return
	}
	*cur = open
	if open {
		if kind == protocol.FrameTerminal {
			close(s.termSeen)
			s.termSeen = make(chan struct{})
		} else {
			close(s.guiSeen)
			s.guiSeen = make(chan struct{})
		}
	}
	if !open && reason == "" && time.Since(s.lastCloseAt) < 3*time.Second {
		reason = s.lastCloseCmd
	}
	// The GUI frame closing while the browser shows the terminal (a switch
	// back to the terminal, or a late report of one) is not news for the
	// learner: no frame-closed event, only the state change.
	silent := kind == protocol.FrameGUI && !open && s.mode != protocol.FrameGUI
	s.mu.Unlock()
	if silent {
		return
	}
	ev := &protocol.EmacsEvent{Event: protocol.EmacsFrameOpened, Frame: kind}
	if !open {
		ev.Event = protocol.EmacsFrameClosed
		ev.Reason = reason
	}
	s.emit(ev)
}

// frameWatcher respawns the terminal frame when its pane dies and restarts
// tmux when the server goes away.
func (s *Supervisor) frameWatcher() {
	defer s.wg.Done()
	for {
		err := s.opt.Tmux.WaitPaneDied(s.ctx)
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			s.log.Warn("tmux went away; restarting it", "err", err)
			s.setFrame(protocol.FrameTerminal, false, "tmux exited")
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			if err := s.opt.Tmux.Start(s.ctx); err != nil {
				s.log.Error("tmux restart failed", "err", err)
			}
			continue
		}
		s.setFrame(protocol.FrameTerminal, false, "")
		s.ensureTerminalFrame()
	}
}

// ensureTerminalFrame respawns a dead pane when the daemon is ready, with a
// growing delay if it keeps dying at once.
func (s *Supervisor) ensureTerminalFrame() {
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	ready := s.ready
	s.mu.Unlock()
	if !ready || !s.opt.Tmux.PaneDead(s.ctx) {
		return
	}
	s.mu.Lock()
	delay := s.opt.RespawnDelay
	if time.Since(s.lastRespawn) < 2*time.Second {
		s.respawnStreak++
		delay = time.Duration(1<<min(s.respawnStreak, 5)) * 250 * time.Millisecond
	} else {
		s.respawnStreak = 0
	}
	s.mu.Unlock()
	select {
	case <-s.ctx.Done():
		return
	case <-time.After(delay):
	}
	s.mu.Lock()
	s.lastRespawn = time.Now()
	seen := s.termSeen
	s.mu.Unlock()
	if err := s.opt.Tmux.RespawnPane(s.ctx); err != nil {
		s.log.Warn("respawn terminal frame", "err", err)
		return
	}
	// The agent reports the new frame; if it does not within 2 s, report it
	// ourselves so the browser's overlay clears.
	go func() {
		select {
		case <-seen:
		case <-time.After(2 * time.Second):
			if s.ctx.Err() == nil && !s.opt.Tmux.PaneDead(s.ctx) {
				s.setFrame(protocol.FrameTerminal, true, "")
			}
		case <-s.ctx.Done():
		}
	}()
}

// heartbeat raises unresponsive after MissedPings missed pings and
// responsive when pings resume.
func (s *Supervisor) heartbeat() {
	defer s.wg.Done()
	t := time.NewTicker(s.opt.PingInterval / 2)
	defer t.Stop()
	limit := time.Duration(MissedPings) * s.opt.PingInterval
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		if !s.opt.Agent.Connected() {
			continue
		}
		last := s.opt.Agent.LastPing()
		late := !last.IsZero() && time.Since(last) > limit
		s.mu.Lock()
		changed := late != s.unresponsive
		s.unresponsive = late
		s.mu.Unlock()
		if !changed {
			continue
		}
		if late {
			s.emit(&protocol.EmacsEvent{Event: protocol.EmacsUnresponsive, Reason: fmt.Sprintf("no heartbeat for %s", limit)})
			if s.opt.BusySignal != 0 {
				if err := s.daemon.SignalProcess(s.opt.BusySignal); err != nil {
					s.log.Warn("signalling the busy daemon", "signal", s.opt.BusySignal, "err", err)
				} else {
					s.log.Info("emacs is unresponsive; sent the debug signal", "signal", s.opt.BusySignal)
				}
			}
		} else {
			s.emit(&protocol.EmacsEvent{Event: protocol.EmacsResponsive})
		}
	}
}

// Unresponsive reports missed heartbeats.
func (s *Supervisor) Unresponsive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unresponsive
}

// snapshotLoop saves the buffer and window spec every SnapshotInterval.
func (s *Supervisor) snapshotLoop() {
	defer s.wg.Done()
	t := time.NewTicker(s.opt.SnapshotInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			if _, err := s.Snapshot(s.ctx); err != nil {
				s.log.Debug("snapshot", "err", err)
			}
		}
	}
}

// Snapshot asks the agent for the buffer and window spec and saves it.
func (s *Supervisor) Snapshot(ctx context.Context) (*protocol.WindowSpec, error) {
	s.mu.Lock()
	ok := s.ready && !s.unresponsive
	s.mu.Unlock()
	if !ok || !s.opt.Agent.Connected() {
		return nil, errors.New("emacs not ready")
	}
	var spec protocol.WindowSpec
	if err := s.opt.Agent.Call(ctx, protocol.AgentOpSnapshot, nil, 2*time.Second, &spec); err != nil {
		return nil, err
	}
	if spec.TakenAt == 0 {
		spec.TakenAt = time.Now().UnixMilli()
	}
	if err := s.saveSnapshot(&spec); err != nil {
		s.log.Warn("saving snapshot", "err", err)
	}
	return &spec, nil
}

func (s *Supervisor) saveSnapshot(spec *protocol.WindowSpec) error {
	if s.opt.SnapshotFile == "" {
		return nil
	}
	b, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.opt.SnapshotFile), 0o755); err != nil {
		return err
	}
	tmp := s.opt.SnapshotFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.opt.SnapshotFile)
}

// LastSnapshot returns the saved spec, or nil.
func (s *Supervisor) LastSnapshot() *protocol.WindowSpec {
	if s.opt.SnapshotFile == "" {
		return nil
	}
	b, err := os.ReadFile(s.opt.SnapshotFile)
	if err != nil {
		return nil
	}
	var spec protocol.WindowSpec
	if json.Unmarshal(b, &spec) != nil {
		return nil
	}
	return &spec
}

// FlushForm saves every modified file buffer before Emacs stops (a
// restart, sessiond's shutdown), whether or not auto-save-visited-mode is
// on: lessons about the modified flag turn it off, and stopping the daemon
// would otherwise lose the learner's edits. Like base's
// auto-save-visited-predicate it never saves over a file that changed on
// disk (a lesson reset just restored it), skips remote and read-only
// files, and a buffer that cannot be saved without a question is left
// alone. The value is the number of buffers saved.
const FlushForm = `(let ((n 0))
  (dolist (b (buffer-list) n)
    (with-current-buffer b
      (when (and buffer-file-name (buffer-modified-p) (not buffer-read-only)
                 (not (file-remote-p buffer-file-name))
                 (verify-visited-file-modtime b))
        (condition-case nil
            (let ((save-silently t) (inhibit-message t))
              (basic-save-buffer)
              (setq n (1+ n)))
          (error nil))))))`

// FlushTimeout bounds FlushForm.
const FlushTimeout = 3 * time.Second

// Flush saves modified file buffers (FlushForm) when the agent is
// connected and Emacs responsive.
func (s *Supervisor) Flush(ctx context.Context) {
	if s.opt.Agent == nil || !s.opt.Agent.Connected() || s.Unresponsive() {
		return
	}
	var res protocol.AgentEvalResult
	if err := s.opt.Agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: FlushForm}, FlushTimeout, &res); err != nil {
		s.log.Info("saving buffers before Emacs stops", "err", err)
		return
	}
	if n, ok := res.Value.(float64); ok && n > 0 {
		s.log.Info("saved modified buffers before Emacs stops", "buffers", int(n))
	}
}

// RestartEmacs restarts the daemon with buffer restore (rpc restart-emacs,
// module changes): snapshot, save modified buffers, stop, start, wait for
// hello, re-open the snapshot, respawn the terminal frame, emit
// daemon-restarted.
func (s *Supervisor) RestartEmacs(ctx context.Context, reason string) error {
	s.restartMu.Lock()
	defer s.restartMu.Unlock()
	if s.ctx == nil {
		return errors.New("supervisor not started")
	}
	s.mu.Lock()
	s.restarting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.restarting = false
		s.mu.Unlock()
	}()
	s.emit(&protocol.EmacsEvent{Event: protocol.EmacsRestarting, Reason: reason})
	if _, err := s.Snapshot(ctx); err != nil {
		s.log.Info("restart without a fresh snapshot", "err", err)
	}
	// The snapshot re-opens files from disk: unsaved edits would be lost.
	s.Flush(ctx)
	s.setIntentional(true)
	before := s.opt.Agent.HelloCount()
	s.daemon.Stop(RestartGrace)
	s.mu.Lock()
	s.failed = ""
	s.crashed = false
	// The old daemon's client frames are gone with it; the terminal frame
	// counts as open again once the new daemon's client reports it
	// (WaitTerminalFrame). No event: the browser shows "restarting".
	s.frames.Terminal = false
	s.mu.Unlock()
	s.daemon.Start()
	_, err := s.waitHello(before)
	s.setIntentional(false)
	if err != nil {
		s.mu.Lock()
		s.failed = "Emacs did not start after the restart"
		s.mu.Unlock()
		s.emit(&protocol.ErrorEvent{Code: protocol.ErrEmacsUnavailable, Message: "Emacs did not start after the restart."})
		return fmt.Errorf("emacs did not say hello after restart: %w", err)
	}
	s.afterRestart(reason)
	return nil
}

// afterRestart restores buffers and frames once a restarted daemon said
// hello.
func (s *Supervisor) afterRestart(reason string) {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	if spec := s.LastSnapshot(); spec != nil && len(spec.Files) > 0 {
		if err := s.opt.Agent.Call(ctx, protocol.AgentOpOpen, spec, 10*time.Second, nil); err != nil {
			s.log.Warn("restoring buffers after restart", "err", err)
		}
	}
	s.ensureTerminalFrame()
	// Lesson setup after a restart needs the learner's frame: before it
	// exists, layouts would go to the daemon's invisible initial frame.
	if !s.waitTerminalFrame(ctx, s.opt.FrameWait) {
		s.log.Warn("the terminal frame did not come back after the restart", "waited", s.opt.FrameWait)
	}
	s.emit(&protocol.EmacsEvent{Event: protocol.EmacsDaemonRestarted, Reason: reason})
}

// waitTerminalFrame waits until the terminal frame is open.
func (s *Supervisor) waitTerminalFrame(ctx context.Context, timeout time.Duration) bool {
	s.mu.Lock()
	open := s.frames.Terminal
	seen := s.termSeen
	s.mu.Unlock()
	if open {
		return true
	}
	select {
	case <-seen:
		return true
	case <-time.After(timeout):
		return false
	case <-ctx.Done():
		return false
	}
}

// WaitReady waits, up to timeout, until no restart is in progress and the
// terminal frame is open, so a lesson's setup lands in the frame the
// learner sees (scenario loader).
func (s *Supervisor) WaitReady(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		s.mu.Lock()
		ok := !s.restarting && s.ready && s.frames.Terminal
		s.mu.Unlock()
		if ok {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// deleteGUIFramesForm deletes every X frame of the daemon (the GUI frame).
const deleteGUIFramesForm = `(progn (dolist (f (frame-list)) (when (eq (framep f) 'x) (delete-frame f t))) nil)`

// SetMode switches the frame shown to the browser (rpc mode, architecture.md
// §3.5).
func (s *Supervisor) SetMode(ctx context.Context, mode protocol.FrameKind) (protocol.FrameKind, error) {
	s.modeMu.Lock()
	defer s.modeMu.Unlock()
	switch mode {
	case protocol.FrameGUI:
		if !s.opt.GUIEnabled || s.xvnc == nil || s.gui == nil {
			return s.Mode(), ErrGUIDisabled
		}
		if s.Mode() == protocol.FrameGUI && s.gui.Wanted() {
			return mode, nil
		}
		s.xvnc.Start()
		if s.opt.VNCAddr != "" {
			if err := waitTCP(ctx, s.opt.VNCAddr, s.opt.GUIStartTimeout); err != nil {
				s.xvnc.Stop(time.Second)
				return s.Mode(), fmt.Errorf("the GUI display did not start: %w", err)
			}
		}
		s.mu.Lock()
		seen := s.guiSeen
		already := s.frames.GUI
		s.mu.Unlock()
		s.gui.Start()
		if !already {
			select {
			case <-seen:
			case <-time.After(s.opt.GUIStartTimeout):
				if s.gui.Running() {
					s.setFrame(protocol.FrameGUI, true, "")
				}
			case <-ctx.Done():
			}
		}
	case protocol.FrameTerminal:
		if s.Mode() == protocol.FrameTerminal {
			return mode, nil
		}
	default:
		return s.Mode(), fmt.Errorf("unknown mode %q", mode)
	}
	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()
	// The hook runs before the display goes away so VNC bridges close with
	// 4002 rather than seeing Xvnc's connection drop.
	if s.opt.OnModeChange != nil {
		s.opt.OnModeChange(mode)
	}
	if mode == protocol.FrameTerminal {
		if s.gui != nil {
			s.gui.Stop(2 * time.Second)
		}
		// Delete the GUI frames inside Emacs before the display goes away:
		// killing emacsclient deletes its frame only when the server's
		// sentinel runs, and an X connection that drops first leaves
		// "Connection lost to X server" in the terminal frame.
		if s.opt.Agent != nil && s.opt.Agent.Connected() {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			if err := s.opt.Agent.Call(cctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: deleteGUIFramesForm}, 2*time.Second, nil); err != nil {
				s.log.Warn("deleting the GUI frame before stopping the display", "err", err)
			}
			cancel()
		}
		if s.xvnc != nil {
			s.xvnc.Stop(2 * time.Second)
		}
		s.setFrame(protocol.FrameGUI, false, "")
	}
	s.emit(&protocol.ModeEvent{Mode: mode})
	return mode, nil
}

func waitTCP(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var d net.Dialer
		dctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		c, err := d.DialContext(dctx, "tcp", addr)
		cancel()
		if err == nil {
			c.Close()
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// SendKeys sends an Emacs key sequence (or literal text) to a frame: tmux
// send-keys for the terminal, xdotool for the GUI frame.
func (s *Supervisor) SendKeys(ctx context.Context, frame protocol.FrameKind, keys, text string) error {
	if frame == "" {
		frame = s.Mode()
	}
	if frame == protocol.FrameGUI {
		if s.Mode() != protocol.FrameGUI || s.opt.Commands.Xdotool == nil {
			return errors.New("the GUI frame is not active")
		}
		var args []string
		if text != "" {
			args = []string{"type", "--delay", "0", "--", text}
		} else {
			ks, err := XdotoolKeys(keys)
			if err != nil {
				return err
			}
			args = append([]string{"key", "--clearmodifiers"}, ks...)
		}
		cmd := s.opt.Commands.Xdotool(args...)
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if out, err := runCtx(cctx, cmd); err != nil {
			return fmt.Errorf("xdotool: %w: %s", err, out)
		}
		return nil
	}
	if text != "" {
		return s.opt.Tmux.SendKeys(ctx, true, text)
	}
	ks, err := TmuxKeys(keys)
	if err != nil {
		return err
	}
	return s.opt.Tmux.SendKeys(ctx, false, ks...)
}

func runCtx(ctx context.Context, cmd *exec.Cmd) (string, error) {
	out := make(chan struct {
		b   []byte
		err error
	}, 1)
	go func() {
		b, err := cmd.CombinedOutput()
		out <- struct {
			b   []byte
			err error
		}{b, err}
	}()
	select {
	case r := <-out:
		return string(r.b), r.err
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return "", ctx.Err()
	}
}

// Interrupt sends C-g to the current frame without going through the agent
// (Rescue while Emacs is busy). If Emacs is still unresponsive RescueGrace
// later, an error event offers a daemon restart (backend.md §3.3).
func (s *Supervisor) Interrupt(ctx context.Context) error {
	if err := s.SendKeys(ctx, "", "C-g", ""); err != nil {
		return err
	}
	if s.ctx == nil {
		return nil
	}
	s.mu.Lock()
	daemonStarts := s.daemonStarts
	s.mu.Unlock()
	go func() {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(s.opt.RescueGrace):
		}
		s.mu.Lock()
		// A restart in the meantime answers the question already.
		still := s.unresponsive && s.daemonStarts == daemonStarts
		s.mu.Unlock()
		if still {
			s.emit(&protocol.ErrorEvent{Code: protocol.ErrEmacsUnavailable, Message: StillBusyMessage})
		}
	}()
	return nil
}

// SignalDaemon sends sig to the daemon's process group.
func (s *Supervisor) SignalDaemon(sig syscall.Signal) error { return s.daemon.Signal(sig) }

// Stop shuts everything down (backend.md §5): the caller has already emitted
// emacs stopping and taken a snapshot.
func (s *Supervisor) Stop() {
	s.setIntentional(true)
	if s.gui != nil {
		s.gui.Stop(2 * time.Second)
	}
	if s.xvnc != nil {
		s.xvnc.Stop(2 * time.Second)
	}
	s.daemon.Stop(StopGrace)
	if s.cancel != nil {
		s.cancel()
	}
	kctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = s.opt.Tmux.Kill(kctx)
	cancel()
	s.wg.Wait()
}
