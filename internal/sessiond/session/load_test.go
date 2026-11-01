package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

func TestLoadFlow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Modules already match: no restart.
	_ = f.mods.Write([]string{"base", "agent", "editing", "files"})
	res, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Restarting || res.Step.StepID != "open" || res.Index != 0 || res.Count != 2 {
		t.Fatalf("result %+v", res)
	}
	if readFile(t, filepath.Join(f.ws, "hello/src/main.rs")) != "fn main() {}\n" {
		t.Fatal("fixtures not extracted under the root")
	}
	calls := f.agent.setups()
	if len(calls) != 1 || calls[0].Open[0] != "hello/README.md" || calls[0].Workspace != f.ws || calls[0].Layout != "single" || calls[0].Root != "hello" {
		t.Fatalf("setup %+v", calls)
	}
	if f.snaps.captured() != lessonID+"/open" {
		t.Fatalf("checkpoints %v", f.snaps.captured())
	}
	if f.evs() != "step:open:loaded" {
		t.Fatalf("events %s", f.evs())
	}
	if a := f.core.Active(); a == nil || a.StepID != "open" {
		t.Fatal("not active")
	}

	// A reload is idempotent: no setup, no checkpoint, no fetch.
	n := f.fetch.numGets()
	res, err = f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID})
	if err != nil || res.Step.StepID != "open" || len(f.agent.setups()) != 1 || f.snaps.captured() != lessonID+"/open" || f.fetch.numGets() != n {
		t.Fatalf("reload: %+v %v calls=%d cps=%s", res, err, len(f.agent.setups()), f.snaps.captured())
	}

	// An explicit step loads it.
	res, err = f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID, StepID: "save"})
	if err != nil || res.Index != 1 {
		t.Fatalf("step: %+v %v", res, err)
	}
	// ETags were sent on the second fetch.
	if !strings.Contains(strings.Join(f.fetch.gets, " "), `scenario:`+lessonID+`:"s"`) {
		t.Fatalf("etag not sent: %v", f.fetch.gets)
	}

	// Errors.
	if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID, StepID: "nope"}); httpx.AsError(err).Status != 400 {
		t.Fatalf("unknown step: %v", err)
	}
	if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: "../x"}); httpx.AsError(err).Status != 400 {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: "09-org/01-x"}); httpx.AsError(err).Status != 404 {
		t.Fatalf("missing lesson: %v", err)
	}

	// The gateway down: the cached scenario and fixtures still load.
	f.fetch.mu.Lock()
	f.fetch.down = true
	f.fetch.mu.Unlock()
	f.core.dropGeneration(lessonID, 0)
	_ = os.RemoveAll(filepath.Join(f.ws, "hello"))
	res, err = f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID, Reset: true})
	if err != nil || res.Step.StepID != "open" {
		t.Fatalf("offline load: %+v %v", res, err)
	}
	if readFile(t, filepath.Join(f.ws, "hello/README.md")) != "# hello\n" {
		t.Fatal("cached fixtures not extracted")
	}
	// Reset with files written retakes the checkpoint.
	if cps := strings.Split(f.snaps.captured(), ","); cps[len(cps)-1] != lessonID+"/open" || strings.Count(f.snaps.captured(), "/open") != 2 {
		t.Fatalf("forced checkpoint %v", cps)
	}
	// Nothing cached and the gateway down: an error event and 503.
	f.fetch.mu.Lock()
	f.fetch.scenarios["01-moving/01-x"] = lesson
	f.fetch.mu.Unlock()
	if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: "01-moving/01-x"}); httpx.AsError(err).Status != 503 {
		t.Fatalf("offline uncached: %v", err)
	}
	if !strings.Contains(f.evs(), "error") {
		t.Fatalf("no error event: %s", f.evs())
	}
}

func TestLoadModuleRestart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	release := make(chan struct{})
	f.sup.release = release
	_ = f.mods.Write([]string{"base", "agent", "git"})
	res, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID})
	if err != nil {
		t.Fatal(err)
	}
	// The early result: the restart is under way, the step not active yet.
	if !res.Restarting || strings.Join(res.Modules, " ") != "base agent git editing files" || res.Step.StepID != "open" {
		t.Fatalf("result %+v", res)
	}
	if b := f.core.Busy(); b == nil || b.Kind != OpLoad || b.LessonID != lessonID || b.StepID != "open" {
		t.Fatalf("busy %+v", b)
	}
	// A repeated request waits behind the restart and gets the final result.
	repeat := make(chan protocol.ScenarioResult, 1)
	go func() {
		res, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID})
		if err != nil {
			t.Error(err)
		}
		repeat <- res
	}()
	select {
	case r := <-repeat:
		t.Fatalf("a load passed the module restart: %+v", r)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if r := <-repeat; r.Restarting || r.Step.StepID != "open" {
		t.Fatalf("repeat after the restart %+v", r)
	}
	if f.sup.numRestarts() != 1 {
		t.Fatalf("restarts %d", f.sup.numRestarts())
	}
	// The restart is over once the lesson is set up (the browser holds keys until then).
	want := "modules:base,agent,git,editing,files:restarting step:open:loaded modules:base,agent,git,editing,files:done"
	if f.evs() != want {
		t.Fatalf("events %s", f.evs())
	}
	if strings.Join(f.mods.Current(), " ") != "base agent git editing files" {
		t.Fatal("modules.el not written")
	}
	f.idle()
	if f.core.Busy() != nil {
		t.Fatal("still busy")
	}
}

// A drop of the lesson during a module restart lets the restart finish
// (modules.el is written; the restart saves the modified buffers) and
// cancels the load before the step activates.
func TestDropDuringModuleRestart(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	f.sup.release = release
	res, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: lessonID})
	if err != nil || !res.Restarting {
		t.Fatalf("load %+v %v", res, err)
	}
	dropped := make(chan error, 1)
	go func() { dropped <- f.core.Drop(context.Background(), lessonID, 1) }()
	f.waitFor("the drop to cancel the load", func() bool { return f.queued() == 1 })
	close(release)
	if err := <-dropped; err != nil {
		t.Fatal(err)
	}
	f.idle()
	f.sup.mu.Lock()
	cut := f.sup.cutShort
	f.sup.mu.Unlock()
	if cut != 0 || f.sup.numRestarts() != 1 {
		t.Fatalf("restarts %d, cut short %d", f.sup.numRestarts(), cut)
	}
	if a := f.core.Active(); a != nil {
		t.Fatalf("active after the drop %+v", a)
	}
	if evs := f.evs(); evs != "modules:base,agent,editing,files:restarting modules:base,agent,editing,files:done" {
		t.Fatalf("events %s", evs)
	}
}

// A load of another lesson during a module restart runs after it, and
// wins; an invalid one fails without disturbing it.
func TestLoadsQueueBehindARestart(t *testing.T) {
	for _, tc := range []struct {
		name, other, scenario, step string
		ok                          bool
	}{
		{"another lesson", "01-moving/01-basics", `{"id":"01-moving/01-basics","modules":["base","agent"],"steps":[{"id":"one","conditions":[{"event":{"command":"next-line"}}]}]}`, "", true},
		{"no such lesson", "01-moving/01-basics", "", "", false},
		{"no such step", "01-moving/01-basics", `{"id":"01-moving/01-basics","steps":[{"id":"one"}]}`, "missing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			release := make(chan struct{})
			f.sup.release = release
			if tc.scenario != "" {
				f.fetch.scenarios[tc.other] = tc.scenario
			}
			ctx := context.Background()
			res, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID})
			if err != nil || !res.Restarting {
				t.Fatalf("first load %+v %v", res, err)
			}
			second := make(chan error, 1)
			go func() {
				_, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: tc.other, StepID: tc.step})
				second <- err
			}()
			close(release)
			if err := <-second; (err == nil) != tc.ok {
				t.Fatalf("second load: %v", err)
			}
			want := lessonID
			if tc.ok {
				want = tc.other
			}
			if a := f.core.Active(); a == nil || a.LessonID != want {
				t.Fatalf("active %+v, want %s", a, want)
			}
		})
	}
}

func TestSetModules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	release := make(chan struct{})
	f.sup.release = release
	restarting, err := f.core.SetModules(ctx, []string{"base", "agent", "files"})
	if err != nil || !restarting {
		t.Fatalf("set modules %v %v", restarting, err)
	}
	if b := f.core.Busy(); b == nil || b.Kind != OpSetModules {
		t.Fatalf("busy %+v", b)
	}
	close(release)
	f.idle()
	if f.evs() != "modules:base,agent,files:restarting modules:base,agent,files:done" {
		t.Fatalf("events %s", f.evs())
	}
	if restarting, _ := f.core.SetModules(ctx, []string{"base", "agent", "files"}); restarting {
		t.Fatal("unchanged set should not restart")
	}
	if _, err := f.core.SetModules(ctx, []string{"base"}); err == nil {
		t.Fatal("agent cannot be disabled")
	}
	if _, err := f.core.SetModules(ctx, []string{"base", "agent", "Bad"}); err == nil {
		t.Fatal("bad name")
	}
	if f.sup.numRestarts() != 1 {
		t.Fatalf("restarts %d", f.sup.numRestarts())
	}
}

func TestResume(t *testing.T) {
	ctx := context.Background()
	activeDuringCall := false
	watch := func(f *fixture, _ *Deps) {
		// Emacs is fresh: the step becomes active only once its buffers are back.
		f.agent.onCall = func(protocol.AgentOp) { activeDuringCall = activeDuringCall || f.core.Active() != nil }
		f.snaps.onOpen = func() { activeDuringCall = activeDuringCall || f.core.Active() != nil }
	}

	// No snapshot: the lesson's setup, without reset.
	f := newFixture(t, watch)
	if err := f.core.Resume(ctx, protocol.StepRef{LessonID: lessonID, StepID: "save"}); err != nil {
		t.Fatal(err)
	}
	if a := f.core.Active(); a == nil || a.StepID != "save" {
		t.Fatalf("resume active %+v", a)
	}
	if calls := f.agent.setups(); len(calls) != 1 || calls[0].Reset || len(calls[0].Open) != 1 || len(f.snaps.opens) != 0 {
		t.Fatalf("resume without a snapshot runs the setup without reset: %+v %v", calls, f.snaps.opens)
	}
	if f.snaps.captured() != "" || activeDuringCall {
		t.Fatalf("resume took checkpoints %v or activated the step before Emacs was set up", f.snaps.captured())
	}
	// A lesson loaded since startup wins over the saved step.
	if err := f.core.Resume(ctx, protocol.StepRef{LessonID: lessonID, StepID: "open"}); err != nil {
		t.Fatal(err)
	}
	if a := f.core.Active(); a == nil || a.StepID != "save" || len(f.agent.setups()) != 1 {
		t.Fatalf("a second resume replaced the active step: %+v", a)
	}

	// A snapshot of the lesson's files is re-opened instead.
	snap := &protocol.WindowSpec{Files: []protocol.FileSpec{{Path: "hello/src/main.rs", Point: 12, Visible: true}}, Layout: "single"}
	g := newFixture(t, watch, func(f *fixture, _ *Deps) { f.snaps.spec = snap })
	if err := g.core.Resume(ctx, protocol.StepRef{LessonID: lessonID, StepID: "save"}); err != nil {
		t.Fatal(err)
	}
	if len(g.snaps.opens) != 1 || g.snaps.opens[0] != snap || len(g.agent.setups()) != 0 || activeDuringCall {
		t.Fatalf("resume with a snapshot: opens %v setups %+v", g.snaps.opens, g.agent.setups())
	}
	if a := g.core.Active(); a == nil || a.StepID != "save" {
		t.Fatalf("resume active %+v", a)
	}

	// A snapshot without the lesson's files predates the lesson: setup.
	stale := &protocol.WindowSpec{Files: []protocol.FileSpec{{Path: "editing/src/main.rs"}}}
	h := newFixture(t, func(f *fixture, _ *Deps) { f.snaps.spec = stale })
	if err := h.core.Resume(ctx, protocol.StepRef{LessonID: lessonID, StepID: "save"}); err != nil {
		t.Fatal(err)
	}
	if len(h.snaps.opens) != 0 || len(h.agent.setups()) != 1 {
		t.Fatalf("a stale snapshot: opens %v setups %+v", h.snaps.opens, h.agent.setups())
	}
}

const demoLesson = `{
  "id": "02-buffers-and-files/04-switch-save-kill",
  "title": "Switching",
  "modules": ["base", "agent", "editing", "files"],
  "setup": {"workspace": "fixtures/projects", "root": "projects", "reset": true, "open": ["projects/hello/README.md"]},
  "steps": [
    {"id": "open", "goal": "Open", "demo": "C-x C-f \"main.rs\" RET", "conditions": [{"event": {"command": "find-file"}}]},
    {"id": "save", "goal": "Save", "demo": "C-x C-s", "conditions": [{"event": {"command": "save-buffer"}}]},
    {"id": "kill", "goal": "Kill", "demo": "C-x k RET", "conditions": [{"event": {"command": "kill-buffer"}}]}
  ]
}`

const demoID = "02-buffers-and-files/04-switch-save-kill"

func newDemoFixture(t *testing.T, mutate ...func(*fixture, *Deps)) *fixture {
	f := newFixture(t, append([]func(*fixture, *Deps){func(f *fixture, _ *Deps) {
		f.fetch.scenarios[demoID] = demoLesson
		f.fetch.fixtures[demoID] = tarball(t, map[string]string{"hello/README.md": "# hello\n"})
		_ = f.mods.Write([]string{"base", "agent", "editing", "files"})
	}}, mutate...)...)
	return f
}

func (f *fixture) played() string {
	f.sup.mu.Lock()
	defer f.sup.mu.Unlock()
	return strings.Join(f.sup.played, "|")
}

func TestGoToStepRestoresItsCheckpoint(t *testing.T) {
	f := newDemoFixture(t)
	f.snaps.restorable["kill"] = true
	res, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "kill", Reset: true})
	if err != nil || res.Index != 2 {
		t.Fatalf("load %+v %v", res, err)
	}
	// Setup ran (with reset), then the step's checkpoint was restored: no
	// replay, and the checkpoint is not retaken from the initial state.
	if calls := f.agent.setups(); len(calls) != 1 || !calls[0].Reset {
		t.Fatalf("setup %+v", calls)
	}
	if strings.Join(f.snaps.restored, ",") != "kill" || f.played() != "" || f.snaps.captured() != "" {
		t.Fatalf("restored %v played %v checkpoints %v", f.snaps.restored, f.played(), f.snaps.captured())
	}
	if f.evs() != "step:kill:loaded" {
		t.Fatalf("events %s", f.evs())
	}
}

func TestGoToStepFastForwards(t *testing.T) {
	f := newDemoFixture(t)
	res, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "save"})
	if err != nil || res.Index != 1 {
		t.Fatalf("load %+v %v", res, err)
	}
	// No checkpoint for save: step open's demo is replayed, quietly, and
	// save's checkpoint is taken (forced) from the result.
	if strings.Join(f.snaps.restored, ",") != "" || f.played() != "C-x|C-f|main.rs|RET" {
		t.Fatalf("restored %v played %v", f.snaps.restored, f.played())
	}
	if f.snaps.captured() != demoID+"/save" {
		t.Fatalf("checkpoints %v", f.snaps.captured())
	}
	if f.evs() != "step:save:loaded" {
		t.Fatalf("the replay must not emit: %s", f.evs())
	}
	if a := f.core.Active(); a == nil || a.StepID != "save" || f.dirty() {
		t.Fatalf("active %+v dirty %v", a, f.dirty())
	}
	// Nothing was recorded for the replayed step.
	if ls := f.core.Saved().Lessons[demoID]; ls != nil && (ls.Done["open"] != 0 || ls.Skipped["open"] != 0) {
		t.Fatalf("replayed step recorded %+v", ls)
	}

	// The first step never restores or replays: its checkpoint is the
	// lesson's initial state, retaken with reset.
	f.sup.mu.Lock()
	f.sup.played = nil
	f.sup.mu.Unlock()
	if _, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "open", Reset: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.snaps.restored) != 0 || f.played() != "" || f.snaps.captured() != demoID+"/save,"+demoID+"/open" {
		t.Fatalf("first step: restored %v played %v checkpoints %v", f.snaps.restored, f.played(), f.snaps.captured())
	}
}

func TestGoToStepRestoreFailure(t *testing.T) {
	f := newDemoFixture(t)
	f.snaps.restoreErr = errors.New("reset-step failed")
	_, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "kill"})
	if err == nil || f.played() != "" || f.snaps.captured() != "" || f.core.Active() != nil {
		t.Fatalf("error=%v played=%v checkpoints=%v active=%v", err, f.played(), f.snaps.captured(), f.core.Active())
	}
}

// A fast-forward that does not bring the workspace to the step (a demo
// fails, or the replay runs out of time) activates the step without taking
// its checkpoint: the lesson's initial state never becomes a later step's.
func TestIncompleteFastForwardTakesNoCheckpoint(t *testing.T) {
	t.Run("demo fails", func(t *testing.T) {
		f := newDemoFixture(t)
		f.sup.onKeys = func(context.Context) error { return errors.New("no frame") }
		res, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "save"})
		if err != nil || res.Index != 1 {
			t.Fatalf("load %+v %v", res, err)
		}
		if a := f.core.Active(); a == nil || a.StepID != "save" || f.snaps.captured() != "" {
			t.Fatalf("active %+v checkpoints %q", a, f.snaps.captured())
		}
	})
	t.Run("out of time", func(t *testing.T) {
		f := newDemoFixture(t)
		// The replay hangs; the load's deadline leaves it 200 ms.
		f.sup.onKeys = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
		v, err := f.core.do(context.Background(), &Op{Kind: OpLoad, Req: protocol.ScenarioRequest{LessonID: demoID, StepID: "kill"},
			Deadline: activationReserve + 200*time.Millisecond, info: OpInfo{LessonID: demoID}})
		if err != nil || v.(protocol.ScenarioResult).Index != 2 {
			t.Fatalf("load %+v %v", v, err)
		}
		if a := f.core.Active(); a == nil || a.StepID != "kill" || f.snaps.captured() != "" {
			t.Fatalf("active %+v checkpoints %q", a, f.snaps.captured())
		}
	})
	t.Run("no time left", func(t *testing.T) {
		f := newDemoFixture(t)
		ctx, cancel := context.WithTimeout(context.Background(), activationReserve/2)
		defer cancel()
		var sc protocol.Scenario
		if err := json.Unmarshal([]byte(demoLesson), &sc); err != nil {
			t.Fatal(err)
		}
		if f.core.fastForward(ctx, &sc, 2) || f.played() != "" {
			t.Fatalf("replayed with no time left: %q", f.played())
		}
	})
}

// A load interrupted mid-replay ends the rehearsal: the step active before
// it is active again, dirty, and "Reset this step" restores it.
func TestInterruptedFastForwardEndsTheRehearsal(t *testing.T) {
	f := newDemoFixture(t)
	if _, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: lessonID}); err != nil {
		t.Fatal(err)
	}
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
	f.core.mu.Lock()
	f.core.running.cancel()
	f.core.mu.Unlock()
	if err := <-load; err == nil {
		t.Fatal("the interrupted load succeeded")
	}
	f.idle()
	if a := f.core.Active(); a == nil || a.LessonID != lessonID || a.StepID != "open" || !f.dirty() {
		t.Fatalf("after the interrupted load: active %+v dirty %v", a, f.dirty())
	}
	if f.snaps.captured() != lessonID+"/open" {
		t.Fatalf("checkpoints %q", f.snaps.captured())
	}
	if _, err := f.core.Restore(context.Background(), protocol.RestoreRequest{}); err != nil {
		t.Fatalf("reset after the interrupted load: %v", err)
	}
	f.sup.mu.Lock()
	f.sup.onKeys = nil
	f.sup.mu.Unlock()
	if res, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "save"}); err != nil || res.Index != 1 {
		t.Fatalf("load %+v %v", res, err)
	}
}

const project = `{
  "id": "projects/p1-hello-cargo",
  "title": "Hello, cargo",
  "modules": ["base", "agent"],
  "tags": ["project"],
  "project": {"roots": ["projects/hello"], "lapMinutes": 20},
  "setup": {"workspace": "fixtures/hello", "root": "projects/hello", "reset": true, "repos": ["."], "run": "echo ran >> setup.log"},
  "steps": [
    {"id": "scaffold", "goal": "Scaffold", "demo": "a", "conditions": [{"event": {"command": "eshell"}}]},
    {"id": "commit", "goal": "Commit", "demo": "b", "conditions": [{"event": {"command": "magit-commit-create"}}]}
  ]
}`

// recShell records setup.run scripts.
type recShell struct {
	mu   sync.Mutex
	runs []string
	res  steps.ShellResult
}

func (s *recShell) Run(ctx context.Context, dir, script string, timeout time.Duration) steps.ShellResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, dir+": "+script)
	return s.res
}

// recGit records git invocations.
type recGit struct {
	mu   sync.Mutex
	runs []string
}

func (g *recGit) Run(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runs = append(g.runs, filepath.Base(dir)+": git "+strings.Join(args, " "))
	if args[0] == "init" {
		_ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	}
	return nil, nil
}

func TestProjectSetupAndLap(t *testing.T) {
	sh := &recShell{res: steps.ShellResult{OK: true}}
	g := &recGit{}
	id := "projects/p1-hello-cargo"
	f := newFixture(t, func(f *fixture, d *Deps) {
		f.fetch.scenarios[id] = project
		f.fetch.fixtures[id] = tarball(t, map[string]string{"Cargo.toml": "[package]\n"})
		d.Shell, d.Git = sh, g
		_ = f.mods.Write([]string{"base", "agent"})
	})
	ctx := context.Background()

	res, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil || res.Lap != nil {
		t.Fatalf("load %+v %v", res, err)
	}
	// setup.reset re-creates the repository on every load.
	want := "hello: git init -q -b main|hello: git config user.name Learner|hello: git config user.email learner@playground.invalid|hello: git add -A|hello: git commit -q --allow-empty --no-verify --no-gpg-sign -m Initial commit"
	if got := strings.Join(g.runs, "|"); got != want {
		t.Fatalf("git runs\n%s\nwant\n%s", got, want)
	}
	if len(sh.runs) != 1 || sh.runs[0] != filepath.Join(f.ws, "projects/hello")+": echo ran >> setup.log" {
		t.Fatalf("setup.run %v", sh.runs)
	}
	if f.core.Lap() != nil {
		t.Fatal("an ordinary load started a lap")
	}

	// A lap: reset, first step, timer started.
	f.core.activate(f.core.Scenario(), 1, activation{status: protocol.StepLoaded, phase: Active, lap: lapEnd})
	res, err = f.core.Load(ctx, protocol.ScenarioRequest{LessonID: id, Lap: true})
	if err != nil || res.Lap == nil || res.Lap.StartedAt == 0 || res.Step.StepID != "scaffold" {
		t.Fatalf("lap load %+v %v", res, err)
	}
	if f.core.Lap() == nil {
		t.Fatal("no lap running")
	}
	// Reloading during the lap keeps it and reports it.
	res, err = f.core.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil || res.Lap == nil {
		t.Fatalf("reload %+v %v", res, err)
	}
	res, err = f.core.Load(ctx, protocol.ScenarioRequest{LessonID: id, StepID: res.Step.StepID})
	if err != nil || res.Lap != nil || f.core.Lap() != nil {
		t.Fatalf("stop lap %+v %v", res, err)
	}
	if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: id, StepID: "commit", Lap: true}); httpx.AsError(err).Status != 400 {
		t.Fatalf("lap from a later step: %v", err)
	}
	// A failing setup.run is an error event, not a failed load.
	sh.mu.Lock()
	sh.res = steps.ShellResult{Output: "boom"}
	sh.mu.Unlock()
	if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: id, Reset: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.evs(), "error") {
		t.Fatalf("no error event for a failed setup.run: %s", f.evs())
	}
	if f.core.Lap() != nil {
		t.Fatal("an ordinary load kept the lap")
	}
}
