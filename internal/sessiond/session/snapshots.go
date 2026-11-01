package session

import (
	"context"
	"errors"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/snapshot"
)

// Snapshots is the only path the ops take to checkpoints and the saved
// editor state; *snapshot.Service implements it (backend.md §3.9).
type Snapshots interface {
	// Exists reports whether ref is a checkpoint (ref and manifest).
	Exists(ctx context.Context, ref string) bool
	// Read returns the manifest of ref.
	Read(ref string) (snapshot.Manifest, error)
	// List returns every checkpoint, newest first.
	List(ctx context.Context) ([]protocol.CheckpointInfo, error)
	// Capture records the workspace and the editor state under ref and
	// announces the checkpoint.
	Capture(ctx context.Context, ref, label string, step *protocol.StepRef) (snapshot.Manifest, error)
	// Restore puts the workspace and the editor state back to ref. It runs
	// to the end once started, whatever ctx does. Errors wrap
	// snapshot.ErrNoCheckpoint, ErrFiles or ErrEditor.
	Restore(ctx context.Context, ref string) (snapshot.Report, error)
	// RestoreEditor restores the editor state of checkpoint ref alone (a
	// restore made while the agent was away).
	RestoreEditor(ctx context.Context, ref string) error
	// SaveEditorState writes the editor's window spec to snapshot.json.
	SaveEditorState(ctx context.Context) (*protocol.WindowSpec, error)
	// LastEditorState returns the saved window spec (snapshot.json), or
	// nil.
	LastEditorState() *protocol.WindowSpec
	// Open visits a spec's files and applies its windows.
	Open(ctx context.Context, spec *protocol.WindowSpec) error
}

var _ Snapshots = (*snapshot.Service)(nil)

// CaptureStep takes refs/playground/<lesson>/<step>; unless force, an
// existing one is kept.
func CaptureStep(ctx context.Context, snaps Snapshots, lessonID, stepID string, force bool) error {
	ref := checkpoint.StepRef(lessonID, stepID)
	if !force && snaps.Exists(ctx, ref) {
		return nil
	}
	_, err := snaps.Capture(ctx, ref, lessonID+"/"+stepID, &protocol.StepRef{LessonID: lessonID, StepID: stepID})
	return err
}

// noCheckpoint reports a missing checkpoint.
func noCheckpoint(err error) bool { return errors.Is(err, snapshot.ErrNoCheckpoint) }
