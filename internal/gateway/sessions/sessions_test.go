package sessions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zsy/emacs-playground/internal/docker"
	"github.com/zsy/emacs-playground/internal/docker/dockertest"
	"github.com/zsy/emacs-playground/internal/gateway/store"
	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.OpenSQLite(t.TempDir() + "/pg.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestStaticSession(t *testing.T) {
	var healthy atomic.Bool
	sessiond := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			if !healthy.Load() {
				w.WriteHeader(503)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/status":
			_, _ = io.WriteString(w, `{"sessionId":"local","emacs":{"version":"31.1","pid":7,"uptimeMs":5,"agent":true,"responsive":true},"frames":{"terminal":true,"gui":false},"mode":"terminal","step":null,"modules":["base","agent"],"activeWebSockets":0}`)
		case "/rpc/hint":
			if r.Header.Get(protocol.HeaderUser) == "" {
				w.WriteHeader(400)
				return
			}
			httpx.WriteOK(w, nil)
		default:
			httpx.WriteError(w, r, httpx.Conflict("no active step"))
		}
	}))
	defer sessiond.Close()
	st, err := store.OpenJSON(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewStaticBackend("local", sessiond.URL)
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock{t: time.Unix(1_800_000_000, 0)}
	ctx := context.Background()
	s, err := New(ctx, Options{Backend: b, Store: st, Static: true, StaticID: "local", StaticToken: "local", StaticWait: 600 * time.Millisecond, Now: clk.Now, Logger: quiet(), GUI: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	// Not healthy yet: Ensure reports starting after the short wait.
	sess, err := s.Ensure(ctx, "local")
	if err != nil || sess.State != protocol.SessionStarting || sess.ID != "local" || !sess.Shared || sess.Failure == "" {
		t.Fatalf("starting: %v %+v", err, sess)
	}
	healthy.Store(true)
	if sess, _ := s.Get(ctx, "local"); sess.State != protocol.SessionReady {
		t.Fatalf("Get re-probes: %+v", sess)
	}
	sess, err = s.Ensure(ctx, "u-other")
	if err != nil || sess.State != protocol.SessionReady || !sess.OwnedBy("u-other") || !sess.GUI {
		t.Fatalf("ready: %v %+v", err, sess)
	}
	if got, err := s.Authenticate(ctx, "local"); err != nil || got.ID != "local" {
		t.Fatalf("token: %v", err)
	}
	if _, err := s.Authenticate(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad token: %v", err)
	}
	var he *httpx.Error
	if err := s.Stop(ctx, "local"); !errors.As(err, &he) || he.Code != protocol.ErrConflict {
		t.Fatalf("stop static: %v", err)
	}
	// Idle after a minute without WebSockets, ready again on a WebSocket.
	clk.Add(2 * time.Minute)
	if err := s.Reap(ctx); err != nil {
		t.Fatal(err)
	}
	if sess, _ := s.Get(ctx, "local"); sess.State != protocol.SessionIdle {
		t.Fatalf("idle: %+v", sess)
	}
	s.Opened("local")
	if sess, _ := s.Get(ctx, "local"); sess.State != protocol.SessionReady || sess.ActiveWebSockets != 1 {
		t.Fatalf("opened: %+v", sess)
	}
	clk.Add(10 * time.Minute)
	_ = s.Reap(ctx)
	if sess, _ := s.Get(ctx, "local"); sess.State != protocol.SessionReady {
		t.Fatalf("an open WebSocket keeps it ready: %+v", sess)
	}
	s.Closed("local")
	// The reaper notices the container going away and coming back.
	healthy.Store(false)
	_ = s.Reap(ctx)
	if sess, _ := s.Get(ctx, "local"); sess.State != protocol.SessionStarting || sess.Failure == "" {
		t.Fatalf("down: %+v", sess)
	}
	healthy.Store(true)
	_ = s.Reap(ctx)
	if sess, _ := s.Get(ctx, "local"); sess.State != protocol.SessionReady {
		t.Fatalf("back: %+v", sess)
	}
	// Status and RPC through the service.
	sess, _ = s.Get(ctx, "local")
	if stt, err := s.Status(ctx, sess); err != nil || stt.Emacs.Version != "31.1" {
		t.Fatalf("status %v %+v", err, stt)
	}
	if err := s.Call(ctx, sess, protocol.OpHint, protocol.HintRequest{StepID: "x", Level: 1}, nil); err != nil {
		t.Fatalf("rpc: %v", err)
	}
	if err := s.Call(ctx, sess, protocol.OpCheck, protocol.Empty{}, nil); !errors.As(err, &he) || he.Code != protocol.ErrConflict {
		t.Fatalf("rpc problem: %v", err)
	}
	// The record survives in the store.
	if rec, err := st.Sessions().Get(ctx, "local"); err != nil || rec.TokenHash != HashToken("local") {
		t.Fatalf("store %v %+v", err, rec)
	}
}

// dockerFixture runs the service over the fake Engine; a container counts
// as healthy while it runs.
type dockerFixture struct {
	fake *dockertest.Fake
	st   store.Store
	clk  *clock
	s    *Service
	opts Options
}

func newDockerFixture(t *testing.T, mutate func(o *Options)) *dockerFixture {
	t.Helper()
	f := &dockerFixture{fake: dockertest.New(), st: openStore(t), clk: &clock{t: time.Unix(1_800_000_000, 0)}}
	ctx := context.Background()
	for _, u := range []string{"u1", "u2", "https://idp.example|42"} {
		if err := f.st.Users().Put(ctx, &store.User{ID: u, Kind: protocol.UserInvite, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	f.opts = Options{
		Backend: &DockerBackend{Client: f.fake, Version: "v1.2.3"},
		Store:   f.st, Image: "ghcr.io/zsy/emacs-playground/session:v1",
		Limits: Limits{CPUs: 2, Memory: 3 << 30, Pids: 1024}, Network: "emacs-playground_sessions", Runtime: "runc",
		Env:             map[string]string{"PG_GATEWAY_URL": "http://gateway:8080", "PG_TERM_SIZE": "120x36"},
		IdleStop:        30 * time.Minute,
		MaxSessions:     2,
		StartTimeout:    300 * time.Millisecond,
		VolumeRetention: 30 * 24 * time.Hour,
		Health:          f.health,
		Now:             f.clk.Now,
		Logger:          quiet(),
	}
	if mutate != nil {
		mutate(&f.opts)
	}
	s, err := New(ctx, f.opts)
	if err != nil {
		t.Fatal(err)
	}
	f.s = s
	return f
}

func (f *dockerFixture) health(_ context.Context, endpoint *url.URL) error {
	name := strings.Split(endpoint.Host, ":")[0]
	for _, c := range f.fake.Live() {
		if c.Spec.Name == name && c.State == "running" {
			return nil
		}
	}
	return errors.New("connection refused")
}

func (f *dockerFixture) container(t *testing.T, sessionID string) dockertest.FakeContainer {
	t.Helper()
	for _, c := range f.fake.Live() {
		if c.Spec.Labels[docker.LabelSession] == sessionID {
			return c
		}
	}
	t.Fatalf("no container for %s", sessionID)
	return dockertest.FakeContainer{}
}

func TestDockerEnsureAndHardening(t *testing.T) {
	f := newDockerFixture(t, nil)
	ctx := context.Background()
	sess, err := f.s.Ensure(ctx, "u1")
	if err != nil || sess.State != protocol.SessionReady || sess.Shared || sess.OwnedBy("u2") || !sess.OwnedBy("u1") {
		t.Fatalf("ensure: %v %+v", err, sess)
	}
	if sess.Endpoint.String() != "http://pg-session-"+sess.ID+":7000" {
		t.Fatalf("endpoint %s", sess.Endpoint)
	}
	c := f.container(t, sess.ID)
	sp := c.Spec
	if sp.Name != "pg-session-"+sess.ID || sp.Image != "ghcr.io/zsy/emacs-playground/session:v1" || sp.Network != "emacs-playground_sessions" {
		t.Fatalf("spec %+v", sp)
	}
	if sp.Labels[docker.LabelSession] != sess.ID || sp.Labels[docker.LabelUser] != "u1" || sp.Labels[docker.LabelVersion] != "v1.2.3" || sp.Labels[docker.LabelRole] != docker.RoleSession {
		t.Fatalf("labels %v", sp.Labels)
	}
	if !sp.ReadOnlyRootfs || sp.CapDrop[0] != "ALL" || sp.User != "1000:1000" || sp.PidsLimit != 1024 || sp.Memory != 3<<30 || sp.MemorySwap != 3<<30 || sp.NanoCPUs != 2e9 {
		t.Fatalf("hardening %+v", sp)
	}
	if len(sp.Volumes) != 1 || sp.Volumes[0].Volume != "pg-ws-u1" || sp.Volumes[0].Target != "/work" || !f.fake.HasVolume("pg-ws-u1") {
		t.Fatalf("volume %+v", sp.Volumes)
	}
	token := sp.Env["PG_SESSION_TOKEN"]
	if len(token) != 64 || sp.Env["PG_SESSION_ID"] != sess.ID || sp.Env["PG_GATEWAY_URL"] != "http://gateway:8080" {
		t.Fatalf("env %v", sp.Env)
	}
	rec, _ := f.st.Sessions().Get(ctx, sess.ID)
	if rec.TokenHash != HashToken(token) || strings.Contains(rec.TokenHash, token) || rec.State != protocol.SessionReady || rec.ContainerID != c.ID {
		t.Fatalf("record %+v", rec)
	}
	if got, err := f.s.Authenticate(ctx, token); err != nil || got.ID != sess.ID {
		t.Fatalf("authenticate %v", err)
	}
	// Idempotent per user.
	again, err := f.s.Ensure(ctx, "u1")
	if err != nil || again.ID != sess.ID || len(f.fake.Live()) != 1 {
		t.Fatalf("second ensure %v %+v %d", err, again, len(f.fake.Live()))
	}
	// OIDC-style ids get a hashed volume name.
	if v := VolumeName("https://idp.example|42"); !strings.HasPrefix(v, "pg-ws-") || strings.ContainsAny(v, ":/|") {
		t.Fatalf("volume name %q", v)
	}
	// Concurrent Ensure for one user creates one session.
	var wg sync.WaitGroup
	ids := make(chan string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := f.s.Ensure(ctx, "u2")
			if err == nil {
				ids <- s.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("two sessions for one user: %s %s", first, id)
		}
	}
	// Capacity: MaxSessions is 2.
	_ = f.st.Users().Put(ctx, &store.User{ID: "u3", Kind: protocol.UserInvite, CreatedAt: 1})
	_, err = f.s.Ensure(ctx, "u3")
	var he *httpx.Error
	if !errors.As(err, &he) || he.Code != protocol.ErrSessionLimit {
		t.Fatalf("limit: %v", err)
	}
}

func TestDockerStopAndRecreate(t *testing.T) {
	f := newDockerFixture(t, nil)
	ctx := context.Background()
	sess, _ := f.s.Ensure(ctx, "u1")
	old := f.container(t, sess.ID)
	oldToken := old.Spec.Env["PG_SESSION_TOKEN"]
	if err := f.s.Stop(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.s.Get(ctx, sess.ID); got.State != protocol.SessionStopped {
		t.Fatalf("stopped: %+v", got)
	}
	if c, _ := f.fake.Container(old.ID); !c.Removed || !f.fake.HasVolume("pg-ws-u1") {
		t.Fatal("stop must remove the container and keep the volume")
	}
	again, err := f.s.Ensure(ctx, "u1")
	if err != nil || again.ID != sess.ID || again.State != protocol.SessionReady {
		t.Fatalf("recreate: %v %+v", err, again)
	}
	nc := f.container(t, sess.ID)
	if nc.ID == old.ID || nc.Spec.Volumes[0].Volume != "pg-ws-u1" {
		t.Fatalf("new container %+v", nc)
	}
	if _, err := f.s.Authenticate(ctx, oldToken); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token still valid")
	}
	if _, err := f.s.Authenticate(ctx, nc.Spec.Env["PG_SESSION_TOKEN"]); err != nil {
		t.Fatal(err)
	}
	log, _ := f.st.AuditLog(ctx, 0)
	var actions []string
	for _, e := range log {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "session.create,session.stop,session.create" {
		t.Fatalf("audit %v", actions)
	}
}

func TestDockerCrashAndReap(t *testing.T) {
	f := newDockerFixture(t, nil)
	ctx := context.Background()
	sess, _ := f.s.Ensure(ctx, "u1")
	c := f.container(t, sess.ID)

	// An OOM-killed container is noticed by the reaper.
	f.fake.Crash(c.ID, true)
	if err := f.s.Reap(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := f.s.Get(ctx, sess.ID)
	if got.State != protocol.SessionStopped || !strings.Contains(got.Failure, "memory") {
		t.Fatalf("after crash: %+v", got)
	}
	// Ensure brings it back.
	if got, err := f.s.Ensure(ctx, "u1"); err != nil || got.State != protocol.SessionReady {
		t.Fatalf("after crash ensure %v %+v", err, got)
	}
	// Idle: after a minute without WebSockets the session is idle, after
	// idleStopMinutes it is stopped. An open WebSocket prevents both.
	f.s.Opened(sess.ID)
	f.clk.Add(time.Hour)
	_ = f.s.Reap(ctx)
	if got, _ := f.s.Get(ctx, sess.ID); got.State != protocol.SessionReady {
		t.Fatalf("websocket open: %+v", got)
	}
	f.s.Closed(sess.ID)
	f.clk.Add(2 * time.Minute)
	_ = f.s.Reap(ctx)
	if got, _ := f.s.Get(ctx, sess.ID); got.State != protocol.SessionIdle {
		t.Fatalf("idle: %+v", got)
	}
	f.clk.Add(29 * time.Minute)
	_ = f.s.Reap(ctx)
	if got, _ := f.s.Get(ctx, sess.ID); got.State != protocol.SessionStopped {
		t.Fatalf("idle stop: %+v", got)
	}
	if !f.fake.HasVolume("pg-ws-u1") {
		t.Fatal("idle stop removed the volume")
	}
	// Retention: 30 days after stopping, the volume goes.
	f.clk.Add(29 * 24 * time.Hour)
	_ = f.s.Reap(ctx)
	if !f.fake.HasVolume("pg-ws-u1") {
		t.Fatal("volume removed too early")
	}
	f.clk.Add(2 * 24 * time.Hour)
	if err := f.s.Reap(ctx); err != nil {
		t.Fatal(err)
	}
	if f.fake.HasVolume("pg-ws-u1") {
		t.Fatal("volume kept past retention")
	}
	if got, _ := f.s.Get(ctx, sess.ID); got.State != protocol.SessionRemoved {
		t.Fatalf("removed: %+v", got)
	}
	// A removed session is replaced by a new one.
	fresh, err := f.s.Ensure(ctx, "u1")
	if err != nil || fresh.ID == sess.ID {
		t.Fatalf("fresh %v %+v", err, fresh)
	}
}

func TestDockerStartFailures(t *testing.T) {
	f := newDockerFixture(t, nil)
	ctx := context.Background()
	f.fake.FailStart = errors.New("no such image")
	_, err := f.s.Ensure(ctx, "u1")
	var he *httpx.Error
	if !errors.As(err, &he) || he.Code != protocol.ErrSessionUnavailable {
		t.Fatalf("start failure: %v", err)
	}
	if len(f.fake.Live()) != 0 || !f.fake.HasVolume("pg-ws-u1") {
		t.Fatalf("failed container must be removed, volume kept: %+v", f.fake.Live())
	}
	sess, _ := f.s.ForUser(ctx, "u1")
	if sess.State != protocol.SessionFailed || sess.Failure == "" {
		t.Fatalf("failed state %+v", sess)
	}
	// Never healthy: fails after StartTimeout.
	f2 := newDockerFixture(t, func(o *Options) {
		o.Health = func(context.Context, *url.URL) error { return errors.New("refused") }
	})
	start := time.Now()
	if _, err := f2.s.Ensure(ctx, "u1"); !errors.As(err, &he) || he.Code != protocol.ErrSessionUnavailable {
		t.Fatalf("health timeout: %v", err)
	}
	if time.Since(start) < 250*time.Millisecond || len(f2.fake.Live()) != 0 {
		t.Fatalf("did not wait for health or left the container: %v", f2.fake.Live())
	}
	// A failed session is retried on the next Ensure.
	if sess, err := f.s.Ensure(ctx, "u1"); err != nil || sess.State != protocol.SessionReady {
		t.Fatalf("retry %v %+v", err, sess)
	}
}

func TestDockerRebuild(t *testing.T) {
	f := newDockerFixture(t, nil)
	ctx := context.Background()
	a, _ := f.s.Ensure(ctx, "u1")
	b, _ := f.s.Ensure(ctx, "u2")
	tokenA := f.container(t, a.ID).Spec.Env["PG_SESSION_TOKEN"]
	// b's container disappears while the gateway is down; an orphan
	// container with our labels appears.
	_ = f.fake.RemoveContainer(ctx, f.container(t, b.ID).ID, true)
	orphan := f.fake.AddContainer(docker.ContainerSpec{Name: "pg-session-zzz", Labels: map[string]string{docker.LabelSession: "zzz", docker.LabelRole: docker.RoleSession, docker.LabelUser: "u9"}}, "running")

	s2, err := New(ctx, f.opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(ctx, a.ID)
	if err != nil || !got.Usable() {
		t.Fatalf("running session after restart: %v %+v", err, got)
	}
	if got, err := s2.Authenticate(ctx, tokenA); err != nil || got.ID != a.ID {
		t.Fatalf("token after restart: %v", err)
	}
	if got, _ := s2.Get(ctx, b.ID); got.State != protocol.SessionStopped {
		t.Fatalf("vanished container: %+v", got)
	}
	if c, _ := f.fake.Container(orphan); !c.Removed {
		t.Fatal("orphan not removed")
	}
	if again, err := s2.Ensure(ctx, "u1"); err != nil || again.ID != a.ID || len(f.fake.Live()) != 1 {
		t.Fatalf("ensure after restart: %v %+v", err, again)
	}
}
