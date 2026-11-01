package session

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/progress"
)

// hookedProgress is an outbox with hooks before Record and Snapshot.
type hookedProgress struct {
	*progress.Outbox
	onRecord, onSnapshot func()
}

func (h *hookedProgress) Record(r protocol.ProgressReport) {
	if h.onRecord != nil {
		h.onRecord()
	}
	h.Outbox.Record(r)
}

func (h *hookedProgress) Snapshot() progress.Snapshot {
	if h.onSnapshot != nil {
		h.onSnapshot()
	}
	return h.Outbox.Snapshot()
}

func withProgress(h *hookedProgress) func(*fixture, *Deps) {
	return func(_ *fixture, d *Deps) { d.Progress = h }
}

// A drop that arrives while state.json's content is read waits for the
// read: the saved outcomes and the generations written together are
// always both from before the drop or both from after it, so a restart
// never queues reset outcomes under the reset's generation.
func TestPersistentIsOneState(t *testing.T) {
	h := &hookedProgress{Outbox: progress.New(progress.Snapshot{}, nil, nil, nil)}
	f := newFixture(t, withProgress(h))
	f.core.RestoreSaved(Saved{Lessons: map[string]*LessonState{lessonID: {Done: map[string]int64{"open": 100}}}})
	dropped := make(chan error, 1)
	h.onSnapshot = func() {
		h.onSnapshot = nil
		go func() { dropped <- f.core.Drop(context.Background(), lessonID, 5) }()
		time.Sleep(50 * time.Millisecond)
	}
	check := func(when string, wantDropped bool) {
		t.Helper()
		saved, ob := f.core.Persistent()
		kept, gen := saved.Lessons[lessonID] != nil, ob.Generations.ForLesson(lessonID)
		if kept == wantDropped || (gen == 5) != wantDropped {
			t.Fatalf("%s: outcomes kept %v with generation %d", when, kept, gen)
		}
	}
	check("read while the drop arrives", false)
	if err := <-dropped; err != nil {
		t.Fatal(err)
	}
	check("read after the drop", true)
}

// The startup reconciliation queues the saved outcomes in one hold of the
// core's lock, inside the sync op: a drop that arrives meanwhile runs
// after it and purges what it queued, instead of the reconciliation
// stamping outcomes it read before the drop with the drop's generation.
func TestReconcileAndDropDoNotInterleave(t *testing.T) {
	h := &hookedProgress{Outbox: progress.New(progress.Snapshot{}, nil, nil, nil)}
	f := newFixture(t, withProgress(h))
	f.core.RestoreSaved(Saved{Lessons: map[string]*LessonState{lessonID: {Done: map[string]int64{"open": 100, "save": 200}}}})
	dropped := make(chan error, 1)
	var once sync.Once
	h.onRecord = func() {
		once.Do(func() {
			go func() { dropped <- f.core.Drop(context.Background(), lessonID, 5) }()
			time.Sleep(50 * time.Millisecond)
		})
	}
	if err := f.core.Sync(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := <-dropped; err != nil {
		t.Fatal(err)
	}
	ob := h.Outbox.Snapshot()
	for _, r := range ob.Entries {
		if r.LessonID == lessonID {
			t.Fatalf("a dropped outcome is queued: %+v", r)
		}
	}
	if ob.Generations.ForLesson(lessonID) != 5 || f.core.Saved().Lessons[lessonID] != nil {
		t.Fatalf("after the drop: generations %v, saved %+v", ob.Generations, f.core.Saved())
	}
}

// A skip or demo start admitted for lesson A's step "open" and queued
// behind a load of lesson B, whose first step is also "open", does not
// run on B's step: it names the attempt it was admitted for.
func TestQueuedOpsKeepTheirAttempt(t *testing.T) {
	for _, op := range []string{"skip", "demo start"} {
		t.Run(op, func(t *testing.T) {
			f := newDemoFixture(t)
			ctx := context.Background()
			if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID}); err != nil {
				t.Fatal(err)
			}
			f.idle()
			block := make(chan struct{})
			f.agent.mu.Lock()
			f.agent.block = block
			f.agent.mu.Unlock()
			loaded := make(chan error, 1)
			go func() {
				_, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: demoID})
				loaded <- err
			}()
			f.waitFor("lesson B's setup", func() bool { return len(f.agent.setups()) == 2 })
			done := make(chan error, 1)
			go func() {
				var err error
				if op == "skip" {
					_, err = f.core.Skip(ctx, "open")
				} else {
					_, err = f.core.DemoStart(ctx, "open")
				}
				done <- err
			}()
			f.waitFor("the queued "+op, func() bool { return f.queued() == 1 })
			close(block)
			if err := <-loaded; err != nil {
				t.Fatal(err)
			}
			if err := <-done; httpx.AsError(err).Status != 409 {
				t.Fatalf("%s admitted for lesson A: %v", op, err)
			}
			f.idle()
			if a := f.core.Active(); a == nil || a.LessonID != demoID || a.StepID != "open" || f.core.Watching() ||
				len(f.core.Saved().Lessons[demoID].Skipped) != 0 || f.played() != "" {
				t.Fatalf("lesson B's step: active %+v watching %v saved %+v played %q", a, f.core.Watching(),
					f.core.Saved().Lessons[demoID], f.played())
			}
		})
	}
}

func (s *fakeSnaps) editorRestored() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.editorRestores)
}

// A restore made while the agent was away leaves its editor state to the
// agent's one hello, even when that hello comes while the op still
// restores the files and its step has not activated: the reconnect op
// runs after the op. An activation since the restore makes it moot.
func TestReconnectFollowsTheRestoringOp(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, step string
		run        func(*fixture) error
	}{
		{"go to step", "save", func(f *fixture) error {
			_, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID, StepID: "save"})
			return err
		}},
		{"reset", "open", func(f *fixture) error {
			_, err := f.core.Restore(ctx, protocol.RestoreRequest{})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID}); err != nil {
				t.Fatal(err)
			}
			f.idle()
			f.snaps.mu.Lock()
			f.snaps.restorable["save"], f.snaps.deferred = true, true
			f.snaps.onRestore = func() { f.core.Reconnected() }
			f.snaps.mu.Unlock()
			if err := tc.run(f); err != nil {
				t.Fatal(err)
			}
			f.idle()
			ref := checkpoint.StepRef(lessonID, tc.step)
			if got := f.snaps.editorRestored(); !slices.Equal(got, []string{ref}) {
				t.Fatalf("editor restores %v, want %s once", got, ref)
			}
			if a := f.core.Active(); a == nil || a.StepID != tc.step {
				t.Fatalf("active %+v", a)
			}
		})
	}

	t.Run("activated since", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: lessonID}); err != nil {
			t.Fatal(err)
		}
		f.idle()
		f.snaps.mu.Lock()
		f.snaps.deferred = true
		f.snaps.mu.Unlock()
		if _, err := f.core.Restore(ctx, protocol.RestoreRequest{}); err != nil {
			t.Fatal(err)
		}
		f.eval.OnKey(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file"})
		f.waitFor("the next step", func() bool { a := f.core.Active(); return a != nil && a.StepID == "save" })
		f.core.Reconnected()
		f.idle()
		if got := f.snaps.editorRestored(); len(got) != 0 {
			t.Fatalf("a replaced editor state was restored: %v", got)
		}
	})
}

// An autosave is queued only while no op runs or waits, so its capture
// never overlaps an op that puts another editor state in place.
func TestAutosaveOnlyWhenIdle(t *testing.T) {
	f := newFixture(t)
	release := make(chan struct{})
	f.sup.release = release
	if _, err := f.core.SetModules(context.Background(), []string{"base", "agent", "files"}); err != nil {
		t.Fatal(err)
	}
	f.core.Autosave()
	if f.queued() != 0 {
		t.Fatal("an autosave queued behind an op")
	}
	close(release)
	f.idle()
	f.core.Autosave()
	f.idle()
	f.snaps.mu.Lock()
	defer f.snaps.mu.Unlock()
	if f.snaps.saves != 1 {
		t.Fatalf("autosaves %d", f.snaps.saves)
	}
}

// The supervisor's AfterRestart hook reopens the editor state inline when
// a restart the core asked for runs it (inside the op: waiting for the
// queue would deadlock), and after a crash waits for the recover op, which
// runs after the op in progress.
func TestAfterRestart(t *testing.T) {
	spec := &protocol.WindowSpec{Files: []protocol.FileSpec{{Path: "hello/README.md"}}}
	f := newFixture(t, func(f *fixture, _ *Deps) { f.snaps.spec = spec })
	ctx := context.Background()
	f.sup.afterRestart = func(ctx context.Context) { f.core.AfterRestart(ctx, false) }
	if _, err := f.core.SetModules(ctx, []string{"base", "agent", "files"}); err != nil {
		t.Fatal(err)
	}
	f.idle()
	opens := func() int {
		f.snaps.mu.Lock()
		defer f.snaps.mu.Unlock()
		return len(f.snaps.opens)
	}
	if opens() != 1 {
		t.Fatalf("reopened %d times inside the restart", opens())
	}

	release := make(chan struct{})
	f.sup.mu.Lock()
	f.sup.release = release
	f.sup.mu.Unlock()
	if _, err := f.core.SetModules(ctx, []string{"base", "agent", "files", "editing"}); err != nil {
		t.Fatal(err)
	}
	recovered := make(chan struct{})
	go func() {
		f.core.AfterRestart(ctx, true)
		close(recovered)
	}()
	f.waitFor("the recover op", func() bool { return f.queued() == 1 })
	if opens() != 1 {
		t.Fatal("a crash recovery reopened during an op")
	}
	close(release)
	<-recovered
	if opens() != 3 {
		t.Fatalf("reopened %d times in all", opens())
	}
}
