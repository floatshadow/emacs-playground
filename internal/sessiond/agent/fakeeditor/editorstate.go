package fakeeditor

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The editor state a checkpoint keeps beside the files: the file buffers'
// unsaved text and undo, and the session variables. sessiond reads and
// restores them through eval forms (internal/sessiond/unsaved.go); the
// fake recognises those forms by their fixed text, as it recognises the
// open and flush forms, and answers them from the model. The undo and
// globals values are opaque to sessiond, so the fake prints them as JSON
// (its own format) instead of Lisp. The functions below are the bodies of
// the typed capture-editor, restore-editor and modified-files ops the
// agent will get.

// litBody matches an Emacs Lisp string literal as sessiond's elispString
// writes it; lit captures one.
const (
	litBody = `"(?:[^"\\]|\\.)*"`
	lit     = `(` + litBody + `)`
)

var (
	modifiedFilesFormRe = regexp.MustCompile(`^\(let \(\(ws ` + lit + `\) out\)\n  \(dolist \(b \(buffer-list\)\)\n`)
	buffersFormRe       = regexp.MustCompile(`^\(let \(\(ws ` + lit + `\) \(total 0\) out\)\n  \(dolist \(b \(buffer-list\)\)\n`)
	buffersTextMax      = regexp.MustCompile(`\(<= \(buffer-size\) (\d+)\)`)
	buffersUndoMax      = regexp.MustCompile(`\(<= \(length undo\) (\d+)\) undo\)`)
	buffersTotalMax     = regexp.MustCompile(`\(<= \(setq total \(\+ total size\)\) (\d+)\)`)
	restoreBuffersRe    = regexp.MustCompile(`^\(let \(\(ws ` + lit + `\) \(n 0\)\)\n  \(dolist \(e '\(`)
	restoreBufferEntry  = regexp.MustCompile(`^\n    \(` + lit + ` (nil|` + litBody + `) (nil|` + litBody + `) (-?\d+)\)`)
	restoreBuffersBody  = "))\n    (condition-case nil\n"
	globalsFormRe       = regexp.MustCompile(`^\(let\* \(\(print-length nil\) \(print-level nil\)\n       \(str \(lambda `)
	globalsVars         = regexp.MustCompile(`'\(([a-z0-9 -]+)\)\)\)\)\n       \(printed \(prin1-to-string out\)\)\)\n  \(and \(<= \(length printed\) (\d+)\) printed\)\)$`)
	restoreGlobalsRe    = regexp.MustCompile(`^\(condition-case nil\n    \(let \(\(g \(car \(read-from-string ` + lit + `\)\)\)\)\n      \(dolist \(e g\) `)
)

// evalEditorState answers the checkpoint forms; ok is false for any other
// form.
func (e *Editor) evalEditorState(form string) (v any, ok bool) {
	switch {
	case modifiedFilesFormRe.MatchString(form) && strings.HasSuffix(form, "(vconcat out))"):
		return e.m.ModifiedFiles(), true
	case buffersFormRe.MatchString(form):
		text, undo, total := formInt(buffersTextMax, form), formInt(buffersUndoMax, form), formInt(buffersTotalMax, form)
		if text < 0 || undo < 0 || total < 0 {
			return nil, false
		}
		return e.captureBuffers(text, undo, total), true
	case restoreBuffersRe.MatchString(form):
		entries, ok := parseRestoreBuffers(form)
		if !ok {
			return nil, false
		}
		n := e.restoreBuffers(entries)
		e.stateLocked()
		return n, true
	case globalsFormRe.MatchString(form):
		m := globalsVars.FindStringSubmatch(form)
		if m == nil {
			return nil, false
		}
		limit, _ := strconv.Atoi(m[2])
		if printed, ok := e.captureGlobals(strings.Fields(m[1]), limit); ok {
			return printed, true
		}
		return nil, true
	case restoreGlobalsRe.MatchString(form):
		m := restoreGlobalsRe.FindStringSubmatch(form)
		if n, ok := e.restoreGlobals(elispUnquote(m[1])); ok {
			return n, true
		}
		return nil, true
	}
	return nil, false
}

func formInt(re *regexp.Regexp, form string) int {
	m := re.FindStringSubmatch(form)
	if m == nil {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

// captureBuffers is buffersForm: for each file buffer, [modified, text
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

// restoreEntry is one buffer of restoreBuffersForm.
type restoreEntry struct {
	path       string
	text, undo *string
	point      int
}

func parseRestoreBuffers(form string) ([]restoreEntry, bool) {
	rest := form[len(restoreBuffersRe.FindString(form)):]
	var out []restoreEntry
	for {
		m := restoreBufferEntry.FindStringSubmatch(rest)
		if m == nil {
			break
		}
		rest = rest[len(m[0]):]
		en := restoreEntry{path: elispUnquote(m[1])}
		if m[2] != "nil" {
			s := elispUnquote(m[2])
			en.text = &s
		}
		if m[3] != "nil" {
			s := elispUnquote(m[3])
			en.undo = &s
		}
		en.point, _ = strconv.Atoi(m[4])
		out = append(out, en)
	}
	return out, strings.HasPrefix(rest, restoreBuffersBody)
}

// restoreBuffers is restoreBuffersForm: each entry's unsaved text (the
// buffer visited when needed, marked modified, no undo recorded), undo
// history and point, also in the windows showing the buffer. An entry
// that fails (unreadable undo) is skipped. It returns the number restored.
func (e *Editor) restoreBuffers(entries []restoreEntry) int {
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

// captureGlobals is globalsForm: each variable's value (lists cut to 100
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

// restoreGlobals is restoreGlobalsForm: every recorded variable is set
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
