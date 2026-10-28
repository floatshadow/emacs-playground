package preview

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func mk(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	parent := t.TempDir()
	ws := filepath.Join(parent, "work")
	mk(t, parent, "secret.txt", "outside")
	mk(t, ws, "site/index.html", "<script>alert(1)</script>")
	mk(t, ws, "site/logo.svg", "<svg xmlns='http://www.w3.org/2000/svg'/>")
	mk(t, ws, "docs/report.pdf", "%PDF-1.7 fake")
	mk(t, ws, "notes/README.md", "# Notes\n")
	mk(t, ws, "noext", "<!DOCTYPE html><html><body>hi</body></html>")
	mk(t, ws, "hello/src/main.rs", "fn main() {}\n")
	mk(t, ws, ".playground/ssh/id_ed25519", "PRIVATE")
	if err := os.Symlink(filepath.Join(parent, "secret.txt"), filepath.Join(ws, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../..", filepath.Join(ws, "notes", "up")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("hello/src/main.rs", filepath.Join(ws, "inside.rs")); err != nil {
		t.Fatal(err)
	}
	s, err := New(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	mux := http.NewServeMux()
	mux.Handle("GET /preview/{path...}", s)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, ws
}

func get(t *testing.T, srv *httptest.Server, path string, hdr ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func TestPreviewPathSafety(t *testing.T) {
	srv, _ := newServer(t)
	cases := []struct {
		path string
		code int
	}{
		{"/preview/hello/src/main.rs", 200},
		{"/preview/inside.rs", 200},           // symlink inside the root
		{"/preview/escape.txt", 404},          // symlink out of the root
		{"/preview/notes/up/secret.txt", 404}, // symlinked directory out of the root
		{"/preview/%2e%2e/secret.txt", 404},
		{"/preview/hello/%2e%2e/%2e%2e/secret.txt", 404},
		{"/preview/hello/..%2f..%2fsecret.txt", 404},
		{"/preview/.playground/ssh/id_ed25519", 404},
		{"/preview/.playground/", 404},
		{"/preview/hello//src/main.rs", 200}, // the mux redirects to the clean path
		{"/preview/hello\\src", 404},
		{"/preview/missing.txt", 404},
		{"/preview/hello/src/main.rs/", 404},
	}
	for _, c := range cases {
		resp, body := get(t, srv, c.path)
		if resp.StatusCode != c.code {
			t.Errorf("%s: %d want %d (%s)", c.path, resp.StatusCode, c.code, body)
		}
		if strings.Contains(body, "outside") || strings.Contains(body, "PRIVATE") {
			t.Errorf("%s leaked content", c.path)
		}
		if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: headers %v", c.path, resp.Header)
		}
	}
}

func TestPreviewHeadersAndListing(t *testing.T) {
	srv, _ := newServer(t)
	for path, want := range map[string]struct {
		ctype string
		csp   bool
	}{
		"/preview/site/index.html":   {"text/html", true},
		"/preview/site/logo.svg":     {"image/svg+xml", true},
		"/preview/noext":             {"text/html", true},
		"/preview/docs/report.pdf":   {"application/pdf", false},
		"/preview/notes/README.md":   {"text/markdown", false},
		"/preview/hello/src/main.rs": {"text/plain", false},
	} {
		resp, _ := get(t, srv, path)
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), want.ctype) {
			t.Errorf("%s: content type %q", path, resp.Header.Get("Content-Type"))
		}
		if got := resp.Header.Get("Content-Security-Policy") == SandboxCSP; got != want.csp {
			t.Errorf("%s: csp %q", path, resp.Header.Get("Content-Security-Policy"))
		}
	}
	// Range requests work (PDF.js).
	resp, body := get(t, srv, "/preview/docs/report.pdf", "Range", "bytes=0-3")
	if resp.StatusCode != http.StatusPartialContent || body != "%PDF" {
		t.Fatalf("range: %d %q", resp.StatusCode, body)
	}

	// Root listing hides .playground; directories first.
	resp, body = get(t, srv, "/preview/")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("listing: %d %s", resp.StatusCode, body)
	}
	var l protocol.PreviewListing
	if err := json.Unmarshal([]byte(body), &l); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range l.Entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "docs,hello,inside.rs,notes,site,escape.txt,noext" &&
		strings.Join(names, ",") != "docs,hello,notes,site,escape.txt,inside.rs,noext" {
		t.Fatalf("root entries %v", names)
	}
	for _, e := range l.Entries {
		if e.Name == "hello" && (!e.Dir || e.Mtime == 0) {
			t.Fatalf("hello entry %+v", e)
		}
		if e.Name == "noext" && (e.Dir || e.Size == 0) {
			t.Fatalf("noext entry %+v", e)
		}
	}
	resp, body = get(t, srv, "/preview/hello/src/")
	_ = json.Unmarshal([]byte(body), &l)
	if resp.StatusCode != 200 || l.Path != "hello/src" || len(l.Entries) != 1 || l.Entries[0].Name != "main.rs" || l.Entries[0].Size != 13 {
		t.Fatalf("subdir listing %d %s", resp.StatusCode, body)
	}
	// A directory without the trailing slash also lists.
	resp, _ = get(t, srv, "/preview/hello")
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("dir without slash: %d", resp.StatusCode)
	}
}

func TestHandlerRejectsRawTraversal(t *testing.T) {
	// Requests that bypass the mux's path cleaning (a proxy forwarding a raw
	// path) still cannot leave the root.
	_, ws := newServer(t)
	s, err := New(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, p := range []string{"../secret.txt", "hello/../../secret.txt", "/../secret.txt", "..", "hello/../.playground/ssh/id_ed25519"} {
		r := httptest.NewRequest("GET", "/preview/x", nil)
		r.SetPathValue("path", p)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "outside") || strings.Contains(rec.Body.String(), "PRIVATE") {
			t.Errorf("%q: %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{
		"":              ".",
		"/":             ".",
		"a/b.txt":       "a/b.txt",
		"a/":            "a",
		"a/../b":        "",
		"./a":           "",
		"/etc/passwd":   "etc/passwd", // leading slashes are trimmed: still inside the root
		".playground/x": "",
	} {
		got, _, ok := cleanPath(in)
		if want == "" && ok {
			t.Errorf("cleanPath(%q) accepted as %q", in, got)
		}
		if want != "" && (!ok || got != want) {
			t.Errorf("cleanPath(%q) = %q %v want %q", in, got, ok, want)
		}
	}
}

type collector struct {
	mu  sync.Mutex
	evs []string
}

func (c *collector) emit(ev *protocol.FileEvent) {
	c.mu.Lock()
	c.evs = append(c.evs, ev.Path+":"+string(ev.Event))
	c.mu.Unlock()
}

func (c *collector) reset() {
	c.mu.Lock()
	c.evs = nil
	c.mu.Unlock()
}

func (c *collector) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]string(nil), c.evs...)
	sort.Strings(out)
	return out
}

func TestWatcher(t *testing.T) {
	ws := t.TempDir()
	mk(t, ws, "hello/src/main.rs", "fn main() {}\n")
	mk(t, ws, "hello/target/debug/x", "")
	c := &collector{}
	w, err := NewWatcher(ws, c.emit, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.SetWindow(80 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	defer w.Close()

	settle := func() { time.Sleep(300 * time.Millisecond) }

	// Several writes to one file coalesce into one modified event.
	for i := 0; i < 5; i++ {
		mk(t, ws, "hello/src/main.rs", strings.Repeat("x", i))
	}
	settle()
	if got := c.get(); strings.Join(got, " ") != "hello/src/main.rs:modified" {
		t.Fatalf("coalesced writes: %v", got)
	}
	c.reset()

	// Create then write is created; create then delete is nothing.
	mk(t, ws, "hello/new.txt", "a")
	mk(t, ws, "hello/new.txt", "ab")
	mk(t, ws, "hello/tmp.txt", "a")
	_ = os.Remove(filepath.Join(ws, "hello/tmp.txt"))
	// Ignored directories and lock files are silent.
	mk(t, ws, "hello/target/debug/y", "")
	mk(t, ws, ".git/HEAD", "ref")
	mk(t, ws, ".playground/state.json", "{}")
	_ = os.Symlink("learner@host.1", filepath.Join(ws, "hello", ".#main.rs"))
	settle()
	if got := c.get(); strings.Join(got, " ") != "hello/new.txt:created" {
		t.Fatalf("create/delete: %v", got)
	}
	c.reset()

	// A new directory tree is watched and its files announced.
	mk(t, ws, "docs/report.typ", "= Report")
	settle()
	mk(t, ws, "docs/report.pdf", "%PDF")
	settle()
	got := c.get()
	if strings.Join(got, " ") != "docs/report.pdf:created docs/report.typ:created" {
		t.Fatalf("new dir: %v", got)
	}
	c.reset()
	_ = os.Remove(filepath.Join(ws, "docs/report.pdf"))
	settle()
	if got := c.get(); strings.Join(got, " ") != "docs/report.pdf:deleted" {
		t.Fatalf("delete: %v", got)
	}
	c.reset()
	// Rename: old path deleted, new path created.
	_ = os.Rename(filepath.Join(ws, "docs/report.typ"), filepath.Join(ws, "docs/final.typ"))
	settle()
	if got := c.get(); strings.Join(got, " ") != "docs/final.typ:created docs/report.typ:deleted" {
		t.Fatalf("rename: %v", got)
	}
}

func TestMerge(t *testing.T) {
	C, M, D := protocol.FileCreated, protocol.FileModified, protocol.FileDeleted
	cases := []struct {
		seq  []protocol.FileEventKind
		want protocol.FileEventKind
	}{
		{[]protocol.FileEventKind{C, M}, C},
		{[]protocol.FileEventKind{C, D}, ""},
		{[]protocol.FileEventKind{C, D, C}, C},
		{[]protocol.FileEventKind{M, D}, D},
		{[]protocol.FileEventKind{D, C}, M},
		{[]protocol.FileEventKind{M, M, M}, M},
	}
	for _, c := range cases {
		cur := c.seq[0]
		for _, k := range c.seq[1:] {
			cur = merge(cur, k, false)
		}
		if cur != c.want {
			t.Errorf("%v → %q want %q", c.seq, cur, c.want)
		}
	}
}

func TestWatcherNestedGit(t *testing.T) {
	ws := t.TempDir()
	mk(t, ws, "projects/hello/.git/HEAD", "ref: refs/heads/main\n")
	mk(t, ws, ".git/HEAD", "ref: refs/heads/main\n")
	c := &collector{}
	w, err := NewWatcher(ws, c.emit, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.SetWindow(50 * time.Millisecond)
	var mu sync.Mutex
	gits := 0
	w.OnGit = func() { mu.Lock(); gits++; mu.Unlock() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	defer w.Close()
	count := func() int { mu.Lock(); defer mu.Unlock(); return gits }

	// A commit rewrites files at the top of the nested .git: one OnGit, no
	// file events.
	mk(t, ws, "projects/hello/.git/index", "x")
	mk(t, ws, "projects/hello/.git/COMMIT_EDITMSG", "msg")
	time.Sleep(250 * time.Millisecond)
	if count() != 1 || len(c.get()) != 0 {
		t.Fatalf("commit: %d OnGit, events %v", count(), c.get())
	}
	// The workspace's own .git stays ignored.
	mk(t, ws, ".git/index", "x")
	time.Sleep(250 * time.Millisecond)
	if count() != 1 {
		t.Fatalf("workspace .git: %d", count())
	}
	// git init in a new directory is noticed, and then its commits.
	mk(t, ws, "other/README", "r")
	time.Sleep(250 * time.Millisecond)
	if err := os.Mkdir(filepath.Join(ws, "other/.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if count() != 2 {
		t.Fatalf("git init: %d", count())
	}
	mk(t, ws, "other/.git/index", "x")
	time.Sleep(250 * time.Millisecond)
	if count() != 3 {
		t.Fatalf("commit in the new repository: %d", count())
	}
	for _, e := range c.get() {
		if strings.Contains(e, ".git") {
			t.Fatalf("file event for .git: %v", c.get())
		}
	}
}
