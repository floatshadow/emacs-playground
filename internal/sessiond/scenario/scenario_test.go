package scenario

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type entry struct {
	name    string
	body    string
	dir     bool
	symlink string
	mode    int64
}

func tarball(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if e.mode != 0 {
			h.Mode = e.mode
		}
		switch {
		case e.dir:
			h.Typeflag, h.Size, h.Mode = tar.TypeDir, 0, 0o755
		case e.symlink != "":
			h.Typeflag, h.Size, h.Linkname = tar.TypeSymlink, 0, e.symlink
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	_ = tw.Close()
	_ = zw.Close()
	return buf.Bytes()
}

func openRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestExtract(t *testing.T) {
	parent := t.TempDir()
	ws := filepath.Join(parent, "work")
	_ = os.MkdirAll(ws, 0o755)
	root := openRoot(t, ws)
	tb := tarball(t,
		entry{name: "src/", dir: true},
		entry{name: "src/main.rs", body: "fn main() {}\n"},
		entry{name: "./Cargo.toml", body: "[package]\n"},
		entry{name: "run.sh", body: "#!/bin/sh\n", mode: 0o755},
		entry{name: "link", symlink: "/etc/passwd"},
	)
	res, err := Extract(bytes.NewReader(tb), root, "hello", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 3 || res.Skipped != 0 {
		t.Fatalf("result %+v", res)
	}
	if readFile(t, filepath.Join(ws, "hello/src/main.rs")) != "fn main() {}\n" {
		t.Fatal("main.rs")
	}
	if st, _ := os.Stat(filepath.Join(ws, "hello/run.sh")); st.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", st.Mode())
	}
	if _, err := os.Lstat(filepath.Join(ws, "hello/link")); err == nil {
		t.Fatal("symlinks must not be extracted")
	}

	// Learner edits survive a re-extraction without reset.
	_ = os.WriteFile(filepath.Join(ws, "hello/src/main.rs"), []byte("edited"), 0o644)
	res, err = Extract(bytes.NewReader(tb), root, "hello", false)
	if err != nil || res.Skipped != 3 || res.Written != 0 {
		t.Fatalf("no-reset %+v %v", res, err)
	}
	if readFile(t, filepath.Join(ws, "hello/src/main.rs")) != "edited" {
		t.Fatal("learner file overwritten")
	}
	// Reset overwrites.
	if _, err := Extract(bytes.NewReader(tb), root, "hello", true); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(ws, "hello/src/main.rs")) != "fn main() {}\n" {
		t.Fatal("reset did not overwrite")
	}

	// A learner symlink pointing outside the workspace is replaced, not
	// written through.
	outside := filepath.Join(parent, "outside.txt")
	_ = os.WriteFile(outside, []byte("precious"), 0o644)
	_ = os.Remove(filepath.Join(ws, "hello/Cargo.toml"))
	_ = os.Symlink(outside, filepath.Join(ws, "hello/Cargo.toml"))
	if _, err := Extract(bytes.NewReader(tb), root, "hello", true); err != nil {
		t.Fatal(err)
	}
	if readFile(t, outside) != "precious" {
		t.Fatal("wrote through a symlink")
	}
	// A symlinked directory pointing outside makes extraction fail rather
	// than escape.
	_ = os.RemoveAll(filepath.Join(ws, "hello/src"))
	_ = os.Symlink(parent, filepath.Join(ws, "hello/src"))
	if _, err := Extract(bytes.NewReader(tb), root, "hello", true); err == nil {
		t.Fatal("expected an error writing through an escaping directory link")
	}
	if _, err := os.Stat(filepath.Join(parent, "main.rs")); err == nil {
		t.Fatal("escaped the workspace")
	}

	// Unsafe names are rejected.
	for _, bad := range []string{"../evil", "/etc/evil", "a/../../evil"} {
		_, err := Extract(bytes.NewReader(tarball(t, entry{name: bad, body: "x"})), root, "x", true)
		if err == nil || !strings.Contains(err.Error(), "unsafe") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// Garbage is an error.
	if _, err := Extract(strings.NewReader("not zstd"), root, "x", false); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestModules(t *testing.T) {
	mods, ok := ParseModulesEl("(setq playground-modules '(base agent\n  editing files))\n")
	if !ok || strings.Join(mods, " ") != "base agent editing files" {
		t.Fatalf("parse %v %v", mods, ok)
	}
	again, ok := ParseModulesEl(RenderModulesEl(mods))
	if !ok || strings.Join(again, " ") != "base agent editing files" {
		t.Fatalf("round trip %v", again)
	}
	if got := Union([]string{"base", "agent", "git"}, []string{"base", "agent", "editing", "files"}); strings.Join(got, " ") != "base agent git editing files" {
		t.Fatalf("union %v", got)
	}
	for _, bad := range [][]string{{"Base"}, {"a b"}, {"x", "x"}, {"../x"}} {
		if ValidateModules(bad) == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	m := &Modules{Path: filepath.Join(t.TempDir(), "emacs.d", "modules.el")}
	if strings.Join(m.Current(), " ") != "base agent" {
		t.Fatal("default modules")
	}
	if err := m.Write([]string{"base", "agent", "lsp"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.Current(), " ") != "base agent lsp" {
		t.Fatal("written modules")
	}
}

func TestExtractResetPrunes(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "work")
	_ = os.MkdirAll(ws, 0o755)
	root := openRoot(t, ws)
	tb := tarball(t,
		entry{name: "inbox/brief.md", body: "brief\n"},
		entry{name: "inbox/scratch.tmp", body: "tmp\n"},
		entry{name: "hello/src/main.rs", body: "fn main() {}\n"},
	)
	if _, err := Extract(bytes.NewReader(tb), root, "projects", true); err != nil {
		t.Fatal(err)
	}
	// A learner moves, deletes and creates files, and builds.
	p := func(s string) string { return filepath.Join(ws, "projects", s) }
	_ = os.MkdirAll(p("inbox/docs"), 0o755)
	_ = os.Rename(p("inbox/brief.md"), p("inbox/docs/brief.md"))
	_ = os.Remove(p("inbox/scratch.tmp"))
	_ = os.WriteFile(p("hello/CHANGES.md"), []byte("new"), 0o644)
	_ = os.MkdirAll(p("hello/target/debug"), 0o755)
	_ = os.WriteFile(p("hello/target/debug/hello"), []byte("bin"), 0o755)
	_ = os.WriteFile(filepath.Join(ws, "outside.txt"), []byte("keep"), 0o644)

	// Without reset nothing is removed.
	res, err := Extract(bytes.NewReader(tb), root, "projects", false)
	if err != nil || res.Removed != 0 {
		t.Fatalf("no reset: %+v %v", res, err)
	}
	if _, err := os.Stat(p("hello/CHANGES.md")); err != nil {
		t.Fatal("a re-extraction without reset removed a learner file")
	}
	// With reset the tree matches the fixture again, build output kept.
	res, err = Extract(bytes.NewReader(tb), root, "projects", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 2 {
		t.Fatalf("removed %d, want 2 (inbox/docs and hello/CHANGES.md): %+v", res.Removed, res)
	}
	for _, gone := range []string{"inbox/docs", "hello/CHANGES.md"} {
		if _, err := os.Stat(p(gone)); err == nil {
			t.Fatalf("%s survived the reset", gone)
		}
	}
	for _, kept := range []string{"inbox/brief.md", "inbox/scratch.tmp", "hello/target/debug/hello"} {
		if _, err := os.Stat(p(kept)); err != nil {
			t.Fatalf("%s missing after the reset", kept)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, "outside.txt")); err != nil {
		t.Fatal("a reset touched files outside its root")
	}
}

func TestExtractReplacesTypes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after []entry
	}{
		{"directory to file", []entry{{name: "a/", dir: true}, {name: "a/child", body: "old"}}, []entry{{name: "a", body: "new"}}},
		{"file to directory", []entry{{name: "a", body: "old"}}, []entry{{name: "a/", dir: true}, {name: "a/child", body: "new"}}},
		{"file to implicit parent", []entry{{name: "a", body: "old"}}, []entry{{name: "a/child", body: "new"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := openRoot(t, t.TempDir())
			if _, err := Extract(bytes.NewReader(tarball(t, tc.before...)), root, "lesson", true); err != nil {
				t.Fatal(err)
			}
			if _, err := Extract(bytes.NewReader(tarball(t, tc.after...)), root, "lesson", false); err == nil {
				t.Fatal("type conflict accepted without overwrite")
			}
			if _, err := Extract(bytes.NewReader(tarball(t, tc.after...)), root, "lesson", true); err != nil {
				t.Fatal(err)
			}
			for _, e := range tc.after {
				if !e.dir {
					b, err := root.ReadFile("lesson/" + e.name)
					if err != nil || string(b) != e.body {
						t.Fatalf("%s: %q %v", e.name, b, err)
					}
				}
			}
		})
	}
}
