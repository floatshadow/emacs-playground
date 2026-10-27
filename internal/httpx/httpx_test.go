package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func TestHandleRendersProblem(t *testing.T) {
	h := Handle(func(w http.ResponseWriter, r *http.Request) error {
		return NotFound("no such lesson").WithDetails(map[string]string{"lessonId": "x"})
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 404 || rec.Header().Get("Content-Type") != protocol.ProblemContentType {
		t.Fatalf("got %d %s", rec.Code, rec.Header())
	}
	var p protocol.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.Code != protocol.ErrNotFound || p.Status != 404 || p.Message != "no such lesson" {
		t.Fatalf("got %+v", p)
	}
	// Plain errors become internal without leaking the message.
	h = Handle(func(w http.ResponseWriter, r *http.Request) error { return errors.New("secret detail") })
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if AsError(context.DeadlineExceeded).Code != protocol.ErrTimeout {
		t.Fatal("deadline should map to timeout")
	}
}

func TestDecodeJSON(t *testing.T) {
	var v protocol.KeysRequest
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"keys":"C-x C-f","extra":1}`))
	if err := DecodeJSON(httptest.NewRecorder(), r, &v); err != nil || v.Keys != "C-x C-f" {
		t.Fatalf("%v %+v", err, v)
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(``))
	if err := DecodeJSON(httptest.NewRecorder(), r, &v); err != nil {
		t.Fatalf("empty body: %v", err)
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(`{"keys":`))
	err := DecodeJSON(httptest.NewRecorder(), r, &v)
	if he := AsError(err); he.Status != 400 {
		t.Fatalf("got %v", err)
	}
}

func TestWriteOK(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteOK(rec, protocol.RescueResult{Did: []string{"cancelled the prompt"}})
	if strings.TrimSpace(rec.Body.String()) != `{"ok":true,"result":{"did":["cancelled the prompt"]}}` {
		t.Fatalf("got %s", rec.Body)
	}
}

func TestOriginCheck(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := OriginCheck("https://emacs.example.edu", func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/internal/") })(ok)
	cases := []struct {
		method, path, origin string
		upgrade              bool
		want                 int
	}{
		{"GET", "/api/v1/me", "", false, 204},
		{"POST", "/api/v1/sessions", "https://emacs.example.edu", false, 204},
		{"POST", "/api/v1/sessions", "https://evil.example", false, 403},
		{"POST", "/api/v1/sessions", "", false, 403},
		{"GET", "/s/x/events", "https://evil.example", true, 403},
		{"GET", "/s/x/events", "https://emacs.example.edu", true, 204},
		{"POST", "/internal/sessions/x/progress", "", false, 204},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.upgrade {
			r.Header.Set("Upgrade", "websocket")
			r.Header.Set("Connection", "Upgrade")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s %s origin=%q: got %d want %d", c.method, c.path, c.origin, rec.Code, c.want)
		}
	}
}

func TestSameOriginLoopback(t *testing.T) {
	cases := []struct {
		public, origin string
		want           bool
	}{
		{"http://localhost:8080", "http://localhost:8080", true},
		{"http://localhost:8080", "http://127.0.0.1:8080", true},
		{"http://127.0.0.1:8080", "http://localhost:8080", true},
		{"http://localhost:8080", "http://[::1]:8080", true},
		{"http://localhost:8080", "http://127.0.0.1:9090", false},
		{"http://localhost:8080", "https://127.0.0.1:8080", false},
		{"http://localhost", "http://127.0.0.1:80", true},
		{"http://localhost:8080", "http://evil.example:8080", false},
		{"https://emacs.example.edu", "https://127.0.0.1", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/api/v1/sessions", nil)
		r.Header.Set("Origin", c.origin)
		if got := SameOrigin(r, c.public); got != c.want {
			t.Errorf("SameOrigin(public %s, origin %s) = %v, want %v", c.public, c.origin, got, c.want)
		}
	}
}

func TestMiddlewareAndWebSocket(t *testing.T) {
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := AcceptWebSocket(w, r, AcceptOptions{TrustProxy: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = WriteJSONText(r.Context(), c, protocol.PtyControl{T: protocol.PtyAttached}, time.Second)
		_ = Close(c, protocol.CloseReplaced, "replaced")
	})
	mux.HandleFunc("GET /panic", func(w http.ResponseWriter, r *http.Request) { panic("boom") })
	srv := httptest.NewServer(Chain(mux, RequestID, Logging(l), Recover))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/panic")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 500 || resp.Header.Get(protocol.HeaderRequestID) == "" {
		t.Fatalf("panic: %d", resp.StatusCode)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://elsewhere.example"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	typ, b, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageText || string(b) != `{"t":"attached"}` {
		t.Fatalf("%v %s", err, b)
	}
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != protocol.CloseReplaced {
		t.Fatalf("close: %v", err)
	}
}
