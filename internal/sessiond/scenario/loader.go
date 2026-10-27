package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// Fetcher is the gateway's internal curriculum API.
type Fetcher interface {
	Scenario(ctx context.Context, lessonID, etag string) (raw []byte, newETag string, notModified bool, err error)
	Fixtures(ctx context.Context, lessonID, etag string) (gwclient.FixtureResult, error)
}

// Agent runs the setup op.
type Agent interface {
	Call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error
}

// Restarter restarts the Emacs daemon with buffer restore.
type Restarter interface {
	RestartEmacs(ctx context.Context, reason string) error
}

// Loader loads scenarios. Load calls are serialised.
type Loader struct {
	Workspace string
	Root      *os.Root
	CacheDir  string
	Fetch     Fetcher
	Agent     Agent
	Restart   Restarter
	Modules   *Modules
	Engine    *steps.Engine
	// Checkpoint takes the checkpoint of a starting step; force retakes an
	// existing one.
	Checkpoint func(ctx context.Context, lessonID, stepID string, force bool) error
	// RestoreStep restores a step's checkpoint (files and windows) and
	// reports false when the step has none. "Go to this step" uses it for
	// steps after the first.
	RestoreStep func(ctx context.Context, lessonID, stepID string) (bool, error)
	// PlayDemo plays a demo string into Emacs at playctl pacing; the
	// fast-forward to a step without a checkpoint replays the earlier
	// steps' demos with it.
	PlayDemo func(ctx context.Context, demo string) error
	Emit     func(protocol.Event)
	Log      *slog.Logger
	// SetupTimeout bounds the agent's setup op (default 10 s).
	SetupTimeout time.Duration
	// Ready, when set, waits until Emacs is not restarting and the
	// terminal frame is open (supervisor.WaitReady); setup runs after it.
	Ready func(ctx context.Context, timeout time.Duration) bool

	mu sync.Mutex
	// pending is the lesson whose load waits for a daemon restart.
	pending string
	bg      sync.WaitGroup
}

func (l *Loader) log() *slog.Logger {
	if l.Log == nil {
		return slog.Default()
	}
	return l.Log
}

func (l *Loader) emit(ev protocol.Event) {
	if l.Emit != nil {
		l.Emit(ev)
	}
}

// Wait waits for background loads (tests, shutdown).
func (l *Loader) Wait() { l.bg.Wait() }

// Load implements rpc scenario.
func (l *Loader) Load(ctx context.Context, req protocol.ScenarioRequest) (protocol.ScenarioResult, error) {
	if !gwclient.ValidLessonID(req.LessonID) {
		return protocol.ScenarioResult{}, httpx.BadRequest("lessonId is not a valid lesson id")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.pending == req.LessonID && !req.Reset {
		return protocol.ScenarioResult{Step: protocol.StepRef{LessonID: req.LessonID, StepID: req.StepID},
			Modules: l.Modules.Current(), Restarting: true}, nil
	}
	if res, ok := l.alreadyActive(req); ok {
		return res, nil
	}

	sc, err := l.scenario(ctx, req.LessonID)
	if err != nil {
		return protocol.ScenarioResult{}, err
	}
	idx := l.Engine.FirstIncomplete(sc)
	if req.StepID != "" {
		idx = sc.StepIndex(req.StepID)
		if idx < 0 {
			return protocol.ScenarioResult{}, httpx.BadRequest(fmt.Sprintf("lesson %s has no step %q", sc.ID, req.StepID))
		}
	}
	if len(sc.Steps) == 0 {
		return protocol.ScenarioResult{}, httpx.Conflict("the lesson has no steps")
	}
	reset := req.Reset || sc.Setup.Reset

	extracted, err := l.fixtures(ctx, sc, reset)
	if err != nil {
		l.emit(&protocol.ErrorEvent{Code: protocol.ErrSessionUnavailable, Message: "Could not load the lesson files. Try again in a moment."})
		return protocol.ScenarioResult{}, err
	}

	cur := l.Modules.Current()
	want := Union(cur, sc.Modules)
	res := protocol.ScenarioResult{
		Step:  protocol.StepRef{LessonID: sc.ID, StepID: sc.Steps[idx].ID},
		Index: idx, Count: len(sc.Steps), Modules: want,
	}
	forceCheckpoint := reset && extracted > 0
	if !slices.Equal(cur, want) {
		if err := l.Modules.Write(want); err != nil {
			return protocol.ScenarioResult{}, httpx.Internal("could not write modules.el").Wrap(err)
		}
		res.Restarting = true
		l.pending = sc.ID
		l.restartForModules(want, func(bctx context.Context) {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.pending = ""
			l.finish(bctx, sc, idx, reset, forceCheckpoint)
		})
		return res, nil
	}
	l.finish(ctx, sc, idx, reset, forceCheckpoint)
	return res, nil
}

// alreadyActive answers a reload of the active lesson without re-running
// setup.
func (l *Loader) alreadyActive(req protocol.ScenarioRequest) (protocol.ScenarioResult, bool) {
	if req.Reset {
		return protocol.ScenarioResult{}, false
	}
	sc := l.Engine.Scenario()
	act := l.Engine.Active()
	if sc == nil || act == nil || sc.ID != req.LessonID {
		return protocol.ScenarioResult{}, false
	}
	if req.StepID != "" && req.StepID != act.StepID {
		return protocol.ScenarioResult{}, false
	}
	idx, n := l.Engine.ActiveIndex()
	return protocol.ScenarioResult{Step: *act, Index: idx, Count: n, Modules: l.Modules.Current()}, true
}

// FastForwardTimeout bounds the replay of earlier steps' demos when "Go to
// this step" targets a step without a checkpoint.
const FastForwardTimeout = 3 * time.Minute

// RehearsalSettle is how long the fast-forward waits for a replayed step's
// conditions after its demo.
const RehearsalSettle = 10 * time.Second

// finish runs setup, brings the workspace to the step's starting state,
// takes the checkpoint and activates the step. Caller holds l.mu.
//
// For the first step the starting state is the lesson's setup. For a later
// step ("Go to this step", "Do this step again", resuming a lesson) it is
// the step's checkpoint, restored after the setup when it exists; else the
// earlier steps' demos are replayed with nothing recorded and the
// checkpoint is taken from the result. A later step's checkpoint is never
// overwritten with the lesson's initial state (api.md §3.4).
func (l *Loader) finish(ctx context.Context, sc *protocol.Scenario, idx int, reset, forceCheckpoint bool) {
	l.setup(ctx, sc, reset)
	st := &sc.Steps[idx]
	if idx > 0 {
		if l.RestoreStep != nil {
			ok, err := l.RestoreStep(ctx, sc.ID, st.ID)
			if err != nil {
				l.log().Warn("restoring the step's checkpoint", "lesson", sc.ID, "step", st.ID, "err", err)
			}
			if ok {
				l.Engine.Activate(sc, idx)
				return
			}
		}
		l.fastForward(ctx, sc, idx)
		forceCheckpoint = true
	}
	if st.TakesCheckpoint() && l.Checkpoint != nil {
		if err := l.Checkpoint(ctx, sc.ID, st.ID, forceCheckpoint); err != nil {
			l.log().Warn("checkpoint at scenario load", "err", err)
		}
	}
	l.Engine.Activate(sc, idx)
}

// fastForward replays the demos of the steps before idx into Emacs, each
// rehearsed by the engine (Engine.Rehearse: conditions are evaluated but
// nothing is recorded or emitted), waiting for each step's conditions before
// the next demo. A step whose demo does not complete it is logged and the
// replay goes on.
func (l *Loader) fastForward(ctx context.Context, sc *protocol.Scenario, idx int) {
	if l.PlayDemo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), FastForwardTimeout)
	defer cancel()
	start := time.Now()
	for i := 0; i < idx && ctx.Err() == nil; i++ {
		st := &sc.Steps[i]
		l.Engine.Rehearse(sc, i)
		if st.Demo == "" {
			l.log().Warn("fast-forward: the step has no demo", "lesson", sc.ID, "step", st.ID)
			continue
		}
		if err := l.PlayDemo(ctx, st.Demo); err != nil {
			l.log().Warn("fast-forward: playing the demo", "lesson", sc.ID, "step", st.ID, "err", err)
			continue
		}
		if !l.rehearsed(ctx, st.ID) {
			l.log().Warn("fast-forward: the demo did not complete the step", "lesson", sc.ID, "step", st.ID)
		}
	}
	l.log().Info("fast-forwarded", "lesson", sc.ID, "to", sc.Steps[idx].ID, "took", time.Since(start).Round(time.Millisecond))
}

// rehearsed polls the rehearsed step's conditions for up to RehearsalSettle.
func (l *Loader) rehearsed(ctx context.Context, stepID string) bool {
	deadline := time.Now().Add(RehearsalSettle)
	for {
		res, ok := l.Engine.Check(ctx)
		if ok && res.StepID == stepID && res.Complete {
			return true
		}
		if !ok || time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (l *Loader) setup(ctx context.Context, sc *protocol.Scenario, reset bool) {
	s := sc.Setup
	layout := s.Layout
	if strings.HasSuffix(layout, ".eld") {
		// TODO(sessiond, CURRICULUM.md Part B.3): window-state files live in
		// the lesson directory; the bundle does not ship them to sessiond yet.
		l.log().Warn("setup.layout window-state files are not supported yet", "layout", layout)
		layout = ""
	}
	if len(s.Open) == 0 && layout == "" && s.Eval == "" && s.Compile == "" {
		return
	}
	if l.Agent == nil {
		return
	}
	// Wait for a restart in progress and for the learner's frame, or the
	// layout would go to the daemon's invisible initial frame.
	if l.Ready != nil && !l.Ready(ctx, 15*time.Second) {
		l.log().Warn("setting up the lesson before the terminal frame is open", "lesson", sc.ID)
	}
	timeout := l.SetupTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	args := protocol.AgentSetupArgs{Open: s.Open, Layout: layout, Eval: s.Eval, Compile: s.Compile, Workspace: l.Workspace, Reset: reset, Root: s.Root}
	if err := l.Agent.Call(ctx, protocol.AgentOpSetup, args, timeout, nil); err != nil {
		l.log().Warn("scenario setup in Emacs failed", "lesson", sc.ID, "err", err)
		l.emit(&protocol.ErrorEvent{Code: protocol.ErrEmacsUnavailable, Message: "Emacs could not open the lesson's files; they are in the workspace."})
	}
}

// Resume re-activates the step saved in state.json after sessiond restarts
// (backend.md §3.2 step 6): no fixtures, setup or checkpoint.
func (l *Loader) Resume(ctx context.Context, ref protocol.StepRef) error {
	if !gwclient.ValidLessonID(ref.LessonID) {
		return fmt.Errorf("invalid saved lesson id %q", ref.LessonID)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	sc, err := l.scenario(ctx, ref.LessonID)
	if err != nil {
		return err
	}
	idx := sc.StepIndex(ref.StepID)
	if idx < 0 {
		idx = l.Engine.FirstIncomplete(sc)
	}
	if len(sc.Steps) == 0 {
		return nil
	}
	l.Engine.Activate(sc, idx)
	return nil
}

// SetModules implements rpc modules: it replaces the set exactly and
// restarts the daemon in the background when it changed.
func (l *Loader) SetModules(mods []string) (bool, error) {
	if err := ValidateModules(mods); err != nil {
		return false, httpx.BadRequest(err.Error())
	}
	if !slices.Contains(mods, "agent") {
		return false, httpx.BadRequest("the agent module cannot be disabled in the playground")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if slices.Equal(l.Modules.Current(), mods) {
		return false, nil
	}
	if err := l.Modules.Write(mods); err != nil {
		return false, httpx.Internal("could not write modules.el").Wrap(err)
	}
	l.restartForModules(mods, nil)
	return true, nil
}

// restartForModules announces and runs, in the background, the daemon
// restart that loads a changed module set, then calls after (when set)
// with the restart's context.
func (l *Loader) restartForModules(mods []string, after func(context.Context)) {
	l.emit(&protocol.ModulesEvent{Enabled: mods, Restarting: true})
	l.bg.Add(1)
	go func() {
		defer l.bg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := l.Restart.RestartEmacs(ctx, "module set changed"); err != nil {
			l.log().Error("restart for modules failed", "err", err)
		}
		l.emit(&protocol.ModulesEvent{Enabled: mods, Restarting: false})
		if after != nil {
			after(ctx)
		}
	}()
}

func (l *Loader) cachePath(kind, lessonID, ext string) string {
	return filepath.Join(l.CacheDir, kind, filepath.FromSlash(lessonID)+ext)
}

// cachedETag returns the ETag of the cached file p, "" when p is not cached.
func cachedETag(p string) string {
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	b, err := os.ReadFile(p + ".etag")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeCached(p string, data []byte, etag string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(p+".tmp", p); err != nil {
		return err
	}
	return os.WriteFile(p+".etag", []byte(etag), 0o644)
}

// scenario fetches the scenario, using and refreshing the cache.
func (l *Loader) scenario(ctx context.Context, lessonID string) (*protocol.Scenario, error) {
	p := l.cachePath("scenarios", lessonID, ".json")
	raw, newETag, notModified, err := l.Fetch.Scenario(ctx, lessonID, cachedETag(p))
	switch {
	case err != nil && gwclient.NotFound(err):
		return nil, httpx.NotFound("no such lesson: " + lessonID)
	case err != nil:
		cached, rerr := os.ReadFile(p)
		if rerr != nil {
			l.emit(&protocol.ErrorEvent{Code: protocol.ErrSessionUnavailable, Message: "Could not load the lesson. Try again in a moment."})
			return nil, httpx.SessionUnavailable("could not fetch the scenario from the gateway").Wrap(err)
		}
		l.log().Warn("gateway unreachable; using the cached scenario", "lesson", lessonID, "err", err)
		raw = cached
	case notModified:
		cached, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil, httpx.Internal("scenario cache missing").Wrap(rerr)
		}
		raw = cached
	default:
		if newETag != "" {
			if err := writeCached(p, raw, newETag); err != nil {
				l.log().Warn("caching scenario", "err", err)
			}
		}
	}
	var sc protocol.Scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		return nil, httpx.Internal("the gateway sent an invalid scenario").Wrap(err)
	}
	if sc.ID == "" {
		sc.ID = lessonID
	}
	sc.AssignIDs()
	return &sc, nil
}

// fixtures fetches (with the ETag cache) and extracts the lesson's
// fixtures; it returns the number of files written.
func (l *Loader) fixtures(ctx context.Context, sc *protocol.Scenario, reset bool) (int, error) {
	p := l.cachePath("fixtures", sc.ID, protocol.FixtureExt)
	fr, err := l.Fetch.Fixtures(ctx, sc.ID, cachedETag(p))
	switch {
	case err != nil && gwclient.NotFound(err):
		return 0, nil
	case err != nil:
		if _, serr := os.Stat(p); serr != nil {
			return 0, httpx.SessionUnavailable("could not fetch the lesson's files from the gateway").Wrap(err)
		}
		l.log().Warn("gateway unreachable; using cached fixtures", "lesson", sc.ID, "err", err)
	case fr.None:
		_ = os.Remove(p)
		_ = os.Remove(p + ".etag")
		return 0, nil
	case fr.NotModified:
	default:
		data, rerr := io.ReadAll(io.LimitReader(fr.Body, MaxFixtureTotal))
		fr.Body.Close()
		if rerr != nil {
			return 0, httpx.SessionUnavailable("could not download the lesson's files").Wrap(rerr)
		}
		if err := writeCached(p, data, fr.ETag); err != nil {
			return 0, httpx.Internal("could not cache the lesson's files").Wrap(err)
		}
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	res, err := Extract(f, l.Root, sc.Setup.Root, reset)
	if err != nil {
		return res.Written, httpx.Internal("could not unpack the lesson's files").Wrap(err)
	}
	l.log().Info("fixtures extracted", "lesson", sc.ID, "root", sc.Setup.Root, "written", res.Written, "skipped", res.Skipped, "removed", res.Removed)
	return res.Written, nil
}
