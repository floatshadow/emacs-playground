package docker

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Labels on everything the gateway creates (deployment.md §2).
const (
	LabelSession = "playground.session"
	LabelUser    = "playground.user"
	LabelVersion = "playground.version"
	// LabelRole is session, remote-box or workspace.
	LabelRole = "playground.role"
)

// Roles.
const (
	RoleSession   = "session"
	RoleRemoteBox = "remote-box"
	RoleWorkspace = "workspace"
	// RoleNetwork labels a session's own network.
	RoleNetwork = "network"
)

// ErrNotFound is returned for missing containers, volumes and networks.
var ErrNotFound = errors.New("docker: not found")

// ErrConflict is returned when a name is already taken.
var ErrConflict = errors.New("docker: name in use")

// ErrSubnetTaken is returned by CreateNetwork when the requested subnet
// overlaps another network's.
var ErrSubnetTaken = errors.New("docker: subnet overlaps another network")

// NetworkSpec describes a bridge network to create.
type NetworkSpec struct {
	Name   string
	Labels map[string]string
	// Internal networks have no route out of the host.
	Internal bool
	// Subnet is the IPv4 subnet; the zero value lets the daemon pick one
	// from its default-address-pools.
	Subnet netip.Prefix
}

// Network is a listed network.
type Network struct {
	ID       string
	Name     string
	Labels   map[string]string
	Internal bool
	Subnets  []netip.Prefix
}

// VolumeMount mounts a named volume.
type VolumeMount struct {
	Volume   string
	Target   string
	ReadOnly bool
}

// Ulimit is a resource limit.
type Ulimit struct {
	Name       string
	Soft, Hard int64
}

// ContainerSpec describes a container to create.
type ContainerSpec struct {
	Name     string
	Image    string
	Hostname string
	User     string
	Cmd      []string
	Env      map[string]string
	Labels   map[string]string
	// Network is the only network the container joins; Aliases are its DNS
	// names there.
	Network string
	Aliases []string
	Volumes []VolumeMount
	// Tmpfs maps a path to mount options ("rw,exec,size=1g").
	Tmpfs          map[string]string
	ReadOnlyRootfs bool
	CapDrop        []string
	SecurityOpt    []string
	Runtime        string
	Init           bool
	NanoCPUs       int64
	Memory         int64
	MemorySwap     int64
	PidsLimit      int64
	Ulimits        []Ulimit
	// StopTimeout in seconds (SIGTERM to SIGKILL).
	StopTimeout int
}

// Container is the inspected or listed state of a container.
type Container struct {
	ID      string
	Name    string
	Image   string
	Labels  map[string]string
	State   string // created, running, paused, restarting, removing, exited, dead
	Running bool
	// OOMKilled and ExitCode are only known from Inspect.
	OOMKilled  bool
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
}

// Volume is a listed volume.
type Volume struct {
	Name      string
	Labels    map[string]string
	CreatedAt time.Time
}

// Client is the subset of the Engine API the gateway uses.
type Client interface {
	Ping(ctx context.Context) error
	CreateContainer(ctx context.Context, spec ContainerSpec) (id string, err error)
	StartContainer(ctx context.Context, id string) error
	// StopContainer sends SIGTERM and SIGKILL after grace.
	StopContainer(ctx context.Context, id string, grace time.Duration) error
	// RemoveContainer removes a container (force kills a running one); it
	// never removes volumes.
	RemoveContainer(ctx context.Context, id string, force bool) error
	InspectContainer(ctx context.Context, id string) (Container, error)
	// ListContainers lists containers in any state carrying every label.
	ListContainers(ctx context.Context, labels map[string]string) ([]Container, error)
	// CreateVolume creates a volume or returns the existing one.
	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	RemoveVolume(ctx context.Context, name string) error
	ListVolumes(ctx context.Context, labels map[string]string) ([]Volume, error)
	// CreateNetwork creates a bridge network: ErrConflict when the name is
	// taken, ErrSubnetTaken when spec.Subnet overlaps another network.
	CreateNetwork(ctx context.Context, spec NetworkSpec) (id string, err error)
	// RemoveNetwork removes a network (name or id).
	RemoveNetwork(ctx context.Context, name string) error
	// ConnectNetwork attaches a container (name, id or id prefix) to a
	// network with DNS aliases; one already attached is left as it is.
	ConnectNetwork(ctx context.Context, network, container string, aliases []string) error
	// DisconnectNetwork detaches a container; one not attached is fine.
	DisconnectNetwork(ctx context.Context, network, container string) error
	// ListNetworks lists the networks carrying every label (all for nil).
	ListNetworks(ctx context.Context, labels map[string]string) ([]Network, error)
	Close() error
}

// Harden applies the session container profile of deployment.md §9.2 to
// spec. Limits (CPUs, memory, pids) are set by the caller.
func Harden(spec *ContainerSpec) {
	spec.User = "1000:1000"
	spec.ReadOnlyRootfs = true
	if spec.Tmpfs == nil {
		spec.Tmpfs = map[string]string{}
	}
	// Builds need exec in /tmp; sessiond creates /run/playground as learner.
	spec.Tmpfs["/tmp"] = "rw,exec,nosuid,nodev,size=1g,mode=1777"
	spec.Tmpfs["/run"] = "rw,noexec,nosuid,nodev,size=64m,mode=1777"
	spec.Tmpfs["/home/learner/.cache"] = "rw,exec,nosuid,nodev,size=2g,uid=1000,gid=1000,mode=0755"
	spec.CapDrop = []string{"ALL"}
	spec.SecurityOpt = []string{"no-new-privileges:true"}
	spec.Init = true
	if spec.MemorySwap == 0 {
		spec.MemorySwap = spec.Memory // no swap beyond the memory limit
	}
	// No nproc ulimit: RLIMIT_NPROC counts every process and thread of the
	// uid on the whole host, and every session runs as uid 1000 (no user
	// namespace with runc), so it would be one budget shared by all
	// sessions. The cgroup pids limit (PidsLimit) bounds each container.
	spec.Ulimits = []Ulimit{{Name: "nofile", Soft: 65536, Hard: 65536}}
	if spec.StopTimeout == 0 {
		spec.StopTimeout = 20
	}
}

// CreateOptions converts spec to the Engine API create request.
func CreateOptions(spec ContainerSpec) client.ContainerCreateOptions {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	cfg := &container.Config{
		Image:    spec.Image,
		Hostname: spec.Hostname,
		User:     spec.User,
		Env:      env,
		Cmd:      spec.Cmd,
		Labels:   spec.Labels,
	}
	if spec.StopTimeout > 0 {
		t := spec.StopTimeout
		cfg.StopTimeout = &t
	}
	hc := &container.HostConfig{
		NetworkMode:    container.NetworkMode(spec.Network),
		ReadonlyRootfs: spec.ReadOnlyRootfs,
		CapDrop:        spec.CapDrop,
		SecurityOpt:    spec.SecurityOpt,
		Tmpfs:          spec.Tmpfs,
		Runtime:        spec.Runtime,
		RestartPolicy:  container.RestartPolicy{Name: container.RestartPolicyDisabled},
		Resources: container.Resources{
			NanoCPUs:   spec.NanoCPUs,
			Memory:     spec.Memory,
			MemorySwap: spec.MemorySwap,
		},
	}
	if spec.Init {
		t := true
		hc.Init = &t
	}
	if spec.PidsLimit > 0 {
		p := spec.PidsLimit
		hc.PidsLimit = &p
	}
	for _, u := range spec.Ulimits {
		hc.Ulimits = append(hc.Ulimits, &container.Ulimit{Name: u.Name, Soft: u.Soft, Hard: u.Hard})
	}
	for _, v := range spec.Volumes {
		hc.Mounts = append(hc.Mounts, mount.Mount{Type: mount.TypeVolume, Source: v.Volume, Target: v.Target, ReadOnly: v.ReadOnly})
	}
	var nc *network.NetworkingConfig
	if spec.Network != "" {
		nc = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			spec.Network: {Aliases: spec.Aliases},
		}}
	}
	return client.ContainerCreateOptions{Name: spec.Name, Config: cfg, HostConfig: hc, NetworkingConfig: nc}
}

// Engine implements Client with the official Engine API client.
type Engine struct{ c *client.Client }

// New connects to host ("tcp://socket-proxy:2375", "unix:///var/run/docker.sock");
// empty uses DOCKER_HOST and the platform default. The API version is
// negotiated on the first request.
func New(host string) (*Engine, error) {
	opts := []client.Opt{client.FromEnv}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	c, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	return &Engine{c: c}, nil
}

// NewWithClient wraps an existing Engine API client (tests).
func NewWithClient(c *client.Client) *Engine { return &Engine{c: c} }

// isNotFound recognises Engine 404s.
func isNotFound(err error) bool { return cerrdefs.IsNotFound(err) }

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return fmt.Errorf("docker: %s: %w", op, ErrNotFound)
	}
	if cerrdefs.IsConflict(err) || strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("docker: %s: %w: %v", op, ErrConflict, err)
	}
	return fmt.Errorf("docker: %s: %w", op, err)
}

func (e *Engine) Close() error { return e.c.Close() }

func (e *Engine) Ping(ctx context.Context) error {
	_, err := e.c.Ping(ctx, client.PingOptions{})
	return wrap("ping", err)
}

func (e *Engine) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	res, err := e.c.ContainerCreate(ctx, CreateOptions(spec))
	if err != nil {
		return "", wrap("create "+spec.Name, err)
	}
	return res.ID, nil
}

func (e *Engine) StartContainer(ctx context.Context, id string) error {
	_, err := e.c.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return wrap("start "+id, err)
}

func (e *Engine) StopContainer(ctx context.Context, id string, grace time.Duration) error {
	t := int(grace / time.Second)
	_, err := e.c.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &t})
	return wrap("stop "+id, err)
}

func (e *Engine) RemoveContainer(ctx context.Context, id string, force bool) error {
	_, err := e.c.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: force})
	return wrap("remove "+id, err)
}

func (e *Engine) InspectContainer(ctx context.Context, id string) (Container, error) {
	res, err := e.c.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return Container{}, wrap("inspect "+id, err)
	}
	r := res.Container
	out := Container{ID: r.ID, Name: strings.TrimPrefix(r.Name, "/"), Image: r.Image}
	if r.Config != nil {
		out.Labels = r.Config.Labels
		out.Image = r.Config.Image
	}
	if s := r.State; s != nil {
		out.State = string(s.Status)
		out.Running = s.Running
		out.OOMKilled = s.OOMKilled
		out.ExitCode = s.ExitCode
		out.StartedAt, _ = time.Parse(time.RFC3339Nano, s.StartedAt)
		out.FinishedAt, _ = time.Parse(time.RFC3339Nano, s.FinishedAt)
	}
	return out, nil
}

func labelFilters(labels map[string]string) client.Filters {
	f := client.Filters{}
	for k, v := range labels {
		f.Add("label", k+"="+v)
	}
	return f
}

func (e *Engine) ListContainers(ctx context.Context, labels map[string]string) ([]Container, error) {
	res, err := e.c.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: labelFilters(labels)})
	if err != nil {
		return nil, wrap("list containers", err)
	}
	out := make([]Container, 0, len(res.Items))
	for _, s := range res.Items {
		name := ""
		if len(s.Names) > 0 {
			name = strings.TrimPrefix(s.Names[0], "/")
		}
		out = append(out, Container{
			ID: s.ID, Name: name, Image: s.Image, Labels: s.Labels,
			State: string(s.State), Running: s.State == container.StateRunning,
		})
	}
	return out, nil
}

func (e *Engine) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	_, err := e.c.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Labels: labels})
	return wrap("create volume "+name, err)
}

func (e *Engine) RemoveVolume(ctx context.Context, name string) error {
	_, err := e.c.VolumeRemove(ctx, name, client.VolumeRemoveOptions{})
	return wrap("remove volume "+name, err)
}

func (e *Engine) ListVolumes(ctx context.Context, labels map[string]string) ([]Volume, error) {
	res, err := e.c.VolumeList(ctx, client.VolumeListOptions{Filters: labelFilters(labels)})
	if err != nil {
		return nil, wrap("list volumes", err)
	}
	out := make([]Volume, 0, len(res.Items))
	for _, v := range res.Items {
		created, _ := time.Parse(time.RFC3339Nano, v.CreatedAt)
		out = append(out, Volume{Name: v.Name, Labels: v.Labels, CreatedAt: created})
	}
	return out, nil
}

func (e *Engine) CreateNetwork(ctx context.Context, spec NetworkSpec) (string, error) {
	opts := client.NetworkCreateOptions{Driver: "bridge", Internal: spec.Internal, Labels: spec.Labels}
	if spec.Subnet.IsValid() {
		opts.IPAM = &network.IPAM{Driver: "default", Config: []network.IPAMConfig{{Subnet: spec.Subnet}}}
	}
	res, err := e.c.NetworkCreate(ctx, spec.Name, opts)
	if err != nil {
		// "invalid pool request: Pool overlaps with other one on this
		// address space"
		if spec.Subnet.IsValid() && strings.Contains(strings.ToLower(err.Error()), "overlaps") {
			return "", fmt.Errorf("docker: create network %s (%s): %w", spec.Name, spec.Subnet, ErrSubnetTaken)
		}
		return "", wrap("create network "+spec.Name, err)
	}
	return res.ID, nil
}

func (e *Engine) RemoveNetwork(ctx context.Context, name string) error {
	_, err := e.c.NetworkRemove(ctx, name, client.NetworkRemoveOptions{})
	return wrap("remove network "+name, err)
}

// attached reports whether container (name, id or id prefix) has an
// endpoint on the network.
func (e *Engine) attached(ctx context.Context, netName, ctr string) (bool, error) {
	res, err := e.c.NetworkInspect(ctx, netName, client.NetworkInspectOptions{})
	if err != nil {
		return false, wrap("inspect network "+netName, err)
	}
	for id, ep := range res.Network.Containers {
		if ep.Name == ctr || (len(ctr) >= 12 && strings.HasPrefix(id, ctr)) {
			return true, nil
		}
	}
	return false, nil
}

func (e *Engine) ConnectNetwork(ctx context.Context, netName, ctr string, aliases []string) error {
	_, err := e.c.NetworkConnect(ctx, netName, client.NetworkConnectOptions{
		Container:      ctr,
		EndpointConfig: &network.EndpointSettings{Aliases: aliases},
	})
	if err == nil {
		return nil
	}
	// "endpoint with name … already exists in network …"
	if ok, ierr := e.attached(ctx, netName, ctr); ierr == nil && ok {
		return nil
	}
	return wrap("connect "+ctr+" to "+netName, err)
}

func (e *Engine) DisconnectNetwork(ctx context.Context, netName, ctr string) error {
	_, err := e.c.NetworkDisconnect(ctx, netName, client.NetworkDisconnectOptions{Container: ctr, Force: true})
	if err == nil || isNotFound(err) {
		return nil
	}
	// "container … is not connected to network …", or the network is gone.
	if ok, ierr := e.attached(ctx, netName, ctr); (ierr == nil && !ok) || errors.Is(ierr, ErrNotFound) {
		return nil
	}
	return wrap("disconnect "+ctr+" from "+netName, err)
}

func (e *Engine) ListNetworks(ctx context.Context, labels map[string]string) ([]Network, error) {
	res, err := e.c.NetworkList(ctx, client.NetworkListOptions{Filters: labelFilters(labels)})
	if err != nil {
		return nil, wrap("list networks", err)
	}
	out := make([]Network, 0, len(res.Items))
	for _, n := range res.Items {
		nw := Network{ID: n.ID, Name: n.Name, Labels: n.Labels, Internal: n.Internal}
		for _, c := range n.IPAM.Config {
			if c.Subnet.IsValid() {
				nw.Subnets = append(nw.Subnets, c.Subnet)
			}
		}
		out = append(out, nw)
	}
	return out, nil
}

var _ Client = (*Engine)(nil)
