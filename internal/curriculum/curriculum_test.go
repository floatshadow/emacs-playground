package curriculum

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zsy/emacs-playground/internal/protocol"
)

func TestLoadGood(t *testing.T) {
	src, diags := Load("testdata/good")
	for _, d := range diags {
		t.Log(d)
	}
	if diags.HasErrors() {
		t.Fatalf("unexpected errors")
	}
	if diags.Count(Warning) != 0 {
		t.Fatalf("unexpected warnings")
	}
	if len(src.Lessons) != 2 {
		t.Fatalf("lessons: %d", len(src.Lessons))
	}
	l := src.Lesson("00-orientation/01-first")
	if l == nil || l.Front["en"].Title != "First lesson" || l.Front["zh-CN"].Title != "第一课" {
		t.Fatalf("front: %+v", l)
	}
	if !strings.HasPrefix(l.Markdown["en"], "# Opening files") {
		t.Fatalf("markdown: %q", l.Markdown["en"])
	}
	if got := l.Scenario.Steps[0].Conditions[1].ID; got != "open/2" {
		t.Fatalf("condition id %q", got)
	}
	if got := l.Scenario.Steps[0].Stuck[0].ID; got != "open/stuck/1" {
		t.Fatalf("stuck id %q", got)
	}
	if got := l.Scenario.Steps[2].Conditions[0].ID; got != "layout-ok" {
		t.Fatalf("explicit id %q", got)
	}
	if len(l.Assets) != 1 || l.Assets[0] != "screen.svg" {
		t.Fatalf("assets %v", l.Assets)
	}
	if strings.Join(src.Locales, ",") != "zh-CN" {
		t.Fatalf("locales %v", src.Locales)
	}
	if len(src.Tables["en"].Exits) != 3 || len(src.Tables["zh-CN"].Exits) != 1 || len(src.Tables["zh-CN"].Habits) != 1 {
		t.Fatalf("tables %+v", src.Tables)
	}
	if strings.Join(src.Order, " ") != "00-orientation/01-first 00-orientation/02-second 01-moving-and-editing/01-motion projects/p1-hello-cargo" {
		t.Fatalf("order %v", src.Order)
	}
}

// copyTree copies testdata/good into a temporary directory for mutation.
func copyTree(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir("testdata/good", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("testdata/good", p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func edit(t *testing.T, dir, file, old, new string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(file))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("%s does not contain %q", file, old)
	}
	if err := os.WriteFile(p, bytes.Replace(data, []byte(old), []byte(new), 1), 0o644); err != nil {
		t.Fatal(err)
	}
}

const first = "00-orientation/01-first/scenario.yaml"

func TestValidateErrors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{"missing ready lesson", func(t *testing.T, dir string) {
			edit(t, dir, "index.yaml", "01-motion, n: \"1.1\", title: Motion, minutes: 15, status: planned", "01-motion, n: \"1.1\", title: Motion, minutes: 15, status: ready")
		}, "has no directory"},
		{"bad demo", func(t *testing.T, dir string) {
			edit(t, dir, first, `demo: 'C-x C-f "src/ma" TAB ~400 RET'`, `demo: 'C-x C-f "src/ma TAB'`)
		}, "unterminated string"},
		{"bad demo key", func(t *testing.T, dir string) {
			edit(t, dir, first, `demo: 'C-x C-f "src/ma" TAB ~400 RET'`, `demo: 'Q-x'`)
		}, "unknown modifier"},
		{"missing demo", func(t *testing.T, dir string) {
			edit(t, dir, "00-orientation/02-second/scenario.yaml", `demo: "M-x ~300 C-g"`, ``)
		}, "steps[0].demo: required"},
		{"two kinds", func(t *testing.T, dir string) {
			edit(t, dir, first, "event: { command: find-file, min: 1 }", "event: { command: find-file, min: 1 }\n        elisp: \"(foo)\"")
		}, "exactly one condition kind"},
		{"unknown field", func(t *testing.T, dir string) {
			edit(t, dir, first, "minutes: 12", "minutes: 12\nminuets: 3")
		}, "unknown field"},
		{"bad kbd", func(t *testing.T, dir string) {
			edit(t, dir, "00-orientation/01-first/lesson.md", ":kbd[C-x C-f]", ":kbd[Ctrl+X]")
		}, "not a key sequence"},
		{"bad state path", func(t *testing.T, dir string) {
			edit(t, dir, first, "state: { narrowed: false", "state: { narowed: false")
		}, "does not start with a state field"},
		{"bad exits path", func(t *testing.T, dir string) {
			edit(t, dir, "tables/exits.yaml", `"minibuffer.kind"`, `"minibufer.kind"`)
		}, "tables/exits.yaml: [0].when"},
		{"bad predicate op", func(t *testing.T, dir string) {
			edit(t, dir, "tables/exits.yaml", "{ in: [help-mode, dired-mode] }", "{ among: [help-mode] }")
		}, "unknown operator"},
		{"bad layout", func(t *testing.T, dir string) {
			edit(t, dir, first, `layout: "(vsplit main.rs README.md)"`, `layout: "(vsplit main.rs"`)
		}, "steps[2].conditions[0].layout"},
		{"missing fixtures", func(t *testing.T, dir string) {
			edit(t, dir, first, "workspace: fixtures/hello", "workspace: fixtures/nope")
		}, "directory fixtures/nope not found"},
		{"escaping path", func(t *testing.T, dir string) {
			edit(t, dir, first, "path: hello/src/main.rs", "path: ../etc/passwd")
		}, "clean path without .."},
		{"scenario id", func(t *testing.T, dir string) {
			edit(t, dir, first, "id: 00-orientation/01-first", "id: 00-orientation/first")
		}, "must equal the lesson directory"},
		{"unknown module", func(t *testing.T, dir string) {
			edit(t, dir, first, "modules: [base, agent]", "modules: [base, agent, evil]")
		}, "unknown module \"evil\""},
		{"elisp parens", func(t *testing.T, dir string) {
			edit(t, dir, first, `elisp: '(equal (buffer-file-name) "/work/hello/src/main.rs")'`, `elisp: '(equal (buffer-file-name) "/work/hello/src/main.rs"'`)
		}, "unclosed parenthesis"},
		{"bad habit key", func(t *testing.T, dir string) {
			edit(t, dir, "tables/habits.yaml", "key: C-z", "key: C-x C-z")
		}, "habits are single keys"},
		{"file cond two predicates", func(t *testing.T, dir string) {
			edit(t, dir, first, `contains: "edited" }`, `contains: "edited", regex: "x" }`)
		}, "exactly one of exists"},
		{"duplicate step", func(t *testing.T, dir string) {
			edit(t, dir, first, "- id: layout", "- id: save")
		}, "duplicate step id"},
		{"bad image", func(t *testing.T, dir string) {
			edit(t, dir, "00-orientation/01-first/lesson.md", "assets/screen.svg", "assets/missing.svg")
		}, "file not found"},
		{"front matter", func(t *testing.T, dir string) {
			edit(t, dir, "00-orientation/02-second/lesson.md", "title: Second lesson", "summary: no title")
		}, "title is required"},
		{"stuck without when", func(t *testing.T, dir string) {
			edit(t, dir, first, "when: { minibuffer_seconds: 30 }", "when: {}")
		}, "needs at least one of"},
		{"symlink fixture", func(t *testing.T, dir string) {
			if err := os.Symlink("/etc/passwd", filepath.Join(dir, "00-orientation/01-first/fixtures/hello/passwd")); err != nil {
				t.Skip(err)
			}
		}, "symlinks are not allowed"},
		{"part status", func(t *testing.T, dir string) {
			edit(t, dir, "index.yaml", "status: planned\n    suggestedAfter: 0", "status: ready\n    suggestedAfter: 0")
		}, "not every lesson is ready"},
		{"lesson id prefix", func(t *testing.T, dir string) {
			edit(t, dir, "index.yaml", "{ id: 01-moving-and-editing/01-motion", "{ id: 02-other/01-motion")
		}, "must start with the part directory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyTree(t)
			c.mutate(t, dir)
			_, diags := Load(dir)
			var all []string
			found := false
			for _, d := range diags {
				all = append(all, d.String())
				if d.Severity == Error && strings.Contains(d.String(), c.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want an error containing %q, got:\n%s", c.want, strings.Join(all, "\n"))
			}
		})
	}
}

func TestValidateWarnings(t *testing.T) {
	dir := copyTree(t)
	edit(t, dir, "00-orientation/01-first/lesson.md", ":jargon[buffer]", ":jargon[frame]")
	if err := os.MkdirAll(filepath.Join(dir, "00-orientation/03-unlisted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "00-orientation/03-unlisted/scenario.yaml"), []byte("id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, diags := Load(dir)
	if diags.HasErrors() {
		t.Fatalf("errors: %v", diags)
	}
	var s []string
	for _, d := range diags {
		s = append(s, d.String())
	}
	joined := strings.Join(s, "\n")
	for _, want := range []string{"term is not in tables/glossary.yaml", "03-unlisted is not listed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing warning %q in\n%s", want, joined)
		}
	}
}

func TestBuild(t *testing.T) {
	src, diags := Load("testdata/good")
	if diags.HasErrors() {
		t.Fatal(diags.Err())
	}
	out := t.TempDir()
	at := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if err := Build(src, out, BuildOptions{Version: "test", BuiltAt: at}); err != nil {
		t.Fatal(err)
	}
	var idx protocol.CurriculumIndex
	readJSON(t, filepath.Join(out, "curriculum.json"), &idx)
	if idx.Format != protocol.CurriculumFormat || idx.Version != "test" || idx.BuiltAt != at.UnixMilli() || idx.Locale != "en" {
		t.Fatalf("index header %+v", idx)
	}
	l0 := idx.Parts[0].Lessons[0]
	if l0.Title != "Opening files" || l0.Minutes != 12 || l0.Steps != 3 || l0.Mode != protocol.LessonTerminal || l0.VSCode != "Ctrl+P, Ctrl+O" || l0.Status != protocol.Ready {
		t.Fatalf("lesson ref from scenario: %+v", l0)
	}
	if l := idx.Parts[1].Lessons[0]; l.Status != protocol.Planned || l.Steps != 0 {
		t.Fatalf("planned ref %+v", l)
	}
	if len(idx.Tables.Exits) != 3 || idx.Tables.Gloss["find-file"] == "" || len(idx.Tables.Cheats["common"]) != 1 {
		t.Fatalf("tables %+v", idx.Tables)
	}
	var zh protocol.CurriculumIndex
	readJSON(t, filepath.Join(out, "curriculum.zh-CN.json"), &zh)
	if zh.Parts[0].Lessons[0].Title != "打开文件" || len(zh.Tables.Exits) != 1 || zh.Locale != "zh-CN" {
		t.Fatalf("zh index %+v", zh.Parts[0].Lessons[0])
	}

	var b protocol.LessonBundle
	readJSON(t, filepath.Join(out, "lessons/00-orientation/01-first/bundle.json"), &b)
	if b.ID != "00-orientation/01-first" || b.Title != "Opening files" || b.Locale != "en" || !strings.HasPrefix(b.Markdown, "# Opening files") || b.Front.Summary == "" {
		t.Fatalf("bundle %+v", b)
	}
	if b.Scenario.Steps[0].Conditions[0].ID != "open/1" || len(b.Assets) != 1 {
		t.Fatalf("bundle scenario %+v", b.Scenario.Steps[0])
	}
	var bz protocol.LessonBundle
	readJSON(t, filepath.Join(out, "lessons/00-orientation/01-first/bundle.zh-CN.json"), &bz)
	if bz.Locale != "zh-CN" || bz.Title != "打开文件" || !strings.Contains(bz.Markdown, "打开文件") {
		t.Fatalf("zh bundle %+v", bz)
	}
	if _, err := os.Stat(filepath.Join(out, "lessons/00-orientation/02-second/bundle.zh-CN.json")); err == nil {
		t.Fatal("no zh bundle expected for an untranslated lesson")
	}
	var sc protocol.Scenario
	readJSON(t, filepath.Join(out, "lessons/00-orientation/01-first/scenario.json"), &sc)
	if sc.ID != "00-orientation/01-first" || sc.Steps[1].Conditions[1].ID != "save/2" {
		t.Fatalf("scenario %+v", sc)
	}
	if _, err := os.Stat(filepath.Join(out, "lessons/00-orientation/01-first/assets/screen.svg")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "fixtures/00-orientation/02-second.tar.zst")); err == nil {
		t.Fatal("lesson without setup.workspace must have no tarball")
	}

	// Fixture tarball: content and reproducibility.
	tarball := filepath.Join(out, "fixtures/00-orientation/01-first.tar.zst")
	data, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	files, err := ReadFixtures(bytes.NewReader(data), 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "README.md,src/main.rs" || !strings.Contains(string(files[1].Data), "Hello, world") {
		t.Fatalf("fixtures %v", names)
	}
	out2 := t.TempDir()
	if err := Build(src, out2, BuildOptions{Version: "test", BuiltAt: at}); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(filepath.Join(out2, "fixtures/00-orientation/01-first.tar.zst"))
	if sha256.Sum256(data) != sha256.Sum256(data2) {
		t.Fatal("fixture tarballs are not reproducible")
	}
}

func TestReadFixturesRejectsUnsafe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := WriteFixtures(&b, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFixtures(bytes.NewReader(b.Bytes()), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFixtures(bytes.NewReader(b.Bytes()), 1); err == nil {
		t.Fatal("size limit not enforced")
	}
}

func TestSplitFrontMatter(t *testing.T) {
	f, body, err := SplitFrontMatter([]byte("---\ntitle: T\nminutes: 3\n---\n\nBody\n"))
	if err != nil || f.Title != "T" || f.Minutes != 3 || body != "Body\n" {
		t.Fatalf("%+v %q %v", f, body, err)
	}
	if _, _, err := SplitFrontMatter([]byte("no front\n")); err == nil {
		t.Fatal("want error")
	}
	if _, _, err := SplitFrontMatter([]byte("---\ntitle: T\nbogus: 1\n---\n")); err == nil {
		t.Fatal("unknown front matter keys must fail")
	}
}

func TestCheckElisp(t *testing.T) {
	for _, ok := range []string{`(foo)`, `(equal ?\( ?a)`, `(string= "a)b" x) ; comment )`, `(null? x)`, `[1 2 (3)]`} {
		if err := checkElisp(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{`(foo`, `(foo))`, `"abc`, ``} {
		if err := checkElisp(bad); err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}
