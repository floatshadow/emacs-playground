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
	"github.com/floatshadow/emacs-playground/internal/sessiond/pty"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/session"
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
	// Clock replaces time.Now in the step engine (tests).
	Clock func() time.Time
	// Debounce replaces the step engine's evaluation debounce, and three
	// times it the longest wait (tests).
	Debounce time.Duration
}

// Server is one sessiond.
type Server struct {
	cfg *protocol.SessiondConfig
	opt Options
	log *slog.Logger

	bus      *events.Bus
	evlog    *events.LogWriter
	agent    *agent.Server
	sup      *supervisor.Supervisor
	tap      *inputtap.Tap
	engine   *steps.Engine
	core     *session.Core
	snaps    *session.Checkpoints
	modules  *scenario.Modules
	ckpt     *checkpoint.Manager
	preview  *preview.Server
	watcher  *preview.Watcher
	gw       *gwclient.Client
	reporter *gwclient.Reporter
	ptyH     *pty.Handler
	vncH     *vnc.Handler
	eventsH  *events.Handler

	wsCount atomic.Int64
	// boxReady: the box alias is set up (sshbox.Setup succeeded).
	boxReady      atomic.Bool
	done          chan struct{}
	stateMu       sync.Mutex
	stateRevision uint64
	// progressMu serialises gateway snapshots with incoming resets.
	progressMu sync.Mutex
	// skipMu serialises rpc skip.
	skipMu sync.Mutex
	bg     sync.WaitGroup
	addr   atomic.Value // string

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
	s.ckpt = checkpoint.New(cfg.Workspace, cfg.GitBin)
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

	s.wire()

	s.agent.SetHandler(s)
	if w, err := preview.NewWatcher(cfg.Workspace, s.onFile, s.log.With("component", "watcher")); err != nil {
		s.log.Warn("file watcher unavailable", "err", err)
	} else {
		s.watcher = w
		w.OnGit = s.engine.Invalidate
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

	s.bg.Add(2)
	go func() {
		defer s.bg.Done()
		s.engine.Run(runCtx)
	}()
	go func() {
		defer s.bg.Done()
		s.core.Run(runCtx)
	}()

	saved := s.readState()

	// 3 and 4. Emacs, tmux, terminal frame.
	if err := s.sup.Start(runCtx); err != nil {
		s.log.Error("emacs startup", "err", err)
	}

	// 6. Resume the saved step.
	s.restoreState(runCtx, saved)
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
	s.shutdown(srv)
	cancel()
	s.bg.Wait()
	// The reporter closes last: the engine and the resume stopped with
	// runCtx and the HTTP handlers in shutdown, so nothing reports any more.
	if s.reporter != nil {
		s.reporter.Close(5 * time.Second)
	}
	return nil
}

// wire builds the components.
func (s *Server) wire() {
	cfg := s.cfg
	log := s.log
	if cfg.GatewayURL != "" {
		s.gw = gwclient.New(cfg.GatewayURL, cfg.SessionToken, cfg.SessionID, log.With("component", "gateway"))
	}
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
	supOpt.SnapshotFile = filepath.Join(s.playgroundDir(), "snapshot.json")
	supOpt.Busy = func() bool { return s.core.Busy() != nil }
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
		OnDetach: func() { s.engine.AbandonDemo() },
		Ready: func() error {
			if ok, reason := s.sup.Ready(); !ok {
				return errors.New(reason)
			}
			return nil
		},
	}

	s.engine = steps.New(steps.Options{
		Root:      s.preview.Root(),
		Workspace: cfg.Workspace,
		Checker:   checker{s.agent},
		Shell:     s.shell(),
		Now:       s.opt.Clock,
		Debounce:  s.opt.Debounce,
		Log:       log.With("component", "steps"),
		Hooks: steps.Hooks{
			Emit: func(ev protocol.Event) { s.bus.Publish(ev) },
			Report: func(r protocol.ProgressReport) {
				if s.reporter != nil {
					s.reporter.Report(r)
				}
			},
			Checkpoint: func(ctx context.Context, lessonID, stepID string) {
				if err := session.CaptureStep(ctx, s.snaps, lessonID, stepID, true); err != nil {
					s.log.Warn("step checkpoint", "err", err)
				}
			},
			Persist: s.persist,
		},
	})
	if s.gw != nil {
		s.reporter = gwclient.NewReporter(s.gw, s.engine.ProgressReports)
	}
	var fetch scenario.Fetcher
	if s.gw != nil {
		fetch = s.gw
	} else {
		fetch = noGateway{}
	}
	s.snaps = &session.Checkpoints{
		Workspace: cfg.Workspace, Ckpt: s.ckpt, Agent: s.agent, Editor: s.sup, Modules: s.modules,
		Emit: func(ev protocol.Event) { s.bus.Publish(ev) },
		Log:  log.With("component", "checkpoints"),
	}
	s.core = session.New(session.Deps{
		Workspace: cfg.Workspace, Root: s.preview.Root(), CacheDir: filepath.Join(s.playgroundDir(), "cache"),
		Fetch: fetch, Agent: s.agent, Sup: s.sup, Snapshots: s.snaps, Modules: s.modules, Engine: s.engine,
		DropProgress: s.dropProgress,
		Git:          checkpoint.ExecRunner{Bin: cfg.GitBin}, Shell: s.shell(),
		Emit: func(ev protocol.Event) { s.bus.Publish(ev) },
		Log:  log.With("component", "session"),
	})
	s.eventsH = &events.Handler{Bus: s.bus, Hello: s.hello, Track: s.track, Done: s.done, Log: log}
}

// checker adapts the agent's check op for the step engine.
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
		Step:      s.engine.Active(),
		Modules:   s.modules.Current(),
		Replayed:  replayed,
		Lap:       s.engine.Lap(),
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
		Step:             s.engine.Active(),
		Modules:          s.modules.Current(),
		Versions:         readVersions(s.cfg.VersionsFile),
		ActiveWebSockets: int(s.wsCount.Load()),
		Lap:              s.engine.Lap(),
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

// OnHello implements agent.Handler.
func (s *Server) OnHello(h *protocol.AgentHelloMsg) {
	s.sup.OnHello(h)
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
		s.engine.OnKey(e)
	case *protocol.State:
		s.bus.Publish(e)
		s.engine.OnState(e)
	case *protocol.MinibufferEvent:
		s.bus.Publish(e)
		s.engine.OnMinibuffer(e)
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
	s.engine.OnFile(ev)
}

// --- checkpoints and state -----------------------------------------------

// stateFile is /work/.playground/state.json.
type stateFile struct {
	Version int `json:"version"`
	steps.Saved
}

func (s *Server) statePath() string { return filepath.Join(s.playgroundDir(), "state.json") }

func (s *Server) persist(saved steps.Saved) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if saved.Revision < s.stateRevision {
		return
	}
	b, err := json.MarshalIndent(stateFile{Version: 1, Saved: saved}, "", "  ")
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
	} else {
		s.stateRevision = saved.Revision
	}
}

// readState reads state.json (empty when missing or unreadable).
func (s *Server) readState() steps.Saved {
	b, err := os.ReadFile(s.statePath())
	if err != nil {
		return steps.Saved{}
	}
	var st stateFile
	if err := json.Unmarshal(b, &st); err != nil {
		s.log.Warn("state.json is unreadable; starting fresh", "err", err)
		return steps.Saved{}
	}
	return st.Saved
}

// syncProgress applies gateway resets missed while this session was unavailable.
func (s *Server) syncProgress(ctx context.Context) {
	if s.gw == nil {
		return
	}
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	generations, err := s.gw.ProgressGenerations(ctx)
	if err != nil {
		s.log.Warn("progress generations", "err", err)
		return
	}
	if s.reporter != nil {
		s.reporter.SyncGenerations(generations)
	}
	s.engine.SyncGenerations(generations)
}

// dropProgress applies a gateway reset (the core's drop op): queued
// reports and saved progress of the scope, under progressMu.
func (s *Server) dropProgress(lessonID string, generation int64) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.reporter != nil {
		s.reporter.Purge(lessonID, generation)
	}
	s.engine.DropGeneration(lessonID, generation)
}

// restoreState loads the saved progress and queues the resume of the
// active step (the core's resume op).
func (s *Server) restoreState(ctx context.Context, saved steps.Saved) {
	s.engine.Restore(saved)
	s.syncProgress(ctx)
	saved = s.engine.Saved()
	if s.reporter != nil {
		s.reporter.Reconcile()
	}
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
	if err := s.ckpt.Init(ctx); err != nil {
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

// shutdown is backend.md §5: emit stopping, snapshot, flush, stop Emacs
// (SIGTERM, 15 s), close WebSockets with 4003, stop HTTP.
func (s *Server) shutdown(srv *http.Server) {
	s.log.Info("sessiond stopping")
	s.bus.Publish(&protocol.EmacsEvent{Event: protocol.EmacsStopping})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := s.sup.Snapshot(ctx); err != nil {
		s.log.Info("no snapshot at shutdown", "err", err)
	}
	s.sup.Flush(ctx)
	cancel()
	close(s.done)
	s.sup.Stop()
	hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(hctx)
	hcancel()
	s.stopFakeAgent()
}
