package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/curriculum"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// watchEngine is a fake gateway session (/s/local/...) with sessiond's demo
// latch: a step completes when it received its demo's keys; under the latch
// it is demonstrated and stays active, restore {} clears the latch. Step
// events go out on /s/local/events; progress resets are recorded.
type watchEngine struct {
	mu       sync.Mutex
	src      *curriculum.Source
	lesson   string
	step     int
	got      []string
	latch    bool
	broken   map[string]bool // steps whose demo completes them as done despite the latch
	seq      int64
	subs     []chan []byte
	resets   []string
	demoOps  []string
	restores int
}

func (e *watchEngine) steps() []protocol.Step { return e.src.Lessons[e.lesson].Scenario.Steps }

func (e *watchEngine) emitLocked(st protocol.StepStatus, stepID string) {
	e.seq++
	ev := &protocol.StepEvent{LessonID: e.lesson, StepID: stepID, Status: st}
	protocol.Stamp(ev, e.seq, time.Now().UnixMilli())
	b, _ := protocol.MarshalEvent(ev)
	for _, c := range e.subs {
		c <- b
	}
}

func (e *watchEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/s/local/events" {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ch := make(chan []byte, 100)
		e.mu.Lock()
		e.subs = append(e.subs, ch)
		e.mu.Unlock()
		for b := range ch {
			if c.Write(r.Context(), websocket.MessageText, b) != nil {
				return
			}
		}
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	switch r.URL.Path {
	case "/api/v1/progress/reset":
		var req protocol.ProgressResetRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		e.resets = append(e.resets, req.LessonID)
		w.WriteHeader(204)
	case "/s/local/status":
		st := protocol.StatusResult{Emacs: protocol.EmacsStatus{Agent: true}}
		if e.step >= 0 {
			st.Step = &protocol.StepRef{LessonID: e.lesson, StepID: e.steps()[e.step].ID}
		}
		httpx.WriteJSON(w, 200, st)
	case "/s/local/rpc/scenario":
		var req protocol.ScenarioRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		e.lesson, e.step, e.got, e.latch = req.LessonID, 0, nil, false
		for i, s := range e.steps() {
			if s.ID == req.StepID {
				e.step = i
			}
		}
		e.emitLocked(protocol.StepLoaded, e.steps()[e.step].ID)
		httpx.WriteOK(w, protocol.ScenarioResult{})
	case "/s/local/rpc/demo":
		var req protocol.DemoRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		e.demoOps = append(e.demoOps, req.StepID+":"+string(req.Phase))
		if e.step < 0 || e.steps()[e.step].ID != req.StepID {
			httpx.WriteError(w, r, httpx.Conflict("not active"))
			return
		}
		if req.Phase == protocol.DemoStart {
			e.latch, e.got = true, nil
		}
		httpx.WriteOK(w, protocol.DemoResult{})
	case "/s/local/rpc/restore":
		e.restores++
		e.latch, e.got = false, nil
		e.emitLocked(protocol.StepReset, e.steps()[e.step].ID)
		httpx.WriteOK(w, protocol.RestoreResult{})
	case "/s/local/rpc/keys":
		var req protocol.KeysRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Keys != "" {
			e.got = append(e.got, "K:"+req.Keys)
		} else {
			e.got = append(e.got, "T:"+req.Text)
		}
		if e.step >= 0 {
			st := e.steps()[e.step]
			if strings.Join(e.got, "|") == strings.Join(expected(st.Demo), "|") {
				e.got = nil
				if e.latch && !e.broken[st.ID] {
					e.emitLocked(protocol.StepDemonstrated, st.ID)
				} else {
					e.emitLocked(protocol.StepComplete, st.ID)
					e.step++
					if e.step >= len(e.steps()) {
						e.step = -1
					} else {
						e.emitLocked(protocol.StepLoaded, e.steps()[e.step].ID)
					}
				}
			}
		}
		httpx.WriteOK(w, nil)
	case "/s/local/rpc/check":
		if e.step < 0 {
			httpx.WriteError(w, r, httpx.Conflict("no active step"))
			return
		}
		st := e.steps()[e.step]
		httpx.WriteOK(w, protocol.CheckResult{LessonID: e.lesson, StepID: st.ID, Conditions: []protocol.CondResult{{ID: st.ID + "/1"}}})
	default:
		http.NotFound(w, r)
	}
}

func TestPlayWatchAndResetProgress(t *testing.T) {
	src, diags := curriculum.Load(goodDir)
	if diags.HasErrors() {
		t.Fatal(diags.Err())
	}
	eng := &watchEngine{src: src, step: -1, broken: map[string]bool{}}
	srv := httptest.NewServer(eng)
	defer srv.Close()
	session := srv.URL + "/s/local"
	if !gatewaySessionURL(session) || gatewaySessionURL(srv.URL) || gatewaySessionURL("http://127.0.0.1:7000/") {
		t.Fatal("gatewaySessionURL")
	}

	// -watch: every step is demonstrated, reset, then completed; the
	// lesson's progress is reset afterwards (a gateway session URL).
	if code := curriculumPlay([]string{"0.1", "-watch", "-session", session, "-dir", goodDir, "-speed", "50", "-timeout", "2s"}); code != 0 {
		t.Fatalf("play -watch: %d", code)
	}
	eng.mu.Lock()
	ops, restores, resets := strings.Join(eng.demoOps, " "), eng.restores, strings.Join(eng.resets, " ")
	eng.mu.Unlock()
	if ops != "open:start open:end save:start save:end layout:start layout:end" || restores != 3 {
		t.Fatalf("demo ops %q, restores %d", ops, restores)
	}
	if resets != "00-orientation/01-first" {
		t.Fatalf("progress resets %q", resets)
	}

	// A demonstration that completes the step as done fails the step.
	eng.mu.Lock()
	eng.broken["save"] = true
	eng.mu.Unlock()
	c := mustClient(t, session)
	ev, err := c.subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer ev.close()
	p := &player{c: c, timeout: time.Second, speed: 50, keepGoing: true, log: io.Discard, events: ev}
	res := p.playLesson(context.Background(), src.Lessons["00-orientation/01-first"])
	if len(res) != 3 || !res[0].OK || res[1].OK || !strings.Contains(res[1].Detail, "completed the step as done") {
		t.Fatalf("a demo completing the step must fail: %+v", res)
	}
	if code := curriculumPlay([]string{"0.1", "-watch", "-session", session, "-dir", goodDir, "-speed", "50", "-timeout", "1s", "-reset-progress=false"}); code != 1 {
		t.Fatalf("a demo completing the step must fail: %d", code)
	}
	eng.mu.Lock()
	resets = strings.Join(eng.resets, " ")
	eng.mu.Unlock()
	if resets != "00-orientation/01-first" {
		t.Fatalf("-reset-progress=false still reset: %q", resets)
	}

	// -repeat plays the lesson twice.
	eng.mu.Lock()
	eng.broken = map[string]bool{}
	eng.mu.Unlock()
	if code := curriculumPlay([]string{"0.1", "-repeat", "2", "-session", session, "-dir", goodDir, "-speed", "50", "-timeout", "2s"}); code != 0 {
		t.Fatalf("-repeat 2: %d", code)
	}
	eng.mu.Lock()
	n := strings.Count(strings.Join(eng.resets, " "), "01-first")
	eng.mu.Unlock()
	if n != 2 {
		t.Fatalf("one reset per lesson, after its repeats: %d", n)
	}
}

func mustClient(t *testing.T, u string) *sessionClient {
	t.Helper()
	c, err := newSessionClient(u)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
