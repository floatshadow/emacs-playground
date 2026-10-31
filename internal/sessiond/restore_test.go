package sessiond

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
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
	s.engine = steps.New(steps.Options{})
	s.engine.Activate(&protocol.Scenario{ID: "a/b", Steps: []protocol.Step{{ID: "one"}, {ID: "two"}}}, 1)
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

func TestRestoreSerializesActivation(t *testing.T) {
	s, git := restoreServer(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	s.engine = steps.New(steps.Options{Hooks: steps.Hooks{Emit: func(ev protocol.Event) {
		if st, ok := ev.(*protocol.StepEvent); ok && st.Status == protocol.StepReset && st.StepID == "one" {
			close(entered)
			<-release
		}
	}}})
	s.engine.Activate(&protocol.Scenario{ID: "a/b", Steps: []protocol.Step{{ID: "one"}, {ID: "two"}}}, 1)
	done := make(chan error, 2)
	go func() {
		_, err := s.rpcRestore(context.Background(), protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "one")})
		done <- err
	}()
	<-entered
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := s.rpcRestore(context.Background(), protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "two")})
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("second restore passed the unfinished activation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	git.mu.Lock()
	n := len(git.restored)
	git.mu.Unlock()
	if n != 1 {
		t.Fatalf("restores before activation completed: %d", n)
	}
	once.Do(func() { close(release) })
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if a := s.engine.Active(); a == nil || a.StepID != "two" {
		t.Fatalf("active %+v", a)
	}
}

func TestRestoreAgentFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "reset-step failure"}[fail], func(t *testing.T) {
			s, _ := restoreServer(t)
			dir, err := os.MkdirTemp("", "pg-agent")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			a, err := agent.Listen(filepath.Join(dir, "agent.sock"), nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			s.agent = a
			fake := &agent.Fake{Path: a.Path(), Handle: func(op protocol.AgentOp, args json.RawMessage) (any, error) {
				if fail && op == protocol.AgentOpResetStep {
					return nil, errors.New("busy")
				}
				return agent.DefaultFakeHandler(op, args)
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = fake.Run(ctx) }()
			wait, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			if _, err := a.WaitHello(wait, 0); err != nil {
				t.Fatal(err)
			}
			before := s.engine.Saved()
			_, err = s.rpcRestore(ctx, protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "one")})
			if (err != nil) != fail {
				t.Fatalf("restore error %v", err)
			}
			saved := s.engine.Saved()
			if fail {
				if !reflect.DeepEqual(saved.Active, before.Active) || !reflect.DeepEqual(saved.Lessons, before.Lessons) {
					t.Fatalf("failed restore changed engine: %+v", saved)
				}
				if got := countCalls(fake, protocol.AgentOpEval); got != 1 {
					t.Fatalf("eval calls after failed reset: %d", got)
				}
			} else if saved.Active == nil || saved.Active.StepID != "one" || saved.Lessons["a/b"] == nil || saved.Lessons["a/b"].Resets["one"] != 1 {
				t.Fatalf("successful restore: %+v", saved)
			}
		})
	}
}

func TestRestoreMissingMetadata(t *testing.T) {
	s, _ := restoreServer(t)
	if err := os.RemoveAll(filepath.Join(s.cfg.Workspace, ".playground", "checkpoints")); err != nil {
		t.Fatal(err)
	}
	_, err := s.rpcRestore(context.Background(), protocol.RestoreRequest{Ref: checkpoint.StepRef("a/b", "one")})
	if err == nil || httpx.AsError(err).Status != 409 || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("restore error %v", err)
	}
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
	saved := s.engine.Saved()
	s.engine = steps.New(steps.Options{})
	s.engine.Restore(saved)
	s.syncProgress(context.Background())
	saved = s.engine.Saved()
	if saved.Active != nil || saved.Generations.ForLesson("a/b") != 3 {
		t.Fatalf("synchronized state %+v", saved)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var restored steps.Saved
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
			saved := s.engine.Saved()
			saved.Generations = protocol.ProgressGenerations{"": 10, "a/b": 18, "a/c": 19}
			saved.Lessons["a/b"] = &steps.LessonState{Done: map[string]int64{"one": 100}}
			s.engine = steps.New(steps.Options{Hooks: steps.Hooks{Persist: s.persist}})
			s.engine.Restore(saved)
			s.syncProgress(context.Background())
			after := s.engine.Saved()
			if !reflect.DeepEqual(after.Generations, generations) || !reflect.DeepEqual(after.Lessons, saved.Lessons) || !reflect.DeepEqual(after.Active, saved.Active) {
				t.Fatalf("synchronized state %+v", after)
			}
			persisted, _ := s.readState()
			if persisted.Generations.ForLesson("a/b") != generations.ForLesson("a/b") {
				t.Fatalf("persisted %+v", persisted)
			}
			if reports := s.engine.ProgressReports(); len(reports) != 1 || reports[0].Generation != generations.ForLesson("a/b") {
				t.Fatalf("reports %+v", reports)
			}
		})
	}
}
