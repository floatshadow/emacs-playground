package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestEventRoundTrip(t *testing.T) {
	for _, typ := range EventTypes {
		ev := NewEvent(typ)
		if ev == nil {
			t.Fatalf("NewEvent(%q) = nil", typ)
		}
		if ev.Type() != typ {
			t.Fatalf("NewEvent(%q).Type() = %q", typ, ev.Type())
		}
		Stamp(ev, 7, 1761472000123)
		b, err := MarshalEvent(ev)
		if err != nil {
			t.Fatalf("marshal %q: %v", typ, err)
		}
		got, err := DecodeEvent(b)
		if err != nil {
			t.Fatalf("decode %q: %v (%s)", typ, err, b)
		}
		if got.Type() != typ || got.Env().Seq != 7 || got.Env().Ts != 1761472000123 {
			t.Fatalf("round trip %q: got %#v", typ, got)
		}
	}
}

func TestKeyEventWire(t *testing.T) {
	in := `{"t":"key","seq":418,"ts":1761472000123,"keys":"C-x C-f","cmd":"find-file","buf":"main.rs","mode":"rust-ts-mode","frame":"terminal"}`
	ev, err := DecodeEvent([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	k, ok := ev.(*KeyEvent)
	if !ok || k.Keys != "C-x C-f" || k.Cmd != "find-file" || k.Frame != FrameTerminal || k.Seq != 418 {
		t.Fatalf("got %#v", ev)
	}
	out, _ := json.Marshal(k)
	if string(out) != in {
		t.Fatalf("re-encode:\n got %s\nwant %s", out, in)
	}
}

func TestUnknownEventPreserved(t *testing.T) {
	in := `{"t":"future","seq":3,"x":1}`
	ev, err := DecodeEvent([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type() != "future" {
		t.Fatalf("type %q", ev.Type())
	}
	out, _ := json.Marshal(ev)
	if string(out) != in {
		t.Fatalf("got %s", out)
	}
}

const stateJSON = `{
  "t": "state", "seq": 419, "ts": 1761472000130,
  "buf": "main.rs", "file": "/work/hello/src/main.rs", "mode": "rust-ts-mode", "modeName": "Rust",
  "minors": ["eglot--managed-mode", "corfu-mode", "flymake-mode"],
  "modified": true, "readonly": false, "narrowed": false, "overwrite": false,
  "line": 9, "col": 20, "point": 182, "markActive": false,
  "windows": [
    {"id": 3, "buf": "main.rs", "mode": "rust-ts-mode", "selected": true, "dedicated": false, "side": null},
    {"id": 7, "buf": "*compilation*", "mode": "compilation-mode", "selected": false, "dedicated": false, "side": "bottom"}
  ],
  "layout": ["vsplit", 3, 7],
  "frame": "terminal", "frames": 1, "tabs": {"count": 1, "current": "main"},
  "minibuffer": null,
  "isearch": false, "recursive": 0, "macro": false, "prefixArg": null,
  "readOnlyReason": null, "diagAtPoint": null, "waiting": null
}`

func TestStateDecode(t *testing.T) {
	ev, err := DecodeEvent([]byte(stateJSON))
	if err != nil {
		t.Fatal(err)
	}
	s := ev.(*State)
	if s.Layout.Split != VSplit || !reflect.DeepEqual(s.Layout.Leaves(), []int{3, 7}) {
		t.Fatalf("layout %v", s.Layout)
	}
	if s.File == nil || *s.File != "/work/hello/src/main.rs" {
		t.Fatalf("file %v", s.File)
	}
	if s.BufferOf(7) != "*compilation*" || len(s.OtherWindows()) != 1 {
		t.Fatal("windows")
	}
	b, _ := json.Marshal(s)
	var a, c map[string]any
	_ = json.Unmarshal([]byte(stateJSON), &a)
	_ = json.Unmarshal(b, &c)
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("state does not round-trip:\n%s", b)
	}
}

func TestLayoutJSON(t *testing.T) {
	cases := []string{`null`, `3`, `["vsplit",3,7]`, `["hsplit",["vsplit",1,2],9]`}
	for _, c := range cases {
		var l Layout
		if err := json.Unmarshal([]byte(c), &l); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		b, _ := json.Marshal(l)
		if string(b) != c {
			t.Fatalf("%s: re-encoded %s", c, b)
		}
	}
	var l Layout
	if err := json.Unmarshal([]byte(`["diagonal",1,2]`), &l); err == nil {
		t.Fatal("want error for unknown split")
	}
}

func TestLocalText(t *testing.T) {
	var s struct {
		A LocalText `json:"a"`
		B LocalText `json:"b"`
		C LocalText `json:"c,omitempty"`
	}
	if err := json.Unmarshal([]byte(`{"a":"hello","b":{"en":"hi","zh-CN":"你好"}}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.A.Get("zh-CN") != "hello" || s.B.Get("zh-CN") != "你好" || s.B.Get("zh") != "hi" || s.B.Get("fr") != "hi" {
		t.Fatalf("%#v", s)
	}
	b, _ := json.Marshal(s)
	if string(b) != `{"a":"hello","b":{"en":"hi","zh-CN":"你好"}}` {
		t.Fatalf("got %s", b)
	}
}

func TestTruth(t *testing.T) {
	var r AgentCheckResult
	if err := json.Unmarshal([]byte(`{"results":[true,false,"unknown",null]}`), &r); err != nil {
		t.Fatal(err)
	}
	want := []Truth{True, False, Unknown, Unknown}
	if !reflect.DeepEqual(r.Results, want) {
		t.Fatalf("got %v", r.Results)
	}
	b, _ := json.Marshal(r)
	if string(b) != `{"results":[true,false,"unknown","unknown"]}` {
		t.Fatalf("got %s", b)
	}
}

const scenarioJSON = `{
  "id": "02-buffers-and-files/03-opening-files",
  "title": "Opening files",
  "minutes": 12,
  "mode": "terminal",
  "tags": [],
  "modules": ["base", "agent", "editing", "files"],
  "vscode": "Ctrl+P, Ctrl+O",
  "preview": {"tabs": ["docs/report.pdf"], "follow": {"docs/report.typ": "docs/report.pdf"}},
  "setup": {"workspace": "fixtures/hello-rust", "root": "hello", "reset": false, "open": ["hello/README.md"], "layout": "single", "eval": "(goto-char (point-min))", "compile": null},
  "steps": [
    {"id": "open", "goal": "Open src/main.rs", "detail": {"en": "Hold Control", "zh-CN": "按住 Control"},
     "demo": "C-x C-f \"ma\" TAB ~400 RET",
     "conditions": [
       {"text": "the Find file prompt was used", "event": {"command": "find-file", "min": 1}, "hint": "C-x then C-f."},
       {"text": "main.rs is the current buffer", "elisp": "(equal (buffer-file-name) \"/work/hello/src/main.rs\")", "hint": "TAB then RET."}
     ],
     "stuck": [{"when": {"minibuffer_seconds": 30}, "say": "Still in the prompt?"}, {"when": {"quits_in_window": {"count": 3, "seconds": 8}}, "say": "Calm."}],
     "checkpoint": true},
    {"id": "save", "goal": "Save it", "demo": "C-x C-s",
     "conditions": [
       {"text": "saved", "all": [{"event": {"command": "save-buffer"}}, {"file": {"path": "hello/src/main.rs", "not_contains": "Hello, world"}}], "hint": "C-x C-s."},
       {"text": "narrowed", "state": {"narrowed": false}},
       {"layout": "(vsplit main.rs *compilation*)"},
       {"shell": {"run": "cargo build", "timeout": 30}},
       {"not": {"file": {"path": "stray.txt", "exists": true}}}
     ]}
  ]
}`

func TestScenarioDecode(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(scenarioJSON))
	dec.DisallowUnknownFields()
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		t.Fatal(err)
	}
	s.AssignIDs()
	if s.Steps[0].Conditions[1].ID != "open/2" || s.Steps[0].Stuck[0].ID != "open/stuck/1" {
		t.Fatalf("ids: %+v", s.Steps[0])
	}
	kinds := []ConditionKind{}
	for _, c := range s.Steps[1].Conditions {
		kinds = append(kinds, c.Kind())
	}
	want := []ConditionKind{CondAll, CondState, CondLayout, CondShell, CondNot}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds %v", kinds)
	}
	if !s.Steps[1].TakesCheckpoint() || s.Steps[0].Detail.Get("zh-CN") != "按住 Control" {
		t.Fatal("defaults or locale")
	}
	if s.Steps[0].Stuck[1].When.QuitsInWindow.Count != 3 {
		t.Fatal("stuck")
	}
	if _, err := ParseDemo(s.Steps[0].Demo); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(s); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeAgentMessage(t *testing.T) {
	lines := map[string]any{
		`{"t":"hello","protocol":1,"agent":"0.1.0","emacs":"31.1","pid":42}`:                &AgentHelloMsg{},
		`{"t":"key","keys":"C-x C-f","cmd":"find-file","buf":"a","mode":"m","frame":"gui"}`: &KeyEvent{},
		`{"t":"frame","event":"opened","kind":"gui"}`:                                       &AgentFrameMsg{},
		`{"t":"reply","id":5,"ok":true,"value":{"did":[]}}`:                                 &AgentReplyMsg{},
		`{"t":"ping"}`: &AgentPingMsg{},
	}
	for line, want := range lines {
		m, err := DecodeAgentMessage([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if reflect.TypeOf(m) != reflect.TypeOf(want) {
			t.Fatalf("%s: got %T", line, m)
		}
	}
	if _, err := DecodeAgentMessage([]byte(`{"t":"nope"}`)); err == nil {
		t.Fatal("want error")
	}
	c, err := NewAgentCall(1, AgentOpCheck, AgentCheckArgs{Forms: []string{"t"}}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	if string(b) != `{"t":"call","id":1,"op":"check","args":{"forms":["t"]},"timeoutMs":2000}` {
		t.Fatalf("got %s", b)
	}
}

func TestRPCSpecsComplete(t *testing.T) {
	seen := map[RPCOp]bool{}
	for _, s := range RPCSpecs {
		if seen[s.Op] {
			t.Fatalf("duplicate op %s", s.Op)
		}
		seen[s.Op] = true
		if s.Request == nil || s.Result == nil {
			t.Fatalf("op %s lacks types", s.Op)
		}
	}
	b, _ := json.Marshal(RPCResponse[RescueResult]{OK: true, Result: RescueResult{Did: []string{"x"}}})
	if string(b) != `{"ok":true,"result":{"did":["x"]}}` {
		t.Fatalf("got %s", b)
	}
}
