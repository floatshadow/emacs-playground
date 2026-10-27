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
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/events"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/inputtap"
	"github.com/floatshadow/emacs-playground/internal/sessiond/preview"
	"github.com/floatshadow/emacs-playground/internal/sessiond/pty"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
	"github.com/floatshadow/emacs-playground/internal/sessiond/supervisor"
	"github.com/floatshadow/emacs-playground/internal/sessiond/vnc"
)

// HeartbeatInterval is how often sessiond tells the gateway its WebSocket
// count (api.md §2.6).
const HeartbeatInterval = 30 * time.Second

// Options adjust a Server for tests and development.
type Options struct {
	// Fake substitutes bash for Emacs, tmux and Xvnc and runs an in-process
	// fake agent.
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
	// FakeAgent answers the fake agent's calls in Fake mode (tests); nil
	// means agent.DefaultFakeHandler.
	FakeAgent agent.FakeHandler
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
	loader   *scenario.Loader
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
	done    chan struct{}
	stateMu sync.Mutex
	// skipMu serialises rpc skip.
	skipMu sync.Mutex
	bg     sync.WaitGroup
	addr   atomic.Value // string

	fakeMu     sync.Mutex
	fakeCancel context.CancelFunc
	fake       *agent.Fake // fake mode's agent, for tests
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
		s.engine.Run(runCtx)
	}()

	// The snapshot of the last run stays until the saved step is resumed.
	saved, resume := s.readState()
	var release func()
	if resume {
		release = s.sup.HoldSnapshots()
	}

	// 3 and 4. Emacs, tmux, terminal frame.
	if err := s.sup.Start(runCtx); err != nil {
		s.log.Error("emacs startup", "err", err)
	}

	// 6. Resume the saved step.
	s.restoreState(runCtx, saved, release)
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
		s.reporter = gwclient.NewReporter(s.gw)
	}
	s.tap = inputtap.NewTap(func(ev *protocol.PrefixEvent) { s.bus.Publish(ev) })

	cmds := supervisor.RealCommands(cfg)
	var tmux supervisor.Tmux = supervisor.RealTmux(cfg)
	attach := supervisor.AttachCommand(cfg)
	supOpt := supervisor.Options{}
	if s.opt.Fake {
		cmds = supervisor.FakeCommands(s.opt.Bash)
		tmux = supervisor.NoTmux{}
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
		Shell:     steps.ExecShell{Env: append([]string{"PG_SESSION_TOKEN=", "PG_PROXY_SECRET="}, checkpoint.SafeDirectoryEnv...)},
		Log:       log.With("component", "steps"),
		Hooks: steps.Hooks{
			Emit: func(ev protocol.Event) { s.bus.Publish(ev) },
			Report: func(r protocol.ProgressReport) {
				if s.reporter != nil {
					s.reporter.Report(r)
				}
			},
			Checkpoint: func(ctx context.Context, lessonID, stepID string) {
				if err := s.stepCheckpoint(ctx, lessonID, stepID, true); err != nil {
					s.log.Warn("step checkpoint", "err", err)
				}
			},
			Persist: s.persist,
		},
	})
	var fetch scenario.Fetcher
	if s.gw != nil {
		fetch = s.gw
	} else {
		fetch = noGateway{}
	}
	s.loader = &scenario.Loader{
		Workspace: cfg.Workspace, Root: s.preview.Root(), CacheDir: filepath.Join(s.playgroundDir(), "cache"),
		Fetch: fetch, Agent: s.agent, Restart: s.sup, Modules: s.modules, Engine: s.engine,
		Checkpoint:  s.stepCheckpoint,
		RestoreStep: s.restoreStep,
		PlayDemo:    s.playDemo,
		Ready:       s.sup.WaitReady,
		Snapshot:    s.sup.LastSnapshot,
		Emit:        func(ev protocol.Event) { s.bus.Publish(ev) },
		Log:         log.With("component", "scenario"),
	}
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
	}
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

// stepCheckpoint takes refs/playground/<lesson>/<step>; unless force, an
// existing one is kept.
func (s *Server) stepCheckpoint(ctx context.Context, lessonID, stepID string, force bool) error {
	ref := checkpoint.StepRef(lessonID, stepID)
	if !force && s.ckpt.Exists(ctx, ref) {
		return nil
	}
	_, err := s.takeCheckpoint(ctx, checkpoint.Meta{Ref: ref, Label: lessonID + "/" + stepID,
		Step: &protocol.StepRef{LessonID: lessonID, StepID: stepID}})
	return err
}

// takeCheckpoint records the workspace files under meta.Ref together with
// the windows, the file buffers' unsaved state and the session variables
// (each captured within 5 s, best effort), and announces the checkpoint.
func (s *Server) takeCheckpoint(ctx context.Context, meta checkpoint.Meta) (checkpoint.Meta, error) {
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	meta.Spec, _ = s.sup.Snapshot(sctx)
	meta.Buffers, meta.Globals = s.bufferStates(sctx), s.globals(sctx)
	cancel()
	meta, err := s.ckpt.TakeWith(ctx, meta)
	if err != nil {
		return meta, err
	}
	s.bus.Publish(&protocol.CheckpointEvent{Label: meta.Label, Ref: meta.Ref, CreatedAt: meta.CreatedAt})
	return meta, nil
}

// stateFile is /work/.playground/state.json.
type stateFile struct {
	Version int `json:"version"`
	steps.Saved
}

func (s *Server) statePath() string { return filepath.Join(s.playgroundDir(), "state.json") }

func (s *Server) persist(saved steps.Saved) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
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
	}
}

// readState reads state.json; resume is true when it has an active step.
func (s *Server) readState() (saved steps.Saved, resume bool) {
	b, err := os.ReadFile(s.statePath())
	if err != nil {
		return saved, false
	}
	var st stateFile
	if err := json.Unmarshal(b, &st); err != nil {
		s.log.Warn("state.json is unreadable; starting fresh", "err", err)
		return saved, false
	}
	return st.Saved, st.Active != nil
}

// restoreState loads the saved progress and resumes the active step in the
// background (Loader.Resume), then calls release (when set).
func (s *Server) restoreState(ctx context.Context, saved steps.Saved, release func()) {
	s.engine.Restore(saved)
	if saved.Active == nil {
		if release != nil {
			release()
		}
		return
	}
	ref := *saved.Active
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		if release != nil {
			defer release()
		}
		rctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		if err := s.loader.Resume(rctx, ref); err != nil {
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
	// TODO(sessiond, deployment.md §6): generate the per-session SSH key pair
	// under /work/.playground/ssh and write the "box" Host alias to
	// ~/.ssh/config once the remote box exists (milestone M6).
	return errors.Join(errs...)
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

func (s *Server) startFakeAgent(pid int) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &agent.Fake{Path: s.cfg.AgentSocket, Handle: s.opt.FakeAgent,
		Hello:   protocol.AgentHelloMsg{Emacs: "fake", PID: pid, Modules: s.modules.Current()},
		Initial: []any{protocol.AgentFrameMsg{T: protocol.AgentFrame, Event: protocol.FrameOpened, Kind: protocol.FrameTerminal}}}
	s.fakeMu.Lock()
	if s.fakeCancel != nil {
		s.fakeCancel()
	}
	s.fakeCancel = cancel
	s.fake = fake
	s.fakeMu.Unlock()
	go func() {
		// The socket may not accept for a moment after a restart.
		for i := 0; i < 50 && ctx.Err() == nil; i++ {
			if err := fake.Run(ctx); err == nil || ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

func (s *Server) stopFakeAgent() {
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
	s.loader.Wait()
	hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(hctx)
	hcancel()
	s.stopFakeAgent()
}
