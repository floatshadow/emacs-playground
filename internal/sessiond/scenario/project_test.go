package scenario

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
)

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
