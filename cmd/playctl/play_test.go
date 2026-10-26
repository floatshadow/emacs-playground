package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zsy/emacs-playground/internal/curriculum"
	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
)

const goodDir = "../../internal/curriculum/testdata/good"

// stepEngine is a fake sessiond: a step completes when the keys and text it
// received equal the step's demo; the engine then advances like sessiond's.
type stepEngine struct {
	mu      sync.Mutex
	src     *curriculum.Source
	lesson  string
	step    int // -1: none
	got     []string
	refuse  map[string]bool // step ids that never complete
	loads   []string
	origins []string
}

func expected(demo string) []string {
	toks, _ := protocol.ParseDemo(demo)
	var out []string
	for _, t := range toks {
		switch t.Kind {
		case protocol.DemoKey:
			out = append(out, "K:"+t.Key)
		case protocol.DemoText:
			for _, c := range splitText(t.Text) {
				switch c {
				case "\n":
					out = append(out, "K:RET")
				case "\t":
					out = append(out, "K:TAB")
				default:
					out = append(out, "T:"+c)
				}
			}
		}
	}
	return out
}

func (e *stepEngine) steps() []protocol.Step { return e.src.Lessons[e.lesson].Scenario.Steps }

func (e *stepEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r.Method == "POST" {
		e.origins = append(e.origins, r.Header.Get("Origin"))
	}
	switch r.URL.Path {
	case "/status":
		st := protocol.StatusResult{Emacs: protocol.EmacsStatus{Agent: true, Version: "31.1"}}
		if e.step >= 0 {
			st.Step = &protocol.StepRef{LessonID: e.lesson, StepID: e.steps()[e.step].ID}
		}
		httpx.WriteJSON(w, 200, st)
	case "/rpc/scenario":
		var req protocol.ScenarioRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		e.lesson = req.LessonID
		e.loads = append(e.loads, req.LessonID+"/"+req.StepID)
		e.step = 0
		for i, s := range e.steps() {
			if s.ID == req.StepID {
				e.step = i
			}
		}
		e.got = nil
		httpx.WriteOK(w, protocol.ScenarioResult{Step: protocol.StepRef{LessonID: e.lesson, StepID: e.steps()[e.step].ID}, Index: e.step, Count: len(e.steps())})
	case "/rpc/keys":
		var req protocol.KeysRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Keys != "" {
			e.got = append(e.got, "K:"+req.Keys)
		} else {
			e.got = append(e.got, "T:"+req.Text)
		}
		if e.step >= 0 {
			st := e.steps()[e.step]
			if !e.refuse[st.ID] && strings.Join(e.got, "|") == strings.Join(expected(st.Demo), "|") {
				e.got = nil
				e.step++
				if e.step >= len(e.steps()) {
					e.step = -1
				}
			}
		}
		httpx.WriteOK(w, nil)
	case "/rpc/check":
		if e.step < 0 {
			httpx.WriteError(w, r, httpx.Conflict("no active step"))
			return
		}
		st := e.steps()[e.step]
		httpx.WriteOK(w, protocol.CheckResult{LessonID: e.lesson, StepID: st.ID, Conditions: []protocol.CondResult{{ID: st.ID + "/1", OK: false, Detail: "waiting"}}})
	default:
		http.NotFound(w, r)
	}
}

func TestPlayLesson(t *testing.T) {
	src, diags := curriculum.Load(goodDir)
	if diags.HasErrors() {
		t.Fatal(diags.Err())
	}
	eng := &stepEngine{src: src, step: -1, refuse: map[string]bool{}}
	srv := httptest.NewServer(eng)
	defer srv.Close()
	c, err := newSessionClient(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	p := &player{c: c, timeout: 2 * time.Second, speed: 50, keepGoing: true, log: io.Discard}
	res := p.playLesson(context.Background(), src.Lessons["00-orientation/01-first"])
	if len(res) != 3 {
		t.Fatalf("results %+v", res)
	}
	for _, r := range res {
		if !r.OK {
			t.Fatalf("step %s failed: %s", r.Step, r.Detail)
		}
	}
	if eng.loads[0] != "00-orientation/01-first/open" {
		t.Fatalf("loads %v", eng.loads)
	}
	for _, o := range eng.origins {
		if o != srv.URL {
			t.Fatalf("origin %q", o)
		}
	}

	// A step that never completes is reported with its unmet conditions,
	// and the next step is loaded explicitly.
	eng.refuse["save"] = true
	p.timeout = 500 * time.Millisecond
	res = p.playLesson(context.Background(), src.Lessons["00-orientation/01-first"])
	if len(res) != 3 || !res[0].OK || res[1].OK || !strings.Contains(res[1].Detail, "save/1") || !res[2].OK {
		t.Fatalf("with a refused step: %+v", res)
	}
	if last := eng.loads[len(eng.loads)-1]; last != "00-orientation/01-first/layout" {
		t.Fatalf("expected a jump to layout, loads %v", eng.loads)
	}
	var out bytes.Buffer
	if code := printResults(&out, res); code != 1 || !strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), "3 steps, 2 passed, 1 failed") {
		t.Fatalf("table (%d):\n%s", code, out.String())
	}
}

func TestPlayCommand(t *testing.T) {
	src, _ := curriculum.Load(goodDir)
	eng := &stepEngine{src: src, step: -1, refuse: map[string]bool{}}
	srv := httptest.NewServer(eng)
	defer srv.Close()
	if code := curriculumPlay([]string{"all", "-session", srv.URL, "-dir", goodDir, "-speed", "50", "-timeout", "2s"}); code != 0 {
		t.Fatalf("play all: %d", code)
	}
	if code := curriculumPlay([]string{"0.9", "-session", srv.URL, "-dir", goodDir}); code != 2 {
		t.Fatalf("unknown selector: %d", code)
	}
	if code := curriculumPlay([]string{"0.2", "-session", "http://127.0.0.1:1", "-dir", goodDir}); code != 1 {
		t.Fatalf("unreachable session: %d", code)
	}
}

func TestSelectLessons(t *testing.T) {
	src, _ := curriculum.Load(goodDir)
	ids := func(sel ...string) string {
		ls, err := selectLessons(src, sel)
		if err != nil {
			return "error"
		}
		var s []string
		for _, l := range ls {
			s = append(s, l.ID)
		}
		return strings.Join(s, " ")
	}
	if got := ids("all"); got != "00-orientation/01-first 00-orientation/02-second" {
		t.Fatal(got)
	}
	if got := ids("0.2", "00-orientation/01-first", "0.2"); got != "00-orientation/02-second 00-orientation/01-first" {
		t.Fatal(got)
	}
	if got := ids("part:0"); got != "00-orientation/01-first 00-orientation/02-second" {
		t.Fatal(got)
	}
	if got := ids("1.1"); got != "error" {
		t.Fatalf("planned lesson selected: %s", got)
	}
}

func TestSplitText(t *testing.T) {
	got := strings.Join(splitText("ab\ncd\t\nx"), "|")
	if got != "ab|\n|cd|\t|\n|x" {
		t.Fatalf("%q", got)
	}
}

func TestConfigSchema(t *testing.T) {
	data, err := ConfigSchema()
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	props, _ := s["properties"].(map[string]any)
	if s["additionalProperties"] != false || props["sessions"] == nil || s["required"] != nil {
		t.Fatalf("schema %v", s)
	}
}

func TestValidateAndBuildCommands(t *testing.T) {
	if code := curriculumValidate([]string{"-q", goodDir}); code != 0 {
		t.Fatalf("validate: %d", code)
	}
	out := t.TempDir()
	if code := curriculumBuild([]string{goodDir, "-o", out, "-version", "t1"}); code != 0 {
		t.Fatalf("build: %d", code)
	}
	if code := curriculumBuild([]string{goodDir}); code != 2 {
		t.Fatalf("build without -o: %d", code)
	}
}
