package fakeeditor

import (
	"encoding/json"
	"slices"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// The editor state a checkpoint keeps beside the files: the file buffers'
// unsaved text and undo, and the session variables, answered from the
// model for capture-editor and restore-editor. The undo and globals values
// are opaque to sessiond, so the fake prints them as JSON (its own format)
// instead of Lisp.

type captureReply struct {
	Spec    snapshotReply     `json:"spec"`
	Buffers map[string][3]any `json:"buffers"`
	Globals any               `json:"globals"`
}

type restoreReply struct {
	Reverted int      `json:"reverted"`
	Killed   int      `json:"killed"`
	Buffers  int      `json:"buffers"`
	Globals  int      `json:"globals"`
	Degraded []string `json:"degraded"`
}

// captureEditor is playground--op-capture-editor.
func (e *Editor) captureEditor(a *protocol.AgentCaptureEditorArgs) captureReply {
	r := captureReply{Spec: e.snapshot(), Buffers: map[string][3]any{}}
	if a.MaxTotal > 0 {
		r.Buffers = e.captureBuffers(a.MaxText, a.MaxUndo, a.MaxTotal)
	}
	if len(a.Vars) > 0 {
		if printed, ok := e.captureGlobals(a.Vars, a.MaxGlobals); ok {
			r.Globals = printed
		}
	}
	return r
}

// restoreEditor is playground--restore-editor-now: the step's start
// (reset-step), then each buffer's unsaved text, undo and point (from the
// spec), then the session variables.
func (e *Editor) restoreEditor(a *protocol.AgentRestoreEditorArgs) restoreReply {
	reset := e.resetStep(&a.Spec)
	points := map[string]int{}
	for _, f := range a.Spec.Files {
		points[f.Path] = f.Point
	}
	r := restoreReply{Reverted: reset.Reverted, Killed: reset.Killed, Degraded: []string{}}
	paths := make([]string, 0, len(a.Buffers))
	for p := range a.Buffers {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var entries []restoreEntry
	for _, p := range paths {
		b := a.Buffers[p]
		en := restoreEntry{path: p, undo: b.Undo, point: points[p]}
		if b.Modified {
			en.text = b.Text
		}
		entries = append(entries, en)
	}
	r.Buffers = e.restoreBuffers(entries, &r.Degraded)
	if a.Globals != "" {
		if n, ok := e.restoreGlobals(a.Globals); ok {
			r.Globals = n
		} else {
			r.Degraded = append(r.Degraded, "session variables: unreadable")
		}
	}
	return r
}

// captureBuffers is playground--capture-buffers: for each file buffer, [modified, text
// when modified, printed undo] when it has text or undo to keep, within
// the bounds (a buffer over them keeps less; the running total counts
// every buffer, kept or not).
func (e *Editor) captureBuffers(maxText, maxUndo, maxTotal int) map[string][3]any {
	out := map[string][3]any{}
	total := 0
	for _, b := range e.m.Buffers {
		if b.Path == "" {
			continue
		}
		var text, undo any
		size := 0
		if b.Modified && runeLen(b.Text) <= maxText {
			text = b.Text
			size += runeLen(b.Text)
		}
		if len(b.Undo) > 0 {
			if p, err := json.Marshal(b.Undo); err == nil && len(p) <= maxUndo {
				undo = string(p)
				size += len(p)
			}
		}
		if text == nil && undo == nil {
			continue
		}
		if total += size; total <= maxTotal {
			out[b.Path] = [3]any{b.Modified, text, undo}
		}
	}
	return out
}

// restoreEntry is one buffer of restore-editor.
type restoreEntry struct {
	path       string
	text, undo *string
	point      int
}

// restoreBuffers is playground--restore-buffers: each entry's unsaved
// text (the buffer visited when needed, marked modified, no undo
// recorded), undo history and point, also in the windows showing the
// buffer. An entry that fails (unreadable undo) is skipped and named in
// degraded. It returns the number restored.
func (e *Editor) restoreBuffers(entries []restoreEntry, degraded *[]string) int {
	n := 0
	for _, en := range entries {
		rel, ok := e.rel(en.path)
		if !ok || rel == "" {
			continue
		}
		b := e.visiting(rel)
		if b == nil && en.text != nil {
			b = e.visit(rel)
		}
		if b == nil {
			continue
		}
		if en.text != nil && b.Text != *en.text {
			b.Text, b.Modified = *en.text, true
		}
		if en.undo != nil {
			var undo []Edit
			if json.Unmarshal([]byte(*en.undo), &undo) != nil {
				*degraded = append(*degraded, en.path+": unreadable undo")
				continue
			}
			b.Undo = undo
		}
		if en.point > 0 {
			b.Point = clampPoint(b.Text, en.point)
			for i := range e.m.Windows {
				if e.m.Windows[i].Buf == b.Name {
					e.m.Windows[i].Point = b.Point
				}
			}
		}
		n++
	}
	return n
}

// captureGlobals is playground--capture-globals: each variable's value (lists cut to 100
// entries, null for an unset one) printed; ok is false over limit.
func (e *Editor) captureGlobals(vars []string, limit int) (string, bool) {
	out := make(map[string]any, len(vars))
	for _, v := range vars {
		val := e.m.Globals[v]
		if l, isList := val.([]string); isList {
			val = slices.Clone(l[:min(len(l), 100)])
		}
		out[v] = val
	}
	b, err := json.Marshal(out)
	if err != nil || len(b) > limit {
		return "", false
	}
	return string(b), true
}

// restoreGlobals is playground--restore-globals: every recorded variable is set
// back (an unset one unset); ok is false when printed does not read.
func (e *Editor) restoreGlobals(printed string) (int, bool) {
	var g map[string]json.RawMessage
	if json.Unmarshal([]byte(printed), &g) != nil {
		return 0, false
	}
	for k, raw := range g {
		var list []string
		var str string
		var val any
		switch {
		case string(raw) == "null":
			delete(e.m.Globals, k)
			continue
		case json.Unmarshal(raw, &list) == nil:
			val = list
		case json.Unmarshal(raw, &str) == nil:
			val = str
		default:
			_ = json.Unmarshal(raw, &val)
		}
		e.m.Globals[k] = val
	}
	return len(g), true
}
