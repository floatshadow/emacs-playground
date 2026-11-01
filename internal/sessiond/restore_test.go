package sessiond

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/session"
)

// progressServer is a server with a core whose saved active step is a/b
// two (no op worker; runCore makes one).
func progressServer(t *testing.T) *Server {
	t.Helper()
	s := New(&protocol.SessiondConfig{Workspace: t.TempDir()}, Options{})
	s.agent = &agent.Server{}
	s.core = session.New(session.Deps{})
	s.core.RestoreSaved(session.Saved{Active: &protocol.StepRef{LessonID: "a/b", StepID: "two"}})
	return s
}

// runCore returns a core with its op worker running until the test ends
// (generation syncs submit drop ops).
func runCore(t *testing.T, d session.Deps) *session.Core {
	c := session.New(d)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return c
}

func TestSyncProgressGenerations(t *testing.T) {
	s := progressServer(t)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/internal/sessions/local/progress-generations" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		httpx.WriteJSON(w, 200, protocol.ProgressGenerations{"": 2, "a/b": 3})
	}))
	defer gw.Close()
	s.gw = gwclient.New(gw.URL, "local", "local", nil)
	// The saved active step exists before the scenario is resumed.
	saved := s.core.Saved()
	s.core = runCore(t, session.Deps{})
	s.core.RestoreSaved(saved)
	s.syncProgress(context.Background())
	saved = s.core.Saved()
	if saved.Active != nil || saved.Generations.ForLesson("a/b") != 3 {
		t.Fatalf("synchronized state %+v", saved)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var restored session.Saved
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Generations.ForLesson("a/b") != 3 {
		t.Fatalf("persisted generations %+v", restored.Generations)
	}
}

func TestSyncProgressLowersGenerations(t *testing.T) {
	for _, generations := range []protocol.ProgressGenerations{{}, {"": 2, "a/b": 3}} {
		t.Run(fmt.Sprint(generations), func(t *testing.T) {
			s := progressServer(t)
			gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.WriteJSON(w, 200, generations) }))
			defer gw.Close()
			s.gw = gwclient.New(gw.URL, "local", "local", nil)
			saved := s.core.Saved()
			saved.Generations = protocol.ProgressGenerations{"": 10, "a/b": 18, "a/c": 19}
			saved.Lessons["a/b"] = &session.LessonState{Done: map[string]int64{"one": 100}}
			s.core = runCore(t, session.Deps{Persist: s.persist})
			s.core.RestoreSaved(saved)
			s.syncProgress(context.Background())
			after := s.core.Saved()
			if !reflect.DeepEqual(after.Generations, generations) || !reflect.DeepEqual(after.Lessons, saved.Lessons) || !reflect.DeepEqual(after.Active, saved.Active) {
				t.Fatalf("synchronized state %+v", after)
			}
			persisted := s.readState()
			if persisted.Generations.ForLesson("a/b") != generations.ForLesson("a/b") {
				t.Fatalf("persisted %+v", persisted)
			}
			if reports := s.core.ProgressReports(); len(reports) != 1 || reports[0].Generation != generations.ForLesson("a/b") {
				t.Fatalf("reports %+v", reports)
			}
		})
	}
}
