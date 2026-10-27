package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// FrameKind names one of the two frame kinds of the single Emacs daemon
// (ADR-006). It is also the "mode" of a session: which frame the browser
// shows.
type FrameKind string

// Frame kinds.
const (
	FrameTerminal FrameKind = "terminal"
	FrameGUI      FrameKind = "gui"
)

// MinibufferKind distinguishes a real minibuffer prompt from isearch, which
// does not use the minibuffer but is reported as a prompt-like state.
type MinibufferKind string

// Minibuffer kinds.
const (
	MinibufferPrompt  MinibufferKind = "prompt"
	MinibufferIsearch MinibufferKind = "isearch"
)

// State is the snapshot of the selected window's buffer and the frame layout
// that the agent emits after commands and UI changes (api.md §5, emacs.md
// §10.2). sessiond forwards it unchanged apart from stamping seq and ts.
// The browser's exits table and the step engine's state and layout
// conditions read it; field names are the paths used by those predicates
// (for example "minibuffer.kind" or "windows.length").
type State struct {
	Envelope
	// Buf is the selected window's buffer name, e.g. "main.rs" or "*Help*".
	Buf string `json:"buf"`
	// File is the absolute path the buffer visits, or null.
	File *string `json:"file"`
	// Mode is the major mode symbol, e.g. "rust-ts-mode".
	Mode string `json:"mode"`
	// ModeName is the mode line lighter of the major mode, e.g. "Rust".
	ModeName string `json:"modeName"`
	// Minors lists the enabled minor mode symbols the agent considers
	// interesting (eglot--managed-mode, flymake-mode, overwrite-mode, ...).
	Minors []string `json:"minors"`
	// Modified is buffer-modified-p.
	Modified bool `json:"modified"`
	// Readonly is buffer-read-only.
	Readonly bool `json:"readonly"`
	// Narrowed is buffer-narrowed-p.
	Narrowed bool `json:"narrowed"`
	// Overwrite is true when overwrite-mode is on.
	Overwrite bool `json:"overwrite"`
	// Line is the 1-based line of point.
	Line int `json:"line"`
	// Col is the 0-based column of point (current-column).
	Col int `json:"col"`
	// Point is the 1-based buffer position of point.
	Point int `json:"point"`
	// MarkActive is region-active-p.
	MarkActive bool `json:"markActive"`
	// Windows lists the live windows of the selected frame in window-list
	// order (selected window first is NOT guaranteed; use Selected).
	Windows []Window `json:"windows"`
	// Layout is the window tree of the selected frame.
	Layout Layout `json:"layout"`
	// Frame is the kind of the selected frame.
	Frame FrameKind `json:"frame"`
	// Frames is (length (frame-list)), including the invisible daemon frame
	// as Emacs reports it.
	Frames int `json:"frames"`
	// Tabs describes tab-bar tabs of the selected frame.
	Tabs Tabs `json:"tabs"`
	// Minibuffer is the active minibuffer, or null when none is open.
	Minibuffer *Minibuffer `json:"minibuffer"`
	// Isearch is true while an incremental search is active.
	Isearch bool `json:"isearch"`
	// Recursive is (recursion-depth).
	Recursive int `json:"recursive"`
	// Macro is true while a keyboard macro is being defined.
	Macro bool `json:"macro"`
	// PrefixArg is the printed current-prefix-arg ("(4)", "-", "3"), or null.
	PrefixArg *string `json:"prefixArg"`
	// ReadOnlyReason explains why the buffer is read-only when the agent
	// knows ("special-mode", "file is write-protected", "view-mode"), or null.
	ReadOnlyReason *string `json:"readOnlyReason"`
	// DiagAtPoint is the Flymake diagnostic at point, or null.
	DiagAtPoint *Diagnostic `json:"diagAtPoint"`
	// Waiting names a loop that reads keys of its own (a menu-bar menu,
	// avy, which-key's pages), or null. Such a loop is neither a prompt nor
	// a recursive edit, and every key goes to it.
	Waiting *Waiting `json:"waiting"`
}

// Type implements Event.
func (*State) Type() EventType { return EventState }

// WaitingKind names the loop of State.Waiting.
type WaitingKind string

// Waiting loops (playground-agent-waiting-loops).
const (
	// WaitingMenu is a menu-bar menu in a terminal frame (F10, a click).
	WaitingMenu WaitingKind = "menu"
	// WaitingAvy is avy reading the characters or letters of its target.
	WaitingAvy WaitingKind = "avy"
	// WaitingWhichKey is which-key paging through a pending prefix's keys.
	WaitingWhichKey WaitingKind = "which-key"
)

// Waiting describes a loop that reads keys of its own.
type Waiting struct {
	// Kind is the loop.
	Kind WaitingKind `json:"kind"`
	// What says what is waiting, for people ("a menu").
	What string `json:"what"`
	// Leave is the keys that leave it ("C-g"; "a" for which-key, where
	// C-g only goes back to the prefix). Rescue sends them.
	Leave string `json:"leave"`
}

// Window is one live window of the selected frame.
type Window struct {
	// ID is a small integer stable for the lifetime of the window, assigned
	// by the agent; Layout leaves refer to it.
	ID int `json:"id"`
	// Buf is the buffer name shown in the window.
	Buf string `json:"buf"`
	// Mode is that buffer's major mode symbol.
	Mode string `json:"mode"`
	// Selected is true for the selected window.
	Selected bool `json:"selected"`
	// Dedicated is true when window-dedicated-p is non-nil.
	Dedicated bool `json:"dedicated"`
	// Side is the window-side parameter ("left", "right", "top", "bottom")
	// for side windows, or null.
	Side *string `json:"side"`
}

// Tabs describes the tab bar of the selected frame.
type Tabs struct {
	// Count is the number of tabs (1 when tab-bar-mode is off).
	Count int `json:"count"`
	// Current is the current tab's name.
	Current string `json:"current"`
}

// Minibuffer describes an open minibuffer or isearch.
type Minibuffer struct {
	// Depth is (minibuffer-depth); 0 for isearch.
	Depth int `json:"depth"`
	// Prompt is (minibuffer-prompt), e.g. "Find file: ", or "I-search: ".
	Prompt string `json:"prompt"`
	// Contents is (minibuffer-contents-no-properties) or the isearch string.
	Contents string `json:"contents"`
	// Kind is prompt or isearch.
	Kind MinibufferKind `json:"kind"`
}

// DiagnosticSeverity is a Flymake diagnostic type reduced to three levels.
type DiagnosticSeverity string

// Diagnostic severities.
const (
	SeverityError   DiagnosticSeverity = "error"
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityNote    DiagnosticSeverity = "note"
)

// Diagnostic is the Flymake diagnostic at point (playground--diag-at-point).
type Diagnostic struct {
	Severity DiagnosticSeverity `json:"severity"`
	// Message is the diagnostic text, first line only.
	Message string `json:"message"`
	// Source is the Flymake backend or LSP server, e.g. "eglot", "rustc".
	Source string `json:"source,omitempty"`
	// Line and Col locate the start of the diagnostic (1-based line,
	// 0-based column).
	Line int `json:"line"`
	Col  int `json:"col"`
}

// SplitKind is the orientation of a window combination. The names follow
// the curriculum: vsplit stacks windows vertically (one above the other,
// C-x 2, Emacs "vertical combination"); hsplit places them side by side
// (C-x 3, Emacs "horizontal combination").
type SplitKind string

// Split kinds.
const (
	VSplit SplitKind = "vsplit"
	HSplit SplitKind = "hsplit"
)

// Layout is the window tree of a frame, reduced from window-state-get to
// splits and window ids (emacs.md §10.2).
//
// JSON form: a leaf is the window id as a number (3); a combination is an
// array whose first element is "vsplit" or "hsplit" followed by two or more
// children (["vsplit", 3, ["hsplit", 7, 9]]). The zero Layout (no window)
// encodes as null.
//
// Schema generators must special-case this type (it has custom JSON); the
// TypeScript form is `type Layout = number | [SplitKind, ...Layout[]] | null`.
type Layout struct {
	// Split is empty for a leaf.
	Split SplitKind
	// Window is the leaf's window id (Split == "").
	Window int
	// Children are the combination's children in visual order (top to
	// bottom for vsplit, left to right for hsplit).
	Children []Layout
}

// IsLeaf reports whether l is a single window.
func (l Layout) IsLeaf() bool { return l.Split == "" }

// IsZero reports whether l is the empty layout (encoded as null).
func (l Layout) IsZero() bool { return l.Split == "" && l.Window == 0 && len(l.Children) == 0 }

// Leaves returns the window ids of the layout in visual order.
func (l Layout) Leaves() []int {
	if l.IsZero() {
		return nil
	}
	if l.IsLeaf() {
		return []int{l.Window}
	}
	var out []int
	for _, c := range l.Children {
		out = append(out, c.Leaves()...)
	}
	return out
}

// String renders the layout as an s-expression with window ids, e.g.
// "(vsplit 3 (hsplit 7 9))".
func (l Layout) String() string {
	if l.IsZero() {
		return "nil"
	}
	if l.IsLeaf() {
		return strconv.Itoa(l.Window)
	}
	parts := []string{string(l.Split)}
	for _, c := range l.Children {
		parts = append(parts, c.String())
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// MarshalJSON implements json.Marshaler.
func (l Layout) MarshalJSON() ([]byte, error) {
	if l.IsZero() {
		return []byte("null"), nil
	}
	if l.IsLeaf() {
		return []byte(strconv.Itoa(l.Window)), nil
	}
	arr := make([]any, 0, len(l.Children)+1)
	arr = append(arr, l.Split)
	for _, c := range l.Children {
		arr = append(arr, c)
	}
	return json.Marshal(arr)
}

// UnmarshalJSON implements json.Unmarshaler.
func (l *Layout) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	*l = Layout{}
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] != '[' {
		var id int
		if err := json.Unmarshal(data, &id); err != nil {
			return fmt.Errorf("layout leaf: %w", err)
		}
		l.Window = id
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("layout: %w", err)
	}
	if len(raw) < 2 {
		return fmt.Errorf("layout: combination needs a split kind and at least one child")
	}
	var kind string
	if err := json.Unmarshal(raw[0], &kind); err != nil {
		return fmt.Errorf("layout: first element must be a split kind: %w", err)
	}
	switch SplitKind(kind) {
	case VSplit, HSplit:
	default:
		return fmt.Errorf("layout: unknown split kind %q", kind)
	}
	l.Split = SplitKind(kind)
	l.Children = make([]Layout, len(raw)-1)
	for i, r := range raw[1:] {
		if err := l.Children[i].UnmarshalJSON(r); err != nil {
			return err
		}
	}
	return nil
}

// OtherWindows returns the non-selected windows. Exit-table predicates refer
// to them as "otherWindows" (CURRICULUM.md Part B.4).
func (s *State) OtherWindows() []Window {
	var out []Window
	for _, w := range s.Windows {
		if !w.Selected {
			out = append(out, w)
		}
	}
	return out
}

// SelectedWindow returns the selected window, or false when the agent sent
// no window marked selected.
func (s *State) SelectedWindow() (Window, bool) {
	for _, w := range s.Windows {
		if w.Selected {
			return w, true
		}
	}
	return Window{}, false
}

// BufferOf returns the buffer name shown in window id, or "".
func (s *State) BufferOf(id int) string {
	for _, w := range s.Windows {
		if w.ID == id {
			return w.Buf
		}
	}
	return ""
}
