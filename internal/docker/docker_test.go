package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func TestHardenAndCreateOptions(t *testing.T) {
	spec := ContainerSpec{
		Name: "pg-session-s1", Image: "ghcr.io/floatshadow/emacs-playground/session:v1",
		Env:     map[string]string{"PG_SESSION_ID": "s1", "PG_SESSION_TOKEN": "tok"},
		Labels:  map[string]string{LabelSession: "s1", LabelRole: RoleSession},
		Network: "emacs-playground_sessions", Aliases: []string{"pg-session-s1"},
		Volumes:  []VolumeMount{{Volume: "pg-ws-u1", Target: "/work"}},
		Runtime:  "runsc",
		NanoCPUs: 2e9, Memory: 3 << 30, PidsLimit: 1024,
	}
	Harden(&spec)
	o := CreateOptions(spec)
	hc := o.HostConfig
	if !hc.ReadonlyRootfs || len(hc.CapDrop) != 1 || hc.CapDrop[0] != "ALL" || hc.Privileged {
		t.Fatalf("hardening: %+v", hc)
	}
	if strings.Join(hc.SecurityOpt, ",") != "no-new-privileges:true" {
		t.Fatalf("security opts %v", hc.SecurityOpt)
	}
	if hc.Memory != 3<<30 || hc.MemorySwap != 3<<30 || hc.NanoCPUs != 2e9 || hc.PidsLimit == nil || *hc.PidsLimit != 1024 {
		t.Fatalf("limits %+v", hc.Resources)
	}
	if hc.Runtime != "runsc" || hc.NetworkMode != "emacs-playground_sessions" || hc.Init == nil || !*hc.Init {
		t.Fatalf("runtime/network/init %+v", hc)
	}
	if len(hc.PortBindings) != 0 || hc.PublishAllPorts || len(hc.Binds) != 0 {
		t.Fatal("no ports and no host paths")
	}
	if !strings.Contains(hc.Tmpfs["/tmp"], "size=1g") || !strings.Contains(hc.Tmpfs["/run"], "size=64m") || !strings.Contains(hc.Tmpfs["/home/learner/.cache"], "size=2g") {
		t.Fatalf("tmpfs %v", hc.Tmpfs)
	}
	if len(hc.Mounts) != 1 || hc.Mounts[0].Source != "pg-ws-u1" || hc.Mounts[0].Target != "/work" || hc.Mounts[0].Type != "volume" {
		t.Fatalf("mounts %+v", hc.Mounts)
	}
	ul := map[string]int64{}
	for _, u := range hc.Ulimits {
		ul[u.Name] = u.Hard
	}
	if ul["nofile"] != 65536 {
		t.Fatalf("ulimits %v", ul)
	}
	// RLIMIT_NPROC is per uid across the host: one budget for every session.
	if _, ok := ul["nproc"]; ok {
		t.Fatalf("nproc ulimit set: %v", ul)
	}
	if o.Config.User != "1000:1000" || o.Config.Env[0] != "PG_SESSION_ID=s1" || o.Config.StopTimeout == nil {
		t.Fatalf("config %+v", o.Config)
	}
	if ep := o.NetworkingConfig.EndpointsConfig["emacs-playground_sessions"]; ep == nil || ep.Aliases[0] != "pg-session-s1" {
		t.Fatalf("network %+v", o.NetworkingConfig)
	}
}

// fakeEngine emulates the Engine API endpoints Engine uses.
type fakeEngine struct {
	mu      sync.Mutex
	created map[string]json.RawMessage
	reqs    []string
}

var versionPrefix = regexp.MustCompile(`^/v[0-9.]+`)

func (f *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := versionPrefix.ReplaceAllString(r.URL.Path, "")
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Method+" "+path)
	f.mu.Unlock()
	w.Header().Set("API-Version", "1.47")
	w.Header().Set("Content-Type", "application/json")
	notFound := func(what string) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"message":"No such `+what+`"}`)
	}
	switch {
	case path == "/_ping":
		_, _ = io.WriteString(w, "OK")
	case r.Method == "POST" && path == "/containers/create":
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.created[r.URL.Query().Get("name")] = body
		f.mu.Unlock()
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"Id":"abc123","Warnings":[]}`)
	case r.Method == "POST" && path == "/containers/abc123/start", r.Method == "POST" && path == "/containers/abc123/stop":
		w.WriteHeader(204)
	case r.Method == "DELETE" && path == "/containers/abc123":
		w.WriteHeader(204)
	case r.Method == "GET" && path == "/containers/abc123/json":
		_, _ = io.WriteString(w, `{"Id":"abc123","Name":"/pg-session-s1","Image":"sha256:x","State":{"Status":"running","Running":true,"OOMKilled":false,"ExitCode":0,"StartedAt":"2026-10-26T12:00:00.5Z"},"Config":{"Image":"session:v1","Labels":{"playground.session":"s1"}}}`)
	case r.Method == "GET" && strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/json") && path != "/containers/json":
		notFound("container")
	case r.Method == "GET" && path == "/containers/json":
		if r.URL.Query().Get("all") != "1" || !strings.Contains(r.URL.Query().Get("filters"), "playground.role=session") {
			w.WriteHeader(400)
			return
		}
		_, _ = io.WriteString(w, `[{"Id":"abc123","Names":["/pg-session-s1"],"Image":"session:v1","Labels":{"playground.session":"s1","playground.role":"session"},"State":"exited"}]`)
	case r.Method == "POST" && path == "/volumes/create":
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"Name":"pg-ws-u1","Driver":"local","Labels":{},"Mountpoint":"/x","Scope":"local"}`)
	case r.Method == "GET" && path == "/volumes":
		_, _ = io.WriteString(w, `{"Volumes":[{"Name":"pg-ws-u1","Driver":"local","Labels":{"playground.role":"workspace"},"Mountpoint":"/x","Scope":"local","CreatedAt":"2026-10-01T00:00:00Z"}],"Warnings":[]}`)
	case r.Method == "DELETE" && path == "/volumes/pg-ws-u1":
		w.WriteHeader(204)
	case r.Method == "DELETE" && strings.HasPrefix(path, "/volumes/"):
		notFound("volume")
	default:
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"message":"unexpected `+r.Method+" "+path+`"}`)
	}
}

func TestEngineAgainstFakeAPI(t *testing.T) {
	fe := &fakeEngine{created: map[string]json.RawMessage{}}
	srv := httptest.NewServer(fe)
	defer srv.Close()
	c, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")), client.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	e := NewWithClient(c)
	defer e.Close()
	ctx := context.Background()
	if err := e.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	spec := ContainerSpec{Name: "pg-session-s1", Image: "session:v1", Network: "net", Memory: 1 << 30, PidsLimit: 100,
		Labels: map[string]string{LabelSession: "s1"}, Volumes: []VolumeMount{{Volume: "pg-ws-u1", Target: "/work"}}}
	Harden(&spec)
	id, err := e.CreateContainer(ctx, spec)
	if err != nil || id != "abc123" {
		t.Fatalf("create %q %v", id, err)
	}
	var req container.CreateRequest
	if err := json.Unmarshal(fe.created["pg-session-s1"], &req); err != nil {
		t.Fatal(err)
	}
	if req.Image != "session:v1" || req.HostConfig == nil || !req.HostConfig.ReadonlyRootfs || *req.HostConfig.PidsLimit != 100 || req.HostConfig.CapDrop[0] != "ALL" {
		t.Fatalf("wire request %+v", req.HostConfig)
	}
	if err := e.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	info, err := e.InspectContainer(ctx, id)
	if err != nil || !info.Running || info.Name != "pg-session-s1" || info.Labels[LabelSession] != "s1" || info.StartedAt.IsZero() {
		t.Fatalf("inspect %+v %v", info, err)
	}
	if _, err := e.InspectContainer(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing container: %v", err)
	}
	list, err := e.ListContainers(ctx, map[string]string{LabelRole: RoleSession})
	if err != nil || len(list) != 1 || list[0].Running || list[0].Name != "pg-session-s1" {
		t.Fatalf("list %+v %v", list, err)
	}
	if err := e.StopContainer(ctx, id, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveContainer(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := e.CreateVolume(ctx, "pg-ws-u1", map[string]string{LabelRole: RoleWorkspace}); err != nil {
		t.Fatal(err)
	}
	vols, err := e.ListVolumes(ctx, map[string]string{LabelRole: RoleWorkspace})
	if err != nil || len(vols) != 1 || vols[0].CreatedAt.IsZero() {
		t.Fatalf("volumes %+v %v", vols, err)
	}
	if err := e.RemoveVolume(ctx, "pg-ws-u1"); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveVolume(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing volume: %v", err)
	}
}
