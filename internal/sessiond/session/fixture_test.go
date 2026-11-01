package session

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/snapshot"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
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

func (f *fakeFetch) numGets() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.gets)
}

// fakeAgent records setup calls; block, when set, holds setup until it is
// closed.
type fakeAgent struct {
	mu    sync.Mutex
	calls []protocol.AgentSetupArgs
	block chan struct{}
	// onCall, when set, observes every call.
	onCall func(protocol.AgentOp)
}

func (a *fakeAgent) Connected() bool { return true }

func (a *fakeAgent) Call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error {
	a.mu.Lock()
	block := a.block
	if a.onCall != nil {
		a.onCall(op)
	}
	if op == protocol.AgentOpSetup {
		a.calls = append(a.calls, args.(protocol.AgentSetupArgs))
	}
	a.mu.Unlock()
	if op == protocol.AgentOpSetup && block != nil {
		<-block
	}
	return nil
}

func (a *fakeAgent) setups() []protocol.AgentSetupArgs {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]protocol.AgentSetupArgs(nil), a.calls...)
}

// demoCommands maps the first key of a demo to the command the fake
// supervisor reports for it.
var demoCommands = map[string]string{"C-x C-f": "find-file", "C-x C-s": "save-buffer", "C-x k": "kill-buffer"}

// fakeSup restarts in 20 ms (or when release is closed) and turns demo
// keys into the evaluator's key events.
type fakeSup struct {
	mu       sync.Mutex
	restarts int
	// cutShort counts restarts whose context ended before they finished.
	cutShort int
	release  chan struct{}
	eval     *steps.Evaluator
	played   []string
	// pending is the key sequence typed so far towards a demo command.
	pending string
	// onKeys, when set, runs before each key chunk is sent; afterRestart,
	// when set, at the end of each restart (the supervisor's hook).
	onKeys       func(ctx context.Context) error
	afterRestart func(ctx context.Context)
}

func (s *fakeSup) Ready() (bool, string)                               { return true, "" }
func (s *fakeSup) WaitReady(ctx context.Context, d time.Duration) bool { return true }

func (s *fakeSup) RestartEmacs(ctx context.Context, reason string) error {
	s.mu.Lock()
	s.restarts++
	release, after := s.release, s.afterRestart
	s.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			s.mu.Lock()
			s.cutShort++
			s.mu.Unlock()
			return ctx.Err()
		}
	} else {
		time.Sleep(20 * time.Millisecond)
	}
	if after != nil {
		after(ctx)
	}
	return nil
}

func (s *fakeSup) SendKeys(ctx context.Context, frame protocol.FrameKind, keys, text string) error {
	s.mu.Lock()
	onKeys := s.onKeys
	s.played = append(s.played, keys+text)
	s.mu.Unlock()
	if onKeys != nil {
		if err := onKeys(ctx); err != nil {
			return err
		}
	}
	if keys == "" {
		return nil
	}
	s.mu.Lock()
	seq := strings.TrimSpace(s.pending + " " + keys)
	cmd, ok := demoCommands[seq]
	s.pending = ""
	for k := range demoCommands {
		if !ok && strings.HasPrefix(k, seq+" ") {
			s.pending = seq
		}
	}
	s.mu.Unlock()
	if ok {
		s.eval.OnKey(&protocol.KeyEvent{Keys: seq, Cmd: cmd})
	}
	return nil
}

func (s *fakeSup) numRestarts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restarts
}

// fakeSnaps records captures and restores; restorable lists the step ids
// with a checkpoint (a capture adds one).
type fakeSnaps struct {
	mu         sync.Mutex
	cps        []string
	restored   []string
	restorable map[string]bool
	restoreErr error
	// deferred makes restores report the agent away; editorRestores lists
	// the refs RestoreEditor restored, saves counts SaveEditorState.
	deferred       bool
	editorRestores []string
	saves          int
	spec           *protocol.WindowSpec
	opens          []*protocol.WindowSpec
	// onCapture, onOpen and onRestore, when set, run at the start of each
	// capture, open and restore.
	onOpen    func()
	onCapture func()
	onRestore func()
}

func (s *fakeSnaps) Exists(ctx context.Context, ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, step, _ := splitStepRef(ref)
	return s.restorable[step] || s.restoreErr != nil
}

func (s *fakeSnaps) Read(ref string) (snapshot.Manifest, error) {
	return snapshot.Manifest{Ref: ref}, nil
}

func (s *fakeSnaps) List(ctx context.Context) ([]protocol.CheckpointInfo, error) { return nil, nil }

func (s *fakeSnaps) Capture(ctx context.Context, ref, label string, step *protocol.StepRef) (snapshot.Manifest, error) {
	s.mu.Lock()
	hook := s.onCapture
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cps = append(s.cps, strings.TrimPrefix(ref, checkpoint.RefPrefix))
	if _, step, ok := splitStepRef(ref); ok {
		s.restorable[step] = true
	}
	return snapshot.Manifest{Ref: ref, Label: label, Step: step}, nil
}

func (s *fakeSnaps) Restore(ctx context.Context, ref string) (snapshot.Report, error) {
	s.mu.Lock()
	hook := s.onRestore
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, step, _ := splitStepRef(ref)
	s.restored = append(s.restored, step)
	if s.restoreErr != nil {
		return snapshot.Report{}, s.restoreErr
	}
	if !s.restorable[step] {
		return snapshot.Report{}, snapshot.ErrNoCheckpoint
	}
	return snapshot.Report{Files: []string{"x"}, EditorDeferred: s.deferred}, nil
}

func (s *fakeSnaps) RestoreEditor(ctx context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editorRestores = append(s.editorRestores, ref)
	return nil
}

func (s *fakeSnaps) SaveEditorState(ctx context.Context) (*protocol.WindowSpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	return s.spec, nil
}

func (s *fakeSnaps) LastEditorState() *protocol.WindowSpec { return s.spec }

func (s *fakeSnaps) Open(ctx context.Context, spec *protocol.WindowSpec) error {
	s.mu.Lock()
	hook := s.onOpen
	s.opens = append(s.opens, spec)
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return nil
}

func (s *fakeSnaps) captured() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.cps, ",")
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

const lessonID = "02-buffers-and-files/03-opening-files"

type fixture struct {
	t      *testing.T
	core   *Core
	fetch  *fakeFetch
	agent  *fakeAgent
	sup    *fakeSup
	snaps  *fakeSnaps
	eval   *steps.Evaluator
	mods   *scenario.Modules
	ws     string
	mu     sync.Mutex
	events []string
	// lessons are the lesson events published, busy ones included.
	lessons []*protocol.LessonEvent
	cancel  context.CancelFunc
	done    chan struct{}
}

// newFixture returns a running core over fakes; mutate adjusts the deps
// before it starts.
func newFixture(t *testing.T, mutate ...func(*fixture, *Deps)) *fixture {
	t.Helper()
	ws := t.TempDir()
	root, err := os.OpenRoot(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	f := &fixture{
		t: t, ws: ws,
		fetch: &fakeFetch{
			scenarios: map[string]string{lessonID: lesson},
			fixtures:  map[string][]byte{lessonID: tarball(t, map[string]string{"README.md": "# hello\n", "src/main.rs": "fn main() {}\n"})},
		},
		agent: &fakeAgent{}, snaps: &fakeSnaps{restorable: map[string]bool{}},
		mods: &scenario.Modules{Path: filepath.Join(ws, "emacs.d", "modules.el")},
	}
	emit := func(ev protocol.Event) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch e := ev.(type) {
		case *protocol.LessonEvent:
			f.lessons = append(f.lessons, e)
			if s := lessonLine(e); s != "" {
				f.events = append(f.events, s)
			}
		case *protocol.ModulesEvent:
			f.events = append(f.events, "modules:"+strings.Join(e.Enabled, ",")+":"+map[bool]string{true: "restarting", false: "done"}[e.Restarting])
		default:
			f.events = append(f.events, string(ev.Type()))
		}
	}
	f.eval = steps.New(steps.Options{Debounce: 5 * time.Millisecond, Observe: func(o steps.Observation) { f.core.Observe(o) }})
	f.sup = &fakeSup{eval: f.eval}
	d := Deps{
		Workspace: ws, Root: root, CacheDir: filepath.Join(ws, ".playground", "cache"),
		Fetch: f.fetch, Agent: f.agent, Sup: f.sup, Snapshots: f.snaps, Modules: f.mods, Evaluator: f.eval,
		Emit: emit, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, m := range mutate {
		m(f, &d)
	}
	f.core = New(d)
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel, f.done = cancel, make(chan struct{})
	go func() {
		defer close(f.done)
		f.eval.Run(ctx)
	}()
	go f.core.Run(ctx)
	t.Cleanup(f.stop)
	return f
}

// stop waits for the queue to drain, then stops the worker.
func (f *fixture) stop() {
	f.idle()
	f.cancel()
	<-f.done
}

// idle waits until no op runs.
func (f *fixture) idle() {
	deadline := time.Now().Add(5 * time.Second)
	for f.core.Busy() != nil || f.queued() > 0 {
		if time.Now().After(deadline) {
			f.t.Fatalf("an op is still running: %+v", f.core.Busy())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *fixture) queued() int {
	f.core.mu.Lock()
	defer f.core.mu.Unlock()
	return len(f.core.queue)
}

func (f *fixture) evs() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.events, " ")
}

// waitFor polls cond for up to 2 s.
// dirty reports whether the active step counts as dirty.
func (f *fixture) dirty() bool {
	f.core.mu.Lock()
	defer f.core.mu.Unlock()
	return f.core.dirtyLocked()
}

func (f *fixture) waitFor(what string, cond func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			f.t.Fatalf("waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lessonLine renders a lesson event as lesson:<step>:<change>, with
// :lesson-complete on a completion or skip that completes the lesson; busy
// changes render as "" (their timing is not what tests pin).
func lessonLine(e *protocol.LessonEvent) string {
	if e.Change == protocol.ChangeBusy {
		return ""
	}
	s := "lesson:" + e.StepID + ":" + string(e.Change)
	if (e.Change == protocol.ChangeComplete || e.Change == protocol.ChangeSkipped) && e.Lesson.Phase == protocol.PhaseComplete {
		s += ":lesson-complete"
	}
	return s
}
