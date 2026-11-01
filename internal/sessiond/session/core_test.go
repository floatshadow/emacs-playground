package session

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent/fakeeditor"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/progress"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/snapshot"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// An op runs to the end when the request that asked for it goes away.
func TestOpsOutliveTheRequest(t *testing.T) {
	f := newFixture(t)
	_ = f.mods.Write([]string{"base", "agent", "editing", "files"})
	block := make(chan struct{})
	f.agent.mu.Lock()
	f.agent.block = block
	f.agent.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID})
		done <- err
	}()
	f.waitFor("the setup op", func() bool { return len(f.agent.setups()) == 1 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("load with the request gone: %v", err)
	}
	close(block)
	f.idle()
	if a := f.core.Active(); a == nil || a.StepID != "open" || f.snaps.captured() != lessonID+"/open" {
		t.Fatalf("the load stopped with its request: active %+v checkpoints %s", a, f.snaps.captured())
	}
}

// A drop of the lesson cancels its fast-forward: dropped at once, nothing
// activates afterwards.
func TestDropPreemptsFastForward(t *testing.T) {
	f := newDemoFixture(t)
	if _, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID}); err != nil {
		t.Fatal(err)
	}
	// Each key of the replay waits until the op is cancelled.
	inReplay := make(chan struct{})
	var once sync.Once
	f.sup.mu.Lock()
	f.sup.onKeys = func(ctx context.Context) error {
		once.Do(func() { close(inReplay) })
		<-ctx.Done()
		return ctx.Err()
	}
	f.sup.mu.Unlock()
	load := make(chan error, 1)
	go func() {
		_, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "kill"})
		load <- err
	}()
	<-inReplay
	if b := f.core.Busy(); b == nil || b.Kind != OpLoad || b.StepID != "kill" {
		t.Fatalf("busy %+v", b)
	}
	start := time.Now()
	if err := f.core.Drop(context.Background(), demoID, 1); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("drop took %v", took)
	}
	if err := <-load; httpx.AsError(err).Status != 409 {
		t.Fatalf("cancelled load: %v", err)
	}
	f.idle()
	if a := f.core.Active(); a != nil {
		t.Fatalf("active after the drop %+v", a)
	}
	evs := f.evs()
	if !strings.HasSuffix(evs, "lesson:open:dropped") || strings.Contains(evs, "kill") {
		t.Fatalf("events %s", evs)
	}
}

// Gateway resets arriving as drop ops (rpc drop-step) cancel the skip
// playing the lesson's step (its demo stops reaching Emacs), the step
// goes, and the generations rise.
func TestSyncDropPreemptsSkip(t *testing.T) {
	f := newDemoFixture(t)
	if _, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID}); err != nil {
		t.Fatal(err)
	}
	f.idle()
	inDemo := make(chan struct{})
	var once sync.Once
	f.sup.mu.Lock()
	f.sup.onKeys = func(ctx context.Context) error {
		once.Do(func() { close(inDemo) })
		<-ctx.Done()
		return ctx.Err()
	}
	f.sup.mu.Unlock()
	skipped := make(chan error, 1)
	go func() {
		_, err := f.core.Skip(context.Background(), "open")
		skipped <- err
	}()
	<-inDemo
	gens := protocol.ProgressGenerations{"": 1, demoID: 3}
	for _, scope := range []string{"", demoID} {
		if err := f.core.Drop(context.Background(), scope, gens[scope]); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-skipped; httpx.AsError(err).Status != 409 {
		t.Fatalf("cancelled skip: %v", err)
	}
	f.idle()
	if a := f.core.Active(); a != nil {
		t.Fatalf("active after the sync %+v", a)
	}
	if got := f.core.d.Progress.(*progress.Outbox).Snapshot().Generations; !maps.Equal(got, gens) {
		t.Fatalf("generations %v", got)
	}
	if evs := f.evs(); !strings.HasSuffix(evs, "lesson:open:dropped lesson::dropped") || strings.Contains(evs, "skipped") {
		t.Fatalf("events %s", evs)
	}
}

// A drop whose generation was already applied cancels nothing: the load
// it would have preempted finishes.
func TestStaleDropDoesNotPreempt(t *testing.T) {
	f := newDemoFixture(t)
	f.core.drop(demoID, 2)
	release := make(chan struct{})
	inReplay := make(chan struct{})
	var once sync.Once
	f.sup.mu.Lock()
	f.sup.onKeys = func(ctx context.Context) error {
		once.Do(func() { close(inReplay) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.sup.mu.Unlock()
	load := make(chan error, 1)
	go func() {
		_, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "save"})
		load <- err
	}()
	<-inReplay
	dropped := make(chan error, 1)
	go func() { dropped <- f.core.Drop(context.Background(), demoID, 2) }()
	f.waitFor("the drop to queue", func() bool { return f.queued() == 1 })
	f.core.mu.Lock()
	cancelled := f.core.running.ctx.Err()
	f.core.mu.Unlock()
	close(release)
	if cancelled != nil {
		t.Fatalf("a stale drop cancelled the load: %v", cancelled)
	}
	if err := <-load; err != nil {
		t.Fatal(err)
	}
	if err := <-dropped; err != nil {
		t.Fatal(err)
	}
	if a := f.core.Active(); a == nil || a.StepID != "save" {
		t.Fatalf("active %+v", a)
	}
}

// A drop of another lesson runs next without cancelling the running op.
func TestDropOfAnotherLessonWaits(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	f.sup.release = release
	res, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: lessonID})
	if err != nil || !res.Restarting {
		t.Fatalf("load %+v %v", res, err)
	}
	queued := make(chan error, 1)
	go func() {
		_, err := f.core.SetModules(context.Background(), []string{"base", "agent"})
		queued <- err
	}()
	f.waitFor("the queued op", func() bool { return f.queued() == 1 })
	dropped := make(chan error, 1)
	go func() { dropped <- f.core.Drop(context.Background(), "01-moving/01-basics", 1) }()
	f.waitFor("the drop to jump the queue", func() bool {
		f.core.mu.Lock()
		defer f.core.mu.Unlock()
		return len(f.core.queue) == 2 && f.core.queue[0].Kind == OpDrop
	})
	close(release)
	if err := <-dropped; err != nil {
		t.Fatal(err)
	}
	if err := <-queued; err != nil {
		t.Fatal(err)
	}
	if a := f.core.Active(); a == nil || a.LessonID != lessonID {
		t.Fatalf("the load was cancelled by another lesson's drop: %+v", a)
	}
}

// Demo start restores the active step's checkpoint only when the learner
// ran a command in it, restarting the step as an uncounted reset.
func TestDemoStartRestoresWhenDirty(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID}); err != nil {
		t.Fatal(err)
	}
	f.idle()
	if res, err := f.core.DemoStart(ctx, "open"); err != nil || res.Restored {
		t.Fatalf("clean step %+v %v", res, err)
	}
	f.eval.OnKey(&protocol.KeyEvent{Keys: "C-n", Cmd: "next-line"})
	// A request naming another step restores nothing.
	if _, err := f.core.DemoStart(ctx, "save"); httpx.AsError(err).Status != 409 {
		t.Fatalf("demo start of another step: %v", err)
	}
	f.snaps.mu.Lock()
	n := len(f.snaps.restored)
	f.snaps.mu.Unlock()
	if n != 0 {
		t.Fatal("a request for another step restored a checkpoint")
	}
	if res, err := f.core.DemoStart(ctx, "open"); err != nil || !res.Restored {
		t.Fatalf("dirty step %+v %v", res, err)
	}
	if !strings.HasSuffix(f.evs(), "lesson:open:reset lesson:open:phase") || f.core.Saved().Lessons[lessonID].Resets["open"] != 0 || !f.core.Watching() || f.dirty() {
		t.Fatalf("events %s saved %+v", f.evs(), f.core.Saved().Lessons[lessonID])
	}
}

func TestStopFailsQueuedOps(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	defer close(release)
	f.sup.release = release
	if _, err := f.core.SetModules(context.Background(), []string{"base", "agent", "files"}); err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 1)
	go func() {
		_, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: lessonID})
		queued <- err
	}()
	f.waitFor("the queued load", func() bool { return f.queued() == 1 })
	f.cancel()
	if err := <-queued; !errors.Is(err, errStopped) {
		t.Fatalf("queued op after stop: %v", err)
	}
}

func TestSplitStepRef(t *testing.T) {
	cases := map[string][2]string{
		"refs/playground/02-buffers-and-files/03-opening-files/open": {"02-buffers-and-files/03-opening-files", "open"},
		"refs/playground/projects/p6-polyglot/deploy":                {"projects/p6-polyglot", "deploy"},
	}
	for ref, want := range cases {
		l, s, ok := splitStepRef(ref)
		if !ok || l != want[0] || s != want[1] {
			t.Errorf("%s → %s %s %v", ref, l, s, ok)
		}
	}
	if _, _, ok := splitStepRef("refs/playground/manual/x"); ok {
		t.Error("manual ref is not a step ref")
	}
}

// --- restore over real checkpoints ----------------------------------------------------

type restoreGit struct {
	mu       sync.Mutex
	restored []string
}

func (g *restoreGit) Run(_ context.Context, _ string, _ []string, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch args[0] {
	case "rev-parse", "commit-tree":
		return []byte("commit"), nil
	case "write-tree":
		return []byte("tree"), nil
	case "restore":
		g.restored = append(g.restored, strings.TrimPrefix(args[1], "--source="))
	}
	return nil, nil
}

type restoreFixture struct {
	core  *Core
	snaps *snapshot.Service
	git   *restoreGit
	ws    string
}

// newRestoreFixture runs a core over checkpoints of steps one and two of
// a/b (taken with a fake git), step two active; ag is the agent (nil: not
// connected) and emit observes the core's events.
func newRestoreFixture(t *testing.T, ag Agent, emit func(protocol.Event)) *restoreFixture {
	t.Helper()
	ws := t.TempDir()
	if ag == nil {
		ag = &agent.Server{}
	}
	g := &restoreGit{}
	snaps := &snapshot.Service{
		Files:   &checkpoint.Manager{Workspace: ws, Git: g, Now: time.Now},
		Editor:  &snapshot.AgentEditor{Agent: ag, Workspace: ws},
		Modules: &scenario.Modules{Path: filepath.Join(ws, "emacs.d", "modules.el")},
	}
	for _, id := range []string{"one", "two"} {
		if _, err := snaps.Capture(context.Background(), checkpoint.StepRef("a/b", id), "a/b/"+id,
			&protocol.StepRef{LessonID: "a/b", StepID: id}); err != nil {
			t.Fatal(err)
		}
	}
	ev := steps.New(steps.Options{})
	core := New(Deps{Workspace: ws, Agent: ag, Sup: &fakeSup{eval: ev}, Snapshots: snaps, Modules: snaps.Modules, Evaluator: ev, Emit: emit})
	core.activate(&protocol.Scenario{ID: "a/b", Steps: []protocol.Step{{ID: "one"}, {ID: "two"}}}, 1, activation{change: protocol.ChangeLoaded, phase: Active})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go core.Run(ctx)
	return &restoreFixture{core: core, snaps: snaps, git: g, ws: ws}
}

// Ops are serial: a second restore waits until the first one's
// activation is over.
func TestRestoreSerializesActivation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	f := newRestoreFixture(t, nil, func(ev protocol.Event) {
		if st, ok := ev.(*protocol.LessonEvent); ok && st.Change == protocol.ChangeReset && st.StepID == "one" {
			close(entered)
			<-release
		}
	})
	done := make(chan error, 2)
	go func() {
		_, err := f.core.Restore(context.Background(), protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "one")})
		done <- err
	}()
	<-entered
	go func() {
		_, err := f.core.Restore(context.Background(), protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "two")})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second restore passed the unfinished activation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	f.git.mu.Lock()
	n := len(f.git.restored)
	f.git.mu.Unlock()
	if n != 1 {
		t.Fatalf("restores before activation completed: %d", n)
	}
	once.Do(func() { close(release) })
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if a := f.core.Active(); a == nil || a.StepID != "two" {
		t.Fatalf("active %+v", a)
	}
}

func TestRestoreAgentFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "restore-editor failure"}[fail], func(t *testing.T) {
			dir, err := os.MkdirTemp("", "pg-agent")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			a, err := agent.Listen(filepath.Join(dir, "agent.sock"), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			f := newRestoreFixture(t, a, nil)
			ed := fakeeditor.New(f.ws)
			if fail {
				ed.Fail(protocol.AgentOpRestoreEditor, errors.New("busy"))
			}
			fake := &agent.Fake{Path: a.Path(), Handle: ed.Handle}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = fake.Run(ctx) }()
			wait, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			if _, err := a.WaitHello(wait, 0); err != nil {
				t.Fatal(err)
			}
			before := f.core.Saved()
			_, err = f.core.Restore(ctx, protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "one")})
			if (err != nil) != fail {
				t.Fatalf("restore error %v", err)
			}
			saved := f.core.Saved()
			if fail {
				if !reflect.DeepEqual(saved.Active, before.Active) || !reflect.DeepEqual(saved.Lessons, before.Lessons) {
					t.Fatalf("failed restore changed the lesson state: %+v", saved)
				}
				if got := ed.CountCalls(protocol.AgentOpRestoreEditor); got != 1 {
					t.Fatalf("restore-editor calls: %d", got)
				}
			} else if saved.Active == nil || saved.Active.StepID != "one" || saved.Lessons["a/b"] == nil || saved.Lessons["a/b"].Resets["one"] != 1 {
				t.Fatalf("successful restore: %+v", saved)
			}
		})
	}
}

// A ref without a manifest (taken before the snapshot service, or cut
// short between its manifest and its ref) is no checkpoint.
func TestRestoreWithoutManifest(t *testing.T) {
	f := newRestoreFixture(t, nil, nil)
	if err := os.RemoveAll(filepath.Join(f.ws, ".playground", "checkpoints")); err != nil {
		t.Fatal(err)
	}
	_, err := f.core.Restore(context.Background(), protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "one")})
	if httpx.AsError(err).Status != 404 {
		t.Fatalf("restore error %v", err)
	}
	f.git.mu.Lock()
	defer f.git.mu.Unlock()
	if len(f.git.restored) != 0 {
		t.Fatalf("files restored: %v", f.git.restored)
	}
}

// The restore op resolves an empty request to the active step's
// checkpoint.
func TestResolveRestore(t *testing.T) {
	f := newRestoreFixture(t, nil, nil)
	ctx := context.Background()
	if _, err := f.core.Restore(ctx, protocol.RestoreRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.core.Restore(ctx, protocol.RestoreRequest{Label: "x", Ref: "y"}); httpx.AsError(err).Status != 400 {
		t.Fatalf("label and ref: %v", err)
	}
	f.git.mu.Lock()
	defer f.git.mu.Unlock()
	if strings.Join(f.git.restored, ",") != checkpoint.StepRef("a/b", "two") {
		t.Fatalf("restored %v", f.git.restored)
	}
}
