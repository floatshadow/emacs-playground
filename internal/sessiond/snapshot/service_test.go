package snapshot

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
)

// fakeEditor records the editor ops and answers them from fields.
type fakeEditor struct {
	mu        sync.Mutex
	connected bool
	// reconnectAfter > 0 reports the agent away for that many Connected
	// calls, then connected (a hello during a restore).
	reconnectAfter int
	state          EditorState
	modified       []string
	restoreErr     error
	restored       []EditorState
	opened         []*protocol.WindowSpec
	limits         []CaptureLimits
}

func (e *fakeEditor) Connected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.reconnectAfter > 0 {
		if e.reconnectAfter--; e.reconnectAfter == 0 {
			e.connected = true
		}
		return false
	}
	return e.connected
}

func (e *fakeEditor) Capture(_ context.Context, lim CaptureLimits) (EditorState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.limits = append(e.limits, lim)
	return e.state, nil
}

func (e *fakeEditor) Restore(_ context.Context, st EditorState) (EditorReport, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.restored = append(e.restored, st)
	return EditorReport{Buffers: len(st.Buffers), Degraded: []string{}}, e.restoreErr
}

func (e *fakeEditor) ModifiedFiles(context.Context) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modified, nil
}

func (e *fakeEditor) Open(_ context.Context, spec *protocol.WindowSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.opened = append(e.opened, spec)
	return nil
}

func (e *fakeEditor) Visit(context.Context, string) (string, error) { return "", nil }
func (e *fakeEditor) Flush(context.Context) (int, error)            { return 0, nil }

func newService(t *testing.T) (*Service, *fakeEditor, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ws := t.TempDir()
	ed := &fakeEditor{connected: true, state: EditorState{
		Spec:    &protocol.WindowSpec{Files: []protocol.FileSpec{{Path: "a.txt", Point: 3, Visible: true}}, Layout: "a.txt"},
		Buffers: map[string]BufferState{"a.txt": {Modified: true, HasText: true, Text: "unsaved\n", Undo: `(nil (1 . 3))`}},
		Globals: `((kill-ring "x"))`,
	}}
	s := &Service{
		Files:     checkpoint.New(ws, "git"),
		Editor:    ed,
		Modules:   &scenario.Modules{Path: filepath.Join(ws, "emacs.d", "modules.el")},
		StateFile: filepath.Join(ws, ".playground", "snapshot.json"),
		Now:       func() time.Time { return time.UnixMilli(1761472000000) },
	}
	if err := s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return s, ed, ws
}

func TestCaptureWritesManifestThenRef(t *testing.T) {
	s, ed, ws := newService(t)
	ctx := context.Background()
	var events []protocol.Event
	s.Emit = func(ev protocol.Event) { events = append(events, ev) }
	ref := checkpoint.StepRef("a/b", "one")
	step := &protocol.StepRef{LessonID: "a/b", StepID: "one"}

	// Stopped between the manifest and the ref: no checkpoint, no event.
	s.SetFault(func(point string) error {
		if point == "publish" {
			if _, err := os.Stat(manifestPath(ws, ref)); err != nil {
				t.Errorf("the manifest must be written before the ref: %v", err)
			}
			return errors.New("stopped")
		}
		return nil
	})
	if _, err := s.Capture(ctx, ref, "a/b/one", step); err == nil {
		t.Fatal("capture past the fault")
	}
	if s.Exists(ctx, ref) || s.Files.Exists(ctx, ref) || len(events) != 0 {
		t.Fatalf("a capture cut short published: exists=%v events=%v", s.Exists(ctx, ref), events)
	}
	if _, err := s.Restore(ctx, ref); !errors.Is(err, ErrNoCheckpoint) {
		t.Fatalf("restore of an unpublished checkpoint: %v", err)
	}

	// The next capture publishes.
	s.SetFault(nil)
	m, err := s.Capture(ctx, ref, "a/b/one", step)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Exists(ctx, ref) || m.Version != ManifestVersion || m.CreatedAt != 1761472000000 || m.Spec == nil || m.Globals == "" {
		t.Fatalf("manifest %+v", m)
	}
	if lim := ed.limits[len(ed.limits)-1]; lim.MaxTotal != maxBuffersSize || !slices.Equal(lim.Vars, GlobalVars) {
		t.Fatalf("capture limits %+v", lim)
	}
	if len(events) != 1 || events[0].(*protocol.CheckpointEvent).Ref != ref {
		t.Fatalf("events %v", events)
	}
	read, err := s.Read(ref)
	if err != nil || read.Buffers["a.txt"].Text != "unsaved\n" || read.Step.StepID != "one" {
		t.Fatalf("read %+v %v", read, err)
	}

	// Without an agent the manifest has no editor state.
	ed.connected = false
	m, err = s.Capture(ctx, checkpoint.ManualRef("plain"), "plain", nil)
	if err != nil || m.Spec != nil || m.Buffers != nil || m.Globals != "" {
		t.Fatalf("manifest without an agent %+v %v", m, err)
	}
}

// A ref without a manifest (or with an unreadable one) predates the
// service: it is invisible, not listed, and a restore of it touches
// nothing.
func TestRefWithoutManifestIsInvisible(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "corrupt"}[corrupt], func(t *testing.T) {
			s, ed, ws := newService(t)
			ctx := context.Background()
			ref := checkpoint.StepRef("a/b", "one")
			if _, err := s.Capture(ctx, ref, "a/b/one", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Capture(ctx, checkpoint.ManualRef("kept"), "kept", nil); err != nil {
				t.Fatal(err)
			}
			if corrupt {
				if err := os.WriteFile(manifestPath(ws, ref), []byte("{"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(manifestPath(ws, ref)); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if s.Exists(ctx, ref) {
				t.Fatal("a ref without a manifest exists")
			}
			items, err := s.List(ctx)
			if err != nil || len(items) != 1 || items[0].Label != "kept" || !slices.Equal(items[0].Files, []string{"a.txt"}) {
				t.Fatalf("list %+v %v", items, err)
			}
			if _, err := s.Restore(ctx, ref); !errors.Is(err, ErrNoCheckpoint) {
				t.Fatalf("restore: %v", err)
			}
			if b, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(b) != "changed\n" || len(ed.restored) != 0 {
				t.Fatalf("a refused restore changed things: %q %d", b, len(ed.restored))
			}
		})
	}
}

func TestRestoreClassesAndSnapshotJSON(t *testing.T) {
	s, ed, ws := newService(t)
	ctx := context.Background()
	ref := checkpoint.StepRef("a/b", "one")
	step := &protocol.StepRef{LessonID: "a/b", StepID: "one"}
	s.Active = func() *protocol.StepRef { return step }
	if _, err := s.Capture(ctx, ref, "a/b/one", step); err != nil {
		t.Fatal(err)
	}
	if err := s.Modules.Write([]string{"base", "agent", "files"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed.modified = []string{"b.txt"}

	// Connected: files, then one restore-editor call, then snapshot.json.
	rep, err := s.Restore(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(rep.Files)
	// modules.el counts: git removed it, the service wrote it back.
	if !slices.Equal(rep.Files, []string{"b.txt", "emacs.d/modules.el", "new.txt"}) || rep.Step.StepID != "one" || rep.EditorDeferred {
		t.Fatalf("report %+v", rep)
	}
	if len(ed.restored) != 1 || ed.restored[0].Buffers["a.txt"].Text != "unsaved\n" || ed.restored[0].Globals == "" {
		t.Fatalf("restore-editor calls %+v", ed.restored)
	}
	if spec := s.LastEditorState(); spec == nil || spec.Layout != "a.txt" {
		t.Fatalf("snapshot.json %+v", spec)
	}
	if got := s.Modules.Current(); strings.Join(got, " ") != "base agent files" {
		t.Fatalf("modules.el not kept: %v", got)
	}

	// Editor failure with the agent connected: ErrEditor, files back.
	ed.restoreErr = errors.New("busy")
	if _, err := s.Restore(ctx, ref); !errors.Is(err, ErrEditor) {
		t.Fatalf("editor failure: %v", err)
	}
	ed.restoreErr = nil

	// Disconnected: deferred; the next hello restores the editor state.
	ed.connected = false
	if err := os.WriteFile(s.StateFile, []byte(`{"files":[],"layout":"other"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = s.Restore(ctx, ref)
	if err != nil || !rep.EditorDeferred {
		t.Fatalf("disconnected restore %+v %v", rep, err)
	}
	if spec := s.LastEditorState(); spec == nil || spec.Layout != "a.txt" {
		t.Fatalf("snapshot.json after a deferred restore %+v", spec)
	}
	n := len(ed.restored)
	ed.connected = true
	s.OnHello(ctx)
	s.OnHello(ctx)
	if len(ed.restored) != n+1 {
		t.Fatalf("hello restores the deferred state once: %d calls", len(ed.restored)-n)
	}

	// After a restart without a pending restore: snapshot.json is opened.
	s.AfterRestart(ctx)
	if len(ed.opened) != 1 || ed.opened[0].Layout != "a.txt" {
		t.Fatalf("opened after a restart %+v", ed.opened)
	}

	// A git failure is ErrFiles.
	if err := os.RemoveAll(filepath.Join(ws, ".git")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(ctx, ref); !errors.Is(err, ErrFiles) && !errors.Is(err, ErrNoCheckpoint) {
		t.Fatalf("git failure: %v", err)
	}
}

// A version 0 manifest (no version field) reads unchanged.
func TestManifestVersionZero(t *testing.T) {
	s, _, ws := newService(t)
	ctx := context.Background()
	ref := checkpoint.StepRef("a/b", "old")
	tr, err := s.Files.TakeTree(ctx, ref, "a/b/old")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Files.PublishRef(ctx, ref, tr.Commit); err != nil {
		t.Fatal(err)
	}
	old := `{"label":"a/b/old","ref":"` + ref + `","createdAt":5,"step":{"lessonId":"a/b","stepId":"old"},
  "buffers":{"a.txt":{"modified":true,"text":"x","hasText":true}},"globals":"()"}`
	if err := os.MkdirAll(filepath.Dir(manifestPath(ws, ref)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath(ws, ref), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := s.Read(ref)
	if err != nil || m.Version != 0 || m.Buffers["a.txt"].Text != "x" || m.Step.StepID != "old" || !s.Exists(ctx, ref) {
		t.Fatalf("version 0 manifest %+v %v", m, err)
	}
}

func TestAutosaveSkipsWhileBusy(t *testing.T) {
	s, ed, _ := newService(t)
	ctx := context.Background()
	busy := true
	s.Busy = func() bool { return busy }
	s.autosave(ctx)
	if s.LastEditorState() != nil {
		t.Fatal("autosave during an op")
	}
	busy = false
	s.autosave(ctx)
	if spec := s.LastEditorState(); spec == nil || spec.Layout != "a.txt" {
		t.Fatalf("autosave %+v", spec)
	}
	if lim := ed.limits[len(ed.limits)-1]; lim.MaxTotal != 0 || len(lim.Vars) != 0 {
		t.Fatalf("autosave captures the spec alone: %+v", lim)
	}
}

func TestFitLine(t *testing.T) {
	big := strings.Repeat(`"\`, 300<<10)
	args := protocol.AgentRestoreEditorArgs{Globals: big, Buffers: map[string]protocol.AgentBufferState{
		"a.txt": {Modified: true, Text: &big, Undo: &big},
	}}
	fitLine(&args)
	b := args.Buffers["a.txt"]
	if b.Undo != nil || b.Text != nil || args.Globals != "" || !b.Modified {
		t.Fatalf("fitLine kept %v %v %d", b.Undo != nil, b.Text != nil, len(args.Globals))
	}
}

// A restore made while the agent was away gives its editor state to the
// next hello only while the checkpoint's step is active; a capture drops
// it.
func TestDeferredEditorStateFollowsTheActiveStep(t *testing.T) {
	s, ed, _ := newService(t)
	ctx := context.Background()
	one := &protocol.StepRef{LessonID: "a/b", StepID: "one"}
	other := &protocol.StepRef{LessonID: "c/d", StepID: "first"}
	var active *protocol.StepRef
	s.Active = func() *protocol.StepRef { return active }
	ref := checkpoint.StepRef("a/b", "one")
	if _, err := s.Capture(ctx, ref, "a/b/one", one); err != nil {
		t.Fatal(err)
	}
	restore := func() {
		t.Helper()
		ed.connected = false
		if rep, err := s.Restore(ctx, ref); err != nil || !rep.EditorDeferred {
			t.Fatalf("deferred restore %+v %v", rep, err)
		}
		ed.connected = true
	}

	// Another lesson became active: the hello leaves the state alone.
	restore()
	active = other
	s.OnHello(ctx)
	if len(ed.restored) != 0 {
		t.Fatalf("another lesson's hello got the editor state: %+v", ed.restored)
	}
	// Its step activates (the hello came before the op activated it).
	active = one
	s.OnHello(ctx)
	if len(ed.restored) != 1 || ed.restored[0].Buffers["a.txt"].Text != "unsaved\n" {
		t.Fatalf("hello on the checkpoint's step: %+v", ed.restored)
	}

	// A capture since the restore drops it.
	restore()
	active = other
	if _, err := s.Capture(ctx, checkpoint.StepRef("c/d", "first"), "c/d/first", other); err != nil {
		t.Fatal(err)
	}
	active = one
	s.OnHello(ctx)
	s.AfterRestart(ctx)
	if len(ed.restored) != 1 || len(ed.opened) != 1 {
		t.Fatalf("a dropped restore came back: restored %d opened %d", len(ed.restored), len(ed.opened))
	}
}

// An agent that says hello while git restores the files gets the editor
// state from the restore itself.
func TestRestoreReconnectDuringFiles(t *testing.T) {
	s, ed, _ := newService(t)
	ctx := context.Background()
	ref := checkpoint.StepRef("a/b", "one")
	if _, err := s.Capture(ctx, ref, "a/b/one", nil); err != nil {
		t.Fatal(err)
	}
	ed.connected, ed.reconnectAfter = false, 1
	rep, err := s.Restore(ctx, ref)
	if err != nil || rep.EditorDeferred || len(ed.restored) != 1 {
		t.Fatalf("restore %+v %v, restore-editor calls %d", rep, err, len(ed.restored))
	}
}

// A recapture that stopped after its manifest leaves the manifest of a
// commit the ref does not name: no checkpoint until the next capture.
func TestRecaptureStoppedBeforeRef(t *testing.T) {
	s, _, ws := newService(t)
	ctx := context.Background()
	ref := checkpoint.StepRef("a/b", "one")
	if _, err := s.Capture(ctx, ref, "a/b/one", nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.SetFault(func(point string) error { return errors.New("stopped") })
	if _, err := s.Capture(ctx, ref, "a/b/one", nil); err == nil {
		t.Fatal("capture past the fault")
	}
	s.SetFault(nil)
	if s.Exists(ctx, ref) {
		t.Fatal("a manifest of another commit makes a checkpoint")
	}
	if _, err := s.Restore(ctx, ref); !errors.Is(err, ErrNoCheckpoint) {
		t.Fatalf("restore: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(b) != "later\n" {
		t.Fatalf("a refused restore changed a.txt: %q", b)
	}
	m, err := s.Capture(ctx, ref, "a/b/one", nil)
	if err != nil || !s.Exists(ctx, ref) || m.Commit == "" {
		t.Fatalf("recapture %+v %v", m, err)
	}
}

// Autosave pauses from BeforeStop until AfterRestart reopened the state.
func TestAutosaveSkipsDuringRestart(t *testing.T) {
	s, ed, _ := newService(t)
	ctx := context.Background()
	s.BeforeStop(ctx)
	saved := s.LastEditorState()
	ed.state = EditorState{Spec: &protocol.WindowSpec{Files: []protocol.FileSpec{}, Layout: "fresh"}}
	s.autosave(ctx)
	if spec := s.LastEditorState(); spec == nil || spec.Layout != saved.Layout {
		t.Fatalf("autosave during a restart wrote %+v", spec)
	}
	s.AfterRestart(ctx)
	if len(ed.opened) != 1 || ed.opened[0].Layout != "a.txt" {
		t.Fatalf("opened after the restart %+v", ed.opened)
	}
	s.autosave(ctx)
	if spec := s.LastEditorState(); spec == nil || spec.Layout != "fresh" {
		t.Fatalf("autosave after the restart %+v", spec)
	}
}
