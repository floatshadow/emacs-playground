package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/events"
	"github.com/floatshadow/emacs-playground/internal/sessiond/snapshot"
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
		res, err = withBody(w, r, s.rpcKeys)
	case protocol.OpEval:
		res, err = withBody(w, r, s.rpcEval)
	case protocol.OpRescue:
		res, err = s.rpcRescue(ctx)
	case protocol.OpScenario:
		s.syncProgress(ctx, false)
		res, err = withBody(w, r, s.core.Load)
	case protocol.OpCheck:
		cr, ok := s.core.Check(ctx)
		if !ok {
			return httpx.Conflict("no step is active; load a lesson first")
		}
		res = cr
	case protocol.OpHint:
		res, err = withBody(w, r, s.rpcHint)
	case protocol.OpCheckpoint:
		res, err = withBody(w, r, s.rpcCheckpoint)
	case protocol.OpCheckpoints:
		items, lerr := s.snap.List(ctx)
		if lerr != nil {
			return httpx.Internal("could not list checkpoints").Wrap(lerr)
		}
		res = protocol.CheckpointsResult{Items: items}
	case protocol.OpRestore:
		res, err = withBody(w, r, s.rpcRestore)
	case protocol.OpMode:
		res, err = withBody(w, r, s.rpcMode)
	case protocol.OpModules:
		res, err = withBody(w, r, s.rpcModules)
	case protocol.OpRestartEmacs:
		s.core.RestartEmacs()
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
		res, err = withBody(w, r, s.rpcDescribe)
	case protocol.OpOpen:
		res, err = withBody(w, r, s.rpcOpen)
	case protocol.OpDropStep:
		res, err = withBody(w, r, s.rpcDropStep)
	case protocol.OpDemo:
		res, err = withBody(w, r, s.rpcDemo)
	case protocol.OpSkip:
		res, err = withBody(w, r, s.rpcSkip)
	default:
		return httpx.NotFound(fmt.Sprintf("unknown operation %q", op))
	}
	if err != nil {
		return err
	}
	httpx.WriteOK(w, res)
	return nil
}

// withBody decodes the request body into a Req and calls f with it.
func withBody[Req, Res any](w http.ResponseWriter, r *http.Request, f func(context.Context, Req) (Res, error)) (any, error) {
	var req Req
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return nil, err
	}
	return f(r.Context(), req)
}

func (s *Server) rpcHint(_ context.Context, req protocol.HintRequest) (any, error) {
	if req.StepID == "" || req.Level < 1 {
		return nil, httpx.BadRequest("stepId and a level of at least 1 are required")
	}
	return protocol.Empty{}, s.core.Hint(req.StepID, req.Level)
}

func (s *Server) rpcModules(ctx context.Context, req protocol.ModulesRequest) (any, error) {
	restarting, err := s.core.SetModules(ctx, req.Enabled)
	return protocol.ModulesResult{Restarting: restarting}, err
}

func (s *Server) rpcDescribe(ctx context.Context, req protocol.DescribeRequest) (any, error) {
	if _, err := protocol.ParseKeys(req.Keys); err != nil {
		return nil, httpx.BadRequest(err.Error())
	}
	var d protocol.DescribeResult
	if err := s.agent.Call(ctx, protocol.AgentOpDescribe, protocol.AgentDescribeArgs{Keys: req.Keys}, 2*time.Second, &d); err != nil {
		return nil, agentError(err)
	}
	return d, nil
}

// rpcDropStep forgets a lesson's active step; an empty lessonId forgets
// every lesson ("Start over").
func (s *Server) rpcDropStep(ctx context.Context, req protocol.DropStepRequest) (any, error) {
	if err := s.core.Drop(ctx, req.LessonID, req.Generation); err != nil {
		return nil, err
	}
	return protocol.Empty{}, nil
}

// agentError maps agent call failures to API errors.
func agentError(err error) error {
	var ce *agent.CallError
	switch {
	case errors.Is(err, agent.ErrNotConnected), errors.Is(err, agent.ErrClosed), errors.Is(err, snapshot.ErrDisconnected):
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
	s.core.DemoTraffic()
	if err := s.sup.SendKeys(ctx, req.Frame, req.Keys, req.Text); err != nil {
		return nil, keysError(err)
	}
	return protocol.Empty{}, nil
}

// keysError maps supervisor send-keys failures to API errors.
func keysError(err error) error {
	switch {
	case errors.Is(err, supervisor.ErrKeyNotSendable):
		return httpx.BadRequest(err.Error())
	case errors.Is(err, supervisor.ErrGUINotActive):
		return httpx.Conflict(err.Error())
	}
	return httpx.EmacsUnavailable("the keys could not be delivered").Wrap(err)
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
	v, err := s.evalRaw(ctx, req.Form, timeout)
	if err != nil {
		return nil, agentError(err)
	}
	if len(v) == 0 {
		v = json.RawMessage("null")
	}
	return protocol.EvalResult{Value: v}, nil
}

// evalRaw runs form through the agent's eval op and returns its value as
// the agent printed it.
func (s *Server) evalRaw(ctx context.Context, form string, timeout time.Duration) (json.RawMessage, error) {
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	err := s.agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: form}, timeout, &out)
	return out.Value, err
}

// rpcOpen visits a workspace file in the learner's window (the agent's
// visit op) after checking that the path is a file inside the workspace.
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
	name, err := s.snap.Visit(ctx, filepath.Join(s.cfg.Workspace, filepath.FromSlash(rel)))
	if err != nil {
		return nil, agentError(err)
	}
	return protocol.OpenResult{Buffer: name}, nil
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
	m, err := s.core.Checkpoint(ctx, label)
	if err != nil {
		return nil, httpx.Internal("could not take the checkpoint").Wrap(err)
	}
	return protocol.CheckpointResult{Ref: m.Ref, CreatedAt: m.CreatedAt}, nil
}

// rpcDemo brackets the browser's "Watch it" (api.md §3.4, §4.1).
func (s *Server) rpcDemo(ctx context.Context, req protocol.DemoRequest) (any, error) {
	switch req.Phase {
	case protocol.DemoStart:
		return s.core.DemoStart(ctx, req.StepID)
	case protocol.DemoEnd:
		return protocol.DemoResult{}, s.core.DemoEnd(req.StepID)
	}
	return nil, httpx.BadRequest("phase must be start or end")
}

// rpcSkip does the active step for the learner ("Skip this step", api.md
// §3.4).
func (s *Server) rpcSkip(ctx context.Context, req protocol.SkipRequest) (any, error) {
	return s.core.Skip(ctx, req.StepID)
}

func (s *Server) rpcRestore(ctx context.Context, req protocol.RestoreRequest) (any, error) {
	return s.core.Restore(ctx, req)
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
		if act := s.core.Active(); act != nil {
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
