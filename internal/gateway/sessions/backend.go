package sessions

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
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
	ID, UserID, Token string
	// ProxySecret is required by sessiond on every request but /healthz
	// (PG_PROXY_SECRET).
	ProxySecret        string
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

// Networker is implemented by backends that give every session its own
// network (DockerBackend with Isolate).
type Networker interface {
	// Attach joins the gateway to a running session's network again (a new
	// gateway container after an upgrade; registry rebuild).
	Attach(ctx context.Context, h Handle) error
	// Prune removes the session networks of sessions for which keep
	// (called after listing, so a session created meanwhile is kept)
	// returns false.
	Prune(ctx context.Context, keep func(id string) bool) error
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

	// Isolate gives every session its own bridge network, NetworkName(id),
	// instead of Spec.Network: the session container is its only member
	// besides the gateway, which joins it under GatewayAlias, so sessions
	// cannot reach each other (architecture.md §1, deployment.md §9.1).
	Isolate bool
	// Gateway is the gateway's own container (name, id or id prefix).
	Gateway string
	// GatewayAlias is the gateway's DNS name on session networks, default
	// "gateway" (PG_GATEWAY_URL).
	GatewayAlias string
	// Internal makes session networks internal: no route out of the host
	// (sessions.egress none and registries).
	// TODO(gateway, deployment.md §9.2): with egress registries, connect the
	// egress proxy to each session network as the remote box will be
	// (TODO(gateway, deployment.md §6) in Create).
	Internal bool
	// SubnetPool is where session networks take their /28 subnets; the
	// zero value lets the daemon choose from its default-address-pools.
	SubnetPool netip.Prefix

	netMu sync.Mutex
}

// NetworkName is a session's own network (DockerBackend.Isolate).
func NetworkName(id string) string { return "pg-net-" + id }

// SessionSubnetBits is the size of a session network: /28, 14 addresses
// for the session, the gateway and later the remote box and egress proxy.
const SessionSubnetBits = 28

func (b *DockerBackend) gatewayAlias() string {
	if b.GatewayAlias == "" {
		return "gateway"
	}
	return b.GatewayAlias
}

// ensureNetwork creates the session's network (reusing one left behind)
// and attaches the gateway to it.
func (b *DockerBackend) ensureNetwork(ctx context.Context, id, userID string) (string, error) {
	if b.Gateway == "" {
		return "", errors.New("sessions: per-session networks need the gateway's container (sessions.gatewayContainer)")
	}
	name := NetworkName(id)
	b.netMu.Lock()
	defer b.netMu.Unlock()
	taken := map[netip.Prefix]bool{}
	for attempt := 0; ; attempt++ {
		subnet, err := b.freeSubnet(ctx, taken)
		if err != nil {
			return "", err
		}
		_, err = b.Client.CreateNetwork(ctx, docker.NetworkSpec{
			Name: name, Internal: b.Internal, Subnet: subnet,
			Labels: map[string]string{
				docker.LabelRole:    docker.RoleNetwork,
				docker.LabelSession: id,
				docker.LabelUser:    userID,
				docker.LabelVersion: b.Version,
			},
		})
		if err == nil || errors.Is(err, docker.ErrConflict) {
			break
		}
		// Taken since the list (another daemon user): try the next one.
		if errors.Is(err, docker.ErrSubnetTaken) && subnet.IsValid() && attempt < 16 {
			taken[subnet] = true
			continue
		}
		return "", err
	}
	if err := b.Client.ConnectNetwork(ctx, name, b.Gateway, []string{b.gatewayAlias()}); err != nil {
		_ = b.Client.RemoveNetwork(context.WithoutCancel(ctx), name)
		return "", fmt.Errorf("sessions: attaching the gateway (%s) to %s: %w", b.Gateway, name, err)
	}
	return name, nil
}

// freeSubnet returns the first /28 of SubnetPool that overlaps no existing
// network and is not in taken; the zero prefix without a pool.
func (b *DockerBackend) freeSubnet(ctx context.Context, taken map[netip.Prefix]bool) (netip.Prefix, error) {
	pool := b.SubnetPool
	if !pool.IsValid() {
		return netip.Prefix{}, nil
	}
	if !pool.Addr().Is4() || pool.Bits() > SessionSubnetBits {
		return netip.Prefix{}, fmt.Errorf("sessions: sessions.subnetPool %s is not an IPv4 range of /%d or larger", pool, SessionSubnetBits)
	}
	nets, err := b.Client.ListNetworks(ctx, nil)
	if err != nil {
		return netip.Prefix{}, err
	}
	var used []netip.Prefix
	for _, n := range nets {
		used = append(used, n.Subnets...)
	}
	pool = pool.Masked()
	base := pool.Addr().As4()
	start := binary.BigEndian.Uint32(base[:])
	count := uint32(1) << (SessionSubnetBits - pool.Bits())
	step := uint32(1) << (32 - SessionSubnetBits)
next:
	for i := uint32(0); i < count; i++ {
		var a [4]byte
		binary.BigEndian.PutUint32(a[:], start+i*step)
		p := netip.PrefixFrom(netip.AddrFrom4(a), SessionSubnetBits)
		if taken[p] {
			continue
		}
		for _, u := range used {
			if u.Overlaps(p) {
				continue next
			}
		}
		return p, nil
	}
	return netip.Prefix{}, fmt.Errorf("sessions: no free /%d left in sessions.subnetPool %s", SessionSubnetBits, b.SubnetPool)
}

// removeNetwork detaches the gateway and removes the session's network;
// best effort (Prune retries leftovers).
func (b *DockerBackend) removeNetwork(ctx context.Context, id string) error {
	name := NetworkName(id)
	if b.Gateway != "" {
		if err := b.Client.DisconnectNetwork(ctx, name, b.Gateway); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return err
		}
	}
	if err := b.Client.RemoveNetwork(ctx, name); err != nil && !errors.Is(err, docker.ErrNotFound) {
		return err
	}
	return nil
}

// Attach implements Networker.
func (b *DockerBackend) Attach(ctx context.Context, h Handle) error {
	if !b.Isolate || b.Gateway == "" {
		return nil
	}
	return b.Client.ConnectNetwork(ctx, NetworkName(h.ID), b.Gateway, []string{b.gatewayAlias()})
}

// Prune implements Networker: session networks (by label) of sessions keep
// rejects are removed.
func (b *DockerBackend) Prune(ctx context.Context, keep func(id string) bool) error {
	b.netMu.Lock()
	defer b.netMu.Unlock()
	nets, err := b.Client.ListNetworks(ctx, map[string]string{docker.LabelRole: docker.RoleNetwork})
	if err != nil {
		return err
	}
	var errs []error
	for _, n := range nets {
		id := n.Labels[docker.LabelSession]
		if id == "" || n.Name != NetworkName(id) || keep(id) {
			continue
		}
		if err := b.removeNetwork(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
	if spec.ProxySecret != "" {
		env["PG_PROXY_SECRET"] = spec.ProxySecret
	}
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
	if b.Isolate {
		net, err := b.ensureNetwork(ctx, spec.ID, spec.UserID)
		if err != nil {
			return h, err
		}
		cs.Network = net
	}
	id, err := b.Client.CreateContainer(ctx, cs)
	if err != nil {
		if b.Isolate {
			_ = b.removeNetwork(context.WithoutCancel(ctx), spec.ID)
		}
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
	if b.Isolate && h.ID != "" {
		// Best effort: a network left behind is reused by the next Create
		// and removed by Prune.
		_ = b.removeNetwork(ctx, h.ID)
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
