package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
	"github.com/zsy/emacs-playground/internal/sessiond/agent"
	"github.com/zsy/emacs-playground/internal/sessiond/checkpoint"
	"github.com/zsy/emacs-playground/internal/sessiond/events"
	"github.com/zsy/emacs-playground/internal/sessiond/supervisor"
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
	case protocol.OpDropStep:
		var req protocol.DropStepRequest
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			return err
		}
		if req.LessonID == "" {
			return httpx.BadRequest("lessonId is required")
		}
		s.engine.Drop(req.LessonID)
		res = protocol.Empty{}
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
	cancel()
	meta, err := s.ckpt.Take(ctx, checkpoint.ManualRef(label), label, nil, spec)
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
	n, meta, err := s.ckpt.Restore(ctx, ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, httpx.NotFound("no such checkpoint")
		}
		return nil, httpx.Internal("restore failed").Wrap(err)
	}
	spec := meta.Spec
	if spec == nil {
		spec = &protocol.WindowSpec{Files: []protocol.FileSpec{}}
	}
	if s.agent.Connected() {
		if err := s.agent.Call(ctx, protocol.AgentOpResetStep, protocol.AgentResetStepArgs{Spec: *spec}, 10*time.Second, nil); err != nil {
			s.log.Warn("reset-step after restore", "err", err)
		}
	}
	if meta.Step != nil {
		s.engine.ResetTo(meta.Step.LessonID, meta.Step.StepID)
	} else if lesson, step, ok := splitStepRef(ref); ok {
		s.engine.ResetTo(lesson, step)
	}
	return protocol.RestoreResult{RestoredFiles: n}, nil
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
