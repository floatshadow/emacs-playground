// Package dockertest provides an in-memory fake of docker.Client for tests
// of the session manager's docker backend.
package dockertest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/zsy/emacs-playground/internal/docker"
)

// FakeContainer is a container held by Fake.
type FakeContainer struct {
	ID      string
	Spec    docker.ContainerSpec
	State   string // created, running, exited
	Removed bool
	// OOMKilled is reported by InspectContainer once the container exits.
	OOMKilled bool
}

// Fake is an in-memory docker.Client. All methods are safe for concurrent
// use. Calls records the method names in order.
type Fake struct {
	mu         sync.Mutex
	next       int
	containers map[string]*FakeContainer
	volumes    map[string]docker.Volume
	Calls      []string

	// FailCreate, FailStart make the next CreateContainer or StartContainer
	// call fail (consumed on use).
	FailCreate error
	FailStart  error
	// OnStart is called after a container starts (tests start a fake
	// sessiond here).
	OnStart func(spec docker.ContainerSpec)
	Now     func() time.Time
}

// New returns an empty fake.
func New() *Fake {
	return &Fake{containers: map[string]*FakeContainer{}, volumes: map[string]docker.Volume{}, Now: time.Now}
}

func (f *Fake) call(name string) { f.Calls = append(f.Calls, name) }

// Ping implements docker.Client.
func (f *Fake) Ping(context.Context) error { return nil }

// Close implements docker.Client.
func (f *Fake) Close() error { return nil }

// CreateContainer implements docker.Client.
func (f *Fake) CreateContainer(_ context.Context, spec docker.ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.call("create " + spec.Name)
	if err := f.FailCreate; err != nil {
		f.FailCreate = nil
		return "", err
	}
	for _, c := range f.containers {
		if c.Spec.Name == spec.Name && !c.Removed {
			return "", fmt.Errorf("conflict: name %s in use", spec.Name)
		}
	}
	for _, v := range spec.Volumes {
		if _, ok := f.volumes[v.Volume]; !ok {
			// Docker creates missing named volumes implicitly.
			f.volumes[v.Volume] = docker.Volume{Name: v.Volume, Labels: map[string]string{}, CreatedAt: f.Now()}
		}
	}
	f.next++
	id := fmt.Sprintf("c%04d", f.next)
	f.containers[id] = &FakeContainer{ID: id, Spec: spec, State: "created"}
	return id, nil
}

func (f *Fake) get(id string) (*FakeContainer, error) {
	c, ok := f.containers[id]
	if !ok || c.Removed {
		return nil, fmt.Errorf("container %s: %w", id, docker.ErrNotFound)
	}
	return c, nil
}

// StartContainer implements docker.Client.
func (f *Fake) StartContainer(_ context.Context, id string) error {
	f.mu.Lock()
	f.call("start " + id)
	if err := f.FailStart; err != nil {
		f.FailStart = nil
		f.mu.Unlock()
		return err
	}
	c, err := f.get(id)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	c.State = "running"
	spec := c.Spec
	hook := f.OnStart
	f.mu.Unlock()
	if hook != nil {
		hook(spec)
	}
	return nil
}

// StopContainer implements docker.Client.
func (f *Fake) StopContainer(_ context.Context, id string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.call("stop " + id)
	c, err := f.get(id)
	if err != nil {
		return err
	}
	c.State = "exited"
	return nil
}

// RemoveContainer implements docker.Client.
func (f *Fake) RemoveContainer(_ context.Context, id string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.call("remove " + id)
	c, err := f.get(id)
	if err != nil {
		return err
	}
	if c.State == "running" && !force {
		return fmt.Errorf("container %s is running", id)
	}
	c.Removed = true
	return nil
}

// InspectContainer implements docker.Client.
func (f *Fake) InspectContainer(_ context.Context, id string) (docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(id)
	if err != nil {
		return docker.Container{}, err
	}
	return f.view(c), nil
}

func (f *Fake) view(c *FakeContainer) docker.Container {
	return docker.Container{
		ID: c.ID, Name: c.Spec.Name, Image: c.Spec.Image, Labels: c.Spec.Labels,
		State: c.State, Running: c.State == "running", OOMKilled: c.OOMKilled && c.State != "running",
	}
}

func matches(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// ListContainers implements docker.Client.
func (f *Fake) ListContainers(_ context.Context, labels map[string]string) ([]docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []docker.Container
	for _, c := range f.containers {
		if !c.Removed && matches(c.Spec.Labels, labels) {
			out = append(out, f.view(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// CreateVolume implements docker.Client.
func (f *Fake) CreateVolume(_ context.Context, name string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.call("volume create " + name)
	if _, ok := f.volumes[name]; !ok {
		f.volumes[name] = docker.Volume{Name: name, Labels: labels, CreatedAt: f.Now()}
	}
	return nil
}

// RemoveVolume implements docker.Client.
func (f *Fake) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.call("volume remove " + name)
	if _, ok := f.volumes[name]; !ok {
		return fmt.Errorf("volume %s: %w", name, docker.ErrNotFound)
	}
	for _, c := range f.containers {
		if c.Removed {
			continue
		}
		for _, v := range c.Spec.Volumes {
			if v.Volume == name {
				return fmt.Errorf("volume %s is in use by %s", name, c.ID)
			}
		}
	}
	delete(f.volumes, name)
	return nil
}

// ListVolumes implements docker.Client.
func (f *Fake) ListVolumes(_ context.Context, labels map[string]string) ([]docker.Volume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []docker.Volume
	for _, v := range f.volumes {
		if matches(v.Labels, labels) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Container returns a copy of a container by id (removed ones included).
func (f *Fake) Container(id string) (FakeContainer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return FakeContainer{}, false
	}
	return *c, true
}

// Live returns the containers that are not removed.
func (f *Fake) Live() []FakeContainer {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []FakeContainer
	for _, c := range f.containers {
		if !c.Removed {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// HasVolume reports whether a volume exists.
func (f *Fake) HasVolume(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.volumes[name]
	return ok
}

// Crash marks a running container as exited (optionally OOM-killed), as if
// it died on its own.
func (f *Fake) Crash(id string, oom bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.containers[id]; ok {
		c.State, c.OOMKilled = "exited", oom
	}
}

// AddContainer injects a container (registry rebuild tests).
func (f *Fake) AddContainer(spec docker.ContainerSpec, state string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprintf("c%04d", f.next)
	f.containers[id] = &FakeContainer{ID: id, Spec: spec, State: state}
	return id
}

var _ docker.Client = (*Fake)(nil)
