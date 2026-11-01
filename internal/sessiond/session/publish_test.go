package session

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// published returns the last lesson event and checks the publisher's
// invariants: revs strictly increase, and the last snapshot published is
// the current one (nothing unpublished).
func (f *fixture) published() *protocol.LessonEvent {
	f.t.Helper()
	f.idle()
	f.mu.Lock()
	evs := append([]*protocol.LessonEvent(nil), f.lessons...)
	f.mu.Unlock()
	if len(evs) == 0 {
		f.t.Fatal("no lesson event")
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Lesson.Rev <= evs[i-1].Lesson.Rev {
			f.t.Fatalf("rev %d after %d", evs[i].Lesson.Rev, evs[i-1].Lesson.Rev)
		}
	}
	last := evs[len(evs)-1]
	if now := f.core.Snapshot(); !reflect.DeepEqual(now, last.Lesson) {
		f.t.Fatalf("unpublished: last %+v, now %+v", last.Lesson, now)
	}
	return last
}

func (f *fixture) key(cmd string) { f.eval.OnKey(&protocol.KeyEvent{Cmd: cmd}) }

func TestSnapshotTransitions(t *testing.T) {
	ctx := context.Background()
	load := func(t *testing.T, step string) *fixture {
		f := newDemoFixture(t)
		if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: demoID, StepID: step}); err != nil {
			t.Fatal(err)
		}
		return f
	}
	steps := map[string]string{"find-file": "open", "save-buffer": "save", "kill-buffer": "kill"}
	finish := func(f *fixture, cmds ...string) {
		for _, cmd := range cmds {
			f.key(cmd)
			f.waitFor(cmd, func() bool { return strings.Contains(f.evs(), "lesson:"+steps[cmd]+":complete") })
			f.idle()
		}
	}
	done := map[string]protocol.ProgressStatus{"open": protocol.ProgressDone, "save": protocol.ProgressDone, "kill": protocol.ProgressDone}

	t.Run("the final completion publishes complete", func(t *testing.T) {
		f := load(t, "")
		finish(f, "find-file", "save-buffer", "kill-buffer")
		last := f.published()
		if last.Change != protocol.ChangeComplete || last.StepID != "kill" || last.Lesson.Phase != protocol.PhaseComplete ||
			last.Lesson.Step != nil || last.Lesson.Attempt != 0 || !reflect.DeepEqual(last.Lesson.Outcomes, done) {
			t.Fatalf("last %+v", last)
		}
		// The drop of a completed lesson forgets it.
		if err := f.core.Drop(ctx, demoID, 0); err != nil {
			t.Fatal(err)
		}
		last = f.published()
		if last.Change != protocol.ChangeDropped || last.StepID != "" || last.Lesson.LessonID != "" || last.Lesson.Phase != protocol.PhaseIdle || len(last.Lesson.Outcomes) != 0 {
			t.Fatalf("after the drop %+v", last)
		}
	})

	t.Run("drops of the active step and of another lesson", func(t *testing.T) {
		f := load(t, "")
		before := f.published().Lesson
		if err := f.core.Drop(ctx, lessonID, 0); err != nil {
			t.Fatal(err)
		}
		last := f.published()
		before.Rev = last.Lesson.Rev
		if last.Change != protocol.ChangeDropped || last.StepID != "" || !reflect.DeepEqual(last.Lesson, before) {
			t.Fatalf("drop of another lesson %+v", last)
		}
		if err := f.core.Drop(ctx, demoID, 0); err != nil {
			t.Fatal(err)
		}
		last = f.published()
		if last.Change != protocol.ChangeDropped || last.StepID != "open" || last.Lesson.LessonID != "" || last.Lesson.Step != nil {
			t.Fatalf("drop of the active step %+v", last)
		}
	})

	t.Run("Watch it, demonstrated, Reset", func(t *testing.T) {
		f := load(t, "")
		res, err := f.core.DemoStart(ctx, "open")
		if err != nil {
			t.Fatal(err)
		}
		last := f.published()
		if !strings.HasSuffix(f.evs(), "lesson:open:phase") || last.Lesson.Phase != protocol.PhaseWatching || last.Lesson.Attempt != res.Attempt {
			t.Fatalf("watching %+v (demo attempt %d)", last, res.Attempt)
		}
		f.key("find-file")
		f.waitFor("demonstrated", func() bool { return strings.Contains(f.evs(), "lesson:open:demonstrated") })
		last = f.published()
		if last.Lesson.Phase != protocol.PhaseWatching || last.Lesson.Outcomes["open"] != protocol.ProgressDemonstrated || last.Lesson.Step.ID != "open" {
			t.Fatalf("demonstrated %+v", last)
		}
		if _, err := f.core.Restore(ctx, protocol.RestoreRequest{}); err != nil {
			t.Fatal(err)
		}
		last = f.published()
		if !strings.HasSuffix(f.evs(), "lesson:open:reset") || last.Lesson.Phase != protocol.PhaseActive || len(last.Lesson.Outcomes) != 0 || last.Lesson.Attempt <= res.Attempt {
			t.Fatalf("reset %+v", last)
		}
	})

	t.Run("Skip of the last step", func(t *testing.T) {
		f := load(t, "")
		finish(f, "find-file", "save-buffer")
		if _, err := f.core.Skip(ctx, "kill"); err != nil {
			t.Fatal(err)
		}
		last := f.published()
		want := map[string]protocol.ProgressStatus{"open": protocol.ProgressDone, "save": protocol.ProgressDone, "kill": protocol.ProgressSkipped}
		if last.Change != protocol.ChangeBusy || !strings.HasSuffix(f.evs(), "lesson:kill:skipped:lesson-complete") ||
			last.Lesson.Phase != protocol.PhaseComplete || last.Lesson.Busy != nil || !reflect.DeepEqual(last.Lesson.Outcomes, want) {
			t.Fatalf("skip %s %+v", f.evs(), last)
		}
	})

	t.Run("a fast-forward never exposes a rehearsed step", func(t *testing.T) {
		f := newDemoFixture(t)
		var mu sync.Mutex
		var during []protocol.LessonSnapshot
		f.sup.onKeys = func(context.Context) error {
			mu.Lock()
			during = append(during, f.core.Snapshot())
			mu.Unlock()
			return nil
		}
		if _, err := f.core.Load(ctx, protocol.ScenarioRequest{LessonID: demoID, StepID: "kill"}); err != nil {
			t.Fatal(err)
		}
		last := f.published()
		f.mu.Lock()
		evs := append([]*protocol.LessonEvent(nil), f.lessons...)
		f.mu.Unlock()
		for _, s := range append(during, func() (out []protocol.LessonSnapshot) {
			for _, e := range evs {
				out = append(out, e.Lesson)
			}
			return out
		}()...) {
			if s.Step != nil && s.Step.ID != "kill" {
				t.Fatalf("a rehearsed step is visible: %+v", s)
			}
		}
		if len(during) == 0 || during[0].Busy == nil || during[0].Busy.Op != string(OpLoad) || last.Lesson.Step.ID != "kill" {
			t.Fatalf("during %+v, last %+v", during, last)
		}
	})

	t.Run("a failed rehearsal publishes the snapshot before the load", func(t *testing.T) {
		f := load(t, "")
		before := f.published().Lesson
		after := interruptLoad(t, f)
		if after.Attempt <= before.Attempt {
			t.Fatalf("attempt %d after %d", after.Attempt, before.Attempt)
		}
		after.Rev, after.Attempt = before.Rev, before.Attempt
		if !reflect.DeepEqual(after, before) {
			t.Fatalf("before %+v, after %+v", before, after)
		}
	})

	t.Run("a failed rehearsal from watching makes the step active without the latch", func(t *testing.T) {
		f := load(t, "")
		res, err := f.core.DemoStart(ctx, "open")
		if err != nil || f.published().Lesson.Phase != protocol.PhaseWatching {
			t.Fatalf("demo start %+v %v", res, err)
		}
		after := interruptLoad(t, f)
		f.core.mu.Lock()
		demo := f.core.lesson.Demo
		f.core.mu.Unlock()
		if after.Phase != protocol.PhaseActive || after.Step.ID != "open" || after.Attempt <= res.Attempt || demo != (Demo{}) {
			t.Fatalf("after %+v, demo %+v", after, demo)
		}
	})
}

// interruptLoad starts a load of the demo lesson's last step, cancels it
// in its fast-forward and returns the snapshot published after it.
func interruptLoad(t *testing.T, f *fixture) protocol.LessonSnapshot {
	t.Helper()
	inReplay := make(chan struct{})
	var once sync.Once
	f.sup.mu.Lock()
	f.sup.onKeys = func(ctx context.Context) error {
		once.Do(func() { close(inReplay) })
		<-ctx.Done()
		return ctx.Err()
	}
	f.sup.mu.Unlock()
	loaded := make(chan error, 1)
	go func() {
		_, err := f.core.Load(context.Background(), protocol.ScenarioRequest{LessonID: demoID, StepID: "kill"})
		loaded <- err
	}()
	<-inReplay
	f.core.mu.Lock()
	f.core.running.cancel()
	f.core.mu.Unlock()
	if err := <-loaded; err == nil {
		t.Fatal("the interrupted load succeeded")
	}
	return f.published().Lesson
}
