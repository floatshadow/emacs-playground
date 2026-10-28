package checkpoint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNestedRepoRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ws := t.TempDir()
	ctx := context.Background()
	m := New(ws, "git")
	m.Now = func() time.Time { return time.UnixMilli(1761472000000) }
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	// A project with its own repository (setup.repos), its build output
	// ignored by its own .gitignore.
	proj := filepath.Join(ws, "projects/hello")
	write(t, ws, "projects/hello/src/main.rs", "fn main() {}\n")
	write(t, ws, "projects/hello/.gitignore", "/target\n")
	write(t, ws, "projects/hello/target/debug/hello", "bin")
	write(t, ws, "notes.txt", "outer\n")
	gitOut(t, proj, "init", "-q", "-b", "main")
	gitOut(t, proj, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitOut(t, proj, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "Initial commit")
	initial := gitOut(t, proj, "rev-parse", "HEAD")

	ref := StepRef("projects/p1-hello-cargo", "commit")
	meta, err := m.Take(ctx, ref, "p1 commit", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Nested) != 1 || meta.Nested[0].Path != "projects/hello" {
		t.Fatalf("nested %+v", meta.Nested)
	}
	// The outer tree has no gitlink and none of the project's files.
	tree := gitOut(t, ws, "ls-tree", "-r", ref)
	if strings.Contains(tree, "projects/hello") || !strings.Contains(tree, "notes.txt") {
		t.Fatalf("outer tree:\n%s", tree)
	}
	// The snapshots are reachable from the checkpoint commit.
	parents := gitOut(t, ws, "rev-list", "--parents", "-n", "1", ref)
	if !strings.Contains(parents, meta.Nested[0].Worktree) || !strings.Contains(parents, meta.Nested[0].GitDir) {
		t.Fatalf("parents %s, nested %+v", parents, meta.Nested)
	}
	if wt := gitOut(t, ws, "ls-tree", "-r", "--name-only", meta.Nested[0].Worktree); strings.Contains(wt, "target") || !strings.Contains(wt, "src/main.rs") {
		t.Fatalf("worktree snapshot:\n%s", wt)
	}

	// The learner edits, stages, commits, makes a branch and a new repo.
	write(t, ws, "projects/hello/src/main.rs", "fn main() { println!(\"hi\"); }\n")
	write(t, ws, "projects/hello/src/lib.rs", "pub fn f() {}\n")
	gitOut(t, proj, "add", "src/main.rs")
	gitOut(t, proj, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "Say hi")
	gitOut(t, proj, "branch", "feature")
	gitOut(t, proj, "add", "src/lib.rs")
	write(t, ws, "projects/hello/target/debug/hello", "bin2")
	write(t, ws, "other/README", "new repo\n")
	gitOut(t, filepath.Join(ws, "other"), "init", "-q")
	write(t, ws, "notes.txt", "changed\n")

	items, err := m.List(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("list %+v %v", items, err)
	}
	for _, want := range []string{"notes.txt", "projects/hello/src/main.rs", "projects/hello/src/lib.rs"} {
		if !slices.Contains(items[0].Files, want) {
			t.Errorf("list files %v lack %s", items[0].Files, want)
		}
	}

	files, _, err := m.Restore(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := gitOut(t, proj, "rev-parse", "HEAD"); got != initial {
		t.Fatalf("HEAD %s, want %s", got, initial)
	}
	if out := gitOut(t, proj, "status", "--porcelain"); out != "" {
		t.Fatalf("project status after restore:\n%s", out)
	}
	if b := gitOut(t, proj, "branch", "--list", "feature"); b != "" {
		t.Fatalf("branch survived: %q", b)
	}
	if c, _ := read(t, ws, "projects/hello/src/main.rs"); c != "fn main() {}\n" {
		t.Fatalf("main.rs %q", c)
	}
	if _, ok := read(t, ws, "projects/hello/src/lib.rs"); ok {
		t.Fatal("lib.rs survived")
	}
	if c, _ := read(t, ws, "projects/hello/target/debug/hello"); c != "bin2" {
		t.Fatal("build output must be kept")
	}
	if _, err := os.Stat(filepath.Join(ws, "other")); err == nil {
		t.Fatal("the repository made after the checkpoint survived")
	}
	if c, _ := read(t, ws, "notes.txt"); c != "outer\n" {
		t.Fatalf("notes.txt %q", c)
	}
	for _, want := range []string{"notes.txt", "projects/hello/src/main.rs", "other/.git"} {
		if !slices.Contains(files, want) {
			t.Errorf("restored files %v lack %s", files, want)
		}
	}
	// git still works in the restored repository.
	gitOut(t, proj, "log", "--oneline")
	gitOut(t, proj, "fsck", "--no-progress")
}
