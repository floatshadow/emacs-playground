package gwclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestValidLessonID(t *testing.T) {
	for id, ok := range map[string]bool{
		"02-buffers-and-files/03-opening-files": true,
		"projects/p6-polyglot":                  true,
		"00-orientation":                        true,
		"../etc/passwd":                         false,
		"a/b/c":                                 false,
		"A/b":                                   false,
		"a/..":                                  false,
		"a b":                                   false,
		"":                                      false,
		"/a":                                    false,
	} {
		if ValidLessonID(id) != ok {
			t.Errorf("ValidLessonID(%q) != %v", id, ok)
		}
	}
}

func TestClient(t *testing.T) {
	var fails atomic.Int32
	fails.Store(2)
	var mu sync.Mutex
	var progress []protocol.ProgressReport
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/curriculum/scenarios/{id...}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		if fails.Add(-1) >= 0 {
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_ = json.NewEncoder(w).Encode(protocol.Scenario{ID: r.PathValue("id")})
	})
	mux.HandleFunc("GET /internal/curriculum/fixtures/{id...}", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "none/lesson":
			w.WriteHeader(204)
		case "missing/lesson":
			w.WriteHeader(404)
		default:
			if r.Header.Get("If-None-Match") == `"f1"` {
				w.WriteHeader(304)
				return
			}
			w.Header().Set("ETag", `"f1"`)
			_, _ = w.Write([]byte("tarball"))
		}
	})
	mux.HandleFunc("POST /internal/sessions/{id}/progress", func(w http.ResponseWriter, r *http.Request) {
		var p protocol.ProgressReport
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		progress = append(progress, p)
		mu.Unlock()
		if r.PathValue("id") != "s1" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(srv.URL+"/", "tok", "s1", nil)
	c.Backoff = []time.Duration{time.Millisecond}
	ctx := context.Background()

	raw, etag, nm, err := c.Scenario(ctx, "02-buffers-and-files/03-opening-files", "")
	if err != nil || nm || etag != `"v1"` {
		t.Fatalf("scenario after retries: %v %q %v", err, etag, nm)
	}
	var sc protocol.Scenario
	_ = json.Unmarshal(raw, &sc)
	if sc.ID != "02-buffers-and-files/03-opening-files" {
		t.Fatalf("lesson id did not reach the path literally: %q", sc.ID)
	}
	if _, _, nm, err := c.Scenario(ctx, "a/b", `"v1"`); err != nil || !nm {
		t.Fatalf("304: %v %v", err, nm)
	}
	if _, _, _, err := c.Scenario(ctx, "../x", ""); err == nil {
		t.Fatal("invalid id")
	}

	fr, err := c.Fixtures(ctx, "a/b", "")
	if err != nil || fr.ETag != `"f1"` {
		t.Fatalf("fixtures %v %+v", err, fr)
	}
	b, _ := io.ReadAll(fr.Body)
	fr.Body.Close()
	if string(b) != "tarball" {
		t.Fatalf("body %q", b)
	}
	if fr, err := c.Fixtures(ctx, "a/b", `"f1"`); err != nil || !fr.NotModified {
		t.Fatalf("fixtures 304 %v %+v", err, fr)
	}
	if fr, err := c.Fixtures(ctx, "none/lesson", ""); err != nil || !fr.None {
		t.Fatalf("fixtures 204 %v %+v", err, fr)
	}
	if _, err := c.Fixtures(ctx, "missing/lesson", ""); !NotFound(err) {
		t.Fatalf("fixtures 404 %v", err)
	}

	// Progress posts once: the outbox retries. A 4xx is Rejected.
	fails.Store(0)
	if err := c.Progress(ctx, protocol.ProgressReport{LessonID: "l/x", StepID: "open", Status: protocol.ProgressDone}); err != nil {
		t.Fatal(err)
	}
	other := New(srv.URL, "tok", "s2", nil)
	if err := other.Progress(ctx, protocol.ProgressReport{LessonID: "l/x", StepID: "save"}); !Rejected(err) {
		t.Fatalf("404 progress: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(progress) != 2 || progress[0].StepID != "open" {
		t.Fatalf("progress %+v", progress)
	}
}

func TestProgressPostsOnce(t *testing.T) {
	var calls atomic.Int32
	c := New("http://gateway", "tok", "s1", nil)
	c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})}
	err := c.Progress(context.Background(), protocol.ProgressReport{LessonID: "a/b", StepID: "one"})
	if err == nil || Rejected(err) || calls.Load() != 1 {
		t.Fatalf("err %v after %d calls", err, calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
