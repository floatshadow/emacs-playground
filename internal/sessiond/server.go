package sessiond

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent/fakeeditor"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/events"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/inputtap"
	"github.com/floatshadow/emacs-playground/internal/sessiond/preview"
	"github.com/floatshadow/emacs-playground/internal/sessiond/progress"
	"github.com/floatshadow/emacs-playground/internal/sessiond/pty"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/session"
	"github.com/floatshadow/emacs-playground/internal/sessiond/snapshot"
	"github.com/floatshadow/emacs-playground/internal/sessiond/sshbox"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
	"github.com/floatshadow/emacs-playground/internal/sessiond/supervisor"
	"github.com/floatshadow/emacs-playground/internal/sessiond/vnc"
)

// HeartbeatInterval is how often sessiond tells the gateway its WebSocket
// count (api.md §2.6).
const HeartbeatInterval = 30 * time.Second

// Options adjust a Server for tests and development.
type Options struct {
	// Fake substitutes bash for Emacs and Xvnc and the fake editor for the
	// agent and tmux (keys sent to Emacs reach the editor).
	Fake bool
	// Bash is the bash binary for Fake (default "bash").
	Bash string
	Log  *slog.Logger
	// Listener overrides cfg.Listen (tests).
	Listener net.Listener
	// HelloTimeout overrides the supervisor's 60 s (tests).
	HelloTimeout time.Duration
	// Ready, when set, is closed once startup finished (tests).
	Ready chan struct{}
	// Editor is the fake editor in Fake mode (tests drive and inspect it);
	// nil makes one for the workspace.
	Editor *fakeeditor.Editor
	// Clock replaces time.Now in the session core and the evaluator
	// (tests).
	Clock func() time.Time
	// Debounce replaces the evaluator's debounce, and three times it the
	// longest wait (tests).
	Debounce time.Duration
	// ProgressBackoff caps the progress outbox's retry delay (tests;
	// default progress.DefaultMaxBackoff).
	ProgressBackoff time.Duration
}

// Server is one sessiond.
type Server struct {
	cfg *protocol.SessiondConfig
	opt Options
	log *slog.Logger

	bus     *events.Bus
	evlog   *events.LogWriter
	agent   *agent.Server
	sup     *supervisor.Supervisor
	tap     *inputtap.Tap
	eval    *steps.Evaluator
	core    *session.Core
	snap    *snapshot.Service
	modules *scenario.Modules
	preview *preview.Server
	watcher *preview.Watcher
	gw      *gwclient.Client
	outbox  *progress.Outbox
	ptyH    *pty.Handler
	vncH    *vnc.Handler
	eventsH *events.Handler

	wsCount atomic.Int64
	// boxReady: the box alias is set up (sshbox.Setup succeeded).
	boxReady atomic.Bool
	done     chan struct{}
	// stateMu makes persist the one writer of state.json.
	stateMu sync.Mutex
	bg      sync.WaitGroup
	addr    atomic.Value // string

	// editor is the fake editor (Fake mode); fakeCancel stops its
	// connection loop.
	editor     *fakeeditor.Editor
	fakeMu     sync.Mutex
	fakeCancel context.CancelFunc
}

// New builds a server from cfg; Run starts it.
func New(cfg *protocol.SessiondConfig, opt Options) *Server {
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.Bash == "" {
		opt.Bash = "bash"
	}
	return &Server{cfg: cfg, opt: opt, log: opt.Log, done: make(chan struct{}),
		modules: &scenario.Modules{Path: filepath.Join(supervisor.InitDirectory(cfg), "modules.el")}}
}

// Addr returns the HTTP listen address once Run started listening.
func (s *Server) Addr() string {
	v, _ := s.addr.Load().(string)
	return v
}

func (s *Server) playgroundDir() string {
	return filepath.Join(s.cfg.Workspace, checkpoint.PlaygroundDir)
}

func (s *Server) track(d int) { s.wsCount.Add(int64(d)) }

// Run executes the startup sequence, serves until ctx ends, then shuts down
// gracefully.
func (s *Server) Run(ctx context.Context) error {
	cfg := s.cfg
	// 1. Directories and the agent socket.
	if err := os.MkdirAll(cfg.RunDir, 0o755); err != nil {
		return fmt.Errorf("run dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.RunDir, "emacs"), 0o700); err != nil {
		return fmt.Errorf("emacs server dir: %w", err)
	}
	if err := os.MkdirAll(cfg.Workspace, 0o755); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.AgentSocket), 0o755); err != nil {
		return fmt.Errorf("agent socket dir: %w", err)
	}
	s.bus = events.NewBus(s.log)
	ag, err := agent.Listen(cfg.AgentSocket, nil, s.log.With("component", "agent"))
	if err != nil {
		return err
	}
	s.agent = ag
	defer ag.Close()

	// 2. Workspace.
	s.snap = &snapshot.Service{
		Files:     checkpoint.New(cfg.Workspace, cfg.GitBin),
		Editor:    &snapshot.AgentEditor{Agent: ag, Workspace: cfg.Workspace},
		Modules:   s.modules,
		StateFile: filepath.Join(s.playgroundDir(), "snapshot.json"),
		Emit:      func(ev protocol.Event) { s.bus.Publish(ev) },
		Log:       s.log.With("component", "snapshot"),
	}
	if err := s.initWorkspace(ctx); err != nil {
		s.log.Error("workspace initialisation", "err", err)
	}
	s.evlog = events.NewLogWriter(filepath.Join(s.playgroundDir(), "events.log"), s.log)
	s.bus.AddSink(s.evlog.Sink)
	defer s.evlog.Close()

	pv, err := preview.New(cfg.Workspace)
	if err != nil {
		return fmt.Errorf("workspace root: %w", err)
	}
	s.preview = pv
	defer pv.Close()

	st := s.readState()
	s.wire(st.outbox())
	s.core.RestoreSaved(st.saved())

	s.agent.SetHandler(s)
	if w, err := preview.NewWatcher(cfg.Workspace, s.onFile, s.log.With("component", "watcher")); err != nil {
		s.log.Warn("file watcher unavailable", "err", err)
	} else {
		s.watcher = w
		w.OnGit = s.eval.Invalidate
		defer w.Close()
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if s.watcher != nil {
		go s.watcher.Run(runCtx)
	}

	// 5 (early). HTTP, so health checks see "starting" rather than nothing.
	ln := s.opt.Listener
	if ln == nil {
		ln, err = net.Listen("tcp", cfg.Listen)
		if err != nil {
			return fmt.Errorf("listen %s: %w", cfg.Listen, err)
		}
	}
	s.addr.Store(ln.Addr().String())
	srv := httpx.NewServer(cfg.Listen, s.Handler(), s.log)
	httpErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErr <- err
		}
	}()
	s.log.Info("sessiond listening", "addr", ln.Addr().String(), "session", cfg.SessionID, "fake", s.opt.Fake,
		"proxy_auth", cfg.ProxySecret != "")

	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		s.eval.Run(runCtx)
	}()
	// The core stops first at shutdown, so the editor state saved before
	// Emacs stops (BeforeStop) never interleaves with an op.
	coreCtx, stopCore := context.WithCancel(runCtx)
	coreDone := make(chan struct{})
	go func() {
		defer close(coreDone)
		s.core.Run(coreCtx)
	}()

	// 3 and 4. Emacs, tmux, terminal frame.
	if err := s.sup.Start(runCtx); err != nil {
		s.log.Error("emacs startup", "err", err)
	}

	// 6. Progress, then the saved step. snapshot.json autosaves from here
	// on: the resume op reads it first.
	outboxDone := s.startProgress(runCtx)
	s.resume(runCtx)
	s.bg.Add(1)
	go s.autosaveLoop(runCtx)
	if s.gw != nil {
		s.bg.Add(1)
		go s.heartbeatLoop(runCtx)
	}
	if s.opt.Ready != nil {
		close(s.opt.Ready)
	}

	select {
	case <-ctx.Done():
	case err := <-httpErr:
		s.log.Error("http server", "err", err)
	}
	stopCore()
	<-coreDone
	s.shutdown(srv)
	cancel()
	s.bg.Wait()
	// The outbox closes last: the evaluator and the core stopped with
	// runCtx and the HTTP handlers in shutdown, so nothing records any
	// more; it delivers for up to 5 s, and what is left stays in
	// state.json.
	s.outbox.Close(5 * time.Second)
	<-outboxDone
	return nil
}

// wire builds the components; the progress outbox starts from saved.
func (s *Server) wire(saved progress.Snapshot) {
	cfg := s.cfg
	log := s.log
	var send func(context.Context, protocol.ProgressReport) error
	if cfg.GatewayURL != "" {
		s.gw = gwclient.New(cfg.GatewayURL, cfg.SessionToken, cfg.SessionID, log.With("component", "gateway"))
		send = s.gw.Progress
	}
	s.outbox = progress.New(saved, send, s.persist, log.With("component", "progress"))
	s.outbox.MaxBackoff = s.opt.ProgressBackoff
	s.tap = inputtap.NewTap(func(ev *protocol.PrefixEvent) { s.bus.Publish(ev) })

	cmds := supervisor.RealCommands(cfg)
	var tmux supervisor.Tmux = supervisor.RealTmux(cfg)
	attach := supervisor.AttachCommand(cfg)
	supOpt := supervisor.Options{}
	if s.opt.Fake {
		s.editor = s.opt.Editor
		if s.editor == nil {
			s.editor = fakeeditor.New(cfg.Workspace)
		}
		s.editor.ModulesPath = s.modules.Path
		s.editor.EmacsVersion = "fake"
		cmds = supervisor.FakeCommands(s.opt.Bash)
		tmux = fakeeditor.FakeTmux{Editor: s.editor}
		attach = supervisor.FakeAttach(s.opt.Bash, cfg.Workspace)
		supOpt.OnDaemonStart = s.startFakeAgent
		supOpt.OnDaemonExit = s.stopFakeAgent
		// No agent reports the fake GUI frame; do not wait long for it.
		supOpt.GUIStartTimeout = 300 * time.Millisecond
	}
	s.vncH = &vnc.Handler{Addr: fmt.Sprintf("127.0.0.1:%d", cfg.VNCPort), Tap: s.tap, Track: s.track, Done: s.done, Log: log}
	supOpt.Commands = cmds
	supOpt.Tmux = tmux
	supOpt.Agent = s.agent
	supOpt.Emit = func(ev protocol.Event) { s.bus.Publish(ev) }
	supOpt.Log = log.With("component", "supervisor")
	supOpt.GUIEnabled = cfg.GUIEnabled
	if !s.opt.Fake {
		supOpt.VNCAddr = s.vncH.Addr
		supOpt.BusySignal = syscall.SIGUSR2
	}
	supOpt.Hooks = supervisor.Hooks{BeforeStop: s.snap.BeforeStop,
		AfterRestart: func(ctx context.Context, crashed bool) { s.core.AfterRestart(ctx, crashed) }}
	supOpt.HelloTimeout = s.opt.HelloTimeout
	supOpt.OnModeChange = func(m protocol.FrameKind) {
		if m == protocol.FrameTerminal {
			s.vncH.CloseAll()
		}
	}
	s.sup = supervisor.New(supOpt)
	s.vncH.Active = func() bool { return s.sup.Mode() == protocol.FrameGUI }

	cols, rows := 120, 36
	fmt.Sscanf(cfg.TermSize, "%dx%d", &cols, &rows)
	s.ptyH = &pty.Handler{
		Spawn: attach, Tap: s.tap, DefaultCols: cols, DefaultRows: rows, Track: s.track, Done: s.done, Log: log,
		// The page that plays a "Watch it" sends its keys while its
		// terminal is attached; when it goes away the demo is over.
		OnDetach: func() { s.core.AbandonDemo() },
		Ready: func() error {
			if ok, reason := s.sup.Ready(); !ok {
				return errors.New(reason)
			}
			return nil
		},
	}

	s.eval = steps.New(steps.Options{
		Root:      s.preview.Root(),
		Workspace: cfg.Workspace,
		Checker:   checker{s.agent},
		Shell:     s.shell(),
		Now:       s.opt.Clock,
		Debounce:  s.opt.Debounce,
		Log:       log.With("component", "steps"),
		Observe:   func(o steps.Observation) { s.core.Observe(o) },
	})
	var fetch scenario.Fetcher = noGateway{}
	var generations func(context.Context) (protocol.ProgressGenerations, error)
	if s.gw != nil {
		fetch, generations = s.gw, s.gw.ProgressGenerations
	}
	s.core = session.New(session.Deps{
		Workspace: cfg.Workspace, Root: s.preview.Root(), CacheDir: filepath.Join(s.playgroundDir(), "cache"),
		Fetch: fetch, Agent: s.agent, Sup: s.sup, Snapshots: s.snap, Modules: s.modules, Evaluator: s.eval,
		Progress: s.outbox, Generations: generations, Persist: s.persist,
		Git: checkpoint.ExecRunner{Bin: cfg.GitBin}, Shell: s.shell(),
		Emit: func(ev protocol.Event) { s.bus.Publish(ev) },
		Log:  log.With("component", "session"),
		Now:  s.opt.Clock,
	})
	s.eventsH = &events.Handler{Bus: s.bus, Hello: s.hello, Track: s.track, Done: s.done, Log: log}
}

// checker adapts the agent's check op for the evaluator.
type checker struct{ a *agent.Server }

func (c checker) Check(ctx context.Context, forms []string, timeout time.Duration) ([]protocol.Truth, error) {
	var res protocol.AgentCheckResult
	if err := c.a.Call(ctx, protocol.AgentOpCheck, protocol.AgentCheckArgs{Forms: forms}, timeout, &res); err != nil {
		return nil, err
	}
	return res.Results, nil
}

// noGateway is the fetcher when PG_GATEWAY_URL is empty.
type noGateway struct{}

var errNoGateway = errors.New("no gateway configured (PG_GATEWAY_URL)")

func (noGateway) Scenario(context.Context, string, string) ([]byte, string, bool, error) {
	return nil, "", false, errNoGateway
}

func (noGateway) Fixtures(context.Context, string, string) (gwclient.FixtureResult, error) {
	return gwclient.FixtureResult{}, errNoGateway
}

// Handler returns the HTTP mux (backend.md §3, api.md §3).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+protocol.PathHealthz, s.healthz)
	mux.Handle("GET "+protocol.PathStatus, httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		httpx.WriteJSON(w, http.StatusOK, s.status())
		return nil
	}))
	mux.Handle("GET "+protocol.PathPty, s.ptyH)
	mux.Handle("GET "+protocol.PathEvents, s.eventsH)
	mux.Handle("GET "+protocol.PathVNC, s.vncH)
	mux.Handle("GET "+protocol.PathPreview+"{path...}", s.preview)
	mux.Handle("HEAD "+protocol.PathPreview+"{path...}", s.preview)
	mux.Handle("POST "+protocol.PathRPC+"{op}", httpx.Handle(s.rpc))
	return httpx.Chain(mux, httpx.RequestID, httpx.Logging(s.log), httpx.Recover, ProxyAuth(s.cfg.ProxySecret))
}

// ProxyAuth requires the session's proxy secret (PG_PROXY_SECRET) in the
// X-Playground-Proxy header of every request except GET /healthz, compared
// in constant time; others get 401. In server mode the gateway sets it on
// everything it sends (proxy, RPC, status), and session containers can
// reach each other's :7000 only on a shared network: without the check a
// learner could drive another learner's Emacs (deployment.md §9.1). An
// empty secret disables the check (local mode, development).
func ProxyAuth(secret string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		if secret == "" {
			return next
		}
		want := []byte(secret)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == protocol.PathHealthz && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
				next.ServeHTTP(w, r)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get(protocol.HeaderProxySecret)), want) != 1 {
				httpx.WriteError(w, r, httpx.Unauthenticated("this session only answers its gateway"))
				return
			}
			r.Header.Del(protocol.HeaderProxySecret)
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if s.sup == nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, protocol.HealthStatus{Reason: "starting"})
		return
	}
	ok, reason := s.sup.Ready()
	if !ok {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, protocol.HealthStatus{Reason: reason})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, protocol.HealthStatus{OK: true})
}

func (s *Server) hello(replayed bool) *protocol.HelloEvent {
	st := s.sup.Status()
	return &protocol.HelloEvent{
		Protocol:  protocol.Version,
		SessionID: s.cfg.SessionID,
		Mode:      s.sup.Mode(),
		Emacs:     protocol.EmacsInfo{Version: st.Version, UptimeMs: st.UptimeMs},
		Frames:    s.sup.Frames(),
		Step:      s.core.Active(),
		Modules:   s.modules.Current(),
		Replayed:  replayed,
		Lap:       s.core.Lap(),
		Busy:      s.busy(),
	}
}

// busy describes the lesson op in progress for status and hello.
func (s *Server) busy() *protocol.BusyInfo {
	op := s.core.Busy()
	if op == nil {
		return nil
	}
	return &protocol.BusyInfo{Op: string(op.Kind), LessonID: op.LessonID, StepID: op.StepID, StartedAt: op.StartedAt.UnixMilli()}
}

func (s *Server) status() protocol.StatusResult {
	return protocol.StatusResult{
		SessionID:        s.cfg.SessionID,
		Emacs:            s.sup.Status(),
		Frames:           s.sup.Frames(),
		Mode:             s.sup.Mode(),
		Step:             s.core.Active(),
		Modules:          s.modules.Current(),
		Versions:         readVersions(s.cfg.VersionsFile),
		ActiveWebSockets: int(s.wsCount.Load()),
		Lap:              s.core.Lap(),
		RemoteBox:        s.boxReady.Load(),
		Busy:             s.busy(),
	}
}

// readVersions parses versions.env (KEY=VALUE lines).
func readVersions(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out
}

// --- agent.Handler -------------------------------------------------------

// OnHello implements agent.Handler. A restore made while the agent was
// away gets its editor state from the core's reconnect op, unless the
// hello is a crashed daemon's restart, whose recovery covers it.
func (s *Server) OnHello(h *protocol.AgentHelloMsg) {
	if !s.sup.OnHello(h) {
		s.core.Reconnected()
	}
	for mod, msg := range h.ModuleErrors {
		s.bus.Publish(&protocol.ErrorEvent{Code: protocol.ErrInternal, Message: fmt.Sprintf("The %s module failed to load: %s", mod, msg)})
	}
}

// OnEvent implements agent.Handler.
func (s *Server) OnEvent(ev protocol.Event) {
	switch e := ev.(type) {
	case *protocol.KeyEvent:
		s.tap.Reset()
		s.sup.OnKey(e)
		s.bus.Publish(e)
		s.eval.OnKey(e)
	case *protocol.State:
		s.bus.Publish(e)
		s.eval.OnState(e)
	case *protocol.MinibufferEvent:
		s.bus.Publish(e)
		s.eval.OnMinibuffer(e)
	default:
		s.bus.Publish(ev)
	}
}

// OnFrame implements agent.Handler.
func (s *Server) OnFrame(f *protocol.AgentFrameMsg) { s.sup.OnFrame(f) }

// OnDisconnect implements agent.Handler.
func (s *Server) OnDisconnect(error) {}

func (s *Server) onFile(ev *protocol.FileEvent) {
	s.bus.Publish(ev)
	s.eval.OnFile(ev)
}

// --- checkpoints and state -----------------------------------------------

// stateFile is /work/.playground/state.json: the session core's saved
// state, and the progress outbox's generations and entries. Version 1
// had no outbox; its outcomes reach the gateway by the reconciliation at
// every start.
type stateFile struct {
	Version     int                             `json:"version"`
	Generations protocol.ProgressGenerations    `json:"generations,omitempty"`
	Active      *protocol.StepRef               `json:"active,omitempty"`
	Lessons     map[string]*session.LessonState `json:"lessons,omitempty"`
	Lap         *protocol.LapInfo               `json:"lap,omitempty"`
	Outbox      []protocol.ProgressReport       `json:"outbox,omitempty"`
}

// stateVersion is the version persist writes.
const stateVersion = 2

func (f stateFile) saved() session.Saved {
	return session.Saved{Active: f.Active, Lessons: f.Lessons, Lap: f.Lap}
}

func (f stateFile) outbox() progress.Snapshot {
	return progress.Snapshot{Generations: f.Generations, Entries: f.Outbox}
}

func (s *Server) statePath() string { return filepath.Join(s.playgroundDir(), "state.json") }

// persist writes state.json from the core's saved state and the outbox,
// read together (Core.Persistent) under stateMu, so a later write never
// holds older state than an earlier one. The core and the outbox call it
// after every change; the file is written outside the core's lock.
func (s *Server) persist() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	saved, ob := s.core.Persistent()
	st := stateFile{Version: stateVersion, Generations: ob.Generations, Active: saved.Active, Lessons: saved.Lessons,
		Lap: saved.Lap, Outbox: ob.Entries}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	p := s.statePath()
	err = os.MkdirAll(filepath.Dir(p), 0o755)
	if err == nil {
		err = os.WriteFile(p+".tmp", b, 0o644)
	}
	if err == nil {
		err = os.Rename(p+".tmp", p)
	}
	if err != nil {
		s.log.Warn("state.json", "err", err)
	}
}

// readState reads state.json, version 1 or 2 (empty when missing or
// unreadable).
func (s *Server) readState() stateFile {
	b, err := os.ReadFile(s.statePath())
	if err != nil {
		return stateFile{}
	}
	var st stateFile
	if err := json.Unmarshal(b, &st); err != nil {
		s.log.Warn("state.json is unreadable; starting fresh", "err", err)
		return stateFile{}
	}
	if st.Version > stateVersion {
		s.log.Warn("state.json is from a newer sessiond; reading what this one knows", "version", st.Version)
	}
	return st
}

// syncProgress applies gateway resets missed while this session was
// unavailable and adopts the gateway's generations (the core's sync op),
// best effort.
func (s *Server) syncProgress(ctx context.Context, reconcile bool) {
	if err := s.core.Sync(ctx, reconcile); err != nil {
		s.log.Warn("progress generations", "err", err)
	}
}

// startProgress syncs the generations, queues every saved outcome, writes
// state.json (version 2 from here on) and starts the outbox's sender,
// which outlives runCtx until Close. The returned channel closes when the
// sender stopped.
func (s *Server) startProgress(runCtx context.Context) <-chan struct{} {
	s.syncProgress(runCtx, true)
	done := make(chan struct{})
	if s.gw == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		s.outbox.Run(context.Background())
	}()
	return done
}

// resume queues the resume of the saved active step (the core's resume
// op).
func (s *Server) resume(ctx context.Context) {
	saved := s.core.Saved()
	if saved.Active == nil {
		return
	}
	ref := *saved.Active
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		if err := s.core.Resume(ctx, ref); err != nil {
			s.log.Warn("resuming the saved step", "lesson", ref.LessonID, "step", ref.StepID, "err", err)
		}
	}()
}

// --- workspace -------------------------------------------------------------

// initWorkspace is backend.md §3.2 step 2.
func (s *Server) initWorkspace(ctx context.Context) error {
	var errs []error
	if err := s.snap.Init(ctx); err != nil {
		errs = append(errs, err)
	}
	initDir := supervisor.InitDirectory(s.cfg)
	if _, err := os.Stat(initDir); errors.Is(err, fs.ErrNotExist) {
		if st, perr := os.Stat(s.cfg.PristineConfig); perr == nil && st.IsDir() {
			if err := copyTree(s.cfg.PristineConfig, initDir); err != nil {
				errs = append(errs, fmt.Errorf("copying the pristine config: %w", err))
			} else {
				s.log.Info("copied the pristine Emacs configuration", "from", s.cfg.PristineConfig, "to", initDir)
			}
		} else if err := os.MkdirAll(initDir, 0o755); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := os.Stat(s.modules.Path); errors.Is(err, fs.ErrNotExist) {
		if err := s.modules.Write(scenario.DefaultModules); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.setupBox(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// shell runs shell conditions and setup.run as the learner, without
// sessiond's secrets and without git's optional locks.
func (s *Server) shell() steps.ExecShell {
	env := []string{"PG_SESSION_TOKEN=", "PG_PROXY_SECRET=", "PG_BOX_KEY="}
	env = append(env, checkpoint.SafeDirectoryEnv...)
	return steps.ExecShell{Env: append(env, checkpoint.NoOptionalLocksEnv...)}
}

// setupBox writes the remote box's key pair and the "box" alias
// (deployment.md §6). Fake mode keeps the developer's ~/.ssh untouched.
func (s *Server) setupBox() error {
	if s.cfg.BoxHost == "" {
		return nil
	}
	sshDir := s.cfg.SSHDir
	if sshDir == "" && !s.opt.Fake {
		if home, err := os.UserHomeDir(); err == nil {
			sshDir = filepath.Join(home, ".ssh")
		}
	}
	res, err := sshbox.Setup(sshbox.Config{
		KeyDir: filepath.Join(s.playgroundDir(), "ssh"), SSHDir: sshDir,
		Host: s.cfg.BoxHost, Port: s.cfg.BoxPort, User: s.cfg.BoxUser,
		PrivateKey: s.cfg.BoxKey, Comment: "playground-" + s.cfg.SessionID,
	})
	if err != nil {
		return fmt.Errorf("remote box key: %w", err)
	}
	if !res.ConfigWritten && sshDir != "" {
		s.log.Warn("~/.ssh/config is the learner's own; the box alias was not written", "dir", sshDir)
	}
	s.boxReady.Store(res.ConfigWritten)
	s.log.Info("remote box alias ready", "host", s.cfg.BoxHost, "generated", res.Generated, "config", res.ConfigWritten)
	return nil
}

// copyTree copies a directory tree (files, directories, symlinks),
// preserving permission bits and making everything owner-writable.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm()|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, in); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
		return nil
	})
}

// --- background loops ------------------------------------------------------

// autosaveLoop asks the core for an autosave of snapshot.json every
// snapshot.AutosaveInterval (skipped while an op runs or waits).
func (s *Server) autosaveLoop(ctx context.Context) {
	defer s.bg.Done()
	t := time.NewTicker(snapshot.AutosaveInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.core.Autosave()
		}
	}
}

func (s *Server) heartbeatLoop(ctx context.Context) {
	defer s.bg.Done()
	t := time.NewTicker(HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if err := s.gw.Heartbeat(hctx, protocol.Heartbeat{ActiveWebSockets: int(s.wsCount.Load())}); err != nil {
				s.log.Debug("gateway heartbeat", "err", err)
			}
			cancel()
		}
	}
}

// --- fake mode -------------------------------------------------------------

// startFakeAgent connects the fake editor, freshly started, as the agent
// of the daemon pid; it reconnects whenever the connection drops and the
// editor is online, like the agent's backoff.
func (s *Server) startFakeAgent(pid int) {
	ctx, cancel := context.WithCancel(context.Background())
	ed := s.editor
	ed.Restart()
	fake := &agent.Fake{Path: s.cfg.AgentSocket, Handle: ed.Handle, Hello: ed.Hello(pid),
		Initial: []any{protocol.AgentFrameMsg{T: protocol.AgentFrame, Event: protocol.FrameOpened, Kind: protocol.FrameTerminal}}}
	ed.Attach(fake)
	s.fakeMu.Lock()
	if s.fakeCancel != nil {
		s.fakeCancel()
	}
	s.fakeCancel = cancel
	s.fakeMu.Unlock()
	go func() {
		for ed.WaitOnline(ctx) {
			if err := fake.Run(ctx); ctx.Err() != nil {
				return
			} else if err != nil {
				s.log.Debug("fake agent connection", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}

func (s *Server) stopFakeAgent() {
	if s.editor != nil {
		s.editor.Detach()
	}
	s.fakeMu.Lock()
	if s.fakeCancel != nil {
		s.fakeCancel()
		s.fakeCancel = nil
	}
	s.fakeMu.Unlock()
}

// --- shutdown ----------------------------------------------------------------

// shutdown is backend.md §5: emit stopping, snapshot.json and the modified
// buffers saved (BeforeStop), stop Emacs (SIGTERM, 15 s), close WebSockets
// with 4003, stop HTTP.
func (s *Server) shutdown(srv *http.Server) {
	s.log.Info("sessiond stopping")
	s.bus.Publish(&protocol.EmacsEvent{Event: protocol.EmacsStopping})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	s.sup.BeforeStop(ctx)
	cancel()
	close(s.done)
	s.sup.Stop()
	hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(hctx)
	hcancel()
	s.stopFakeAgent()
}
