package sessiond

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
	"github.com/floatshadow/emacs-playground/internal/sessiond/session"
)

type restoreGit struct {
	mu       sync.Mutex
	restored []string
}

func (g *restoreGit) Run(_ context.Context, _ string, _ []string, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch args[0] {
	case "rev-parse", "commit-tree":
		return []byte("commit"), nil
	case "write-tree":
		return []byte("tree"), nil
	case "restore":
		g.restored = append(g.restored, strings.TrimPrefix(args[1], "--source="))
	}
	return nil, nil
}

func restoreServer(t *testing.T) (*Server, *restoreGit) {
	t.Helper()
	s := New(&protocol.SessiondConfig{Workspace: t.TempDir()}, Options{})
	g := &restoreGit{}
	s.agent = &agent.Server{}
	s.ckpt = &checkpoint.Manager{Workspace: s.cfg.Workspace, Git: g, Now: time.Now}
	s.core = session.New(session.Deps{})
	s.core.RestoreSaved(session.Saved{Active: &protocol.StepRef{LessonID: "a/b", StepID: "two"}})
	for _, id := range []string{"one", "two"} {
		if _, err := s.ckpt.TakeWith(context.Background(), checkpoint.Meta{
			Ref: checkpoint.StepRef("a/b", id), Step: &protocol.StepRef{LessonID: "a/b", StepID: id},
			Globals: "()", Buffers: map[string]checkpoint.BufferState{"a.txt": {Modified: true, HasText: true, Text: "saved"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return s, g
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
	s, _ := restoreServer(t)
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
			s, _ := restoreServer(t)
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
