package docker

import (
	"context"
	"errors"
	"fmt"
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
)

// ErrNotFound is returned for missing containers and volumes.
var ErrNotFound = errors.New("docker: not found")

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
	spec.Ulimits = []Ulimit{{Name: "nofile", Soft: 65536, Hard: 65536}, {Name: "nproc", Soft: 2048, Hard: 2048}}
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

var _ Client = (*Engine)(nil)
