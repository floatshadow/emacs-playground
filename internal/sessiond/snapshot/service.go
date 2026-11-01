// Package snapshot is the only path to checkpoints and to the saved editor
// state (backend.md §3.9): a checkpoint is a git commit of the workspace
// (checkpoint.Manager) plus a manifest with the editor state, read and
// written through typed agent ops (Editor). The service also keeps
// snapshot.json, the one editor state every recovery path reopens: the
// periodic autosave, a restore (the checkpoint's windows) and the state
// saved before Emacs stops. It holds no recovery state and no lock of its
// own: the session core calls Capture, Restore, RestoreEditor and
// SaveEditorState from its ops, one at a time, and decides when an editor
// state is reopened; BeforeStop runs inside an op's restart or after the
// core stopped.
package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sort"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
)

// Restore failure classes.
var (
	// ErrNoCheckpoint: the ref or its manifest is missing or unreadable
	// (404).
	ErrNoCheckpoint = errors.New("no such checkpoint")
	// ErrFiles: git failed; the workspace may be partly restored and
	// nothing activates.
	ErrFiles = errors.New("restoring the checkpoint's files failed")
	// ErrEditor: the agent is connected but restore-editor failed; the
	// files are back, nothing activates, and the learner can Reset again.
	ErrEditor = errors.New("restoring the editor state failed")
)

// AutosaveInterval is how often sessiond writes snapshot.json
// (SaveEditorState).
const AutosaveInterval = 30 * time.Second

// Report is what a restore did.
type Report struct {
	// Files are the workspace-relative paths the restore changed: files
	// restored or removed on disk and files whose buffers had unsaved
	// changes, which the editor restore discards (rpc restore's
	// restoredFiles counts them).
	Files []string
	// Step is the step the checkpoint was taken for, if any.
	Step *protocol.StepRef
	// Editor is what restore-editor did.
	Editor EditorReport
	// EditorDeferred is set when the agent was not connected or went away
	// during restore-editor: the files are back and the editor state is
	// for the agent's next hello (RestoreEditor).
	EditorDeferred bool
}

// Service captures and restores checkpoints and keeps snapshot.json.
type Service struct {
	Files   *checkpoint.Manager
	Editor  Editor
	Modules *scenario.Modules
	// StateFile is snapshot.json, the last editor state (restart restore).
	StateFile string
	// Now stamps manifests (default time.Now).
	Now func() time.Time
	// Emit announces a checkpoint (it must not block).
	Emit func(protocol.Event)
	Log  *slog.Logger
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// Init makes the workspace the repository of its checkpoints
// (checkpoint.Manager.Init); it is idempotent.
func (s *Service) Init(ctx context.Context) error { return s.Files.Init(ctx) }

// Capture records the checkpoint ref: the editor state first (within 5 s;
// none without an agent), then the workspace commit, then the manifest
// naming that commit, and last the ref, which publishes it. The
// checkpoint event follows.
func (s *Service) Capture(ctx context.Context, ref, label string, step *protocol.StepRef) (Manifest, error) {
	if !checkpoint.ValidRef(ref) {
		return Manifest{}, fmt.Errorf("checkpoint: invalid ref %q", ref)
	}
	m := Manifest{Version: ManifestVersion, Ref: ref, Label: label, Step: step}
	if s.Editor != nil && s.Editor.Connected() {
		ectx, cancel := context.WithTimeout(ctx, captureTimeout)
		st, err := s.Editor.Capture(ectx, Checkpoint)
		cancel()
		if err != nil {
			s.log().Info("checkpoint without editor state", "ref", ref, "err", err)
		} else {
			m.Spec, m.Buffers, m.Globals = st.Spec, st.Buffers, st.Globals
		}
	}
	tree, err := s.Files.TakeTree(ctx, ref, label)
	if err != nil {
		return Manifest{}, err
	}
	m.Commit, m.Nested, m.EmptyDirs, m.CreatedAt = tree.Commit, tree.Nested, tree.EmptyDirs, s.now().UnixMilli()
	if err := writeJSON(manifestPath(s.Files.Workspace, ref), m, true); err != nil {
		return Manifest{}, err
	}
	if err := s.Files.PublishRef(ctx, ref, tree.Commit); err != nil {
		return Manifest{}, err
	}
	if s.Emit != nil {
		s.Emit(&protocol.CheckpointEvent{Label: m.Label, Ref: m.Ref, CreatedAt: m.CreatedAt})
	}
	return m, nil
}

// Read returns the manifest of ref.
func (s *Service) Read(ref string) (Manifest, error) {
	if !checkpoint.ValidRef(ref) {
		return Manifest{}, fmt.Errorf("checkpoint: invalid ref %q", ref)
	}
	b, err := os.ReadFile(manifestPath(s.Files.Workspace, ref))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Exists reports whether ref is a checkpoint: the ref and a readable
// manifest of the commit it names.
func (s *Service) Exists(ctx context.Context, ref string) bool {
	m, err := s.Read(ref)
	return err == nil && s.published(ctx, ref, m)
}

// published reports whether ref names m's commit. A recapture writes the
// manifest before it moves the ref, so a capture that stopped in between
// leaves a manifest of a commit the ref does not name: that checkpoint is
// missing until the next capture. A manifest without a commit (version 0)
// trusts the ref.
func (s *Service) published(ctx context.Context, ref string, m Manifest) bool {
	commit, err := s.Files.CommitOf(ctx, ref)
	return err == nil && (m.Commit == "" || m.Commit == commit)
}

// List returns every checkpoint, newest first, with the files a restore
// would change. Refs without a manifest are left out.
func (s *Service) List(ctx context.Context) ([]protocol.CheckpointInfo, error) {
	refs, err := s.Files.Refs(ctx)
	if err != nil {
		return nil, err
	}
	manifests := map[string]Manifest{}
	nested := map[string][]checkpoint.NestedRepo{}
	for _, ref := range refs {
		if m, err := s.Read(ref); err == nil {
			manifests[ref], nested[ref] = m, m.Nested
		}
	}
	changes, err := s.Files.Changes(ctx, nested)
	if err != nil {
		return nil, err
	}
	items := []protocol.CheckpointInfo{}
	for _, ref := range refs {
		m, ok := manifests[ref]
		if !ok {
			continue
		}
		files := changes[ref]
		if files == nil {
			files = []string{}
		}
		items = append(items, protocol.CheckpointInfo{Ref: ref, Label: m.Label, CreatedAt: m.CreatedAt, Step: m.Step, Files: files})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

// Restore puts the workspace and the editor back to ref (backend.md §3.9):
// repositories the manifest lacks dropped, files (restore, clean, nested,
// empty directories), modules.el kept, the editor in one restore-editor
// call, snapshot.json from the manifest's spec. Once started it runs to
// the end whatever ctx does. Without an agent the editor state is left
// to the caller (Report.EditorDeferred, RestoreEditor). Errors wrap
// ErrNoCheckpoint, ErrFiles or ErrEditor.
func (s *Service) Restore(ctx context.Context, ref string) (Report, error) {
	ctx = context.WithoutCancel(ctx)
	m, err := s.Read(ref)
	if err != nil {
		return Report{}, fmt.Errorf("%w: %s: %v", ErrNoCheckpoint, ref, err)
	}
	if !s.published(ctx, ref, m) {
		return Report{}, fmt.Errorf("%w: %s: the manifest is not the ref's", ErrNoCheckpoint, ref)
	}
	var unsaved []string
	connected := s.connected()
	if connected {
		unsaved = s.modifiedFiles(ctx)
	}
	files, err := s.restoreFiles(ctx, ref, m)
	if err != nil {
		return Report{}, err
	}
	changed := slices.Clone(files)
	for _, f := range unsaved {
		if !slices.Contains(changed, f) {
			changed = append(changed, f)
		}
	}
	rep := Report{Files: changed, Step: m.Step, EditorDeferred: !connected}
	var editorErr error
	if connected {
		rep.Editor, editorErr = s.Editor.Restore(ctx, m.Editor())
		for _, d := range rep.Editor.Degraded {
			s.log().Warn("part of the editor state was not restored", "ref", ref, "part", d)
		}
		// The agent went away during the call: the restore failed, and the
		// agent that says hello next still gets the checkpoint's state.
		rep.EditorDeferred = errors.Is(editorErr, ErrDisconnected)
	}
	if m.Spec != nil {
		if err := s.writeState(m.Spec); err != nil {
			s.log().Warn("snapshot.json after a restore", "err", err)
		}
	}
	if editorErr != nil {
		return rep, fmt.Errorf("%w: %w", ErrEditor, editorErr)
	}
	return rep, nil
}

// restoreFiles is the git phase of Restore, with modules.el kept: modules
// only ever grow (emacs.md §4), and the running Emacs has the current set
// loaded.
func (s *Service) restoreFiles(ctx context.Context, ref string, m Manifest) ([]string, error) {
	var mods []string
	if s.Modules != nil {
		mods = s.Modules.Current()
	}
	files, err := s.Files.Restore(ctx, ref, m.Nested, m.EmptyDirs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNoCheckpoint, ref)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFiles, err)
	}
	if s.Modules != nil {
		if after := s.Modules.Current(); !slices.Equal(after, mods) {
			if err := s.Modules.Write(mods); err != nil {
				s.log().Warn("keeping modules.el across a restore", "err", err)
			}
		}
	}
	return files, nil
}

func (s *Service) connected() bool { return s.Editor != nil && s.Editor.Connected() }

// modifiedFiles lists the files with unsaved buffers, which a restore
// discards.
func (s *Service) modifiedFiles(ctx context.Context) []string {
	paths, err := s.Editor.ModifiedFiles(ctx)
	if err != nil {
		s.log().Debug("modified buffers before a restore", "err", err)
	}
	return paths
}

// RestoreEditor restores the editor state of checkpoint ref alone, read
// again from its manifest: a restore made while the agent was away
// (Report.EditorDeferred) finishes when it says hello.
func (s *Service) RestoreEditor(ctx context.Context, ref string) error {
	m, err := s.Read(ref)
	if err != nil || !s.published(ctx, ref, m) {
		return fmt.Errorf("%w: %s", ErrNoCheckpoint, ref)
	}
	rep, err := s.Editor.Restore(ctx, m.Editor())
	if err != nil {
		return err
	}
	for _, d := range rep.Degraded {
		s.log().Warn("part of the editor state was not restored", "ref", ref, "part", d)
	}
	return nil
}

// Open visits a spec's files and applies its windows; nothing for a spec
// without files.
func (s *Service) Open(ctx context.Context, spec *protocol.WindowSpec) error {
	if spec == nil || len(spec.Files) == 0 || s.Editor == nil {
		return nil
	}
	return s.Editor.Open(ctx, spec)
}

// Visit opens a workspace file in the learner's window (rpc open).
func (s *Service) Visit(ctx context.Context, path string) (string, error) {
	return s.Editor.Visit(ctx, path)
}

// BeforeStop saves snapshot.json and the modified file buffers before
// Emacs stops (a restart, sessiond's shutdown): the reopened spec visits
// the files from disk, so unsaved edits would be lost.
func (s *Service) BeforeStop(ctx context.Context) {
	if _, err := s.SaveEditorState(ctx); err != nil {
		s.log().Info("no fresh snapshot.json before Emacs stops", "err", err)
	}
	if s.Editor == nil || !s.Editor.Connected() {
		return
	}
	fctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	n, err := s.Editor.Flush(fctx)
	switch {
	case err != nil:
		s.log().Info("saving buffers before Emacs stops", "err", err)
	case n > 0:
		s.log().Info("saved modified buffers before Emacs stops", "buffers", n)
	}
}

// SaveEditorState writes the editor's window spec to snapshot.json.
func (s *Service) SaveEditorState(ctx context.Context) (*protocol.WindowSpec, error) {
	if s.Editor == nil || !s.Editor.Connected() {
		return nil, ErrDisconnected
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	st, err := s.Editor.Capture(cctx, SpecOnly)
	if err != nil {
		return nil, err
	}
	return st.Spec, s.writeState(st.Spec)
}

// LastEditorState returns the spec in snapshot.json, or nil.
func (s *Service) LastEditorState() *protocol.WindowSpec {
	if s.StateFile == "" {
		return nil
	}
	b, err := os.ReadFile(s.StateFile)
	if err != nil {
		return nil
	}
	var spec protocol.WindowSpec
	if json.Unmarshal(b, &spec) != nil {
		return nil
	}
	return &spec
}

func (s *Service) writeState(spec *protocol.WindowSpec) error {
	if s.StateFile == "" || spec == nil {
		return nil
	}
	return writeJSON(s.StateFile, spec, false)
}
