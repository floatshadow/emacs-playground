package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
)

// Snapshots is the only path the ops take to checkpoints and the saved
// editor state (backend.md §3.9).
type Snapshots interface {
	// Exists reports whether the checkpoint ref exists.
	Exists(ctx context.Context, ref string) bool
	// ReadMeta returns the metadata saved with ref.
	ReadMeta(ref string) (checkpoint.Meta, error)
	// List returns every checkpoint, newest first.
	List(ctx context.Context) ([]protocol.CheckpointInfo, error)
	// Capture records the workspace and the editor state under meta.Ref
	// and announces the checkpoint.
	Capture(ctx context.Context, meta checkpoint.Meta) (checkpoint.Meta, error)
	// Restore puts the workspace and the editor state back to ref and
	// returns the number of files it changed. It runs to the end once
	// started, whatever ctx does. A missing checkpoint is os.ErrNotExist.
	Restore(ctx context.Context, ref string) (int, checkpoint.Meta, error)
	// LastEditorState returns the last saved window spec (snapshot.json),
	// or nil.
	LastEditorState() *protocol.WindowSpec
}

// CaptureStep takes refs/playground/<lesson>/<step>; unless force, an
// existing one is kept.
func CaptureStep(ctx context.Context, snaps Snapshots, lessonID, stepID string, force bool) error {
	ref := checkpoint.StepRef(lessonID, stepID)
	if !force && snaps.Exists(ctx, ref) {
		return nil
	}
	_, err := snaps.Capture(ctx, checkpoint.Meta{Ref: ref, Label: lessonID + "/" + stepID,
		Step: &protocol.StepRef{LessonID: lessonID, StepID: stepID}})
	return err
}

// EditorSnapshots is the supervisor's periodic editor snapshot
// (snapshot.json).
type EditorSnapshots interface {
	Snapshot(ctx context.Context) (*protocol.WindowSpec, error)
	LastSnapshot() *protocol.WindowSpec
}

// Checkpoints implements Snapshots with git checkpoints and the editor
// state read and written through the agent's snapshot and reset-step ops
// and eval forms (unsaved.go).
// TODO(sessiond, backend.md §3.9): replaced by the snapshot service with
// typed agent ops.
type Checkpoints struct {
	Workspace string
	Ckpt      *checkpoint.Manager
	Agent     Agent
	Editor    EditorSnapshots
	Modules   *scenario.Modules
	Emit      func(protocol.Event)
	Log       *slog.Logger
}

func (s *Checkpoints) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

// Exists implements Snapshots.
func (s *Checkpoints) Exists(ctx context.Context, ref string) bool { return s.Ckpt.Exists(ctx, ref) }

// ReadMeta implements Snapshots.
func (s *Checkpoints) ReadMeta(ref string) (checkpoint.Meta, error) { return s.Ckpt.ReadMeta(ref) }

// List implements Snapshots.
func (s *Checkpoints) List(ctx context.Context) ([]protocol.CheckpointInfo, error) {
	return s.Ckpt.List(ctx)
}

// LastEditorState implements Snapshots.
func (s *Checkpoints) LastEditorState() *protocol.WindowSpec {
	if s.Editor == nil {
		return nil
	}
	return s.Editor.LastSnapshot()
}

// Capture records the workspace files under meta.Ref together with the
// windows, the file buffers' unsaved state and the session variables (each
// captured within 5 s, best effort), and announces the checkpoint.
func (s *Checkpoints) Capture(ctx context.Context, meta checkpoint.Meta) (checkpoint.Meta, error) {
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if s.Editor != nil {
		meta.Spec, _ = s.Editor.Snapshot(sctx)
	}
	meta.Buffers, meta.Globals = s.bufferStates(sctx), s.globals(sctx)
	cancel()
	meta, err := s.Ckpt.TakeWith(ctx, meta)
	if err != nil {
		return meta, err
	}
	if s.Emit != nil {
		s.Emit(&protocol.CheckpointEvent{Label: meta.Label, Ref: meta.Ref, CreatedAt: meta.CreatedAt})
	}
	return meta, nil
}

// Restore puts the workspace files back to ref and asks the agent to
// restore the windows recorded with it (reset-step). It returns the number
// of files the restore changed: those restored or removed on disk and those
// whose buffer had unsaved changes, which reset-step discards. The module
// set (emacs.d/modules.el) is kept: modules only ever grow (emacs.md §4),
// and the running Emacs has the current set loaded. A missing checkpoint is
// os.ErrNotExist. Once started, a restore runs to the end even when ctx is
// cancelled: files restored without reset-step, buffers and session
// variables would leave the step half restored. Each agent call keeps its
// own timeout.
func (s *Checkpoints) Restore(ctx context.Context, ref string) (int, checkpoint.Meta, error) {
	ctx = context.WithoutCancel(ctx)
	mods := s.Modules.Current()
	var unsaved []string
	if s.Agent.Connected() {
		unsaved = s.modifiedFiles(ctx)
	}
	files, meta, err := s.Ckpt.Restore(ctx, ref)
	if err != nil {
		return 0, meta, err
	}
	changed := make(map[string]bool, len(files)+len(unsaved))
	for _, f := range files {
		changed[f] = true
	}
	for _, f := range unsaved {
		changed[f] = true
	}
	if after := s.Modules.Current(); !slices.Equal(after, mods) {
		if err := s.Modules.Write(mods); err != nil {
			s.log().Warn("keeping modules.el across a restore", "err", err)
		}
	}
	spec := meta.Spec
	if spec == nil {
		spec = &protocol.WindowSpec{Files: []protocol.FileSpec{}}
	}
	if s.Agent.Connected() {
		if err := s.Agent.Call(ctx, protocol.AgentOpResetStep, protocol.AgentResetStepArgs{Spec: *spec}, 10*time.Second, nil); err != nil {
			return 0, meta, fmt.Errorf("restore incomplete: reset-step: %w", err)
		}
		// reset-step put the buffers on the restored files; the edits
		// that were unsaved when the checkpoint was taken, the undo
		// histories and the session variables go back.
		s.restoreBuffers(ctx, meta.Buffers, spec)
		s.restoreGlobals(ctx, meta.Globals)
	}
	return len(changed), meta, nil
}

// evalRaw runs form through the agent's eval op and returns its value as
// the agent printed it.
func (s *Checkpoints) evalRaw(ctx context.Context, form string, timeout time.Duration) (json.RawMessage, error) {
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	err := s.Agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: form}, timeout, &out)
	return out.Value, err
}

// isNotExist reports a missing checkpoint.
func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }
