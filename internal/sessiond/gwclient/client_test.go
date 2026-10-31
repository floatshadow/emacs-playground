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
	"testing/synctest"
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

	r := NewReporter(c, nil)
	r.Report(protocol.ProgressReport{LessonID: "l/x", StepID: "open", Status: protocol.ProgressDone})
	r.Close(5 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	if len(progress) != 1 || progress[0].StepID != "open" {
		t.Fatalf("progress %+v", progress)
	}
	mu.Unlock()
	// Reports after Close are dropped, not sent on the closed queue.
	r.Report(protocol.ProgressReport{LessonID: "l/x", StepID: "save", Status: protocol.ProgressDone})
	r.Close(time.Second)
	mu.Lock()
	if len(progress) != 1 {
		t.Fatalf("a report after Close was sent: %+v", progress)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReporterRetriesUntilDelivered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		c := New("http://gateway", "tok", "s1", nil)
		c.Attempts = 1
		c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status := 503
			if calls.Add(1) > 9 {
				status = 204
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
		r := NewReporter(c, nil)
		r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 100})
		r.Close(time.Hour)
		if got := calls.Load(); got != 10 {
			t.Fatalf("attempts %d", got)
		}
	})
}

func TestReporterPurge(t *testing.T) {
	for _, lesson := range []string{"a/b", ""} {
		t.Run("lesson="+lesson, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var old, fresh atomic.Int32
				c := New("http://gateway", "tok", "s1", nil)
				c.Attempts = 1
				c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					var p protocol.ProgressReport
					_ = json.NewDecoder(req.Body).Decode(&p)
					status := 204
					if p.At == 100 {
						old.Add(1)
						status = 503
					} else {
						fresh.Add(1)
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
				})}
				r := NewReporter(c, nil)
				r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 100})
				r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "two", Status: protocol.ProgressDone, At: 100})
				synctest.Wait()
				r.Purge(lesson, 0)
				r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 200})
				r.Close(time.Hour)
				if old.Load() != 1 || fresh.Load() != 1 {
					t.Fatalf("old=%d fresh=%d", old.Load(), fresh.Load())
				}
			})
		})
	}
}

func TestReporterReconcile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var received []protocol.ProgressReport
		c := New("http://gateway", "tok", "s1", nil)
		c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var p protocol.ProgressReport
			_ = json.NewDecoder(req.Body).Decode(&p)
			received = append(received, p)
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
		saved := protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 100}
		r := NewReporter(c, func() []protocol.ProgressReport { return []protocol.ProgressReport{saved} })
		r.Reconcile()
		synctest.Wait()
		r.Close(time.Second)
		if len(received) != 1 || received[0] != saved {
			t.Fatalf("received %+v", received)
		}
	})
}

func TestReporterOverflowReconciles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var first sync.Once
		var recovered atomic.Bool
		c := New("http://gateway", "tok", "s1", nil)
		c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			first.Do(func() { <-release })
			var p protocol.ProgressReport
			_ = json.NewDecoder(req.Body).Decode(&p)
			if p.StepID == "overflow" {
				recovered.Store(true)
			}
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
		saved := protocol.ProgressReport{LessonID: "a/b", StepID: "overflow", Status: protocol.ProgressDone, At: 100}
		r := NewReporter(c, func() []protocol.ProgressReport { return []protocol.ProgressReport{saved} })
		r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "first", Status: protocol.ProgressDone})
		synctest.Wait()
		for range cap(r.queue) {
			r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "queued", Status: protocol.ProgressDone})
		}
		r.Report(saved)
		close(release)
		synctest.Wait()
		r.Close(time.Second)
		if !recovered.Load() {
			t.Fatal("overflowed progress was not reconciled")
		}
	})
}

func TestReporterCloseCancelsRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New("http://gateway", "tok", "s1", nil)
		c.Attempts = 1
		c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
		r := NewReporter(c, nil)
		r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone})
		r.Close(50 * time.Millisecond)
		synctest.Wait()
		select {
		case <-r.done:
		default:
			t.Fatal("reporter keeps retrying after Close")
		}
	})
}

func TestReporterRetryDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var delivered atomic.Bool
		c := New("http://gateway", "tok", "s1", nil)
		c.Attempts = 1
		c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var p protocol.ProgressReport
			_ = json.NewDecoder(req.Body).Decode(&p)
			status := 503
			if p.StepID == "next" {
				status = 204
				delivered.Store(true)
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
		r := NewReporter(c, nil)
		r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "poisoned"})
		r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "next"})
		start := time.Now()
		r.Close(2 * time.Hour)
		if !delivered.Load() || time.Since(start) < time.Hour || time.Since(start) > time.Hour+time.Minute {
			t.Fatalf("delivered=%v after %s", delivered.Load(), time.Since(start))
		}
	})
}

func TestReporterResetGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var received []protocol.ProgressReport
		c := New("http://gateway", "tok", "s1", nil)
		c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var p protocol.ProgressReport
			_ = json.NewDecoder(req.Body).Decode(&p)
			received = append(received, p)
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
		r := NewReporter(c, nil)
		r.Purge("a/b", 4)
		r.Purge("", 3)
		for _, generation := range []int64{2, 4} {
			r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Generation: generation})
		}
		r.Close(time.Second)
		if len(received) != 1 || received[0].Generation != 4 {
			t.Fatalf("reports %+v", received)
		}
	})
}

func TestReporterSyncLowersGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var generations []int64
		c := New("http://gateway", "tok", "s1", nil)
		c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var p protocol.ProgressReport
			_ = json.NewDecoder(req.Body).Decode(&p)
			generations = append(generations, p.Generation)
			return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
		})}
		r := NewReporter(c, nil)
		r.Purge("a/b", 18)
		r.SyncGenerations(protocol.ProgressGenerations{})
		for _, generation := range []int64{18, 0} {
			r.Report(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Generation: generation})
		}
		r.Close(time.Second)
		if len(generations) != 2 || generations[0] != 18 || generations[1] != 0 {
			t.Fatalf("sent generations %v", generations)
		}
	})
}
