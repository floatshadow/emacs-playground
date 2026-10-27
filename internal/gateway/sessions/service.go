package sessions

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/zsy/emacs-playground/internal/gateway/store"
	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
)

// ErrNotFound is returned for unknown session ids and tokens.
var ErrNotFound = errors.New("sessions: not found")

// Session is a snapshot of one registry entry.
type Session struct {
	ID           string
	UserID       string
	State        protocol.SessionState
	Endpoint     *url.URL
	Image        string
	CreatedAt    int64
	LastActiveAt int64
	Failure      string
	// Shared is true for the static session: every user of a local install
	// uses it.
	Shared bool
	GUI    bool
	// ActiveWebSockets is the proxy's count of open WebSockets.
	ActiveWebSockets int
	// ProxySecret goes in the X-Playground-Proxy header of everything sent
	// to this session's sessiond; empty for the static session.
	ProxySecret string
}

// Usable reports whether the session accepts traffic (ready or idle).
func (s *Session) Usable() bool {
	return s.State == protocol.SessionReady || s.State == protocol.SessionIdle
}

// OwnedBy reports whether userID may use the session.
func (s *Session) OwnedBy(userID string) bool { return s.Shared || s.UserID == userID }

// Info is the API view (api.md §2.3) without the sessiond status fields.
func (s *Session) Info() protocol.SessionInfo {
	return protocol.SessionInfo{
		ID: s.ID, State: s.State, CreatedAt: s.CreatedAt, LastActiveAt: s.LastActiveAt,
		Image: s.Image, GUI: s.GUI, Failure: s.Failure,
	}
}

// Manager is the session lifecycle interface (backend.md §2.3).
type Manager interface {
	// Ensure returns the user's running session, creating it if needed.
	Ensure(ctx context.Context, userID string) (*Session, error)
	Get(ctx context.Context, id string) (*Session, error)
	Stop(ctx context.Context, id string) error
	// Touch records activity for the reaper.
	Touch(ctx context.Context, id string)
	// Reap stops idle sessions and removes old volumes.
	Reap(ctx context.Context) error
}

// Options configure New.
type Options struct {
	Backend Backend
	Store   store.Store
	// Static selects the local-mode behaviour: one shared session with id
	// StaticID authenticated by StaticToken that the gateway cannot stop.
	Static      bool
	StaticID    string
	StaticToken string

	Image       string
	RemoteImage string
	Limits      Limits
	Network     string
	Runtime     string
	// Env is passed to every session container (PG_GATEWAY_URL,
	// PG_VNC_GEOMETRY, PG_TERM_SIZE, PG_GUI_ENABLED, PG_AUTHOR_MODE).
	Env map[string]string
	// ProxyKey derives each session's proxy secret (ProxySecret), which
	// the container gets as PG_PROXY_SECRET. Unused in static mode.
	ProxyKey []byte
	GUI      bool

	// IdleStop stops sessions without WebSockets for this long (0: never).
	IdleStop time.Duration
	// IdleAfter marks sessions idle after this long without WebSockets,
	// default 1 minute.
	IdleAfter time.Duration
	// VolumeRetention removes volumes of sessions stopped this long ago
	// (0: never).
	VolumeRetention time.Duration
	MaxSessions     int
	// StartTimeout bounds the /healthz wait after starting, default 30 s.
	StartTimeout time.Duration
	// StaticWait bounds the /healthz wait of Ensure in static mode,
	// default 10 s; after it the session is reported as starting.
	StaticWait time.Duration
	// StopGrace is the SIGTERM-to-SIGKILL grace, default 20 s.
	StopGrace time.Duration

	// HTTPClient talks to sessiond (health, RPC, status).
	HTTPClient *http.Client
	// Health overrides the /healthz probe (tests).
	Health func(ctx context.Context, endpoint *url.URL) error
	Logger *slog.Logger
	Now    func() time.Time
}

type entry struct {
	rec      store.Session
	h        Handle
	endpoint *url.URL
	ws       int
	// lastActivity is the last WebSocket presence, heartbeat with
	// WebSockets, touch, or start.
	lastActivity  time.Time
	lastPersisted time.Time
}

// Service implements Manager.
type Service struct {
	o     Options
	users keyedMutex

	mu      sync.Mutex
	entries map[string]*entry
}

var _ Manager = (*Service)(nil)

// New returns a session service. In static mode the fixed session's record
// is created in the store if missing.
func New(ctx context.Context, o Options) (*Service, error) {
	if o.Backend == nil || o.Store == nil {
		return nil, errors.New("sessions: backend and store are required")
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if o.StartTimeout == 0 {
		o.StartTimeout = 30 * time.Second
	}
	if o.StaticWait == 0 {
		o.StaticWait = 10 * time.Second
	}
	if o.StopGrace == 0 {
		o.StopGrace = 20 * time.Second
	}
	if o.IdleAfter == 0 {
		o.IdleAfter = time.Minute
	}
	if o.MaxSessions == 0 {
		o.MaxSessions = 50
	}
	s := &Service{o: o, entries: map[string]*entry{}}
	if o.Static {
		if o.StaticID == "" || o.StaticToken == "" {
			return nil, errors.New("sessions: static mode needs local.sessionId and local.sessionToken")
		}
		if err := s.initStatic(ctx); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) now() time.Time { return s.o.Now() }

// ProxySecret derives the proxy secret of session id from key:
// hex(HMAC-SHA256(key, "playground session proxy\x00" + id)). It is stable
// across gateway restarts and re-creations of the session on its volume.
func ProxySecret(key []byte, id string) string {
	if len(key) == 0 {
		return ""
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte("playground session proxy\x00" + id))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Service) proxySecret(id string) string {
	if s.o.Static {
		return ""
	}
	return ProxySecret(s.o.ProxyKey, id)
}

// HashToken returns the hex SHA-256 stored for a session token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) initStatic(ctx context.Context) error {
	now := s.now()
	// The static session belongs to the local user; make sure the row
	// exists for stores with foreign keys.
	if _, err := s.o.Store.Users().Get(ctx, protocol.LocalUserID); errors.Is(err, store.ErrNotFound) {
		if err := s.o.Store.Users().Put(ctx, &store.User{ID: protocol.LocalUserID, Kind: protocol.UserLocal, DisplayName: "You", CreatedAt: now.UnixMilli()}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	rec, err := s.o.Store.Sessions().Get(ctx, s.o.StaticID)
	if errors.Is(err, store.ErrNotFound) {
		rec = &store.Session{ID: s.o.StaticID, UserID: protocol.LocalUserID, CreatedAt: now.UnixMilli()}
	} else if err != nil {
		return err
	}
	rec.State = protocol.SessionStarting
	rec.TokenHash = HashToken(s.o.StaticToken)
	// Compose decides the local session's image; the gateway does not know
	// it (sessions.image is for the docker backend).
	rec.Image = ""
	rec.Failure = ""
	h := Handle{ID: rec.ID, UserID: rec.UserID}
	s.entries[rec.ID] = &entry{rec: *rec, h: h, endpoint: s.o.Backend.Endpoint(h), lastActivity: now}
	return s.o.Store.Sessions().Put(ctx, rec)
}

// snapshot copies an entry; callers hold mu.
func (s *Service) snapshot(e *entry) *Session {
	ep := *e.endpoint
	return &Session{
		ID: e.rec.ID, UserID: e.rec.UserID, State: e.rec.State, Endpoint: &ep,
		Image: e.rec.Image, CreatedAt: e.rec.CreatedAt, LastActiveAt: e.rec.LastActiveAt,
		Failure: e.rec.Failure, Shared: s.o.Static, GUI: s.o.GUI, ActiveWebSockets: e.ws,
		ProxySecret: s.proxySecret(e.rec.ID),
	}
}

// persist writes the entry's record; callers must not hold mu.
func (s *Service) persist(ctx context.Context, id string) {
	s.mu.Lock()
	e := s.entries[id]
	if e == nil {
		s.mu.Unlock()
		return
	}
	rec := e.rec
	e.lastPersisted = s.now()
	s.mu.Unlock()
	if err := s.o.Store.Sessions().Put(context.WithoutCancel(ctx), &rec); err != nil {
		s.o.Logger.Error("sessions: persisting", "session_id", id, "err", err)
	}
}

// update mutates an entry under mu and persists it.
func (s *Service) update(ctx context.Context, id string, f func(e *entry)) {
	s.mu.Lock()
	e := s.entries[id]
	if e != nil {
		f(e)
	}
	s.mu.Unlock()
	if e != nil {
		s.persist(ctx, id)
	}
}

func (s *Service) audit(ctx context.Context, userID, action, details string) {
	if err := s.o.Store.Audit(context.WithoutCancel(ctx), store.AuditEvent{At: s.now().UnixMilli(), UserID: userID, Action: action, Details: details}); err != nil {
		s.o.Logger.Warn("sessions: audit", "err", err)
	}
}

func (s *Service) health(ctx context.Context, endpoint *url.URL) error {
	if s.o.Health != nil {
		return s.o.Health(ctx, endpoint)
	}
	return probeHealth(ctx, s.o.HTTPClient, endpoint)
}

// waitHealthy polls /healthz every 500 ms until it answers 200 or d passes.
func (s *Service) waitHealthy(ctx context.Context, endpoint *url.URL, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		err := s.health(ctx, endpoint)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Get returns a session by id.
func (s *Service) Get(ctx context.Context, id string) (*Session, error) {
	s.mu.Lock()
	e := s.entries[id]
	if e == nil {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	state := e.rec.State
	endpoint := e.endpoint
	s.mu.Unlock()
	if s.o.Static && state != protocol.SessionReady && state != protocol.SessionIdle {
		// The local session may have come up since: probe once.
		if s.health(ctx, endpoint) == nil {
			s.markReady(ctx, id)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot(s.entries[id]), nil
}

// ForUser returns the user's current session without creating one.
func (s *Service) ForUser(ctx context.Context, userID string) (*Session, error) {
	if s.o.Static {
		return s.Get(ctx, s.o.StaticID)
	}
	s.mu.Lock()
	var best *entry
	for _, e := range s.entries {
		if e.rec.UserID == userID && e.rec.State != protocol.SessionRemoved && (best == nil || e.rec.CreatedAt > best.rec.CreatedAt) {
			best = e
		}
	}
	defer s.mu.Unlock()
	if best == nil {
		return nil, ErrNotFound
	}
	return s.snapshot(best), nil
}

func (s *Service) markReady(ctx context.Context, id string) {
	now := s.now()
	s.update(ctx, id, func(e *entry) {
		if e.rec.State != protocol.SessionReady && e.rec.State != protocol.SessionIdle {
			e.lastActivity = now
		}
		e.rec.State = protocol.SessionReady
		e.rec.Failure = ""
		e.rec.LastActiveAt = now.UnixMilli()
	})
}

// Ensure implements Manager.
func (s *Service) Ensure(ctx context.Context, userID string) (*Session, error) {
	unlock := s.users.Lock(userID)
	defer unlock()
	if s.o.Static {
		return s.ensureStatic(ctx)
	}
	return s.ensureDocker(ctx, userID)
}

func (s *Service) ensureStatic(ctx context.Context) (*Session, error) {
	id := s.o.StaticID
	s.mu.Lock()
	endpoint := s.entries[id].endpoint
	s.mu.Unlock()
	if err := s.waitHealthy(ctx, endpoint, s.o.StaticWait); err != nil {
		s.update(ctx, id, func(e *entry) {
			e.rec.State = protocol.SessionStarting
			e.rec.Failure = "waiting for the session container to answer " + endpoint.String() + "/healthz"
		})
	} else {
		s.markReady(ctx, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot(s.entries[id]), nil
}

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

// VolumeName is the workspace volume of a user: pg-ws-<user id>, or a hash
// of the id when it is not a valid volume name (OIDC ids).
func VolumeName(userID string) string {
	if safeName.MatchString(userID) {
		return "pg-ws-" + userID
	}
	sum := sha256.Sum256([]byte(userID))
	return "pg-ws-" + hex.EncodeToString(sum[:8])
}

func (s *Service) activeCount() int {
	n := 0
	for _, e := range s.entries {
		switch e.rec.State {
		case protocol.SessionCreating, protocol.SessionStarting, protocol.SessionReady, protocol.SessionIdle, protocol.SessionStopping:
			n++
		}
	}
	return n
}

func (s *Service) ensureDocker(ctx context.Context, userID string) (*Session, error) {
	// Reuse the user's current session if its container still runs.
	s.mu.Lock()
	var cur *entry
	for _, e := range s.entries {
		if e.rec.UserID == userID && e.rec.State != protocol.SessionRemoved && (cur == nil || e.rec.CreatedAt > cur.rec.CreatedAt) {
			cur = e
		}
	}
	var curID string
	var h Handle
	var state protocol.SessionState
	if cur != nil {
		curID, h, state = cur.rec.ID, cur.h, cur.rec.State
	}
	s.mu.Unlock()

	if cur != nil {
		switch state {
		case protocol.SessionReady, protocol.SessionIdle, protocol.SessionStarting, protocol.SessionCreating:
			st, err := s.o.Backend.Inspect(ctx, h)
			if err != nil {
				return nil, httpx.SessionUnavailable("could not inspect your session").Wrap(err)
			}
			if st.Running {
				if state == protocol.SessionReady || state == protocol.SessionIdle {
					s.markReady(ctx, curID)
					return s.Get(ctx, curID)
				}
				// Interrupted start: wait for health again.
				return s.finishStart(ctx, curID)
			}
			// The container died (OOM, crash) or vanished: re-create.
			reason := "the session container stopped"
			if st.OOMKilled {
				reason = "the session ran out of memory"
			}
			s.audit(ctx, userID, "session.exited", "session="+curID+" state="+st.State)
			_ = s.o.Backend.Remove(ctx, h, true)
			s.update(ctx, curID, func(e *entry) {
				e.rec.State = protocol.SessionStopped
				e.rec.StoppedAt = s.now().UnixMilli()
				e.rec.Failure = reason
				e.h.ContainerID = ""
				e.rec.ContainerID = ""
			})
		default:
			// Stopped, failed, or a stale stopping: make sure no container
			// holds the name before re-creating.
			if h.ContainerID != "" {
				if err := s.o.Backend.Remove(ctx, h, true); err != nil {
					return nil, httpx.SessionUnavailable("could not clean up your previous session").Wrap(err)
				}
				s.update(ctx, curID, func(e *entry) { e.h.ContainerID, e.rec.ContainerID = "", "" })
			}
		}
	}

	s.mu.Lock()
	if s.activeCount() >= s.o.MaxSessions {
		s.mu.Unlock()
		return nil, httpx.SessionLimit("the playground is full right now; try again in a few minutes").
			WithDetails(map[string]int{"maxSessions": s.o.MaxSessions})
	}
	now := s.now()
	token := newID(32)
	var id, volume string
	if cur != nil {
		// Re-create on the same volume, keeping the session id (and URL).
		e := s.entries[curID]
		id, volume = curID, e.rec.Volume
		e.rec.State = protocol.SessionCreating
		e.rec.TokenHash = HashToken(token)
		e.rec.Image = s.o.Image
		e.rec.Failure = ""
		e.rec.StoppedAt = 0
		e.rec.LastActiveAt = now.UnixMilli()
		e.lastActivity = now
	} else {
		id, volume = newID(8), VolumeName(userID)
		rec := store.Session{
			ID: id, UserID: userID, State: protocol.SessionCreating, Volume: volume, Image: s.o.Image,
			TokenHash: HashToken(token), CreatedAt: now.UnixMilli(), LastActiveAt: now.UnixMilli(),
		}
		h := Handle{ID: id, UserID: userID, Volume: volume, Image: s.o.Image}
		s.entries[id] = &entry{rec: rec, h: h, endpoint: s.o.Backend.Endpoint(h), lastActivity: now}
	}
	if volume == "" {
		volume = VolumeName(userID)
		s.entries[id].rec.Volume = volume
	}
	s.mu.Unlock()
	s.persist(ctx, id)

	start := time.Now()
	spec := Spec{
		ID: id, UserID: userID, Token: token, ProxySecret: s.proxySecret(id), Image: s.o.Image, RemoteImage: s.o.RemoteImage,
		Limits: s.o.Limits, Network: s.o.Network, Runtime: s.o.Runtime, Volume: volume, Env: s.o.Env,
	}
	h, err := s.o.Backend.Create(ctx, spec)
	if err != nil {
		return nil, s.fail(ctx, id, h, "could not create the session container", err)
	}
	s.update(ctx, id, func(e *entry) {
		e.h = h
		e.endpoint = s.o.Backend.Endpoint(h)
		e.rec.ContainerID = h.ContainerID
		e.rec.RemoteContainerID = h.RemoteContainerID
		e.rec.State = protocol.SessionStarting
	})
	if err := s.o.Backend.Start(ctx, h); err != nil {
		return nil, s.fail(ctx, id, h, "could not start the session container", err)
	}
	sess, err := s.finishStart(ctx, id)
	if err == nil {
		s.audit(ctx, userID, "session.create", fmt.Sprintf("session=%s took_ms=%d", id, time.Since(start).Milliseconds()))
	}
	return sess, err
}

// finishStart waits for /healthz and marks the session ready or failed.
func (s *Service) finishStart(ctx context.Context, id string) (*Session, error) {
	s.mu.Lock()
	e := s.entries[id]
	endpoint, h := e.endpoint, e.h
	s.mu.Unlock()
	if err := s.waitHealthy(ctx, endpoint, s.o.StartTimeout); err != nil {
		return nil, s.fail(ctx, id, h, "the session did not become healthy in time", err)
	}
	s.markReady(ctx, id)
	return s.Get(ctx, id)
}

// fail marks a session failed, removes its container (keeping the volume)
// and returns the error for the API.
func (s *Service) fail(ctx context.Context, id string, h Handle, msg string, cause error) error {
	s.o.Logger.Error("sessions: "+msg, "session_id", id, "err", cause)
	if h.ContainerID != "" {
		if err := s.o.Backend.Remove(context.WithoutCancel(ctx), h, true); err != nil {
			s.o.Logger.Warn("sessions: removing a failed container", "session_id", id, "err", err)
		}
	}
	var userID string
	s.update(ctx, id, func(e *entry) {
		e.rec.State = protocol.SessionFailed
		e.rec.Failure = msg
		e.rec.StoppedAt = s.now().UnixMilli()
		e.rec.ContainerID = ""
		e.h.ContainerID = ""
		userID = e.rec.UserID
	})
	s.audit(ctx, userID, "session.failed", "session="+id+" reason="+msg+": "+cause.Error())
	return httpx.SessionUnavailable(msg).WithDetails(map[string]string{"sessionId": id}).Wrap(cause)
}

// Stop implements Manager: stops the container and keeps the volume.
func (s *Service) Stop(ctx context.Context, id string) error {
	return s.stop(ctx, id, "session.stop")
}

func (s *Service) stop(ctx context.Context, id, action string) error {
	if s.o.Static {
		return httpx.Conflict("the local session is managed by docker compose; stop it with docker compose stop session")
	}
	s.mu.Lock()
	e := s.entries[id]
	if e == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	userID := e.rec.UserID
	s.mu.Unlock()
	unlock := s.users.Lock(userID)
	defer unlock()

	s.mu.Lock()
	e = s.entries[id]
	state, h := e.rec.State, e.h
	if state == protocol.SessionStopped || state == protocol.SessionRemoved || state == protocol.SessionFailed {
		s.mu.Unlock()
		return nil
	}
	e.rec.State = protocol.SessionStopping
	s.mu.Unlock()
	s.persist(ctx, id)

	ctx = context.WithoutCancel(ctx)
	err := s.o.Backend.Stop(ctx, h, s.o.StopGrace)
	if err == nil {
		err = s.o.Backend.Remove(ctx, h, true)
	}
	if err != nil {
		s.o.Logger.Error("sessions: stopping", "session_id", id, "err", err)
	}
	s.update(ctx, id, func(e *entry) {
		e.rec.State = protocol.SessionStopped
		e.rec.StoppedAt = s.now().UnixMilli()
		e.rec.ContainerID = ""
		e.h.ContainerID = ""
		e.ws = 0
	})
	s.audit(ctx, userID, action, "session="+id)
	return err
}

// Touch implements Manager.
func (s *Service) Touch(ctx context.Context, id string) {
	now := s.now()
	s.mu.Lock()
	e := s.entries[id]
	if e == nil {
		s.mu.Unlock()
		return
	}
	e.lastActivity = now
	e.rec.LastActiveAt = now.UnixMilli()
	due := now.Sub(e.lastPersisted) > time.Minute
	s.mu.Unlock()
	if due {
		s.persist(ctx, id)
	}
}

// Opened records a WebSocket opened through the proxy.
func (s *Service) Opened(id string) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[id]; e != nil {
		e.ws++
		e.lastActivity = now
		if e.rec.State == protocol.SessionIdle {
			e.rec.State = protocol.SessionReady
		}
	}
}

// Closed records a WebSocket closed.
func (s *Service) Closed(id string) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[id]; e != nil {
		if e.ws > 0 {
			e.ws--
		}
		e.lastActivity = now
	}
}

// Heartbeat applies sessiond's report (api.md §2.6).
func (s *Service) Heartbeat(ctx context.Context, id string, activeWebSockets int) {
	if activeWebSockets > 0 {
		s.Touch(ctx, id)
	}
}

// Authenticate maps a bearer token to its session (/internal).
func (s *Service) Authenticate(_ context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	h := []byte(HashToken(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	var found *entry
	for _, e := range s.entries {
		if e.rec.State == protocol.SessionRemoved {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(e.rec.TokenHash), h) == 1 {
			found = e
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	return s.snapshot(found), nil
}

// Reap implements Manager.
func (s *Service) Reap(ctx context.Context) error {
	now := s.now()
	type item struct {
		id, user  string
		state     protocol.SessionState
		ws        int
		idleFor   time.Duration
		h         Handle
		stoppedAt int64
	}
	s.mu.Lock()
	items := make([]item, 0, len(s.entries))
	for id, e := range s.entries {
		items = append(items, item{id, e.rec.UserID, e.rec.State, e.ws, now.Sub(e.lastActivity), e.h, e.rec.StoppedAt})
	}
	s.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })

	var errs []error
	if s.o.Static {
		s.refreshStatic(ctx)
	} else if err := s.pruneNetworks(ctx); err != nil {
		errs = append(errs, err)
	}
	for _, it := range items {
		switch it.state {
		case protocol.SessionReady, protocol.SessionIdle:
		case protocol.SessionStopped, protocol.SessionFailed:
			if s.o.Static || s.o.VolumeRetention <= 0 || it.stoppedAt == 0 || now.Sub(time.UnixMilli(it.stoppedAt)) < s.o.VolumeRetention {
				continue
			}
			if err := s.o.Backend.Remove(ctx, it.h, false); err != nil {
				errs = append(errs, err)
				continue
			}
			s.update(ctx, it.id, func(e *entry) { e.rec.State = protocol.SessionRemoved })
			s.audit(ctx, it.user, "volume.remove", "session="+it.id+" volume="+it.h.Volume)
			continue
		default:
			continue
		}
		if !s.o.Static {
			st, err := s.o.Backend.Inspect(ctx, it.h)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !st.Running {
				_ = s.o.Backend.Remove(ctx, it.h, true)
				reason := "the session container stopped"
				if st.OOMKilled {
					reason = "the session ran out of memory"
				}
				s.update(ctx, it.id, func(e *entry) {
					e.rec.State = protocol.SessionStopped
					e.rec.StoppedAt = now.UnixMilli()
					e.rec.Failure = reason
					e.rec.ContainerID = ""
					e.h.ContainerID = ""
					e.ws = 0
				})
				s.audit(ctx, it.user, "session.exited", "session="+it.id+" state="+st.State)
				continue
			}
		}
		if it.ws > 0 {
			continue
		}
		if !s.o.Static && s.o.IdleStop > 0 && it.idleFor >= s.o.IdleStop {
			if err := s.stop(ctx, it.id, "session.idle-stop"); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if it.state == protocol.SessionReady && it.idleFor >= s.o.IdleAfter {
			s.update(ctx, it.id, func(e *entry) {
				if e.ws == 0 && e.rec.State == protocol.SessionReady {
					e.rec.State = protocol.SessionIdle
				}
			})
		}
	}
	return errors.Join(errs...)
}

// refreshStatic probes the local session so GET /sessions/{id} reports a
// container that went away (starting) or came back (ready).
func (s *Service) refreshStatic(ctx context.Context) {
	id := s.o.StaticID
	s.mu.Lock()
	endpoint, state := s.entries[id].endpoint, s.entries[id].rec.State
	s.mu.Unlock()
	err := s.health(ctx, endpoint)
	switch {
	case err == nil && state != protocol.SessionReady && state != protocol.SessionIdle:
		s.markReady(ctx, id)
	case err != nil && (state == protocol.SessionReady || state == protocol.SessionIdle):
		s.update(ctx, id, func(e *entry) {
			e.rec.State = protocol.SessionStarting
			e.rec.Failure = "the session container stopped answering " + endpoint.String() + "/healthz"
		})
	}
}

// Run reaps every interval until ctx ends.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Reap(ctx); err != nil {
				s.o.Logger.Warn("sessions: reap", "err", err)
			}
		}
	}
}

// Rebuild loads the registry from the store and reconciles it with the
// backend's labelled containers (backend.md §2.4): records with a running
// container stay usable, records whose container is gone become stopped,
// and containers without a record (orphans) are removed with their volume
// kept.
func (s *Service) Rebuild(ctx context.Context) error {
	if s.o.Static {
		// Probe once so the first GET sees the right state.
		s.mu.Lock()
		endpoint := s.entries[s.o.StaticID].endpoint
		s.mu.Unlock()
		if s.health(ctx, endpoint) == nil {
			s.markReady(ctx, s.o.StaticID)
		}
		return nil
	}
	recs, err := s.o.Store.Sessions().List(ctx)
	if err != nil {
		return err
	}
	handles, err := s.o.Backend.List(ctx)
	if err != nil {
		return err
	}
	byID := map[string]Handle{}
	for _, h := range handles {
		byID[h.ID] = h
	}
	now := s.now()
	s.mu.Lock()
	for _, rec := range recs {
		h := Handle{ID: rec.ID, UserID: rec.UserID, ContainerID: rec.ContainerID, RemoteContainerID: rec.RemoteContainerID, Volume: rec.Volume, Image: rec.Image}
		if bh, ok := byID[rec.ID]; ok {
			h.ContainerID = bh.ContainerID
		}
		s.entries[rec.ID] = &entry{rec: rec, h: h, endpoint: s.o.Backend.Endpoint(h), lastActivity: now}
	}
	s.mu.Unlock()

	var errs []error
	for _, h := range handles {
		s.mu.Lock()
		e := s.entries[h.ID]
		orphan := e == nil || e.rec.State == protocol.SessionRemoved
		s.mu.Unlock()
		if orphan {
			s.o.Logger.Warn("sessions: removing an orphan session container", "session_id", h.ID, "container", h.ContainerID)
			if err := s.o.Backend.Remove(ctx, Handle{ID: h.ID, ContainerID: h.ContainerID}, true); err != nil {
				errs = append(errs, err)
			}
			s.audit(ctx, h.UserID, "session.orphan-removed", "session="+h.ID)
		}
	}
	for _, rec := range recs {
		switch rec.State {
		case protocol.SessionStopped, protocol.SessionFailed, protocol.SessionRemoved:
			continue
		}
		s.mu.Lock()
		h := s.entries[rec.ID].h
		s.mu.Unlock()
		st, err := s.o.Backend.Inspect(ctx, h)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if st.Running && (rec.State == protocol.SessionReady || rec.State == protocol.SessionIdle) {
			// Running sessions survive a gateway restart; nobody is
			// connected through this gateway yet. A new gateway container
			// (an upgrade) joins their networks again.
			if nw, ok := s.o.Backend.(Networker); ok {
				if err := nw.Attach(ctx, h); err != nil {
					errs = append(errs, fmt.Errorf("sessions: re-attaching to session %s: %w", rec.ID, err))
				}
			}
			s.update(ctx, rec.ID, func(e *entry) { e.rec.State = protocol.SessionIdle })
			continue
		}
		// Gone, or caught mid-start or mid-stop: stop it cleanly; the next
		// Ensure re-creates it on its volume.
		_ = s.o.Backend.Remove(ctx, h, true)
		s.update(ctx, rec.ID, func(e *entry) {
			e.rec.State = protocol.SessionStopped
			e.rec.StoppedAt = now.UnixMilli()
			e.rec.ContainerID = ""
			e.h.ContainerID = ""
		})
	}
	if err := s.pruneNetworks(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// pruneNetworks removes the networks of sessions that have no container
// (Networker backends): left behind by a crash or a failed removal.
func (s *Service) pruneNetworks(ctx context.Context) error {
	nw, ok := s.o.Backend.(Networker)
	if !ok || s.o.Static {
		return nil
	}
	return nw.Prune(ctx, func(id string) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		e := s.entries[id]
		if e == nil {
			return false
		}
		switch e.rec.State {
		case protocol.SessionStopped, protocol.SessionFailed, protocol.SessionRemoved:
			return false
		}
		return true
	})
}

// Counts returns the number of sessions per state and open WebSockets
// (metrics).
func (s *Service) Counts() (map[protocol.SessionState]int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[protocol.SessionState]int{}
	ws := 0
	for _, e := range s.entries {
		m[e.rec.State]++
		ws += e.ws
	}
	return m, ws
}

// Call sends a session RPC to sessiond (api.md §3.4) and decodes the result
// into res. Non-2xx answers become *httpx.Error from the problem body.
func (s *Service) Call(ctx context.Context, sess *Session, op protocol.RPCOp, req, res any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	u := sess.Endpoint.JoinPath(protocol.PathRPC + string(op))
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(protocol.HeaderUser, sess.UserID)
	if sess.ProxySecret != "" {
		r.Header.Set(protocol.HeaderProxySecret, sess.ProxySecret)
	}
	if id := httpx.RequestIDFrom(ctx); id != "" {
		r.Header.Set(protocol.HeaderRequestID, id)
	}
	resp, err := s.o.HTTPClient.Do(r)
	if err != nil {
		return httpx.SessionUnavailable("the session did not answer").Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return httpx.DecodeProblem(resp)
	}
	var env protocol.RawRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&env); err != nil {
		return fmt.Errorf("sessions: rpc %s: %w", op, err)
	}
	if res != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, res)
	}
	return nil
}

// Status fetches sessiond's GET /status.
func (s *Service) Status(ctx context.Context, sess *Session) (*protocol.StatusResult, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, sess.Endpoint.JoinPath(protocol.PathStatus).String(), nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set(protocol.HeaderUser, sess.UserID)
	if sess.ProxySecret != "" {
		r.Header.Set(protocol.HeaderProxySecret, sess.ProxySecret)
	}
	resp, err := s.o.HTTPClient.Do(r)
	if err != nil {
		return nil, httpx.SessionUnavailable("the session did not answer").Wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpx.DecodeProblem(resp)
	}
	var st protocol.StatusResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

// keyedMutex serialises work per key (per user).
type keyedMutex struct {
	mu sync.Mutex
	m  map[string]*keyedEntry
}

type keyedEntry struct {
	mu sync.Mutex
	n  int
}

// Lock locks key and returns the unlock function.
func (k *keyedMutex) Lock(key string) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[string]*keyedEntry{}
	}
	e := k.m[key]
	if e == nil {
		e = &keyedEntry{}
		k.m[key] = e
	}
	e.n++
	k.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		k.mu.Lock()
		e.n--
		if e.n == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}
