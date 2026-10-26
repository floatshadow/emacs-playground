package curriculum

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	source "github.com/zsy/emacs-playground/internal/curriculum"
	"github.com/zsy/emacs-playground/internal/protocol"
)

func buildBundle(t *testing.T) string {
	t.Helper()
	src, diags := source.Load("../../curriculum/testdata/good")
	if diags.HasErrors() {
		t.Fatal(diags.Err())
	}
	out := t.TempDir()
	if err := source.Build(src, out, source.BuildOptions{Version: "test", BuiltAt: time.Unix(1_800_000_000, 0)}); err != nil {
		t.Fatal(err)
	}
	return out
}

func newServer(t *testing.T, dir string) (*Server, *http.ServeMux) {
	t.Helper()
	s, err := Open(Options{Dir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	id := func(h http.Handler) http.Handler { return h }
	s.Routes(mux, id, id)
	return s, mux
}

func get(mux http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func TestServeBundle(t *testing.T) {
	dir := buildBundle(t)
	s, mux := newServer(t, dir)
	if ok, v := s.Loaded(); !ok || v != "test" {
		t.Fatalf("loaded %v %q", ok, v)
	}
	if steps, ok := s.StepIDs("00-orientation/01-first"); !ok || strings.Join(steps, ",") != "open,save,layout" {
		t.Fatalf("steps %v", steps)
	}

	rec := get(mux, "/api/v1/curriculum")
	var idx protocol.CurriculumIndex
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &idx) != nil || idx.Locale != "en" || rec.Header().Get("ETag") == "" {
		t.Fatalf("index %d %s", rec.Code, rec.Header())
	}
	if rec2 := get(mux, "/api/v1/curriculum", "If-None-Match", rec.Header().Get("ETag")); rec2.Code != 304 {
		t.Fatalf("conditional index: %d", rec2.Code)
	}
	for _, c := range []struct{ hdr, q, want string }{
		{"zh-CN,zh;q=0.9,en;q=0.5", "", "zh-CN"},
		{"zh", "", "zh-CN"},
		{"fr, en;q=0.5", "", "en"},
		{"", "zh-CN", "zh-CN"},
		{"zh-CN", "en", "en"},
		{"", "de", "en"},
	} {
		path := "/api/v1/curriculum"
		if c.q != "" {
			path += "?locale=" + c.q
		}
		rec := get(mux, path, "Accept-Language", c.hdr)
		var idx protocol.CurriculumIndex
		_ = json.Unmarshal(rec.Body.Bytes(), &idx)
		if idx.Locale != c.want || rec.Header().Get("Content-Language") != c.want {
			t.Errorf("Accept-Language %q locale %q: got %q", c.hdr, c.q, idx.Locale)
		}
	}

	rec = get(mux, "/api/v1/curriculum/lessons/00-orientation/01-first")
	var b protocol.LessonBundle
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &b) != nil || b.ID != "00-orientation/01-first" || len(b.Scenario.Steps) != 3 {
		t.Fatalf("lesson %d %s", rec.Code, rec.Body)
	}
	rec = get(mux, "/api/v1/curriculum/lessons/00-orientation/01-first", "Accept-Language", "zh-CN")
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if b.Locale != "zh-CN" {
		t.Fatalf("zh lesson %q", b.Locale)
	}
	// Untranslated lesson falls back to the default bundle.
	rec = get(mux, "/api/v1/curriculum/lessons/00-orientation/02-second", "Accept-Language", "zh-CN")
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != 200 || b.Locale != "en" {
		t.Fatalf("fallback %d %q", rec.Code, b.Locale)
	}
	for _, p := range []string{
		"/api/v1/curriculum/lessons/01-moving-and-editing/01-motion", // planned
		"/api/v1/curriculum/lessons/00-orientation",
		"/api/v1/curriculum/lessons/00-orientation/01-first/extra",
	} {
		if rec := get(mux, p); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}

	rec = get(mux, "/api/v1/curriculum/assets/00-orientation/01-first/screen.svg")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<svg") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("asset %d %s", rec.Code, rec.Header())
	}
	// ServeMux redirects paths with ".." to their clean form.
	for _, p := range []string{"/api/v1/curriculum/lessons/../../etc/passwd", "/api/v1/curriculum/assets/00-orientation/01-first/../bundle.json"} {
		if rec := get(mux, p); rec.Code == 200 {
			t.Errorf("%s served", p)
		}
	}
	for _, p := range []string{
		"/api/v1/curriculum/assets/00-orientation/01-first/missing.png",
		"/api/v1/curriculum/assets/00-orientation/screen.svg",
	} {
		if rec := get(mux, p); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}

	rec = get(mux, "/internal/curriculum/scenarios/00-orientation/01-first")
	var sc protocol.Scenario
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &sc) != nil || sc.Steps[0].Conditions[0].ID != "open/1" {
		t.Fatalf("scenario %d", rec.Code)
	}
	rec = get(mux, "/internal/curriculum/fixtures/00-orientation/01-first")
	etag := rec.Header().Get("ETag")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != protocol.FixturesContentType || etag == "" {
		t.Fatalf("fixtures %d %s", rec.Code, rec.Header())
	}
	files, err := source.ReadFixtures(rec.Body, 0)
	if err != nil || len(files) != 2 {
		t.Fatalf("tarball %v %d", err, len(files))
	}
	if rec := get(mux, "/internal/curriculum/fixtures/00-orientation/01-first", "If-None-Match", etag); rec.Code != 304 {
		t.Fatalf("fixtures etag: %d", rec.Code)
	}
	if rec := get(mux, "/internal/curriculum/fixtures/00-orientation/02-second"); rec.Code != 204 {
		t.Fatalf("no fixtures: %d", rec.Code)
	}
	if rec := get(mux, "/internal/curriculum/fixtures/00-orientation/09-nope"); rec.Code != 404 {
		t.Fatalf("unknown lesson fixtures: %d", rec.Code)
	}
}

func TestMissingBundleAndReload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "curriculum")
	s, mux := newServer(t, dir)
	if rec := get(mux, "/api/v1/curriculum"); rec.Code != 503 {
		t.Fatalf("missing bundle: %d", rec.Code)
	}
	built := buildBundle(t)
	if err := os.Rename(built, dir); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if rec := get(mux, "/api/v1/curriculum"); rec.Code != 200 {
		t.Fatalf("after reload: %d", rec.Code)
	}
}

func TestFormatMismatch(t *testing.T) {
	dir := buildBundle(t)
	p := filepath.Join(dir, "curriculum.json")
	data, _ := os.ReadFile(p)
	data = []byte(strings.Replace(string(data), `"format": 1`, `"format": 99`, 1))
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(Options{Dir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("want ErrFormat, got %v", err)
	}
}

func TestAutoReload(t *testing.T) {
	dir := buildBundle(t)
	s, err := Open(Options{Dir: dir, AutoReload: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "curriculum.json")
	data, _ := os.ReadFile(p)
	data = []byte(strings.Replace(string(data), `"version": "test"`, `"version": "test2"`, 1))
	_ = os.WriteFile(p, data, 0o644)
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(p, future, future)
	s.mu.Lock()
	s.checked = time.Time{}
	s.mu.Unlock()
	if _, err := s.rootOrErr(); err != nil {
		t.Fatal(err)
	}
	if _, v := s.Loaded(); v != "test2" {
		t.Fatalf("version after auto reload %q", v)
	}
}
