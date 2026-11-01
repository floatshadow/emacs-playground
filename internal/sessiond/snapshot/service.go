// Package snapshot is the only path to checkpoints and to the saved editor
// state (backend.md §3.9): a checkpoint is a git commit of the workspace
// (checkpoint.Manager) plus a manifest with the editor state, read and
// written through typed agent ops (Editor). The service also keeps
// snapshot.json, the one editor state every recovery path reopens: the
// periodic autosave, a restore (the checkpoint's windows) and the state
// saved before Emacs stops.
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
	"sync"
	"sync/atomic"
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

// AutosaveInterval is how often Autosave writes snapshot.json.
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
	// EditorDeferred is set when the agent was not connected: the files
	// are back and the editor state follows on the agent's next hello.
	EditorDeferred bool
}

// Service captures and restores checkpoints and keeps snapshot.json.
type Service struct {
	Files   *checkpoint.Manager
	Editor  Editor
	Modules *scenario.Modules
	// StateFile is snapshot.json, the last editor state (restart restore).
	StateFile string
	// Busy reports a lesson op in progress; Autosave skips while it does,
	// so the state an op puts in place is not overwritten halfway.
	Busy func() bool
	// Active returns the active step, or nil. A restore made while the
	// agent was away gives the agent's next hello its editor state only
	// while the step it was made for is still active.
	Active func() *protocol.StepRef
	// Now stamps manifests (default time.Now).
	Now func() time.Time
	// Emit announces a checkpoint (it must not block).
	Emit func(protocol.Event)
	Log  *slog.Logger
	// Interval overrides AutosaveInterval (tests).
	Interval time.Duration

	// mu serialises the git and manifest phases of captures and restores.
	mu sync.Mutex
	// pendingMu guards pending: a restore made while the agent was away,
	// whose editor state its next hello gets. A capture or another restore
	// replaces it.
	pendingMu sync.Mutex
	pending   *pendingRestore
	// stopping is set from BeforeStop to the end of AfterRestart: the
	// daemon going away or the fresh one not yet reopened, whose state
	// Autosave must not write over snapshot.json.
	stopping atomic.Bool
	fault    atomic.Pointer[func(point string) error]
}

// pendingRestore names the checkpoint of a restore made while the agent
// was away and the step that must be active when its editor state is
// applied: a different active step means a load or a restore replaced
// that state since.
type pendingRestore struct {
	ref  string
	step *protocol.StepRef
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

// SetFault installs a hook run at the named points of a capture ("publish":
// after the manifest is written, before the ref is published); an error
// from it ends the capture there, as a crash would. Tests only.
func (s *Service) SetFault(f func(point string) error) {
	if f == nil {
		s.fault.Store(nil)
		return
	}
	s.fault.Store(&f)
}

func (s *Service) faultAt(point string) error {
	if f := s.fault.Load(); f != nil {
		return (*f)(point)
	}
	return nil
}

// Init makes the workspace the repository of its checkpoints
// (checkpoint.Manager.Init); it is idempotent.
func (s *Service) Init(ctx context.Context) error { return s.Files.Init(ctx) }

// Capture records the checkpoint ref: the editor state first (within 5 s;
// none without an agent), then the workspace commit, then the manifest
// naming that commit, and last the ref, which publishes it. The
// checkpoint event follows. A restore still waiting for the agent is
// dropped: the workspace has moved on from it.
func (s *Service) Capture(ctx context.Context, ref, label string, step *protocol.StepRef) (Manifest, error) {
	if !checkpoint.ValidRef(ref) {
		return Manifest{}, fmt.Errorf("checkpoint: invalid ref %q", ref)
	}
	s.setPending(nil)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	tree, err := s.Files.TakeTree(ctx, ref, label)
	if err != nil {
		return Manifest{}, err
	}
	m.Commit, m.Nested, m.EmptyDirs, m.CreatedAt = tree.Commit, tree.Nested, tree.EmptyDirs, s.now().UnixMilli()
	if err := writeJSON(manifestPath(s.Files.Workspace, ref), m, true); err != nil {
		return Manifest{}, err
	}
	if err := s.faultAt("publish"); err != nil {
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
// the end whatever ctx does. Without an agent the editor state is restored
// on its next hello while the checkpoint's step is active
// (Report.EditorDeferred). Errors wrap ErrNoCheckpoint, ErrFiles or
// ErrEditor.
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
	asked := s.connected()
	if asked {
		unsaved = s.modifiedFiles(ctx)
	}
	files, err := s.restoreFiles(ctx, ref, m)
	if err != nil {
		return Report{}, err
	}
	// Decided after the git phase: an agent that said hello during it
	// found nothing pending, so it gets the editor state now.
	connected := s.connected()
	if connected && !asked {
		unsaved = s.modifiedFiles(ctx)
	}
	changed := slices.Clone(files)
	for _, f := range unsaved {
		if !slices.Contains(changed, f) {
			changed = append(changed, f)
		}
	}
	rep := Report{Files: changed, Step: m.Step}
	st := m.Editor()
	var editorErr error
	if connected {
		rep.Editor, editorErr = s.Editor.Restore(ctx, st)
		for _, d := range rep.Editor.Degraded {
			s.log().Warn("part of the editor state was not restored", "ref", ref, "part", d)
		}
	}
	switch {
	case !connected:
		rep.EditorDeferred = true
		s.deferEditor(ref, m.Step)
	case errors.Is(editorErr, ErrDisconnected):
		// The agent went away during the call: the restore failed, and the
		// agent that says hello next still gets the checkpoint's state.
		s.deferEditor(ref, m.Step)
	default:
		s.setPending(nil)
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
	s.mu.Lock()
	defer s.mu.Unlock()
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

// deferEditor leaves ref's editor state to the agent's next hello, for
// the checkpoint's step or, for a checkpoint of no step, the step active
// now.
func (s *Service) deferEditor(ref string, step *protocol.StepRef) {
	if step == nil {
		step = s.active()
	}
	s.setPending(&pendingRestore{ref: ref, step: step})
}

func (s *Service) active() *protocol.StepRef {
	if s.Active == nil {
		return nil
	}
	return s.Active()
}

func (s *Service) setPending(p *pendingRestore) {
	s.pendingMu.Lock()
	s.pending = p
	s.pendingMu.Unlock()
}

// takePending returns the restore waiting for the agent when its step is
// the active one, and forgets it. A pending restore for another step stays
// until a capture or restore replaces it: a hello that arrives before the
// op activates the step must not discard it.
func (s *Service) takePending() *pendingRestore {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	p := s.pending
	if p == nil || !sameStep(p.step, s.active()) {
		return nil
	}
	s.pending = nil
	return p
}

func sameStep(a, b *protocol.StepRef) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// OnHello finishes a restore made while the agent was away: the agent
// that said hello gets that checkpoint's editor state, read again from
// its manifest. It runs outside the agent's reader (agent.Handler must not
// call the agent).
func (s *Service) OnHello(ctx context.Context) {
	if p := s.takePending(); p != nil {
		s.restorePending(ctx, p)
	}
}

// restorePending applies a pending restore's editor state; it reports
// false when the checkpoint is gone.
func (s *Service) restorePending(ctx context.Context, p *pendingRestore) bool {
	m, err := s.Read(p.ref)
	if err != nil || !s.published(ctx, p.ref, m) {
		s.log().Info("the checkpoint of a restore made while the agent was away is gone", "ref", p.ref)
		return false
	}
	rep, err := s.Editor.Restore(ctx, m.Editor())
	if err != nil {
		s.log().Warn("restoring the editor state of a restore made while the agent was away", "err", err)
		return true
	}
	for _, d := range rep.Degraded {
		s.log().Warn("part of the editor state was not restored", "part", d)
	}
	return true
}

// AfterRestart reopens the editor state in a daemon that restarted: a
// restore's state still waiting for the agent, else snapshot.json. It ends
// the stop BeforeStop began.
func (s *Service) AfterRestart(ctx context.Context) {
	defer s.stopping.Store(false)
	if p := s.takePending(); p != nil && s.restorePending(ctx, p) {
		return
	}
	if err := s.Open(ctx, s.LastEditorState()); err != nil {
		s.log().Warn("reopening the buffers after a restart", "err", err)
	}
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
// the files from disk, so unsaved edits would be lost. Autosave pauses
// until AfterRestart reopened that state.
func (s *Service) BeforeStop(ctx context.Context) {
	s.stopping.Store(true)
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

// Autosave writes snapshot.json every Interval until ctx ends, skipping
// while a lesson op runs and while Emacs restarts (BeforeStop to
// AfterRestart).
func (s *Service) Autosave(ctx context.Context) {
	iv := s.Interval
	if iv <= 0 {
		iv = AutosaveInterval
	}
	t := time.NewTicker(iv)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.autosave(ctx)
		}
	}
}

func (s *Service) autosave(ctx context.Context) {
	if s.stopping.Load() || (s.Busy != nil && s.Busy()) {
		return
	}
	if _, err := s.SaveEditorState(ctx); err != nil {
		s.log().Debug("autosave of snapshot.json", "err", err)
	}
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
