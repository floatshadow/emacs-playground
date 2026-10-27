package scenario

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

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

// demoCommands maps the first key of a demo to the command the fake agent
// reports for it.
var demoCommands = map[string]string{"C-x C-f": "find-file", "C-x C-s": "save-buffer", "C-x k": "kill-buffer"}

func newDemoLoader(t *testing.T) (*loaderFixture, *[]string, *[]string) {
	f := newLoader(t)
	id := "02-buffers-and-files/04-switch-save-kill"
	f.fetch.scenarios[id] = demoLesson
	f.fetch.fixtures[id] = tarball(t, entry{name: "hello/README.md", body: "# hello\n"})
	_ = f.l.Modules.Write([]string{"base", "agent", "editing", "files"})
	var mu sync.Mutex
	var played, restored []string
	f.l.PlayDemo = func(ctx context.Context, demo string) error {
		mu.Lock()
		played = append(played, demo)
		mu.Unlock()
		for k, cmd := range demoCommands {
			if strings.HasPrefix(demo, k) {
				f.engine.OnKey(&protocol.KeyEvent{Keys: k, Cmd: cmd})
			}
		}
		return nil
	}
	f.l.RestoreStep = func(ctx context.Context, lessonID, stepID string) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		restored = append(restored, stepID)
		return stepID == "kill", nil
	}
	return f, &played, &restored
}

func TestGoToStepRestoresItsCheckpoint(t *testing.T) {
	f, played, restored := newDemoLoader(t)
	id := "02-buffers-and-files/04-switch-save-kill"
	res, err := f.l.Load(context.Background(), protocol.ScenarioRequest{LessonID: id, StepID: "kill", Reset: true})
	if err != nil || res.Index != 2 {
		t.Fatalf("load %+v %v", res, err)
	}
	// Setup ran (with reset), then the step's checkpoint was restored: no
	// replay, and the checkpoint is not retaken from the initial state.
	if len(f.agent.calls) != 1 || !f.agent.calls[0].Reset {
		t.Fatalf("setup %+v", f.agent.calls)
	}
	if strings.Join(*restored, ",") != "kill" || len(*played) != 0 || len(f.cps) != 0 {
		t.Fatalf("restored %v played %v checkpoints %v", *restored, *played, f.cps)
	}
	if f.evs() != "step:kill:loaded" {
		t.Fatalf("events %s", f.evs())
	}
}

func TestGoToStepFastForwards(t *testing.T) {
	f, played, restored := newDemoLoader(t)
	id := "02-buffers-and-files/04-switch-save-kill"
	res, err := f.l.Load(context.Background(), protocol.ScenarioRequest{LessonID: id, StepID: "save"})
	if err != nil || res.Index != 1 {
		t.Fatalf("load %+v %v", res, err)
	}
	// No checkpoint for save: step open's demo is replayed, quietly, and
	// save's checkpoint is taken (forced) from the result.
	if strings.Join(*restored, ",") != "save" || strings.Join(*played, "|") != `C-x C-f "main.rs" RET` {
		t.Fatalf("restored %v played %v", *restored, *played)
	}
	if strings.Join(f.cps, ",") != id+"/save!" {
		t.Fatalf("checkpoints %v", f.cps)
	}
	if f.evs() != "step:save:loaded" {
		t.Fatalf("the replay must not emit: %s", f.evs())
	}
	if a := f.engine.Active(); a == nil || a.StepID != "save" || f.engine.Dirty() {
		t.Fatalf("active %+v dirty %v", a, f.engine.Dirty())
	}
	// Nothing was recorded for the replayed step.
	if ls := f.engine.Saved().Lessons[id]; ls != nil && (ls.Done["open"] != 0 || ls.Skipped["open"] != 0) {
		t.Fatalf("replayed step recorded %+v", ls)
	}

	// The first step never restores or replays: its checkpoint is the
	// lesson's initial state.
	*played, *restored = nil, nil
	f.cps = nil
	if _, err := f.l.Load(context.Background(), protocol.ScenarioRequest{LessonID: id, StepID: "open", Reset: true}); err != nil {
		t.Fatal(err)
	}
	if len(*restored) != 0 || len(*played) != 0 || strings.Join(f.cps, ",") != id+"/open!" {
		t.Fatalf("first step: restored %v played %v checkpoints %v", *restored, *played, f.cps)
	}
}
