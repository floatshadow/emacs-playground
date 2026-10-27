package scenario

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
	"github.com/zsy/emacs-playground/internal/sessiond/gwclient"
	"github.com/zsy/emacs-playground/internal/sessiond/steps"
)

type entry struct {
	name    string
	body    string
	dir     bool
	symlink string
	mode    int64
}

func tarball(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.mode != 0 {
			h.Mode = e.mode
		}
		switch {
		case e.dir:
			h.Typeflag, h.Size, h.Mode = tar.TypeDir, 0, 0o755
		case e.symlink != "":
			h.Typeflag, h.Size, h.Linkname = tar.TypeSymlink, 0, e.symlink
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestExtract(t *testing.T) {
	parent := t.TempDir()
	ws := filepath.Join(parent, "work")
	_ = os.MkdirAll(ws, 0o755)
	root := openRoot(t, ws)
	tb := tarball(t,
		entry{name: "src/", dir: true},
		entry{name: "src/main.rs", body: "fn main() {}\n"},
		entry{name: "./Cargo.toml", body: "[package]\n"},
		entry{name: "run.sh", body: "#!/bin/sh\n", mode: 0o755},
		entry{name: "link", symlink: "/etc/passwd"},
	)
	res, err := Extract(bytes.NewReader(tb), root, "hello", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 3 || res.Skipped != 0 {
		t.Fatalf("result %+v", res)
	}
	if readFile(t, filepath.Join(ws, "hello/src/main.rs")) != "fn main() {}\n" {
		t.Fatal("main.rs")
	}
	if st, _ := os.Stat(filepath.Join(ws, "hello/run.sh")); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}
	if _, err := os.Lstat(filepath.Join(ws, "hello/link")); err == nil {
		t.Fatal("symlinks must not be extracted")
	}

	// Learner edits survive a re-extraction without reset.
	_ = os.WriteFile(filepath.Join(ws, "hello/src/main.rs"), []byte("edited"), 0o644)
	res, err = Extract(bytes.NewReader(tb), root, "hello", false)
	if err != nil || res.Skipped != 3 || res.Written != 0 {
		t.Fatalf("no-reset %+v %v", res, err)
	}
	if readFile(t, filepath.Join(ws, "hello/src/main.rs")) != "edited" {
		t.Fatal("learner file overwritten")
	}
	// Reset overwrites.
	if _, err := Extract(bytes.NewReader(tb), root, "hello", true); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(ws, "hello/src/main.rs")) != "fn main() {}\n" {
		t.Fatal("reset did not overwrite")
	}

	// A learner symlink pointing outside the workspace is replaced, not
	// written through.
	outside := filepath.Join(parent, "outside.txt")
	_ = os.WriteFile(outside, []byte("precious"), 0o644)
	_ = os.Remove(filepath.Join(ws, "hello/Cargo.toml"))
	_ = os.Symlink(outside, filepath.Join(ws, "hello/Cargo.toml"))
	if _, err := Extract(bytes.NewReader(tb), root, "hello", true); err != nil {
		t.Fatal(err)
	}
	if readFile(t, outside) != "precious" {
		t.Fatal("wrote through a symlink")
	}
	// A symlinked directory pointing outside makes extraction fail rather
	// than escape.
	_ = os.RemoveAll(filepath.Join(ws, "hello/src"))
	_ = os.Symlink(parent, filepath.Join(ws, "hello/src"))
	if _, err := Extract(bytes.NewReader(tb), root, "hello", true); err == nil {
		t.Fatal("expected an error writing through an escaping directory link")
	}
	if _, err := os.Stat(filepath.Join(parent, "main.rs")); err == nil {
		t.Fatal("escaped the workspace")
	}

	// Unsafe names are rejected.
	for _, bad := range []string{"../evil", "/etc/evil", "a/../../evil"} {
		_, err := Extract(bytes.NewReader(tarball(t, entry{name: bad, body: "x"})), root, "x", true)
		if err == nil || !strings.Contains(err.Error(), "unsafe") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// Garbage is an error.
	if _, err := Extract(strings.NewReader("not zstd"), root, "x", false); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestModules(t *testing.T) {
	mods, ok := ParseModulesEl("(setq playground-modules '(base agent\n  editing files))\n")
	if !ok || strings.Join(mods, " ") != "base agent editing files" {
		t.Fatalf("parse %v %v", mods, ok)
	}
	again, ok := ParseModulesEl(RenderModulesEl(mods))
	if !ok || strings.Join(again, " ") != "base agent editing files" {
		t.Fatalf("round trip %v", again)
	}
	if got := Union([]string{"base", "agent", "git"}, []string{"base", "agent", "editing", "files"}); strings.Join(got, " ") != "base agent git editing files" {
		t.Fatalf("union %v", got)
	}
	for _, bad := range [][]string{{"Base"}, {"a b"}, {"x", "x"}, {"../x"}} {
		if ValidateModules(bad) == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	m := &Modules{Path: filepath.Join(t.TempDir(), "emacs.d", "modules.el")}
	if strings.Join(m.Current(), " ") != "base agent" {
		t.Fatal("default modules")
	}
	if err := m.Write([]string{"base", "agent", "lsp"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Current(), " ") != "base agent lsp" {
		t.Fatal("written modules")
	}
}

// fakeFetch serves scenarios and fixtures from memory.
type fakeFetch struct {
	mu        sync.Mutex
	scenarios map[string]string
	fixtures  map[string][]byte
	down      bool
	gets      []string
}

func (f *fakeFetch) Scenario(ctx context.Context, id, etag string) ([]byte, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, "scenario:"+id+":"+etag)
	if f.down {
		return nil, "", false, errors.New("connection refused")
	}
	s, ok := f.scenarios[id]
	if !ok {
		return nil, "", false, &gwclient.StatusError{Status: 404}
	}
	if etag == `"s"` {
		return nil, etag, true, nil
	}
	return []byte(s), `"s"`, false, nil
}

func (f *fakeFetch) Fixtures(ctx context.Context, id, etag string) (gwclient.FixtureResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, "fixtures:"+id+":"+etag)
	if f.down {
		return gwclient.FixtureResult{}, errors.New("connection refused")
	}
	b, ok := f.fixtures[id]
	if !ok {
		return gwclient.FixtureResult{None: true}, nil
	}
	if etag == `"f"` {
		return gwclient.FixtureResult{NotModified: true, ETag: etag}, nil
	}
	return gwclient.FixtureResult{Body: io.NopCloser(bytes.NewReader(b)), ETag: `"f"`}, nil
}

type fakeAgent struct {
	mu    sync.Mutex
	calls []protocol.AgentSetupArgs
}

func (a *fakeAgent) Call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if op == protocol.AgentOpSetup {
		a.calls = append(a.calls, args.(protocol.AgentSetupArgs))
	}
	return nil
}

type fakeRestart struct {
	mu sync.Mutex
	n  int
}

func (r *fakeRestart) RestartEmacs(ctx context.Context, reason string) error {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	return nil
}

const lesson = `{
  "id": "02-buffers-and-files/03-opening-files",
  "title": "Opening files",
  "modules": ["base", "agent", "editing", "files"],
  "setup": {"workspace": "fixtures/hello-rust", "root": "hello", "open": ["hello/README.md"], "layout": "single"},
  "steps": [
    {"id": "open", "goal": "Open", "conditions": [{"event": {"command": "find-file"}}]},
    {"id": "save", "goal": "Save", "conditions": [{"event": {"command": "save-buffer"}}]}
  ]
}`

type loaderFixture struct {
	l       *Loader
	fetch   *fakeFetch
	agent   *fakeAgent
	restart *fakeRestart
	engine  *steps.Engine
	ws      string
	mu      sync.Mutex
	events  []string
	cps     []string
}

func newLoader(t *testing.T) *loaderFixture {
	t.Helper()
	ws := t.TempDir()
	f := &loaderFixture{
		fetch: &fakeFetch{
			scenarios: map[string]string{"02-buffers-and-files/03-opening-files": lesson},
			fixtures: map[string][]byte{"02-buffers-and-files/03-opening-files": tarball(t,
				entry{name: "README.md", body: "# hello\n"}, entry{name: "src/main.rs", body: "fn main() {}\n"})},
		},
		agent: &fakeAgent{}, restart: &fakeRestart{}, ws: ws,
	}
	emit := func(ev protocol.Event) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch e := ev.(type) {
		case *protocol.StepEvent:
			f.events = append(f.events, "step:"+e.StepID+":"+string(e.Status))
		case *protocol.ModulesEvent:
			f.events = append(f.events, "modules:"+strings.Join(e.Enabled, ",")+":"+map[bool]string{true: "restarting", false: "done"}[e.Restarting])
		default:
			f.events = append(f.events, string(ev.Type()))
		}
	}
	f.engine = steps.New(steps.Options{Hooks: steps.Hooks{Emit: emit}})
	f.l = &Loader{
		Workspace: ws, Root: openRoot(t, ws), CacheDir: filepath.Join(ws, ".playground", "cache"),
		Fetch: f.fetch, Agent: f.agent, Restart: f.restart,
		Modules: &Modules{Path: filepath.Join(ws, "emacs.d", "modules.el")},
		Engine:  f.engine, Emit: emit,
		Checkpoint: func(ctx context.Context, l, s string, force bool) error {
			f.mu.Lock()
			f.cps = append(f.cps, l+"/"+s+map[bool]string{true: "!", false: ""}[force])
			f.mu.Unlock()
			return nil
		},
	}
	return f
}

func (f *loaderFixture) evs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.events, " ")
}

func TestLoaderFlow(t *testing.T) {
	f := newLoader(t)
	ctx := context.Background()
	id := "02-buffers-and-files/03-opening-files"

	// Modules already match: synchronous load.
	_ = f.l.Modules.Write([]string{"base", "agent", "editing", "files"})
	res, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil {
		t.Fatal(err)
	}
	if res.Restarting || res.Step.StepID != "open" || res.Index != 0 || res.Count != 2 {
		t.Fatalf("result %+v", res)
	}
	if readFile(t, filepath.Join(f.ws, "hello/src/main.rs")) != "fn main() {}\n" {
		t.Fatal("fixtures not extracted under the root")
	}
	if len(f.agent.calls) != 1 || f.agent.calls[0].Open[0] != "hello/README.md" || f.agent.calls[0].Workspace != f.ws || f.agent.calls[0].Layout != "single" || f.agent.calls[0].Root != "hello" {
		t.Fatalf("setup %+v", f.agent.calls)
	}
	if strings.Join(f.cps, ",") != id+"/open" {
		t.Fatalf("checkpoints %v", f.cps)
	}
	if f.evs() != "step:open:loaded" {
		t.Fatalf("events %s", f.evs())
	}
	if a := f.engine.Active(); a == nil || a.StepID != "open" {
		t.Fatal("not active")
	}

	// A reload is idempotent: no setup, no checkpoint, no fetch.
	n := len(f.fetch.gets)
	res, err = f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil || res.Step.StepID != "open" || len(f.agent.calls) != 1 || len(f.cps) != 1 || len(f.fetch.gets) != n {
		t.Fatalf("reload: %+v %v calls=%d cps=%d", res, err, len(f.agent.calls), len(f.cps))
	}

	// An explicit step loads it.
	res, err = f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id, StepID: "save"})
	if err != nil || res.Index != 1 {
		t.Fatalf("step: %+v %v", res, err)
	}
	// ETags were sent on the second fetch.
	if !strings.Contains(strings.Join(f.fetch.gets, " "), `scenario:`+id+`:"s"`) {
		t.Fatalf("etag not sent: %v", f.fetch.gets)
	}

	// Errors.
	if _, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id, StepID: "nope"}); httpx.AsError(err).Status != 400 {
		t.Fatalf("unknown step: %v", err)
	}
	if _, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: "../x"}); httpx.AsError(err).Status != 400 {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: "09-org/01-x"}); httpx.AsError(err).Status != 404 {
		t.Fatalf("missing lesson: %v", err)
	}

	// The gateway down: the cached scenario and fixtures still load.
	f.fetch.down = true
	f.engine.Drop(id)
	_ = os.RemoveAll(filepath.Join(f.ws, "hello"))
	res, err = f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id, Reset: true})
	if err != nil || res.Step.StepID != "open" {
		t.Fatalf("offline load: %+v %v", res, err)
	}
	if readFile(t, filepath.Join(f.ws, "hello/README.md")) != "# hello\n" {
		t.Fatal("cached fixtures not extracted")
	}
	// Reset with files written retakes the checkpoint.
	if last := f.cps[len(f.cps)-1]; last != id+"/open!" {
		t.Fatalf("forced checkpoint %v", f.cps)
	}
	// Nothing cached and the gateway down: an error event and 503.
	f.fetch.scenarios["01-moving/01-x"] = lesson
	if _, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: "01-moving/01-x"}); httpx.AsError(err).Status != 503 {
		t.Fatalf("offline uncached: %v", err)
	}
	if !strings.Contains(f.evs(), "error") {
		t.Fatalf("no error event: %s", f.evs())
	}
}

func TestLoaderModuleChangeRestarts(t *testing.T) {
	f := newLoader(t)
	ctx := context.Background()
	id := "02-buffers-and-files/03-opening-files"
	_ = f.l.Modules.Write([]string{"base", "agent", "git"})
	res, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Restarting || strings.Join(res.Modules, " ") != "base agent git editing files" {
		t.Fatalf("result %+v", res)
	}
	// While restarting, a repeated request answers restarting too.
	res2, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil || !res2.Restarting {
		t.Fatalf("repeat during restart %+v %v", res2, err)
	}
	f.l.Wait()
	if f.restart.n != 1 {
		t.Fatalf("restarts %d", f.restart.n)
	}
	want := "modules:base,agent,git,editing,files:restarting modules:base,agent,git,editing,files:done step:open:loaded"
	if f.evs() != want {
		t.Fatalf("events %s", f.evs())
	}
	if strings.Join(f.l.Modules.Current(), " ") != "base agent git editing files" {
		t.Fatal("modules.el not written")
	}

	// rpc modules replaces exactly.
	restarting, err := f.l.SetModules([]string{"base", "agent"})
	if err != nil || !restarting {
		t.Fatalf("set modules %v %v", restarting, err)
	}
	f.l.Wait()
	if restarting, _ := f.l.SetModules([]string{"base", "agent"}); restarting {
		t.Fatal("unchanged set should not restart")
	}
	if _, err := f.l.SetModules([]string{"base"}); err == nil {
		t.Fatal("agent cannot be disabled")
	}
	if _, err := f.l.SetModules([]string{"base", "agent", "Bad"}); err == nil {
		t.Fatal("bad name")
	}
}

func TestResume(t *testing.T) {
	f := newLoader(t)
	ctx := context.Background()
	id := "02-buffers-and-files/03-opening-files"
	if err := f.l.Resume(ctx, protocol.StepRef{LessonID: id, StepID: "save"}); err != nil {
		t.Fatal(err)
	}
	if a := f.engine.Active(); a == nil || a.StepID != "save" {
		t.Fatalf("resume active %+v", a)
	}
	if len(f.agent.calls) != 0 || len(f.cps) != 0 {
		t.Fatal("resume must not run setup or checkpoints")
	}
	var sc protocol.Scenario
	_ = json.Unmarshal([]byte(lesson), &sc)
}

func TestExtractResetPrunes(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "work")
	_ = os.MkdirAll(ws, 0o755)
	root := openRoot(t, ws)
	tb := tarball(t,
		entry{name: "inbox/brief.md", body: "brief\n"},
		entry{name: "inbox/scratch.tmp", body: "tmp\n"},
		entry{name: "hello/src/main.rs", body: "fn main() {}\n"},
	)
	if _, err := Extract(bytes.NewReader(tb), root, "projects", true); err != nil {
		t.Fatal(err)
	}
	// A learner moves, deletes and creates files, and builds.
	p := func(s string) string { return filepath.Join(ws, "projects", s) }
	_ = os.MkdirAll(p("inbox/docs"), 0o755)
	_ = os.Rename(p("inbox/brief.md"), p("inbox/docs/brief.md"))
	_ = os.Remove(p("inbox/scratch.tmp"))
	_ = os.WriteFile(p("hello/CHANGES.md"), []byte("new"), 0o644)
	_ = os.MkdirAll(p("hello/target/debug"), 0o755)
	_ = os.WriteFile(p("hello/target/debug/hello"), []byte("bin"), 0o755)
	_ = os.WriteFile(filepath.Join(ws, "outside.txt"), []byte("keep"), 0o644)

	// Without reset nothing is removed.
	res, err := Extract(bytes.NewReader(tb), root, "projects", false)
	if err != nil || res.Removed != 0 {
		t.Fatalf("no reset: %+v %v", res, err)
	}
	if _, err := os.Stat(p("hello/CHANGES.md")); err != nil {
		t.Fatal("a re-extraction without reset removed a learner file")
	}
	// With reset the tree matches the fixture again, build output kept.
	res, err = Extract(bytes.NewReader(tb), root, "projects", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("removed %d, want 2 (inbox/docs and hello/CHANGES.md): %+v", res.Removed, res)
	}
	for _, gone := range []string{"inbox/docs", "hello/CHANGES.md"} {
		if _, err := os.Stat(p(gone)); err == nil {
			t.Fatalf("%s survived the reset", gone)
		}
	}
	for _, kept := range []string{"inbox/brief.md", "inbox/scratch.tmp", "hello/target/debug/hello"} {
		if _, err := os.Stat(p(kept)); err != nil {
			t.Fatalf("%s missing after the reset", kept)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, "outside.txt")); err != nil {
		t.Fatal("a reset touched files outside its root")
	}
}
