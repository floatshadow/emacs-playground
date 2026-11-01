package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
)

// GlobalVars are the Emacs session variables a checkpoint keeps: steps
// rely on the kill ring (M-y after C-y), the minibuffer and search
// histories (M-p in M-x, C-s C-s), and the keyboard macro and its counter.
// A demonstration followed by "Do it yourself" would otherwise leave the
// demo's kills, commands and macro counts in them. The list is curriculum
// knowledge; the agent's capture-editor applies the printing rules.
var GlobalVars = []string{
	"kill-ring", "extended-command-history", "minibuffer-history", "file-name-history",
	"buffer-name-history", "search-ring", "regexp-search-ring", "query-replace-history",
	"query-replace-defaults", "read-expression-history", "shell-command-history",
	"kmacro-counter", "kmacro-initial-counter-value", "kmacro-default-counter-format", "last-kbd-macro",
}

// Bounds on the editor state a checkpoint keeps: restore-editor carries it
// in one agent line (protocol.MaxAgentLine). A buffer over them keeps less
// (no text, then nothing); larger printed variables are not kept.
const (
	maxBufferText  = 256 << 10
	maxBufferUndo  = 128 << 10
	maxBuffersSize = 512 << 10
	maxGlobals     = 256 << 10
)

// CaptureLimits bound a capture-editor call.
type CaptureLimits struct {
	Vars                                   []string
	MaxText, MaxUndo, MaxTotal, MaxGlobals int
}

// Checkpoint is what a checkpoint captures; SpecOnly captures the window
// spec alone (snapshot.json).
var (
	Checkpoint = CaptureLimits{Vars: GlobalVars, MaxText: maxBufferText, MaxUndo: maxBufferUndo,
		MaxTotal: maxBuffersSize, MaxGlobals: maxGlobals}
	SpecOnly = CaptureLimits{}
)

// EditorReport is what restore-editor did.
type EditorReport = protocol.AgentRestoreEditorResult

// ErrDisconnected is an editor call made while the agent is not
// connected.
var ErrDisconnected = errors.New("the agent is not connected")

// Editor is the editor state half of a checkpoint: typed agent ops
// (api.md §7). The snapshot service is its only caller.
type Editor interface {
	// Connected reports whether the agent is connected.
	Connected() bool
	// Capture reads the editor state within lim (capture-editor).
	Capture(ctx context.Context, lim CaptureLimits) (EditorState, error)
	// Restore puts the editor state back after the files were restored
	// (restore-editor).
	Restore(ctx context.Context, st EditorState) (EditorReport, error)
	// ModifiedFiles lists the workspace files with unsaved buffers.
	ModifiedFiles(ctx context.Context) ([]string, error)
	// Open visits a spec's files and applies its windows (open; a fresh
	// daemon after a restart).
	Open(ctx context.Context, spec *protocol.WindowSpec) error
	// Visit opens a workspace file in the learner's window and returns
	// its buffer's name (visit).
	Visit(ctx context.Context, path string) (string, error)
	// Flush saves the modified file buffers before Emacs stops
	// (flush-buffers) and returns how many it saved.
	Flush(ctx context.Context) (int, error)
}

// Agent is the agent connection AgentEditor calls.
type Agent interface {
	Call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error
	Connected() bool
}

// AgentEditor implements Editor with the agent's ops.
type AgentEditor struct {
	Agent Agent
	// Workspace is the absolute workspace root.
	Workspace string
}

// Agent call timeouts.
const (
	captureTimeout  = 5 * time.Second
	restoreTimeout  = 10 * time.Second
	modifiedTimeout = 2 * time.Second
	openTimeout     = 10 * time.Second
	flushTimeout    = 3 * time.Second
)

func (e *AgentEditor) call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error {
	if e.Agent == nil || !e.Agent.Connected() {
		return ErrDisconnected
	}
	err := e.Agent.Call(ctx, op, args, timeout, out)
	if errors.Is(err, agent.ErrNotConnected) || errors.Is(err, agent.ErrClosed) {
		return errors.Join(ErrDisconnected, err)
	}
	return err
}

// Connected implements Editor.
func (e *AgentEditor) Connected() bool { return e.Agent != nil && e.Agent.Connected() }

// Capture implements Editor.
func (e *AgentEditor) Capture(ctx context.Context, lim CaptureLimits) (EditorState, error) {
	args := protocol.AgentCaptureEditorArgs{Workspace: e.Workspace, Vars: lim.Vars, MaxText: lim.MaxText,
		MaxUndo: lim.MaxUndo, MaxTotal: lim.MaxTotal, MaxGlobals: lim.MaxGlobals}
	if args.Vars == nil {
		args.Vars = []string{}
	}
	var res protocol.AgentCaptureEditorResult
	if err := e.call(ctx, protocol.AgentOpCaptureEditor, args, captureTimeout, &res); err != nil {
		return EditorState{}, err
	}
	spec := res.Spec
	if spec.TakenAt == 0 {
		spec.TakenAt = time.Now().UnixMilli()
	}
	st := EditorState{Spec: &spec}
	if res.Globals != nil {
		st.Globals = *res.Globals
	}
	for rel, b := range res.Buffers {
		if !relativePath(rel) {
			continue
		}
		bs := BufferState{Modified: b.Modified}
		if b.Text != nil {
			bs.Text, bs.HasText = *b.Text, true
		}
		if b.Undo != nil {
			bs.Undo = *b.Undo
		}
		if st.Buffers == nil {
			st.Buffers = map[string]BufferState{}
		}
		st.Buffers[rel] = bs
	}
	return st, nil
}

// Restore implements Editor. A spec without files (a checkpoint taken
// without an agent) closes no buffers.
func (e *AgentEditor) Restore(ctx context.Context, st EditorState) (EditorReport, error) {
	spec := st.Spec
	if spec == nil {
		spec = &protocol.WindowSpec{}
	}
	args := protocol.AgentRestoreEditorArgs{Workspace: e.Workspace, Spec: *spec, Globals: st.Globals,
		Buffers: map[string]protocol.AgentBufferState{}}
	if args.Spec.Files == nil {
		args.Spec.Files = []protocol.FileSpec{}
	}
	for rel, b := range st.Buffers {
		wb := protocol.AgentBufferState{Modified: b.Modified}
		if b.Modified && b.HasText {
			wb.Text = &b.Text
		}
		if b.Undo != "" {
			wb.Undo = &b.Undo
		}
		args.Buffers[rel] = wb
	}
	fitLine(&args)
	var rep EditorReport
	err := e.call(ctx, protocol.AgentOpRestoreEditor, args, restoreTimeout, &rep)
	return rep, err
}

// fitLine drops undo histories, then unsaved texts, then the variables
// until the call fits in one agent line: JSON escapes can make the
// bounded state longer than the line.
func fitLine(args *protocol.AgentRestoreEditorArgs) {
	fits := func() bool {
		b, err := json.Marshal(args)
		return err == nil && len(b) < protocol.MaxAgentLine-4096
	}
	if fits() {
		return
	}
	for rel, b := range args.Buffers {
		b.Undo = nil
		args.Buffers[rel] = b
	}
	if fits() {
		return
	}
	for rel, b := range args.Buffers {
		b.Text = nil
		args.Buffers[rel] = b
	}
	if !fits() {
		args.Globals = ""
	}
}

// ModifiedFiles implements Editor.
func (e *AgentEditor) ModifiedFiles(ctx context.Context) ([]string, error) {
	var res protocol.AgentModifiedFilesResult
	if err := e.call(ctx, protocol.AgentOpModifiedFiles, protocol.AgentModifiedFilesArgs{Workspace: e.Workspace}, modifiedTimeout, &res); err != nil {
		return nil, err
	}
	out := res.Paths[:0]
	for _, rel := range res.Paths {
		if relativePath(rel) {
			out = append(out, rel)
		}
	}
	return out, nil
}

// Open implements Editor.
func (e *AgentEditor) Open(ctx context.Context, spec *protocol.WindowSpec) error {
	return e.call(ctx, protocol.AgentOpOpen, spec, openTimeout, nil)
}

// Visit implements Editor.
func (e *AgentEditor) Visit(ctx context.Context, path string) (string, error) {
	var res protocol.AgentVisitResult
	err := e.call(ctx, protocol.AgentOpVisit, protocol.AgentVisitArgs{Path: path}, agent.DefaultTimeout, &res)
	return res.Buffer, err
}

// Flush implements Editor.
func (e *AgentEditor) Flush(ctx context.Context) (int, error) {
	var res protocol.AgentFlushResult
	err := e.call(ctx, protocol.AgentOpFlushBuffers, nil, flushTimeout, &res)
	return res.Saved, err
}

// relativePath reports whether rel is a clean workspace-relative path.
func relativePath(rel string) bool {
	return rel != "" && path.Clean(rel) == rel && rel != ".." && !strings.HasPrefix(rel, "../") && !path.IsAbs(rel)
}
