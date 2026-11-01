package session

import (
	"context"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// resume re-activates the saved step ref at startup (backend.md §3.2 step
// 6). No fixtures are extracted and no checkpoint is taken: the workspace
// stays as the learner left it. The fresh Emacs gets the lesson's buffers
// and windows back before the step is active (reopen), so the step never
// runs on an empty frame. A lesson loaded since startup wins over the
// saved step.
func (c *Core) resume(ctx context.Context, ref protocol.StepRef) error {
	if c.d.Engine.Active() != nil {
		return nil
	}
	sc, err := scenario.FetchScenario(ctx, c.d.Fetch, c.d.CacheDir, ref.LessonID, c.log)
	if err != nil {
		return err
	}
	if len(sc.Steps) == 0 {
		return nil
	}
	idx := sc.StepIndex(ref.StepID)
	if idx < 0 {
		idx = c.d.Engine.FirstIncomplete(sc)
	}
	c.reopen(ctx, sc)
	if ctx.Err() != nil {
		return interrupted(ctx)
	}
	c.d.Engine.ActivateLap(sc, idx, steps.LapKeep)
	return nil
}

// reopen puts the lesson's buffers and windows back in a fresh Emacs: the
// last editor state (saved at shutdown, else at most SnapshotInterval old)
// when it shows the lesson's files, else the lesson's setup without reset.
func (c *Core) reopen(ctx context.Context, sc *protocol.Scenario) {
	if c.d.Agent == nil {
		return
	}
	spec := c.d.Snapshots.LastEditorState()
	if !showsLesson(spec, sc.Setup.Root) {
		c.setup(ctx, sc, false)
		return
	}
	if !c.d.Sup.WaitReady(ctx, 15*time.Second) {
		c.log.Warn("re-opening the snapshot before the terminal frame is open", "lesson", sc.ID)
	}
	if err := c.d.Agent.Call(ctx, protocol.AgentOpOpen, spec, 10*time.Second, nil); err != nil {
		c.log.Warn("re-opening the snapshot failed; running the lesson setup", "lesson", sc.ID, "err", err)
		c.setup(ctx, sc, false)
	}
}

// showsLesson reports whether spec has a file under the lesson root (any
// file when the lesson has no root). A snapshot without one predates the
// lesson, or shows a frame the lesson does not use.
func showsLesson(spec *protocol.WindowSpec, root string) bool {
	if spec == nil {
		return false
	}
	root = strings.Trim(root, "/")
	for _, f := range spec.Files {
		if root == "" || f.Path == root || strings.HasPrefix(f.Path, root+"/") {
			return true
		}
	}
	return false
}
