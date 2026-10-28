package scenario

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

const project = `{
  "id": "projects/p1-hello-cargo",
  "title": "Hello, cargo",
  "modules": ["base", "agent"],
  "tags": ["project"],
  "project": {"roots": ["projects/hello"], "lapMinutes": 20},
  "setup": {"workspace": "fixtures/hello", "root": "projects/hello", "reset": true, "repos": ["."], "run": "echo ran >> setup.log"},
  "steps": [
    {"id": "scaffold", "goal": "Scaffold", "demo": "a", "conditions": [{"event": {"command": "eshell"}}]},
    {"id": "commit", "goal": "Commit", "demo": "b", "conditions": [{"event": {"command": "magit-commit-create"}}]}
  ]
}`

// recShell records setup.run scripts.
type recShell struct {
	mu   sync.Mutex
	runs []string
	res  steps.ShellResult
}

func (s *recShell) Run(ctx context.Context, dir, script string, timeout time.Duration) steps.ShellResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, dir+": "+script)
	return s.res
}

// recGit records git invocations.
type recGit struct {
	mu   sync.Mutex
	runs []string
}

func (g *recGit) Run(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.runs = append(g.runs, filepath.Base(dir)+": git "+strings.Join(args, " "))
	if args[0] == "init" {
		_ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	}
	return nil, nil
}

func newProjectLoader(t *testing.T) (*loaderFixture, *recShell, *recGit) {
	f := newLoader(t)
	id := "projects/p1-hello-cargo"
	f.fetch.scenarios[id] = project
	f.fetch.fixtures[id] = tarball(t, entry{name: "Cargo.toml", body: "[package]\n"})
	sh := &recShell{res: steps.ShellResult{OK: true}}
	g := &recGit{}
	f.l.Shell, f.l.Git = sh, g
	_ = f.l.Modules.Write([]string{"base", "agent"})
	return f, sh, g
}

func TestProjectSetupAndLap(t *testing.T) {
	f, sh, g := newProjectLoader(t)
	ctx := context.Background()
	id := "projects/p1-hello-cargo"

	res, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil || res.Lap != nil {
		t.Fatalf("load %+v %v", res, err)
	}
	// setup.reset re-creates the repository on every load.
	want := "hello: git init -q -b main|hello: git config user.name Learner|hello: git config user.email learner@playground.invalid|hello: git add -A|hello: git commit -q --allow-empty --no-verify --no-gpg-sign -m Initial commit"
	if got := strings.Join(g.runs, "|"); got != want {
		t.Fatalf("git runs\n%s\nwant\n%s", got, want)
	}
	if len(sh.runs) != 1 || sh.runs[0] != filepath.Join(f.ws, "projects/hello")+": echo ran >> setup.log" {
		t.Fatalf("setup.run %v", sh.runs)
	}
	if f.engine.Lap() != nil {
		t.Fatal("an ordinary load started a lap")
	}

	// A lap: reset, first step, timer started.
	f.engine.ActivateLap(f.engine.Scenario(), 1, steps.LapEnd)
	res, err = f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id, Lap: true})
	if err != nil || res.Lap == nil || res.Lap.StartedAt == 0 || res.Step.StepID != "scaffold" {
		t.Fatalf("lap load %+v %v", res, err)
	}
	if f.engine.Lap() == nil {
		t.Fatal("no lap running")
	}
	// Reloading during the lap keeps it and reports it.
	res, err = f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id})
	if err != nil || res.Lap == nil {
		t.Fatalf("reload %+v %v", res, err)
	}
	if _, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id, StepID: "commit", Lap: true}); httpx.AsError(err).Status != 400 {
		t.Fatalf("lap from a later step: %v", err)
	}
	// A failing setup.run is an error event, not a failed load.
	sh.res = steps.ShellResult{Output: "boom"}
	if _, err := f.l.Load(ctx, protocol.ScenarioRequest{LessonID: id, Reset: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.evs(), "error") {
		t.Fatalf("no error event for a failed setup.run: %s", f.evs())
	}
	if f.engine.Lap() != nil {
		t.Fatal("an ordinary load kept the lap")
	}
}

func TestRepoDir(t *testing.T) {
	for _, tc := range []struct {
		root, dir, want string
		ok              bool
	}{
		{"projects/hello", ".", "projects/hello", true},
		{"projects/poly", "service", "projects/poly/service", true},
		{"projects/poly", "../other", "", false},
		{"projects/poly", "/abs", "", false},
		{"", ".", "", false},
		{"", "hello", "hello", true},
	} {
		got, err := RepoDir(tc.root, tc.dir)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("RepoDir(%q, %q) = %q, %v", tc.root, tc.dir, got, err)
		}
	}
}

func TestInitRepoWithGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	ctx := context.Background()
	git := checkpoint.ExecRunner{}
	if err := InitRepo(ctx, git, dir, false); err != nil {
		t.Fatal(err)
	}
	head, err := git.Run(ctx, dir, nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// Same fixtures, same commit.
	dir2 := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir2, "a.txt"), []byte("a\n"), 0o644)
	_ = InitRepo(ctx, git, dir2, false)
	head2, _ := git.Run(ctx, dir2, nil, "rev-parse", "HEAD")
	if string(head) != string(head2) {
		t.Fatalf("initial commits differ: %s %s", head, head2)
	}
	// Existing repositories are kept unless reset.
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	_, _ = git.Run(ctx, dir, nil, "add", "b.txt")
	_, _ = git.Run(ctx, dir, nil, "commit", "-q", "-m", "b")
	_ = InitRepo(ctx, git, dir, false)
	if n, _ := git.Run(ctx, dir, nil, "rev-list", "--count", "HEAD"); strings.TrimSpace(string(n)) != "2" {
		t.Fatalf("history lost without reset: %s", n)
	}
	_ = InitRepo(ctx, git, dir, true)
	if n, _ := git.Run(ctx, dir, nil, "rev-list", "--count", "HEAD"); strings.TrimSpace(string(n)) != "1" {
		t.Fatalf("reset kept history: %s", n)
	}
	if name, _ := git.Run(ctx, dir, nil, "config", "user.name"); strings.TrimSpace(string(name)) != LearnerName {
		t.Fatalf("identity %q", name)
	}
}
