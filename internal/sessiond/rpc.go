package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/events"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/supervisor"
)

// rpc dispatches POST /rpc/{op} (api.md §3.4).
func (s *Server) rpc(w http.ResponseWriter, r *http.Request) error {
	op := protocol.RPCOp(r.PathValue("op"))
	ctx := r.Context()
	var (
		res any
		err error
	)
	switch op {
	case protocol.OpKeys:
		var req protocol.KeysRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcKeys(ctx, req)
	case protocol.OpEval:
		var req protocol.EvalRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcEval(ctx, req)
	case protocol.OpRescue:
		res, err = s.rpcRescue(ctx)
	case protocol.OpScenario:
		var req protocol.ScenarioRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.loader.Load(ctx, req)
	case protocol.OpCheck:
		cr, ok := s.engine.Check(ctx)
		if !ok {
			return httpx.Conflict("no step is active; load a lesson first")
		}
		res = cr
	case protocol.OpHint:
		var req protocol.HintRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		if req.StepID == "" || req.Level < 1 {
			return httpx.BadRequest("stepId and a level of at least 1 are required")
		}
		if !s.engine.Hint(req.StepID, req.Level) {
			return httpx.Conflict("that step is not part of the active lesson")
		}
		res = protocol.Empty{}
	case protocol.OpCheckpoint:
		var req protocol.CheckpointRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcCheckpoint(ctx, req)
	case protocol.OpCheckpoints:
		items, lerr := s.ckpt.List(ctx)
		if lerr != nil {
			return httpx.Internal("could not list checkpoints").Wrap(lerr)
		}
		res = protocol.CheckpointsResult{Items: items}
	case protocol.OpRestore:
		var req protocol.RestoreRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcRestore(ctx, req)
	case protocol.OpMode:
		var req protocol.ModeRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcMode(ctx, req)
	case protocol.OpModules:
		var req protocol.ModulesRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		restarting, merr := s.loader.SetModules(req.Enabled)
		res, err = protocol.ModulesResult{Restarting: restarting}, merr
	case protocol.OpRestartEmacs:
		// The restart takes seconds; progress arrives as emacs events.
		s.bg.Add(1)
		go func() {
			defer s.bg.Done()
			rctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := s.sup.RestartEmacs(rctx, "requested"); err != nil {
				s.log.Error("restart-emacs", "err", err)
			}
		}()
		res = protocol.Empty{}
	case protocol.OpStuck:
		var req protocol.StuckRPCRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res = s.rpcStuck(r, req)
	case protocol.OpStatus:
		res = s.status()
	case protocol.OpDescribe:
		var req protocol.DescribeRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		if _, perr := protocol.ParseKeys(req.Keys); perr != nil {
			return httpx.BadRequest(perr.Error())
		}
		var d protocol.DescribeResult
		if cerr := s.agent.Call(ctx, protocol.AgentOpDescribe, protocol.AgentDescribeArgs{Keys: req.Keys}, 2*time.Second, &d); cerr != nil {
			return agentError(cerr)
		}
		res = d
	case protocol.OpOpen:
		var req protocol.OpenRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcOpen(ctx, req)
	case protocol.OpDropStep:
		var req protocol.DropStepRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		if req.LessonID != "" && !gwclient.ValidLessonID(req.LessonID) {
			return httpx.BadRequest("lessonId is not a valid lesson id")
		}
		// An empty lessonId forgets every lesson ("Start over").
		s.engine.Drop(req.LessonID)
		res = protocol.Empty{}
	case protocol.OpDemo:
		var req protocol.DemoRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcDemo(ctx, req)
	case protocol.OpSkip:
		var req protocol.SkipRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		res, err = s.rpcSkip(ctx, req)
	default:
		return httpx.NotFound(fmt.Sprintf("unknown operation %q", op))
	}
	if err != nil {
		return err
	}
	httpx.WriteOK(w, res)
	return nil
}

// agentError maps agent call failures to API errors.
func agentError(err error) error {
	var ce *agent.CallError
	switch {
	case errors.Is(err, agent.ErrNotConnected), errors.Is(err, agent.ErrClosed):
		return httpx.EmacsUnavailable("Emacs is not connected")
	case errors.Is(err, agent.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return httpx.Timeout("Emacs did not answer in time")
	case errors.As(err, &ce):
		return httpx.BadRequest(ce.Msg)
	}
	return err
}

func (s *Server) rpcKeys(ctx context.Context, req protocol.KeysRequest) (any, error) {
	if (req.Keys == "") == (req.Text == "") {
		return nil, httpx.BadRequest("send exactly one of keys and text")
	}
	if req.Keys != "" {
		if _, err := protocol.ParseKeys(req.Keys); err != nil {
			return nil, httpx.BadRequest(err.Error())
		}
	}
	if len(req.Text) > 4096 {
		return nil, httpx.BadRequest("text is limited to 4096 bytes")
	}
	switch req.Frame {
	case "", protocol.FrameTerminal, protocol.FrameGUI:
	default:
		return nil, httpx.BadRequest("frame must be terminal or gui")
	}
	if ok, reason := s.sup.Ready(); !ok {
		return nil, httpx.EmacsUnavailable(reason)
	}
	if err := s.sup.SendKeys(ctx, req.Frame, req.Keys, req.Text); err != nil {
		if strings.Contains(err.Error(), "cannot be sent") {
			return nil, httpx.BadRequest(err.Error())
		}
		if strings.Contains(err.Error(), "not active") {
			return nil, httpx.Conflict(err.Error())
		}
		return nil, httpx.EmacsUnavailable("the keys could not be delivered").Wrap(err)
	}
	return protocol.Empty{}, nil
}

func (s *Server) rpcEval(ctx context.Context, req protocol.EvalRequest) (any, error) {
	if !s.cfg.AuthorMode {
		return nil, httpx.Forbidden("eval is only available in author mode")
	}
	if strings.TrimSpace(req.Form) == "" {
		return nil, httpx.BadRequest("form is required")
	}
	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = agent.DefaultTimeout
	}
	if timeout > agent.MaxEvalTimeout {
		timeout = agent.MaxEvalTimeout
	}
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	if err := s.agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: req.Form}, timeout, &out); err != nil {
		return nil, agentError(err)
	}
	if len(out.Value) == 0 {
		out.Value = json.RawMessage("null")
	}
	return protocol.EvalResult{Value: out.Value}, nil
}

// rpcOpen visits a workspace file in the learner's window. The form is
// built here from a path checked to stay inside the workspace, so it is
// available without author mode (unlike eval).
func (s *Server) rpcOpen(ctx context.Context, req protocol.OpenRequest) (any, error) {
	rel := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(req.Path, "/")))
	if req.Path == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || strings.ContainsRune(rel, 0) {
		return nil, httpx.BadRequest("path must be a workspace-relative file")
	}
	root, err := os.OpenRoot(s.cfg.Workspace)
	if err != nil {
		return nil, httpx.Internal("workspace unavailable").Wrap(err)
	}
	defer root.Close()
	st, err := root.Stat(rel)
	if err != nil {
		return nil, httpx.NotFound("no such file in the workspace: " + rel)
	}
	if st.IsDir() {
		return nil, httpx.BadRequest("path is a directory")
	}
	abs := filepath.Join(s.cfg.Workspace, filepath.FromSlash(rel))
	form := "(progn (find-file " + elispString(abs) + ") (buffer-name))"
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	if err := s.agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: form}, agent.DefaultTimeout, &out); err != nil {
		return nil, agentError(err)
	}
	var name string
	_ = json.Unmarshal(out.Value, &name)
	return protocol.OpenResult{Buffer: name}, nil
}

// elispString quotes s as an Emacs Lisp string literal.
func elispString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func (s *Server) rpcRescue(ctx context.Context) (any, error) {
	if ok, reason := s.sup.Ready(); !ok {
		return nil, httpx.EmacsUnavailable(reason)
	}
	if s.sup.Unresponsive() || !s.agent.Connected() {
		// Emacs is busy: interrupt it without going through the agent.
		if err := s.sup.Interrupt(ctx); err != nil {
			return nil, httpx.EmacsUnavailable("Emacs is busy and could not be interrupted").Wrap(err)
		}
		return protocol.RescueResult{Did: []string{"interrupted Emacs (C-g)"}}, nil
	}
	var res protocol.AgentRescueResult
	if err := s.agent.Call(ctx, protocol.AgentOpRescue, nil, 5*time.Second, &res); err != nil {
		if errors.Is(err, agent.ErrTimeout) {
			_ = s.sup.Interrupt(ctx)
			return protocol.RescueResult{Did: []string{"interrupted Emacs (C-g)"}}, nil
		}
		return nil, agentError(err)
	}
	if res.Did == nil {
		res.Did = []string{}
	}
	return protocol.RescueResult(res), nil
}

func (s *Server) rpcCheckpoint(ctx context.Context, req protocol.CheckpointRequest) (any, error) {
	label := strings.TrimSpace(req.Label)
	if label == "" {
		return nil, httpx.BadRequest("label is required")
	}
	if len(label) > 200 {
		return nil, httpx.BadRequest("label is limited to 200 characters")
	}
	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	spec, _ := s.sup.Snapshot(sctx)
	bufs := s.bufferStates(sctx)
	cancel()
	meta, err := s.ckpt.TakeWith(ctx, checkpoint.Meta{Ref: checkpoint.ManualRef(label), Label: label, Spec: spec, Buffers: bufs})
	if err != nil {
		return nil, httpx.Internal("could not take the checkpoint").Wrap(err)
	}
	s.bus.Publish(&protocol.CheckpointEvent{Label: meta.Label, Ref: meta.Ref, CreatedAt: meta.CreatedAt})
	return protocol.CheckpointResult{Ref: meta.Ref, CreatedAt: meta.CreatedAt}, nil
}

// resolveRestore turns a restore request into a ref.
func (s *Server) resolveRestore(ctx context.Context, req protocol.RestoreRequest) (string, error) {
	switch {
	case req.Ref != "" && req.Label != "":
		return "", httpx.BadRequest("send either label or ref, not both")
	case req.Ref != "":
		if !checkpoint.ValidRef(req.Ref) {
			return "", httpx.BadRequest("ref must be a refs/playground/ checkpoint")
		}
		return req.Ref, nil
	case req.Label != "":
		if ref := checkpoint.ManualRef(req.Label); s.ckpt.Exists(ctx, ref) {
			if meta, err := s.ckpt.ReadMeta(ref); err != nil || meta.Label == req.Label {
				return ref, nil
			}
		}
		items, err := s.ckpt.List(ctx)
		if err != nil {
			return "", httpx.Internal("could not list checkpoints").Wrap(err)
		}
		for _, it := range items {
			if it.Label == req.Label {
				return it.Ref, nil
			}
		}
		return "", httpx.NotFound("no checkpoint labelled " + req.Label)
	default:
		act := s.engine.Active()
		if act == nil {
			return "", httpx.Conflict("no step is active; name a checkpoint")
		}
		return checkpoint.StepRef(act.LessonID, act.StepID), nil
	}
}

func (s *Server) rpcRestore(ctx context.Context, req protocol.RestoreRequest) (any, error) {
	ref, err := s.resolveRestore(ctx, req)
	if err != nil {
		return nil, err
	}
	n, meta, err := s.restoreCheckpoint(ctx, ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, httpx.NotFound("no such checkpoint")
		}
		return nil, httpx.Internal("restore failed").Wrap(err)
	}
	if meta.Step != nil {
		s.engine.ResetTo(meta.Step.LessonID, meta.Step.StepID)
	} else if lesson, step, ok := splitStepRef(ref); ok {
		s.engine.ResetTo(lesson, step)
	}
	return protocol.RestoreResult{RestoredFiles: n}, nil
}

// restoreCheckpoint puts the workspace files back to ref and asks the agent
// to restore the windows recorded with it (reset-step). The module set
// (emacs.d/modules.el) is kept: modules only ever grow (emacs.md §4), and
// the running Emacs has the current set loaded. A missing checkpoint is
// os.ErrNotExist.
func (s *Server) restoreCheckpoint(ctx context.Context, ref string) (int, checkpoint.Meta, error) {
	mods := s.modules.Current()
	n, meta, err := s.ckpt.Restore(ctx, ref)
	if err != nil {
		return 0, meta, err
	}
	if after := s.modules.Current(); !slices.Equal(after, mods) {
		if err := s.modules.Write(mods); err != nil {
			s.log.Warn("keeping modules.el across a restore", "err", err)
		}
	}
	spec := meta.Spec
	if spec == nil {
		spec = &protocol.WindowSpec{Files: []protocol.FileSpec{}}
	}
	if s.agent.Connected() {
		if err := s.agent.Call(ctx, protocol.AgentOpResetStep, protocol.AgentResetStepArgs{Spec: *spec}, 10*time.Second, nil); err != nil {
			s.log.Warn("reset-step after restore", "err", err)
		}
		// reset-step put the buffers on the restored files; the edits
		// that were unsaved when the checkpoint was taken and the undo
		// histories go back.
		s.restoreBuffers(ctx, meta.Buffers, spec)
	}
	return n, meta, nil
}

// restoreStep restores a step's checkpoint for the scenario loader ("Go to
// this step"): files and windows, no engine change (the loader activates
// the step next). It reports false when the step has no checkpoint.
func (s *Server) restoreStep(ctx context.Context, lessonID, stepID string) (bool, error) {
	ref := checkpoint.StepRef(lessonID, stepID)
	if !s.ckpt.Exists(ctx, ref) {
		return false, nil
	}
	if _, _, err := s.restoreCheckpoint(ctx, ref); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// restartIfDirty puts the active step back to its checkpoint when the
// learner ran a command in it since it started or was last reset, without
// counting a reset (demo start and skip, api.md §3.4). It reports whether
// it restored.
func (s *Server) restartIfDirty(ctx context.Context, act protocol.StepRef) bool {
	if !s.engine.Dirty() {
		return false
	}
	if _, _, err := s.restoreCheckpoint(ctx, checkpoint.StepRef(act.LessonID, act.StepID)); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.log.Warn("restoring the step before its demo", "lesson", act.LessonID, "step", act.StepID, "err", err)
		}
		return false
	}
	return s.engine.Restart(act.LessonID, act.StepID)
}

// rpcDemo brackets the browser's "Watch it" (api.md §3.4, §4.1).
func (s *Server) rpcDemo(ctx context.Context, req protocol.DemoRequest) (any, error) {
	if req.Phase != protocol.DemoStart && req.Phase != protocol.DemoEnd {
		return nil, httpx.BadRequest("phase must be start or end")
	}
	act := s.engine.Active()
	if act == nil || req.StepID == "" || act.StepID != req.StepID {
		return nil, httpx.Conflict("that step is not the active step")
	}
	if req.Phase == protocol.DemoEnd {
		if !s.engine.EndDemo(req.StepID) {
			return nil, httpx.Conflict("that step is not the active step")
		}
		return protocol.DemoResult{}, nil
	}
	restored := s.restartIfDirty(ctx, *act)
	if !s.engine.StartDemo(req.StepID) {
		return nil, httpx.Conflict("that step is not the active step")
	}
	return protocol.DemoResult{Restored: restored}, nil
}

// SkipTimeout bounds rpc skip: the demo, the wait for its conditions and
// the next step's checkpoint.
const SkipTimeout = 60 * time.Second

// skipSettle is how long rpc skip waits for the step's conditions after
// its demo (file events, compilation output).
const skipSettle = 8 * time.Second

// rpcSkip does the active step for the learner ("Skip this step", api.md
// §3.4): back to the step's checkpoint when commands ran, the step's demo
// played into Emacs with completions held, then the step is recorded
// skipped and the next one starts.
func (s *Server) rpcSkip(ctx context.Context, req protocol.SkipRequest) (any, error) {
	act := s.engine.Active()
	if act == nil || req.StepID == "" || act.StepID != req.StepID {
		return nil, httpx.Conflict("that step is not the active step")
	}
	if !s.skipMu.TryLock() {
		return nil, httpx.Conflict("a skip is already running")
	}
	defer s.skipMu.Unlock()
	// The skip finishes even when the browser goes away.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), SkipTimeout)
	defer cancel()
	var demo string
	if sc := s.engine.Scenario(); sc != nil && sc.ID == act.LessonID {
		if i := sc.StepIndex(act.StepID); i >= 0 {
			demo = sc.Steps[i].Demo
		}
	}
	s.restartIfDirty(ctx, *act)
	if !s.engine.BeginSkip(act.StepID) {
		return nil, httpx.Conflict("that step is not the active step")
	}
	completed := false
	if demo != "" {
		if err := s.playDemo(ctx, demo); err != nil {
			s.log.Warn("skip: playing the demo", "lesson", act.LessonID, "step", act.StepID, "err", err)
		}
		completed = s.awaitComplete(ctx, act.StepID, skipSettle)
		if !completed {
			s.log.Warn("skip: the step's demo did not complete it (curriculum defect)", "lesson", act.LessonID, "step", act.StepID)
		}
	}
	next, ok := s.engine.FinishSkip(ctx, act.StepID)
	if !ok {
		s.engine.AbortSkip()
		return nil, httpx.Conflict("the step changed while it was being skipped")
	}
	return protocol.SkipResult{Next: next, DemoCompleted: completed}, nil
}

// awaitComplete evaluates the active step until its conditions hold, it is
// no longer stepID, or d passes.
func (s *Server) awaitComplete(ctx context.Context, stepID string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		res, ok := s.engine.Check(ctx)
		if !ok || res.StepID != stepID {
			return false
		}
		if res.Complete {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// playDemo plays a demo string into the frame the learner sees, at
// playctl's pacing (protocol.PlayDemo).
func (s *Server) playDemo(ctx context.Context, demo string) error {
	return protocol.PlayDemo(ctx, demo, 1, func(ctx context.Context, req protocol.KeysRequest) error {
		if ok, reason := s.sup.Ready(); !ok {
			return errors.New(reason)
		}
		return s.sup.SendKeys(ctx, "", req.Keys, req.Text)
	})
}

// splitStepRef parses refs/playground/<part>/<lesson>/<step>.
func splitStepRef(ref string) (lesson, step string, ok bool) {
	rest := strings.TrimPrefix(ref, checkpoint.RefPrefix)
	if rest == ref || strings.HasPrefix(ref, checkpoint.ManualPrefix) {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '/')
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

func (s *Server) rpcMode(ctx context.Context, req protocol.ModeRequest) (any, error) {
	switch req.Mode {
	case protocol.FrameTerminal, protocol.FrameGUI:
	default:
		return nil, httpx.BadRequest("mode must be terminal or gui")
	}
	if req.Mode == protocol.FrameGUI {
		if ok, reason := s.sup.Ready(); !ok {
			return nil, httpx.EmacsUnavailable(reason)
		}
	}
	mctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	m, err := s.sup.SetMode(mctx, req.Mode)
	if err != nil {
		if errors.Is(err, supervisor.ErrGUIDisabled) {
			return nil, httpx.Conflict(err.Error())
		}
		return nil, httpx.EmacsUnavailable("could not switch frames").Wrap(err)
	}
	return protocol.ModeResult{Mode: m}, nil
}

// viaProxy reports whether a request came from the browser through the
// gateway's reverse proxy (which sets X-Forwarded-For) rather than from the
// gateway itself (sessions.Call sets X-Playground-User but no forwarding
// headers).
func viaProxy(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Forwarded") != ""
}

// rpcStuck returns the last 200 events and, when the browser called it
// directly through the gateway proxy, files the report with the gateway
// (api.md §3.4). The browser's "I'm stuck" button uses POST /api/v1/stuck
// instead: the gateway calls this op itself and stores the events, so
// filing here as well would duplicate the report.
func (s *Server) rpcStuck(r *http.Request, req protocol.StuckRPCRequest) any {
	evs := s.bus.Recent(events.StuckWindow)
	if evs == nil {
		evs = []json.RawMessage{}
	}
	if s.gw != nil && viaProxy(r) {
		rep := protocol.InternalStuckReport{Note: req.Note, Events: evs}
		if act := s.engine.Active(); act != nil {
			rep.LessonID, rep.StepID = act.LessonID, act.StepID
		}
		s.bg.Add(1)
		go func() {
			defer s.bg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := s.gw.Stuck(ctx, rep); err != nil {
				s.log.Warn("filing the stuck report", "err", err)
			}
		}()
	}
	return protocol.StuckRPCResult{Events: evs}
}
