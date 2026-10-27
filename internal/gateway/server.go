package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/floatshadow/emacs-playground/internal/buildinfo"
	"github.com/floatshadow/emacs-playground/internal/config"
	"github.com/floatshadow/emacs-playground/internal/docker"
	"github.com/floatshadow/emacs-playground/internal/gateway/auth"
	"github.com/floatshadow/emacs-playground/internal/gateway/curriculum"
	"github.com/floatshadow/emacs-playground/internal/gateway/limit"
	"github.com/floatshadow/emacs-playground/internal/gateway/progress"
	"github.com/floatshadow/emacs-playground/internal/gateway/proxy"
	"github.com/floatshadow/emacs-playground/internal/gateway/sessions"
	"github.com/floatshadow/emacs-playground/internal/gateway/store"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Options are the parts of a gateway that are not in the configuration.
type Options struct {
	// Web is the built web bundle (web.Dist()); nil serves a placeholder.
	Web fs.FS
	// Fonts is served at /fonts/; nil answers 404.
	Fonts fs.FS
	// Logger, default slog.Default().
	Logger *slog.Logger
	// Docker overrides the Engine client in server mode (tests).
	Docker docker.Client
	// Backend overrides the session backend (tests).
	Backend sessions.Backend
	// SessionHealth overrides the sessiond health probe (tests).
	SessionHealth func(ctx context.Context, endpoint *url.URL) error
	// StaticWait overrides how long POST /sessions waits for the local
	// session (tests).
	StaticWait time.Duration
	Now        func() time.Time
}

// App is a wired gateway.
type App struct {
	cfg        *protocol.Config
	o          Options
	log        *slog.Logger
	Store      store.Store
	Auth       auth.Authenticator
	Sessions   *sessions.Service
	Curriculum *curriculum.Server
	Progress   *progress.Service
	Stuck      *progress.Stuck
	Proxy      *proxy.Proxy
	handler    http.Handler
	createLim  *limit.Limiter
	closers    []func() error
}

// Build wires a gateway from cfg.
func Build(ctx context.Context, cfg *protocol.Config, o Options) (*App, error) {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	a := &App{cfg: cfg, o: o, log: o.Logger, createLim: limit.New(10, time.Minute)}
	ok := false
	defer func() {
		if !ok {
			a.Close()
		}
	}()

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("gateway: data directory: %w", err)
	}
	var err error
	if cfg.Mode == protocol.ModeServer {
		a.Store, err = store.OpenSQLite(filepath.Join(cfg.DataDir, store.SQLiteFile))
	} else {
		a.Store, err = store.OpenJSON(cfg.DataDir)
	}
	if err != nil {
		return nil, err
	}
	a.closers = append(a.closers, a.Store.Close)

	var signer *auth.Signer
	if cfg.Auth.Mode != protocol.AuthNone {
		key, err := auth.LoadOrCreateSecret(cfg.Auth.CookieSecretFile)
		if err != nil {
			return nil, err
		}
		if signer, err = auth.NewSigner(key); err != nil {
			return nil, err
		}
	}
	if cfg.Auth.Mode == protocol.AuthInvite {
		if err := a.SyncInvites(ctx); err != nil {
			return nil, err
		}
	}
	a.Auth, err = auth.New(cfg.Auth.Mode, auth.Options{
		PublicURL: cfg.PublicURL, SessionDays: cfg.Auth.SessionDays, Signer: signer,
		Store: a.Store, Logger: o.Logger, Now: o.Now,
	})
	if err != nil {
		return nil, err
	}

	a.Curriculum, err = curriculum.Open(curriculum.Options{
		Dir: cfg.Curriculum.Dir, DefaultLocale: cfg.Curriculum.DefaultLocale,
		AutoReload: cfg.Features.AuthorMode, Logger: o.Logger,
	})
	if err != nil {
		return nil, err
	}

	if err := a.buildSessions(ctx); err != nil {
		return nil, err
	}
	a.Progress = &progress.Service{Store: a.Store, Steps: a.Curriculum, Now: o.Now}
	a.Stuck = progress.NewStuck(a.Store)
	a.Proxy = proxy.New(proxy.Options{Sessions: a.Sessions, AuthorMode: cfg.Features.AuthorMode, Logger: o.Logger})
	a.handler = a.routes()
	ok = true
	return a, nil
}

// SyncInvites loads auth.invites.file into the store (start and SIGHUP).
func (a *App) SyncInvites(ctx context.Context) error {
	if a.cfg.Auth.Mode != protocol.AuthInvite {
		return nil
	}
	invs, err := auth.LoadInvites(a.cfg.Auth.Invites.File)
	if err != nil {
		return err
	}
	if err := a.Store.Invites().Sync(ctx, invs); err != nil {
		return err
	}
	a.log.Info("auth: invites loaded", "codes", len(invs), "file", a.cfg.Auth.Invites.File)
	return nil
}

func (a *App) buildSessions(ctx context.Context) error {
	cfg := a.cfg
	mem, err := config.ParseSize(cfg.Sessions.Limits.Memory)
	if err != nil {
		return err
	}
	so := sessions.Options{
		Store:   a.Store,
		Image:   cfg.Sessions.Image,
		Limits:  sessions.Limits{CPUs: cfg.Sessions.Limits.CPUs, Memory: mem, Pids: cfg.Sessions.Limits.Pids},
		Network: cfg.Sessions.Network, Runtime: cfg.Sessions.Runtime,
		GUI:             cfg.Sessions.GUI.Enabled,
		IdleStop:        time.Duration(cfg.Sessions.IdleStopMinutes) * time.Minute,
		VolumeRetention: time.Duration(cfg.Sessions.VolumeRetentionDays) * 24 * time.Hour,
		MaxSessions:     cfg.Sessions.MaxSessions,
		StaticWait:      a.o.StaticWait,
		Health:          a.o.SessionHealth,
		Logger:          a.log,
		Now:             a.o.Now,
		Env: map[string]string{
			"PG_GATEWAY_URL":  internalGatewayURL(cfg.Listen),
			"PG_VNC_GEOMETRY": cfg.Sessions.GUI.Geometry,
			"PG_TERM_SIZE":    cfg.Sessions.Terminal.InitialSize,
			"PG_GUI_ENABLED":  strconv.FormatBool(cfg.Sessions.GUI.Enabled),
			"PG_AUTHOR_MODE":  strconv.FormatBool(cfg.Features.AuthorMode),
		},
	}
	if cfg.Sessions.RemoteBox.Enabled {
		so.RemoteImage = cfg.Sessions.RemoteBox.Image
	}
	switch {
	case a.o.Backend != nil:
		so.Backend = a.o.Backend
		so.Static = cfg.Sessions.Backend == protocol.BackendStatic
	case cfg.Sessions.Backend == protocol.BackendStatic:
		b, err := sessions.NewStaticBackend(cfg.Local.SessionID, cfg.Local.SessionURL)
		if err != nil {
			return err
		}
		so.Backend = b
		so.Static = true
	default:
		dc := a.o.Docker
		if dc == nil {
			e, err := docker.New(cfg.Sessions.DockerHost)
			if err != nil {
				return err
			}
			a.closers = append(a.closers, e.Close)
			dc = e
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = dc.Ping(pctx)
			cancel()
			if err != nil {
				a.log.Warn("sessions: the Docker Engine is not reachable yet", "host", cfg.Sessions.DockerHost, "err", err)
			}
		}
		db := &sessions.DockerBackend{Client: dc, Version: buildinfo.Version}
		if cfg.Sessions.Isolation == protocol.IsolationNetwork {
			db.Isolate = true
			db.GatewayAlias = internalGatewayHost
			db.Internal = cfg.Sessions.Egress != protocol.EgressAll
			if cfg.Sessions.SubnetPool != "" {
				db.SubnetPool, err = netip.ParsePrefix(cfg.Sessions.SubnetPool)
				if err != nil {
					return fmt.Errorf("sessions.subnetPool: %w", err)
				}
			}
			db.Gateway = cfg.Sessions.GatewayContainer
			if db.Gateway == "" {
				// Docker sets a container's hostname to its id.
				db.Gateway, _ = os.Hostname()
			}
			a.log.Info("sessions: one network per session", "gateway_container", db.Gateway,
				"subnet_pool", cfg.Sessions.SubnetPool, "internal", db.Internal)
		}
		so.Backend = db
		a.log.Warn("sessions: the docker backend is experimental in this release (slice 1)")
	}
	if so.Static {
		so.StaticID, so.StaticToken = cfg.Local.SessionID, cfg.Local.SessionToken
	} else {
		// Per-session proxy secrets (PG_PROXY_SECRET) derive from a key kept
		// with the gateway's data, apart from the cookie secret so that
		// rotating that one does not lock the gateway out of running
		// sessions.
		key, err := auth.LoadOrCreateSecret(filepath.Join(cfg.DataDir, ProxyKeyFile))
		if err != nil {
			return fmt.Errorf("sessions: proxy key: %w", err)
		}
		so.ProxyKey = key
	}
	a.Sessions, err = sessions.New(ctx, so)
	if err != nil {
		return err
	}
	if err := a.Sessions.Rebuild(ctx); err != nil {
		a.log.Warn("sessions: registry rebuild", "err", err)
	}
	return nil
}

// ProxyKeyFile, in the data directory, holds the key the per-session proxy
// secrets derive from (sessions.ProxySecret); created on first use.
const ProxyKeyFile = "session-proxy.secret"

// internalGatewayHost is the gateway's name for sessions: its Compose
// service name on the shared sessions network, and its alias on every
// per-session network.
const internalGatewayHost = "gateway"

// internalGatewayURL is PG_GATEWAY_URL for session containers: the
// gateway's name on the sessions network and its port.
func internalGatewayURL(listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		port = "8080"
	}
	return "http://" + internalGatewayHost + ":" + port
}

// Handler is the gateway's HTTP handler.
func (a *App) Handler() http.Handler { return a.handler }

// Close releases the store and the Engine client.
func (a *App) Close() error {
	var errs []error
	for i := len(a.closers) - 1; i >= 0; i-- {
		errs = append(errs, a.closers[i]())
	}
	a.closers = nil
	return errors.Join(errs...)
}

// Run serves cfg.listen (and metrics.listen when enabled) and runs the
// reaper until ctx ends, then shuts down gracefully (10 s drain). SIGHUP
// reloads the curriculum bundle and the invites file.
func (a *App) Run(ctx context.Context) error {
	srv := httpx.NewServer(a.cfg.Listen, a.handler, a.log)
	ln, err := net.Listen("tcp", a.cfg.Listen)
	if err != nil {
		return err
	}
	a.log.Info("gateway: listening", "addr", ln.Addr().String(), "mode", a.cfg.Mode, "auth", a.cfg.Auth.Mode,
		"sessions", a.cfg.Sessions.Backend, "public_url", a.cfg.PublicURL, "version", buildinfo.Version)
	errc := make(chan error, 2)
	go func() { errc <- srv.Serve(ln) }()

	var msrv *http.Server
	if a.cfg.Metrics.Enabled {
		msrv = httpx.NewServer(a.cfg.Metrics.Listen, a.metricsHandler(), a.log)
		go func() {
			a.log.Info("gateway: metrics", "addr", a.cfg.Metrics.Listen)
			if err := msrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}
	rctx, stopReaper := context.WithCancel(ctx)
	defer stopReaper()
	go a.Sessions.Run(rctx, time.Minute)

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	for {
		select {
		case <-hup:
			if err := a.Curriculum.Reload(); err != nil {
				a.log.Error("gateway: curriculum reload", "err", err)
			}
			if err := a.SyncInvites(ctx); err != nil {
				a.log.Error("gateway: invites reload", "err", err)
			}
			continue
		case err := <-errc:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ctx.Done():
		}
		break
	}
	a.log.Info("gateway: shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if msrv != nil {
		_ = msrv.Shutdown(sctx)
	}
	return srv.Shutdown(sctx)
}

// ---- routes -----------------------------------------------------------------

type sessionCtxKey struct{}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	protect := a.Auth.Middleware
	api := protocol.APIPrefix

	mux.HandleFunc("GET /healthz", a.healthz)

	// Authentication (public).
	mux.Handle("GET "+api+"/auth/config", httpx.Handle(a.authConfig))
	a.Auth.Routes(mux)

	mux.Handle("GET "+api+"/me", protect(httpx.Handle(a.me)))
	mux.Handle("POST "+api+"/sessions", protect(httpx.Handle(a.createSession)))
	mux.Handle("GET "+api+"/sessions/{id}", protect(httpx.Handle(a.getSession)))
	mux.Handle("DELETE "+api+"/sessions/{id}", protect(httpx.Handle(a.deleteSession)))
	mux.Handle("POST "+api+"/sessions/{id}/restart-emacs", protect(httpx.Handle(a.restartEmacs)))
	mux.Handle("GET "+api+"/progress", protect(httpx.Handle(a.getProgress)))
	mux.Handle("POST "+api+"/progress/reset", protect(httpx.Handle(a.resetProgress)))
	mux.Handle("POST "+api+"/stuck", protect(httpx.Handle(a.stuck)))
	// TODO(gateway, deployment.md §9.4): DELETE /api/v1/me removes the
	// user, their sessions, progress and workspace volume after a
	// confirmation; playctl export-user is its counterpart.
	a.Curriculum.Routes(mux, protect, a.internalAuth)
	mux.Handle(api+"/", httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("no such endpoint")
	}))

	// Session proxy.
	mux.Handle("/s/{id}/{rest...}", protect(a.Proxy))

	// sessiond → gateway.
	mux.Handle("POST "+protocol.InternalSessionsPath+"{id}/progress", a.internalAuth(httpx.Handle(a.internalProgress)))
	mux.Handle("POST "+protocol.InternalSessionsPath+"{id}/stuck", a.internalAuth(httpx.Handle(a.internalStuck)))
	mux.Handle("POST "+protocol.InternalSessionsPath+"{id}/heartbeat", a.internalAuth(httpx.Handle(a.internalHeartbeat)))
	mux.Handle("/internal/", httpx.Handle(func(w http.ResponseWriter, r *http.Request) error {
		return httpx.NotFound("no such endpoint")
	}))

	// Fonts and the web app.
	mux.Handle("GET /fonts/{file...}", a.fonts())
	mux.Handle("/", a.webApp())

	skip := func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/internal/") }
	return httpx.Chain(mux,
		httpx.RequestID,
		httpx.Logging(a.log),
		httpx.Recover,
		httpx.OriginCheck(a.cfg.PublicURL, skip),
	)
}

func (a *App) healthz(w http.ResponseWriter, r *http.Request) {
	status := protocol.HealthStatus{OK: true}
	if loaded, _ := a.Curriculum.Loaded(); !loaded {
		// Still healthy (the app works, lessons do not): say why.
		status.Reason = "curriculum bundle not loaded"
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (a *App) authConfig(w http.ResponseWriter, r *http.Request) error {
	info := protocol.AuthConfigInfo{Mode: a.cfg.Mode, AuthMode: a.cfg.Auth.Mode}
	if a.cfg.Auth.Mode == protocol.AuthOIDC {
		info.OIDCStartURL = auth.PathOIDCStart
	}
	httpx.WriteJSON(w, http.StatusOK, info)
	return nil
}

func (a *App) user(r *http.Request) (*protocol.User, error) {
	u, ok := auth.UserFrom(r.Context())
	if !ok {
		return nil, httpx.Unauthenticated("sign in to continue")
	}
	return u, nil
}

func (a *App) me(w http.ResponseWriter, r *http.Request) error {
	u, err := a.user(r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, protocol.Me{
		User: *u, Mode: a.cfg.Mode, AuthMode: a.cfg.Auth.Mode, Version: buildinfo.Version,
		Features: protocol.MeFeatures{
			GUI: a.cfg.Sessions.GUI.Enabled,
			// TODO(gateway, deployment.md §6): true once remote boxes are
			// provisioned (M6).
			RemoteBox:    false,
			StuckReports: a.cfg.Features.StuckReports,
			AuthorMode:   a.cfg.Features.AuthorMode,
		},
	})
	return nil
}

// ownedSession loads {id} and checks it belongs to the user.
func (a *App) ownedSession(r *http.Request, id string) (*protocol.User, *sessions.Session, error) {
	u, err := a.user(r)
	if err != nil {
		return nil, nil, err
	}
	sess, err := a.Sessions.Get(r.Context(), id)
	if errors.Is(err, sessions.ErrNotFound) {
		return nil, nil, httpx.NotFound("no such session")
	}
	if err != nil {
		return nil, nil, err
	}
	if !sess.OwnedBy(u.ID) {
		return nil, nil, httpx.Forbidden("this session belongs to someone else")
	}
	return u, sess, nil
}

func (a *App) createSession(w http.ResponseWriter, r *http.Request) error {
	u, err := a.user(r)
	if err != nil {
		return err
	}
	if !a.createLim.Allow(u.ID) {
		return httpx.RateLimited("too many session requests; wait a minute")
	}
	sess, err := a.Sessions.Ensure(r.Context(), u.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, sess.Info())
	return nil
}

func (a *App) getSession(w http.ResponseWriter, r *http.Request) error {
	_, sess, err := a.ownedSession(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	info := sess.Info()
	if sess.Usable() {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if st, err := a.Sessions.Status(ctx, sess); err == nil {
			info.Emacs = &protocol.EmacsInfo{Version: st.Emacs.Version, UptimeMs: st.Emacs.UptimeMs}
			info.Mode = st.Mode
			info.Step = st.Step
		}
	}
	if !sess.Shared && sess.Image != "" && sess.Image != a.cfg.Sessions.Image {
		info.UpdateAvailable = true
	}
	httpx.WriteJSON(w, http.StatusOK, info)
	return nil
}

func (a *App) deleteSession(w http.ResponseWriter, r *http.Request) error {
	_, sess, err := a.ownedSession(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	if err := a.Sessions.Stop(r.Context(), sess.ID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (a *App) restartEmacs(w http.ResponseWriter, r *http.Request) error {
	_, sess, err := a.ownedSession(r, r.PathValue("id"))
	if err != nil {
		return err
	}
	if !sess.Usable() {
		return httpx.SessionUnavailable("the session is " + string(sess.State))
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var res json.RawMessage
	if err := a.Sessions.Call(ctx, sess, protocol.OpRestartEmacs, protocol.Empty{}, &res); err != nil {
		return err
	}
	if len(res) == 0 {
		res = json.RawMessage("{}")
	}
	httpx.WriteOK(w, res)
	return nil
}

func (a *App) getProgress(w http.ResponseWriter, r *http.Request) error {
	u, err := a.user(r)
	if err != nil {
		return err
	}
	p, err := a.Progress.Get(r.Context(), u.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

func (a *App) resetProgress(w http.ResponseWriter, r *http.Request) error {
	u, err := a.user(r)
	if err != nil {
		return err
	}
	// {lessonId} clears one lesson; no body, {} or an empty lessonId clears
	// every lesson ("Start over", api.md §2.4).
	var req protocol.ProgressResetRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := a.Progress.Reset(r.Context(), u.ID, req.LessonID); err != nil {
		return err
	}
	// Tell the user's running session to forget the step and its saved
	// progress: drop-step {lessonId}, or {} for everything (best effort).
	if sess, err := a.Sessions.ForUser(r.Context(), u.ID); err == nil && sess.Usable() {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := a.Sessions.Call(ctx, sess, protocol.OpDropStep, protocol.DropStepRequest{LessonID: req.LessonID}, nil); err != nil {
			httpx.Logger(r.Context()).Warn("progress reset: drop-step", "session_id", sess.ID, "err", err)
		}
	}
	httpx.NoContent(w)
	return nil
}

func (a *App) stuck(w http.ResponseWriter, r *http.Request) error {
	if !a.cfg.Features.StuckReports {
		return httpx.Forbidden("stuck reports are turned off on this playground")
	}
	var req protocol.StuckRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.SessionID == "" {
		return httpx.BadRequest("sessionId is required")
	}
	u, sess, err := a.ownedSession(r, req.SessionID)
	if err != nil {
		return err
	}
	if !a.Stuck.Allow(u.ID) {
		return httpx.RateLimited("you have sent many reports in the last hour; try again later")
	}
	var events []json.RawMessage
	if sess.Usable() {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var res protocol.StuckRPCResult
		if err := a.Sessions.Call(ctx, sess, protocol.OpStuck, protocol.StuckRPCRequest{Note: req.Note}, &res); err != nil {
			httpx.Logger(r.Context()).Warn("stuck: fetching events", "session_id", sess.ID, "err", err)
		} else {
			events = res.Events
		}
	}
	id, err := a.Stuck.File(r.Context(), u.ID, req.LessonID, req.StepID, req.Note, events)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, protocol.Created{ID: strconv.FormatInt(id, 10)})
	return nil
}

// ---- /internal ----------------------------------------------------------------

// internalAuth authenticates sessiond with its bearer token; for
// /internal/sessions/{id}/… the token must belong to {id}.
func (a *App) internalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /internal is for sessiond on the sessions network (api.md §2.6).
		// On a server everything public arrives through Caddy, which adds
		// X-Forwarded-For; refuse those requests outright.
		if a.cfg.Mode == protocol.ModeServer && (r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != "") {
			httpx.WriteError(w, r, httpx.NotFound("no such endpoint"))
			return
		}
		sess, err := a.Sessions.Authenticate(r.Context(), httpx.BearerToken(r))
		if err != nil {
			httpx.WriteError(w, r, httpx.Unauthenticated("bad or missing session token"))
			return
		}
		if id := r.PathValue("id"); id != "" && id != sess.ID {
			httpx.WriteError(w, r, httpx.Forbidden("the token belongs to another session"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionCtxKey{}, sess)))
	})
}

func internalSession(r *http.Request) *sessions.Session {
	s, _ := r.Context().Value(sessionCtxKey{}).(*sessions.Session)
	return s
}

func (a *App) internalProgress(w http.ResponseWriter, r *http.Request) error {
	sess := internalSession(r)
	var rep protocol.ProgressReport
	if err := httpx.DecodeJSON(w, r, &rep); err != nil {
		return err
	}
	// In the shared local session every report belongs to the local user.
	if err := a.Progress.Apply(r.Context(), sess.UserID, rep); err != nil {
		return err
	}
	a.Sessions.Touch(r.Context(), sess.ID)
	httpx.NoContent(w)
	return nil
}

func (a *App) internalStuck(w http.ResponseWriter, r *http.Request) error {
	sess := internalSession(r)
	if !a.cfg.Features.StuckReports {
		return httpx.Forbidden("stuck reports are turned off")
	}
	// The same per-user hourly limit as POST /api/v1/stuck (deployment.md
	// §9.3): sessiond files these for rpc stuck calls through the proxy,
	// and a learner can call /internal with their own session's token.
	if !a.Stuck.Allow(sess.UserID) {
		return httpx.RateLimited("many reports in the last hour; try again later")
	}
	var rep protocol.InternalStuckReport
	if err := httpx.DecodeJSON(w, r, &rep); err != nil {
		return err
	}
	id, err := a.Stuck.File(r.Context(), sess.UserID, rep.LessonID, rep.StepID, rep.Note, rep.Events)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, protocol.Created{ID: strconv.FormatInt(id, 10)})
	return nil
}

func (a *App) internalHeartbeat(w http.ResponseWriter, r *http.Request) error {
	sess := internalSession(r)
	var hb protocol.Heartbeat
	if err := httpx.DecodeJSON(w, r, &hb); err != nil {
		return err
	}
	a.Sessions.Heartbeat(r.Context(), sess.ID, hb.ActiveWebSockets)
	httpx.NoContent(w)
	return nil
}
