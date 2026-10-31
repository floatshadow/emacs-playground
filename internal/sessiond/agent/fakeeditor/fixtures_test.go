package fakeeditor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// The contract fixtures are recorded from the real agent by
// emacs.d/test/record-fixtures.el (make agent-fixtures) and replayed
// against it by make emacs-test. Here the fake editor replays them under
// the same rule: replies and events have the recorded shape (keys and
// JSON types, recursively) and the fields a fixture marks exact are
// equal. Every recorded reply decodes, without unknown fields, into the
// Go type of its op (an op without one fails), and every event into its
// protocol type.

type fixture struct {
	Name  string `json:"name"`
	Setup []struct {
		File     string `json:"file"`
		Contents string `json:"contents"`
	} `json:"setup"`
	Steps []struct {
		Press string          `json:"press"`
		Type  string          `json:"type"`
		Sleep int             `json:"sleep"`
		Call  string          `json:"call"`
		Args  json.RawMessage `json:"args"`
		As    string          `json:"as"`
		Exact []string        `json:"exact"`
	} `json:"steps"`
	Calls []struct {
		Op    string   `json:"op"`
		OK    bool     `json:"ok"`
		Reply any      `json:"reply"`
		Exact []string `json:"exact"`
	} `json:"calls"`
	Events      []map[string]any    `json:"events"`
	EventsExact map[string][]string `json:"eventsExact"`
}

func fixtureDir(t *testing.T) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "emacs.d", "test", "fixtures", "agent")
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(fixtureDir(t), "*.json"))
	if err != nil || len(files) < 16 {
		t.Fatalf("contract fixtures: %d files, %v", len(files), err)
	}
	var out []fixture
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx fixture
		if err := json.Unmarshal(b, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out = append(out, fx)
	}
	return out
}

var droppedFields = []string{"takenAt", "pid", "ts", "uptime", "uptimeMs"}

// normalize writes the workspace as $WS and drops the varying fields.
func normalize(v any, ws string) any {
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, ws, "$WS")
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = normalize(y, ws)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, y := range x {
			if !slices.Contains(droppedFields, k) {
				out[k] = normalize(y, ws)
			}
		}
		return out
	}
	return v
}

func jsonValue(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	}
	return "object"
}

// shapeDiffs is pg-fixture--shape-diffs: where got differs from want in
// keys or JSON types.
func shapeDiffs(want, got any, where string) []string {
	tw, tg := jsonType(want), jsonType(got)
	if tw != tg {
		return []string{fmt.Sprintf("%s: %s, want %s", where, tg, tw)}
	}
	switch w := want.(type) {
	case map[string]any:
		g := got.(map[string]any)
		kw, kg := keys(w), keys(g)
		if !slices.Equal(kw, kg) {
			return []string{fmt.Sprintf("%s: keys %v, want %v", where, kg, kw)}
		}
		var out []string
		for _, k := range kw {
			out = append(out, shapeDiffs(w[k], g[k], where+"."+k)...)
		}
		return out
	case []any:
		g := got.([]any)
		var out []string
		for i := 0; i < min(len(w), len(g)); i++ {
			out = append(out, shapeDiffs(w[i], g[i], fmt.Sprintf("%s[%d]", where, i))...)
		}
		return out
	}
	return nil
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// values is pg-fixture--values: the values at a path ("files[].path").
func values(v any, path string) []any {
	vals := []any{v}
	for _, seg := range strings.Split(path, ".") {
		each := strings.HasSuffix(seg, "[]")
		key := strings.TrimSuffix(seg, "[]")
		var next []any
		for _, x := range vals {
			m, _ := x.(map[string]any)
			y := m[key]
			if each {
				arr, _ := y.([]any)
				next = append(next, arr...)
			} else {
				next = append(next, y)
			}
		}
		vals = next
	}
	return vals
}

func exactDiffs(want, got any, paths []string, where string) []string {
	var out []string
	for _, p := range paths {
		w, g := values(want, p), values(got, p)
		if !reflect.DeepEqual(w, g) {
			out = append(out, fmt.Sprintf("%s %s: %v, want %v", where, p, g, w))
		}
	}
	return out
}

// resolve substitutes "$WS" and "$NAME" (a saved reply) in call arguments.
func resolve(v any, ws string, saved map[string]any) any {
	switch x := v.(type) {
	case string:
		if x == "$WS" {
			return ws
		}
		if r, ok := saved[strings.TrimPrefix(x, "$")]; ok && strings.HasPrefix(x, "$") {
			return r
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = resolve(y, ws, saved)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, y := range x {
			out[k] = resolve(y, ws, saved)
		}
		return out
	}
	return v
}

type replayedCall struct {
	op    string
	ok    bool
	reply any
}

// replay plays fx against a fresh editor.
func replay(t *testing.T, fx fixture) ([]replayedCall, []map[string]any) {
	t.Helper()
	ws := t.TempDir()
	for _, f := range fx.Setup {
		p := filepath.Join(ws, filepath.FromSlash(f.File))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.Contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e := New(ws)
	e.ModulesPath = filepath.Join(ws, "modules.el")
	defer e.Close()
	saved := map[string]any{}
	var calls []replayedCall
	for _, s := range fx.Steps {
		switch {
		case s.Press != "":
			if err := e.Press(s.Press); err != nil {
				t.Fatalf("%s: press %s: %v", fx.Name, s.Press, err)
			}
		case s.Type != "":
			e.Type(s.Type)
		case s.Sleep > 0:
			time.Sleep(time.Duration(s.Sleep) * time.Millisecond)
		case s.Call != "":
			var args any = map[string]any{}
			if len(s.Args) > 0 && string(s.Args) != "null" {
				_ = json.Unmarshal(s.Args, &args)
			}
			raw, _ := json.Marshal(resolve(args, ws, saved))
			var v any
			var err error
			if s.Call == "hello" {
				v = e.Hello(1)
			} else {
				v, err = e.Handle(protocol.AgentOp(s.Call), raw)
			}
			c := replayedCall{op: s.Call, ok: err == nil}
			if err != nil {
				c.reply = err.Error()
			} else {
				value := jsonValue(t, v)
				if s.As != "" {
					saved[s.As] = value
				}
				c.reply = normalize(value, ws)
			}
			calls = append(calls, c)
		}
	}
	var events []map[string]any
	for _, b := range e.Sent() {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		switch m["t"] {
		case "key", "state", "minibuffer":
			events = append(events, normalize(m, ws).(map[string]any))
		}
	}
	return calls, events
}

func fixtureDiffs(fx fixture, calls []replayedCall, events []map[string]any) []string {
	var out []string
	if len(calls) != len(fx.Calls) {
		out = append(out, fmt.Sprintf("%d calls, want %d", len(calls), len(fx.Calls)))
	}
	for i := 0; i < min(len(calls), len(fx.Calls)); i++ {
		w, g := fx.Calls[i], calls[i]
		where := fmt.Sprintf("call %d (%s)", i, w.Op)
		if w.OK != g.ok {
			out = append(out, fmt.Sprintf("%s: ok %v (%v), want %v", where, g.ok, g.reply, w.OK))
			continue
		}
		if w.OK {
			out = append(out, shapeDiffs(w.Reply, g.reply, where)...)
			out = append(out, exactDiffs(w.Reply, g.reply, w.Exact, where)...)
		}
	}
	split := func(evs []map[string]any) (other []map[string]any, states []map[string]any) {
		for _, e := range evs {
			if e["t"] == "state" {
				states = append(states, e)
			} else {
				other = append(other, e)
			}
		}
		return
	}
	wo, ws := split(fx.Events)
	gotOther, gs := split(events)
	if len(wo) != len(gotOther) {
		out = append(out, fmt.Sprintf("key and minibuffer events %v, want %v", summary(gotOther), summary(wo)))
	}
	for i := 0; i < min(len(wo), len(gotOther)); i++ {
		w, g := wo[i], gotOther[i]
		where := fmt.Sprintf("%v event %d", w["t"], i)
		if w["t"] != g["t"] {
			out = append(out, fmt.Sprintf("%s: got %v", where, g["t"]))
			continue
		}
		out = append(out, shapeDiffs(any(w), any(g), where)...)
		out = append(out, exactDiffs(any(w), any(g), fx.EventsExact[w["t"].(string)], where)...)
	}
	// The recorded last state must be among the replay's states (the
	// replay may report more: a batch Emacs never redisplays, so ops send
	// none there).
	if len(ws) > 0 {
		w := ws[len(ws)-1]
		var best []string
		for i, g := range gs {
			d := append(shapeDiffs(any(w), any(g), "last state"), exactDiffs(any(w), any(g), fx.EventsExact["state"], "last state")...)
			if i == 0 || len(d) < len(best) {
				best = d
			}
		}
		if len(gs) == 0 {
			best = []string{"no state events"}
		}
		out = append(out, best...)
	}
	return out
}

func summary(evs []map[string]any) []string {
	var out []string
	for _, e := range evs {
		if k, ok := e["keys"]; ok {
			out = append(out, fmt.Sprint(k))
		} else {
			out = append(out, fmt.Sprintf("mb:%v", e["open"]))
		}
	}
	return out
}

func TestContractFixtures(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			calls, events := replay(t, fx)
			for _, d := range fixtureDiffs(fx, calls, events) {
				t.Error(d)
			}
		})
	}
}

// strict decodes v into out, refusing fields the Go type lacks.
func strict(v any, out any) error {
	b, _ := json.Marshal(v)
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}

func TestFixturesDecodeIntoProtocolTypes(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		for i, c := range fx.Calls {
			if !c.OK {
				continue
			}
			var out any
			switch protocol.AgentOp(c.Op) {
			case protocol.AgentOpSnapshot:
				out = &protocol.WindowSpec{}
			case protocol.AgentOpCheck:
				out = &protocol.AgentCheckResult{}
			case protocol.AgentOpRescue:
				out = &protocol.AgentRescueResult{}
			case protocol.AgentOpDescribe:
				out = &protocol.DescribeResult{}
			case protocol.AgentOpEval:
				out = &protocol.AgentEvalResult{}
			// sessiond discards these replies; the fake's reply types
			// state their shape.
			case protocol.AgentOpResetStep:
				out = &resetReply{}
			case protocol.AgentOpOpen:
				out = &openReply{}
			case protocol.AgentOpSetup:
				out = &setupReply{}
			case protocol.AgentOpSetModules:
				out = &setModulesReply{}
			case "hello":
				out = &protocol.AgentHelloMsg{}
			default:
				t.Errorf("%s call %d: no Go result type for op %s", fx.Name, i, c.Op)
				continue
			}
			if err := strict(c.Reply, out); err != nil {
				t.Errorf("%s call %d (%s): %v", fx.Name, i, c.Op, err)
			}
		}
		for i, ev := range fx.Events {
			b, _ := json.Marshal(ev)
			m, err := protocol.DecodeAgentMessage(b)
			if err != nil {
				t.Errorf("%s event %d: %v", fx.Name, i, err)
				continue
			}
			if err := strict(ev, m); err != nil {
				t.Errorf("%s event %d (%v): %v", fx.Name, i, ev["t"], err)
			}
		}
	}
}
