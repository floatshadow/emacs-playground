package checkpoint

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestCommandBuilding(t *testing.T) {
	cmds := TakeCommands("/work/.playground/index.tmp", "refs/playground/02-buffers-and-files/03-opening-files/open",
		"checkpoint 02-buffers-and-files/03-opening-files/open", "abc123", "")
	want := [][]string{
		{"add", "-A", "--", ".", ":!.playground"},
		{"write-tree"},
		{"commit-tree", "abc123", "-m", "checkpoint 02-buffers-and-files/03-opening-files/open"},
		{"update-ref", "refs/playground/02-buffers-and-files/03-opening-files/open", "$commit"},
	}
	for i, c := range cmds {
		if !reflect.DeepEqual(c.Args, want[i]) {
			t.Errorf("take %d: %q want %q", i, c.Args, want[i])
		}
	}
	for i := 0; i < 3; i++ {
		if cmds[i].Env[0] != "GIT_INDEX_FILE=/work/.playground/index.tmp" {
			t.Errorf("take %d env %v", i, cmds[i].Env)
		}
	}
	if len(cmds[3].Env) != 0 {
		t.Errorf("update-ref must not use the temporary index")
	}
	withParent := TakeCommands("/i", "refs/playground/a/b", "m", "t", "p0")
	if got := withParent[2].Args; !reflect.DeepEqual(got, []string{"commit-tree", "t", "-m", "m", "-p", "p0"}) {
		t.Errorf("parent: %q", got)
	}
	if !slices.Contains(withParent[2].Env, "GIT_AUTHOR_NAME=Emacs Playground") {
		t.Errorf("commit-tree needs an identity: %v", withParent[2].Env)
	}
	rc := RestoreCommands("refs/playground/a/b")
	if !reflect.DeepEqual(rc[0].Args, []string{"restore", "--source=refs/playground/a/b", "--worktree", "--staged", "--", "."}) {
		t.Errorf("restore %q", rc[0].Args)
	}
	if strings.Join(rc[1].Args, " ") != "clean -fd -e target -e _build -e .venv -e node_modules -e .playground" {
		t.Errorf("clean %q", rc[1].Args)
	}
}

func TestSlugAndRefs(t *testing.T) {
	cases := map[string]string{
		"Before refactor!": "before-refactor",
		"  ":               "checkpoint",
		"v1.2 / tests ok":  "v1-2-tests-ok",
		"中文":               "checkpoint",
		"a--b":             "a--b",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q want %q", in, got, want)
		}
	}
	if StepRef("02-buffers-and-files/03-opening-files", "open") != "refs/playground/02-buffers-and-files/03-opening-files/open" {
		t.Error("step ref")
	}
	for ref, ok := range map[string]bool{
		"refs/playground/a/b":        true,
		"refs/playground/manual/x":   true,
		"refs/heads/main":            false,
		"refs/playground/":           false,
		"refs/playground/../heads/x": false,
		"refs/playground/a b":        false,
		"refs/playground/a/.hidden":  false,
		"refs/playground/a/b.lock":   false,
		"refs/playground/a/b@{1}":    false,
		"refs/playground/a//b":       false,
		"refs/playground/x;rm -rf /": false,
	} {
		if ValidRef(ref) != ok {
			t.Errorf("ValidRef(%q) = %v", ref, !ok)
		}
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestTakeListRestoreRealRepo(t *testing.T) {
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
	if err := m.Init(ctx); err != nil { // idempotent
		t.Fatal(err)
	}
	excl, _ := read(t, ws, ".git/info/exclude")
	if strings.Count(excl, "/emacs.d/elpa/") != 1 || !strings.Contains(excl, "target/") {
		t.Fatalf("exclude:\n%s", excl)
	}
	if gi, _ := read(t, ws, ".playground/.gitignore"); gi != "*\n" {
		t.Fatalf(".playground/.gitignore %q", gi)
	}

	write(t, ws, "hello/src/main.rs", "fn main() { println!(\"Hello, world\"); }\n")
	write(t, ws, "hello/README.md", "# hello\n")
	write(t, ws, "hello/target/debug/hello", "binary v1")
	write(t, ws, "emacs.d/modules.el", "(setq playground-modules '(base agent))\n")
	write(t, ws, "emacs.d/elpa/vertico/vertico.el", ";; pkg")
	write(t, ws, ".playground/state.json", "{}")
	// Empty directories, which git cannot hold: a restore recreates them.
	_ = os.MkdirAll(filepath.Join(ws, "hello/docs"), 0o755)
	_ = os.MkdirAll(filepath.Join(ws, "emacs.d/elpa/empty"), 0o755)

	ref := StepRef("02-buffers-and-files/03-opening-files", "open")
	spec := &protocol.WindowSpec{Files: []protocol.FileSpec{{Path: "hello/src/main.rs", Point: 1, Visible: true}}, Layout: "single"}
	step := &protocol.StepRef{LessonID: "02-buffers-and-files/03-opening-files", StepID: "open"}
	meta, err := m.Take(ctx, ref, "02-buffers-and-files/03-opening-files/open", step, spec)
	if err != nil {
		t.Fatal(err)
	}
	if meta.CreatedAt != 1761472000000 || meta.Ref != ref {
		t.Fatalf("meta %+v", meta)
	}
	if !reflect.DeepEqual(meta.EmptyDirs, []string{"hello/docs"}) {
		t.Fatalf("empty dirs %v", meta.EmptyDirs)
	}
	// The learner's index and branch are untouched: no HEAD, nothing staged.
	if out := gitOut(t, ws, "status", "--porcelain"); strings.Contains(out, "A ") || strings.Contains(out, "M ") || strings.Contains(out, ".playground") {
		t.Fatalf("learner index changed or .playground visible:\n%s", out)
	}
	if _, err := m.Git.Run(ctx, ws, nil, "rev-parse", "-q", "--verify", "HEAD"); err == nil {
		t.Fatal("HEAD must not be created")
	}
	// The checkpoint tree has the files but not build output, packages or
	// playground state.
	tree := gitOut(t, ws, "ls-tree", "-r", "--name-only", ref)
	for _, want := range []string{"hello/src/main.rs", "hello/README.md", "emacs.d/modules.el"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree lacks %s:\n%s", want, tree)
		}
	}
	for _, not := range []string{"target", "elpa", ".playground"} {
		if strings.Contains(tree, not) {
			t.Errorf("tree has %s:\n%s", not, tree)
		}
	}
	saved, err := m.ReadMeta(ref)
	if err != nil || saved.Spec == nil || saved.Spec.Files[0].Path != "hello/src/main.rs" || saved.Step.StepID != "open" {
		t.Fatalf("saved meta %+v %v", saved, err)
	}
	if _, ok := read(t, ws, ".playground/checkpoints/02-buffers-and-files/03-opening-files/open.json"); !ok {
		t.Fatal("meta file not at the documented path")
	}

	// The learner edits, deletes, creates; builds produce output.
	write(t, ws, "hello/src/main.rs", "fn main() {}\n")
	_ = os.Remove(filepath.Join(ws, "hello/README.md"))
	write(t, ws, "hello/notes.txt", "scratch")
	write(t, ws, "hello/target/debug/hello", "binary v2")
	write(t, ws, "emacs.d/elpa/consult/consult.el", ";; installed later")
	write(t, ws, ".playground/cache/x", "cache")

	m.Now = func() time.Time { return time.UnixMilli(1761472060000) }
	bufs := map[string]BufferState{"hello/src/main.rs": {Modified: true, HasText: true, Text: "fn main() {}\n", Undo: `(nil ("x" . 1))`}}
	manual, err := m.TakeWith(ctx, Meta{Ref: ManualRef("Before refactor!"), Label: "Before refactor!", Buffers: bufs})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Ref != "refs/playground/manual/before-refactor" || manual.CreatedAt != 1761472060000 {
		t.Fatalf("manual %+v", manual)
	}
	if got, err := m.ReadMeta(manual.Ref); err != nil || got.Buffers["hello/src/main.rs"] != bufs["hello/src/main.rs"] {
		t.Fatalf("buffer state not kept with the checkpoint: %+v %v", got.Buffers, err)
	}

	items, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Label != "Before refactor!" || items[1].Ref != ref {
		t.Fatalf("list %+v", items)
	}
	if len(items[0].Files) != 0 {
		t.Fatalf("manual checkpoint should match the worktree: %v", items[0].Files)
	}
	slices.Sort(items[1].Files)
	if !reflect.DeepEqual(items[1].Files, []string{"hello/README.md", "hello/notes.txt", "hello/src/main.rs"}) {
		t.Fatalf("files %v", items[1].Files)
	}
	if items[1].Step == nil || items[1].Step.StepID != "open" {
		t.Fatalf("step %+v", items[1].Step)
	}

	// A file the learner staged that the checkpoint does not have, and an
	// unchanged file with an old mtime that the restore must not rewrite.
	write(t, ws, "hello/staged.txt", "staged")
	gitOut(t, ws, "add", "hello/staged.txt")
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(filepath.Join(ws, "emacs.d/modules.el"), old, old); err != nil {
		t.Fatal(err)
	}

	// A file moved into the empty directory, and a new empty directory.
	write(t, ws, "hello/docs/moved.md", "moved")
	_ = os.MkdirAll(filepath.Join(ws, "hello/extra"), 0o755)

	n, rmeta, err := m.Restore(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || rmeta.Spec == nil {
		t.Fatalf("restored %d meta %+v", n, rmeta)
	}
	if ents, err := os.ReadDir(filepath.Join(ws, "hello/docs")); err != nil || len(ents) != 0 {
		t.Fatalf("hello/docs must be back and empty: %v %v", ents, err)
	}
	if _, err := os.Stat(filepath.Join(ws, "hello/extra")); err == nil {
		t.Fatal("a directory made after the checkpoint survived")
	}
	if st, err := os.Stat(filepath.Join(ws, "emacs.d/modules.el")); err != nil || !st.ModTime().Equal(old) {
		t.Fatalf("unchanged file rewritten: %v %v", st.ModTime(), err)
	}
	if _, ok := read(t, ws, "hello/staged.txt"); ok {
		t.Fatal("staged file not in the checkpoint survived")
	}
	if out := gitOut(t, ws, "diff-index", "--cached", "--name-only", ref, "--"); out != "" {
		t.Fatalf("index differs from the checkpoint: %q", out)
	}
	if s, _ := read(t, ws, "hello/src/main.rs"); !strings.Contains(s, "Hello, world") {
		t.Fatalf("main.rs not restored: %q", s)
	}
	if _, ok := read(t, ws, "hello/README.md"); !ok {
		t.Fatal("deleted file not restored")
	}
	if _, ok := read(t, ws, "hello/notes.txt"); ok {
		t.Fatal("new file not removed")
	}
	if s, _ := read(t, ws, "hello/target/debug/hello"); s != "binary v2" {
		t.Fatalf("build output must be kept, got %q", s)
	}
	if _, ok := read(t, ws, "emacs.d/elpa/consult/consult.el"); !ok {
		t.Fatal("installed package removed")
	}
	if _, ok := read(t, ws, ".playground/cache/x"); !ok {
		t.Fatal(".playground removed")
	}

	// Re-taking the same ref chains to the previous commit.
	if _, err := m.Take(ctx, ref, "again", step, spec); err != nil {
		t.Fatal(err)
	}
	if parents := gitOut(t, ws, "rev-list", "--count", ref); parents != "2" {
		t.Fatalf("history length %s", parents)
	}
	if _, _, err := m.Restore(ctx, "refs/playground/missing/x"); err == nil {
		t.Fatal("missing ref must fail")
	}
	if _, _, err := m.Restore(ctx, "refs/heads/main"); err == nil {
		t.Fatal("foreign ref must fail")
	}
	if !m.Exists(ctx, ref) || m.Exists(ctx, "refs/playground/missing/x") {
		t.Fatal("Exists")
	}
}

// recRunner records invocations without running git.
type recRunner struct{ calls [][]string }

func (r *recRunner) Run(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append(append([]string{}, env...), args...))
	switch args[0] {
	case "write-tree":
		return []byte("tree1\n"), nil
	case "commit-tree":
		return []byte("commit1\n"), nil
	case "rev-parse":
		return nil, os.ErrNotExist
	}
	return nil, nil
}

func TestTakeUsesPlan(t *testing.T) {
	r := &recRunner{}
	m := &Manager{Workspace: t.TempDir(), Git: r, Now: time.Now}
	if _, err := m.Take(context.Background(), "refs/playground/l/s", "l/s", nil, nil); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range r.calls {
		got = append(got, c[len(c)-1])
	}
	last := r.calls[len(r.calls)-1]
	if !reflect.DeepEqual(last, []string{"update-ref", "refs/playground/l/s", "commit1"}) {
		t.Fatalf("update-ref call %v (all %v)", last, r.calls)
	}
	commit := r.calls[3]
	if !slices.Contains(commit, "tree1") || slices.Contains(commit, "-p") {
		t.Fatalf("commit-tree call %v", commit)
	}
}
