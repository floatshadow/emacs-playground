package sessions

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/zsy/emacs-playground/internal/docker"
	"github.com/zsy/emacs-playground/internal/protocol"
)

// Limits are per-session resource limits.
type Limits struct {
	CPUs   float64
	Memory int64
	Pids   int64
}

// Spec describes a session to create (backend.md §2.3).
type Spec struct {
	ID, UserID, Token  string
	Image, RemoteImage string
	Limits             Limits
	Network            string
	Runtime            string
	// Volume is the workspace volume mounted at /work.
	Volume string
	Env    map[string]string
}

// Handle identifies a backend session.
type Handle struct {
	ID                string
	UserID            string
	ContainerID       string
	RemoteContainerID string
	Volume            string
	Image             string
}

// Status is the backend's view of a session.
type Status struct {
	// Exists is false when the container is gone.
	Exists  bool
	Running bool
	// State is the container state (running, exited, …) or "unreachable".
	State     string
	OOMKilled bool
	ExitCode  int
}

// Backend provides session containers (backend.md §2.3).
type Backend interface {
	Create(ctx context.Context, spec Spec) (Handle, error)
	Start(ctx context.Context, h Handle) error
	Stop(ctx context.Context, h Handle, grace time.Duration) error
	Remove(ctx context.Context, h Handle, keepVolume bool) error
	Inspect(ctx context.Context, h Handle) (Status, error)
	// List returns the sessions the backend knows by label, for registry
	// rebuild.
	List(ctx context.Context) ([]Handle, error)
	// Endpoint is sessiond's base URL (http://pg-session-<id>:7000).
	Endpoint(h Handle) *url.URL
}

// ---- static -----------------------------------------------------------------

// StaticBackend is the local-mode backend: a single pre-existing session.
type StaticBackend struct {
	ID     string
	URL    *url.URL
	Client *http.Client
}

// NewStaticBackend returns the backend for the fixed session id at rawURL.
func NewStaticBackend(id, rawURL string) (*StaticBackend, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("sessions: local.sessionUrl %q is not an absolute URL", rawURL)
	}
	return &StaticBackend{ID: id, URL: u, Client: &http.Client{Timeout: 3 * time.Second}}, nil
}

func (b *StaticBackend) Create(_ context.Context, spec Spec) (Handle, error) {
	return Handle{ID: b.ID, UserID: spec.UserID}, nil
}
func (b *StaticBackend) Start(context.Context, Handle) error               { return nil }
func (b *StaticBackend) Stop(context.Context, Handle, time.Duration) error { return nil }
func (b *StaticBackend) Remove(context.Context, Handle, bool) error        { return nil }
func (b *StaticBackend) List(context.Context) ([]Handle, error) {
	return []Handle{{ID: b.ID, UserID: protocol.LocalUserID}}, nil
}
func (b *StaticBackend) Endpoint(Handle) *url.URL { c := *b.URL; return &c }

// Inspect reports Running when sessiond answers /healthz with 200.
func (b *StaticBackend) Inspect(ctx context.Context, h Handle) (Status, error) {
	if err := probeHealth(ctx, b.Client, b.Endpoint(h)); err != nil {
		return Status{Exists: true, State: "unreachable"}, nil
	}
	return Status{Exists: true, Running: true, State: "running"}, nil
}

// probeHealth GETs <endpoint>/healthz and wants 200.
func probeHealth(ctx context.Context, hc *http.Client, endpoint *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.JoinPath(protocol.PathHealthz).String(), nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", resp.Status)
	}
	return nil
}

// ---- docker -----------------------------------------------------------------

// DockerBackend creates one hardened container per session (server mode,
// experimental in slice 1).
type DockerBackend struct {
	Client docker.Client
	// Version is the gateway version, recorded in playground.version.
	Version string
	// Port is sessiond's port inside the container, default 7000.
	Port int
	// EndpointFor overrides Endpoint (tests).
	EndpointFor func(h Handle) *url.URL
}

// Extra labels beyond deployment.md §2.
const labelVolume = "playground.volume"

// ContainerName is the session container's name and DNS alias.
func ContainerName(id string) string { return "pg-session-" + id }

func (b *DockerBackend) Create(ctx context.Context, spec Spec) (Handle, error) {
	h := Handle{ID: spec.ID, UserID: spec.UserID, Volume: spec.Volume, Image: spec.Image}
	if err := b.Client.CreateVolume(ctx, spec.Volume, map[string]string{
		docker.LabelRole: docker.RoleWorkspace,
		docker.LabelUser: spec.UserID,
	}); err != nil {
		return h, err
	}
	// TODO(gateway, deployment.md §6): create the remote box from
	// spec.RemoteImage with the learner's public key.
	// TODO(gateway, deployment.md §1): refuse a session image whose
	// org.opencontainers.image.version has another major version than the
	// gateway (needs ImageInspect in docker.Client).
	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}
	env["PG_SESSION_ID"] = spec.ID
	env["PG_SESSION_TOKEN"] = spec.Token
	cs := docker.ContainerSpec{
		Name:     ContainerName(spec.ID),
		Hostname: ContainerName(spec.ID),
		Image:    spec.Image,
		Env:      env,
		Labels: map[string]string{
			docker.LabelSession: spec.ID,
			docker.LabelUser:    spec.UserID,
			docker.LabelVersion: b.Version,
			docker.LabelRole:    docker.RoleSession,
			labelVolume:         spec.Volume,
		},
		Network:   spec.Network,
		Aliases:   []string{ContainerName(spec.ID)},
		Volumes:   []docker.VolumeMount{{Volume: spec.Volume, Target: "/work"}},
		Runtime:   spec.Runtime,
		NanoCPUs:  int64(spec.Limits.CPUs * 1e9),
		Memory:    spec.Limits.Memory,
		PidsLimit: spec.Limits.Pids,
	}
	docker.Harden(&cs)
	id, err := b.Client.CreateContainer(ctx, cs)
	if err != nil {
		return h, err
	}
	h.ContainerID = id
	return h, nil
}

func (b *DockerBackend) Start(ctx context.Context, h Handle) error {
	return b.Client.StartContainer(ctx, h.ContainerID)
}

func (b *DockerBackend) Stop(ctx context.Context, h Handle, grace time.Duration) error {
	if h.ContainerID == "" {
		return nil
	}
	if err := b.Client.StopContainer(ctx, h.ContainerID, grace); err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}
	return nil
}

func (b *DockerBackend) Remove(ctx context.Context, h Handle, keepVolume bool) error {
	if h.ContainerID != "" {
		if err := b.Client.RemoveContainer(ctx, h.ContainerID, true); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return err
		}
	}
	if !keepVolume && h.Volume != "" {
		if err := b.Client.RemoveVolume(ctx, h.Volume); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return err
		}
	}
	return nil
}

func (b *DockerBackend) Inspect(ctx context.Context, h Handle) (Status, error) {
	if h.ContainerID == "" {
		return Status{}, nil
	}
	c, err := b.Client.InspectContainer(ctx, h.ContainerID)
	if errors.Is(err, docker.ErrNotFound) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	return Status{Exists: true, Running: c.Running, State: c.State, OOMKilled: c.OOMKilled, ExitCode: c.ExitCode}, nil
}

func (b *DockerBackend) List(ctx context.Context) ([]Handle, error) {
	cs, err := b.Client.ListContainers(ctx, map[string]string{docker.LabelRole: docker.RoleSession})
	if err != nil {
		return nil, err
	}
	out := make([]Handle, 0, len(cs))
	for _, c := range cs {
		id := c.Labels[docker.LabelSession]
		if id == "" {
			continue
		}
		out = append(out, Handle{ID: id, UserID: c.Labels[docker.LabelUser], ContainerID: c.ID, Volume: c.Labels[labelVolume], Image: c.Image})
	}
	return out, nil
}

func (b *DockerBackend) Endpoint(h Handle) *url.URL {
	if b.EndpointFor != nil {
		return b.EndpointFor(h)
	}
	port := b.Port
	if port == 0 {
		port = 7000
	}
	return &url.URL{Scheme: "http", Host: fmt.Sprintf("%s:%d", ContainerName(h.ID), port)}
}
