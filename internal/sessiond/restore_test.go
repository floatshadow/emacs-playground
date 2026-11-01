package sessiond

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/progress"
	"github.com/floatshadow/emacs-playground/internal/sessiond/session"
)

// progressServer is a server whose core (its op worker running until the
// test ends) has saved the active step a/b two and the outcome a/b one,
// with the outbox at generations gens, against a gateway that answers
// the generations endpoint with gw.
func progressServer(t *testing.T, gens, gw protocol.ProgressGenerations) *Server {
	t.Helper()
	s := New(&protocol.SessiondConfig{Workspace: t.TempDir(), SessionID: "local"}, Options{})
	s.agent = &agent.Server{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/internal/sessions/local/progress-generations" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		httpx.WriteJSON(w, 200, gw)
	}))
	t.Cleanup(srv.Close)
	s.gw = gwclient.New(srv.URL, "local", "local", nil)
	s.outbox = progress.New(progress.Snapshot{Generations: gens}, nil, s.persist, nil)
	runCore(t, s, session.Saved{Active: &protocol.StepRef{LessonID: "a/b", StepID: "two"},
		Lessons: map[string]*session.LessonState{"a/b": {Done: map[string]int64{"one": 100}}}})
	return s
}

// runCore gives s a core over its outbox and gateway, starting from saved,
// whose op worker runs until the test ends.
func runCore(t *testing.T, s *Server, saved session.Saved) {
	s.core = session.New(session.Deps{Progress: s.outbox, Generations: s.gw.ProgressGenerations, Persist: s.persist})
	s.core.RestoreSaved(saved)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	core := s.core
	go func() {
		defer close(done)
		core.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// A reset missed while the session was stopped drops the lesson's saved
// progress and active step; the generations are adopted and persisted.
func TestSyncProgressGenerations(t *testing.T) {
	gens := protocol.ProgressGenerations{"": 2, "a/b": 3}
	s := progressServer(t, nil, gens)
	s.outbox.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone})
	s.syncProgress(context.Background(), false)
	saved := s.core.Saved()
	if saved.Active != nil || len(saved.Lessons) != 0 || s.outbox.Len() != 0 {
		t.Fatalf("synchronized state %+v, %d entries", saved, s.outbox.Len())
	}
	if st := s.readState(); !reflect.DeepEqual(st.Generations, gens) || st.Version != 2 {
		t.Fatalf("persisted %+v", st)
	}
}

// Lower gateway generations (a store replaced) are adopted exactly and
// forget nothing.
func TestSyncProgressLowersGenerations(t *testing.T) {
	for _, gens := range []protocol.ProgressGenerations{{}, {"": 2, "a/b": 3}} {
		t.Run(fmt.Sprint(gens), func(t *testing.T) {
			s := progressServer(t, protocol.ProgressGenerations{"": 10, "a/b": 18, "a/c": 19}, gens)
			before := s.core.Saved()
			s.syncProgress(context.Background(), false)
			after := s.core.Saved()
			if got := s.outbox.Snapshot().Generations; !reflect.DeepEqual(got, gens) || !reflect.DeepEqual(after, before) {
				t.Fatalf("generations %v, state %+v", got, after)
			}
			if st := s.readState(); st.Generations.ForLesson("a/b") != gens.ForLesson("a/b") {
				t.Fatalf("persisted %+v", st)
			}
		})
	}
}

// state.json version 1 (no outbox) reads, and the startup sync writes
// version 2 with the saved outcomes in the outbox.
func TestStateFileVersions(t *testing.T) {
	s := progressServer(t, nil, protocol.ProgressGenerations{"a/b": 4})
	v1 := `{"version": 1, "generations": {"a/b": 4}, "active": {"lessonId": "a/b", "stepId": "two"},
  "lessons": {"a/b": {"done": {"one": 100}, "hints": {"one": 2}}}, "lap": {"lessonId": "a/b", "startedAt": 7}}`
	if err := os.MkdirAll(filepath.Dir(s.statePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.statePath(), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	st := s.readState()
	if st.Version != 1 || st.Generations["a/b"] != 4 || st.saved().Active.StepID != "two" ||
		st.saved().Lessons["a/b"].Hints["one"] != 2 || st.saved().Lap.StartedAt != 7 || len(st.Outbox) != 0 {
		t.Fatalf("version 1 read as %+v", st)
	}
	s.outbox = progress.New(st.outbox(), nil, nil, nil)
	runCore(t, s, st.saved())
	s.syncProgress(context.Background(), true)
	v2 := s.readState()
	if len(v2.Outbox) != 1 {
		t.Fatalf("version 2 written as %+v", v2)
	}
	want := []protocol.ProgressReport{{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 100, HintsUsed: v2.Outbox[0].HintsUsed, Generation: 4}}
	if v2.Version != 2 || !reflect.DeepEqual(v2.Outbox, want) || *v2.Outbox[0].HintsUsed != 2 || !reflect.DeepEqual(v2.saved(), st.saved()) {
		t.Fatalf("version 2 written as %+v", v2)
	}
}
