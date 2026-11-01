package fakeeditor

import (
	"path"
	"slices"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// The helpers below run with e.mu held.

func (e *Editor) buffer(name string) *Buffer {
	for _, b := range e.m.Buffers {
		if b.Name == name {
			return b
		}
	}
	return nil
}

func (e *Editor) visiting(rel string) *Buffer {
	for _, b := range e.m.Buffers {
		if b.Path != "" && b.Path == rel {
			return b
		}
	}
	return nil
}

// addBuffer appends b to the buffer list (new buffers go last, as
// get-buffer-create puts them).
func (e *Editor) addBuffer(b *Buffer) *Buffer {
	if b.Point == 0 {
		b.Point = 1
	}
	e.m.Buffers = append(e.m.Buffers, b)
	return b
}

// record moves b to the front of the buffer list (select-window,
// switch-to-buffer).
func (e *Editor) record(b *Buffer) {
	if i := slices.Index(e.m.Buffers, b); i > 0 {
		e.m.Buffers = slices.Delete(e.m.Buffers, i, i+1)
		e.m.Buffers = slices.Insert(e.m.Buffers, 0, b)
	}
}

// killBuffer removes b; windows showing it switch to another buffer.
func (e *Editor) killBuffer(b *Buffer) {
	i := slices.Index(e.m.Buffers, b)
	if i < 0 {
		return
	}
	e.m.Buffers = slices.Delete(e.m.Buffers, i, i+1)
	for wi := range e.m.Windows {
		if e.m.Windows[wi].Buf == b.Name {
			o := e.otherBuffer(b.Name)
			e.m.Windows[wi].Buf, e.m.Windows[wi].Point = o.Name, o.Point
		}
	}
	for ti := range e.m.Tabs {
		for wi := range e.m.Tabs[ti].Windows {
			if e.m.Tabs[ti].Windows[wi].Buf == b.Name {
				o := e.otherBuffer(b.Name)
				e.m.Tabs[ti].Windows[wi].Buf, e.m.Tabs[ti].Windows[wi].Point = o.Name, o.Point
			}
		}
	}
}

// otherBuffer is the buffer a window shows when its buffer goes: the most
// recent visible-to-the-learner buffer other than name, else *scratch*.
func (e *Editor) otherBuffer(name string) *Buffer {
	for _, b := range e.m.Buffers {
		if b.Name != name && !strings.HasPrefix(b.Name, " ") && b.Mode != modeMessages {
			return b
		}
	}
	if b := e.buffer("*scratch*"); b != nil {
		return b
	}
	return e.addBuffer(&Buffer{Name: "*scratch*", Mode: modeScratch})
}

func (e *Editor) selectedIndex() int {
	for i, w := range e.m.Windows {
		if w.Selected {
			return i
		}
	}
	return 0
}

func (e *Editor) selected() *Window { return &e.m.Windows[e.selectedIndex()] }

func (e *Editor) window(id int) *Window {
	for i := range e.m.Windows {
		if e.m.Windows[i].ID == id {
			return &e.m.Windows[i]
		}
	}
	return nil
}

// current is the selected window's buffer.
func (e *Editor) current() *Buffer {
	if b := e.buffer(e.selected().Buf); b != nil {
		return b
	}
	return e.otherBuffer("")
}

// syncWindows orders Windows as the layout's leaves.
func (e *Editor) syncWindows() {
	ids := e.m.Layout.Leaves()
	slices.SortStableFunc(e.m.Windows, func(a, b Window) int { return indexOf(ids, a.ID) - indexOf(ids, b.ID) })
}

// setPoint moves point in the selected window and its buffer.
func (e *Editor) setPoint(p int) {
	b := e.current()
	b.Point = clampPoint(b.Text, p)
	e.selected().Point = b.Point
}

// selectWindow selects window id and records its buffer.
func (e *Editor) selectWindow(id int) {
	cur := e.selected()
	if b := e.buffer(cur.Buf); b != nil {
		cur.Point = b.Point
	}
	for i := range e.m.Windows {
		e.m.Windows[i].Selected = e.m.Windows[i].ID == id
	}
	w := e.selected()
	if b := e.buffer(w.Buf); b != nil {
		b.Point = clampPoint(b.Text, w.Point)
		e.record(b)
	}
}

// showIn makes window w show b at b's point.
func (e *Editor) showIn(w *Window, b *Buffer) {
	if w.Selected {
		if old := e.buffer(w.Buf); old != nil && old != b {
			old.Point = clampPoint(old.Text, w.Point)
		}
	}
	w.Buf, w.Point = b.Name, b.Point
}

// switchTo shows b in the selected window and records it (switch-to-buffer).
func (e *Editor) switchTo(b *Buffer) {
	e.showIn(e.selected(), b)
	e.record(b)
}

func (e *Editor) newWindowID() int {
	id := e.nextWin
	e.nextWin++
	return id
}

// split splits the selected window; the new window shows the same buffer
// and the selection stays.
func (e *Editor) split(kind protocol.SplitKind) *Window { return e.splitWindow(e.selected().ID, kind) }

// splitWindow splits window id; the new window, after it, shows the same
// buffer.
func (e *Editor) splitWindow(id int, kind protocol.SplitKind) *Window {
	w := *e.window(id)
	if w.Selected {
		if b := e.buffer(w.Buf); b != nil {
			w.Point = b.Point
		}
	}
	nid := e.newWindowID()
	e.m.Layout = splitLeaf(e.m.Layout, id, nid, kind)
	e.m.Windows = append(e.m.Windows, Window{ID: nid, Buf: w.Buf, Point: w.Point})
	e.syncWindows()
	return e.window(nid)
}

// onlyWindow deletes the other windows.
func (e *Editor) onlyWindow() {
	sel := *e.selected()
	e.m.Windows = []Window{sel}
	e.m.Layout = leaf(sel.ID)
}

// deleteWindow deletes window id (not the last one).
func (e *Editor) deleteWindow(id int) {
	if len(e.m.Windows) < 2 {
		return
	}
	wasSelected := e.window(id).Selected
	ids := e.m.Layout.Leaves()
	i := indexOf(ids, id)
	e.m.Layout, _ = removeLeaf(e.m.Layout, id)
	e.m.Windows = slices.DeleteFunc(e.m.Windows, func(w Window) bool { return w.ID == id })
	e.syncWindows()
	if wasSelected {
		next := e.m.Layout.Leaves()[max(i-1, 0)]
		e.selectWindow(next)
	}
}

// displayBuffer shows b without selecting it (display-buffer): in a
// window already showing it, else in another window, else in a new
// window below the selected one.
func (e *Editor) displayBuffer(b *Buffer) {
	for i := range e.m.Windows {
		if e.m.Windows[i].Buf == b.Name {
			return
		}
	}
	for i := range e.m.Windows {
		if !e.m.Windows[i].Selected {
			e.showIn(&e.m.Windows[i], b)
			return
		}
	}
	w := e.split(protocol.VSplit)
	e.showIn(w, b)
}

// visit returns the buffer visiting rel, visiting it when needed (an
// absent file gives an empty buffer).
func (e *Editor) visit(rel string) *Buffer {
	if b := e.visiting(rel); b != nil {
		return b
	}
	text, _ := e.readFile(rel)
	name := path.Base(rel)
	if other := e.buffer(name); other != nil && other.Path != "" {
		// uniquify (post-forward-angle-brackets): both get their directory.
		other.Name = name + "<" + path.Base(path.Dir(other.Path)) + ">"
		e.renameInWindows(name, other.Name)
		name = name + "<" + path.Base(path.Dir(rel)) + ">"
	}
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	return e.addBuffer(&Buffer{Name: name, Path: rel, Dir: dir, Mode: modeFor(rel), Text: text, Saved: text})
}

func (e *Editor) renameInWindows(from, to string) {
	for i := range e.m.Windows {
		if e.m.Windows[i].Buf == from {
			e.m.Windows[i].Buf = to
		}
	}
}

// modeFor is the major mode batch Emacs picks for a file.
func modeFor(rel string) string {
	switch path.Ext(rel) {
	case ".txt":
		return "text-mode"
	case ".el":
		return "emacs-lisp-mode"
	case ".py":
		return "python-mode"
	case ".c", ".h":
		return "c-mode"
	case ".sh":
		return "sh-mode"
	}
	return modeFundamental
}

// leafNameOf is the name a window's buffer has in layout patterns.
func (e *Editor) leafNameOf(buf string) string {
	if b := e.buffer(buf); b != nil && b.Path != "" {
		return leafName(b.Path)
	}
	return leafName(buf)
}

// findBuffer resolves a layout leaf (playground--find-buffer): a buffer
// name, a workspace path, a file name, or with create a file to visit or
// a special buffer to make.
func (e *Editor) findBuffer(name string, create bool) *Buffer {
	if b := e.buffer(name); b != nil {
		return b
	}
	if rel, ok := e.rel(name); ok && rel != "" {
		if b := e.visiting(rel); b != nil {
			return b
		}
		if _, exists := e.readFile(rel); exists && create {
			return e.visit(rel)
		}
	}
	for _, b := range e.m.Buffers {
		if (b.Path != "" && path.Base(b.Path) == name) || stripUniquify(b.Name) == name {
			return b
		}
	}
	if !create {
		return nil
	}
	if strings.HasPrefix(name, "*") {
		mode := modeFundamental
		if name == "*compilation*" {
			mode = modeCompilation
		}
		return e.addBuffer(&Buffer{Name: name, Mode: mode})
	}
	if rel, ok := e.rel(name); ok && e.isDir(path.Dir(rel)) {
		return e.visit(rel)
	}
	return nil
}

func stripUniquify(name string) string {
	if i := strings.LastIndexByte(name, '<'); i > 0 && strings.HasSuffix(name, ">") {
		return name[:i]
	}
	return name
}

// applyPattern arranges the selected window by a layout pattern.
func (e *Editor) applyPattern(p pattern) {
	e.onlyWindow()
	e.buildPattern(p, e.selected().ID)
}

func (e *Editor) buildPattern(p pattern, id int) {
	if p.split == "" {
		if p.name == "" || p.name == "single" || p.name == "*" {
			return
		}
		if b := e.findBuffer(p.name, true); b != nil {
			e.showIn(e.window(id), b)
		}
		return
	}
	// Like playground--build-layout: the newest window is split each time.
	ids := []int{id}
	for range len(p.children) - 1 {
		ids = append(ids, e.splitWindow(ids[len(ids)-1], p.split).ID)
	}
	for i, c := range p.children {
		e.buildPattern(c, ids[i])
	}
}

// applyWindowState puts back a state printed by the fake (windowState).
func (e *Editor) applyWindowState(ws windowState) {
	e.m.Layout = cloneLayout(ws.Layout)
	e.m.Windows = nil
	maxID := 0
	for _, sw := range ws.Windows {
		var b *Buffer
		if sw.Path != "" {
			b = e.visit(sw.Path)
		} else {
			b = e.findBuffer(sw.Buf, true)
		}
		if b == nil {
			b = e.otherBuffer("")
		}
		e.m.Windows = append(e.m.Windows, Window{ID: sw.ID, Buf: b.Name, Point: clampPoint(b.Text, sw.Point), Selected: sw.ID == ws.Selected})
		maxID = max(maxID, sw.ID)
	}
	e.nextWin = max(e.nextWin, maxID+1)
	if !slices.ContainsFunc(e.m.Windows, func(w Window) bool { return w.Selected }) {
		e.m.Windows[0].Selected = true
	}
	e.syncWindows()
	if b := e.buffer(e.selected().Buf); b != nil {
		b.Point = e.selected().Point
	}
}

// selectLeaf selects the window showing the buffer named by a leaf.
func (e *Editor) selectLeaf(name string) {
	b := e.findBuffer(name, false)
	if b == nil {
		return
	}
	for _, w := range e.m.Windows {
		if w.Buf == b.Name {
			e.selectWindow(w.ID)
			return
		}
	}
}

// --- tabs ------------------------------------------------------------------

func (e *Editor) currentTab() *Tab { return &e.m.Tabs[e.m.TabIndex-1] }

func (e *Editor) tabName(i int) string {
	t := e.m.Tabs[i]
	if t.Name != "" {
		return t.Name
	}
	if i == e.m.TabIndex-1 {
		return e.selected().Buf
	}
	for _, w := range t.Windows {
		if w.Selected {
			return w.Buf
		}
	}
	return ""
}

// selectTab switches to tab n (1-based), swapping window configurations.
func (e *Editor) selectTab(n int) {
	if n == e.m.TabIndex || n < 1 || n > len(e.m.Tabs) {
		return
	}
	cur := e.currentTab()
	cur.Windows, cur.Layout = slices.Clone(e.m.Windows), cloneLayout(e.m.Layout)
	e.m.TabIndex = n
	t := e.currentTab()
	e.m.Windows, e.m.Layout = t.Windows, t.Layout
	t.Windows, t.Layout = nil, protocol.Layout{}
	e.selectWindow(e.selected().ID)
}

// newTab opens a tab after the current one showing the current buffer
// alone (tab-bar-new-tab-choice t).
func (e *Editor) newTab() {
	cur := e.currentTab()
	cur.Windows, cur.Layout = slices.Clone(e.m.Windows), cloneLayout(e.m.Layout)
	b := e.current()
	id := e.newWindowID()
	e.m.Tabs = slices.Insert(e.m.Tabs, e.m.TabIndex, Tab{})
	e.m.TabIndex++
	e.m.Windows = []Window{{ID: id, Buf: b.Name, Point: b.Point, Selected: true}}
	e.m.Layout = leaf(id)
}

// closeTab closes tab n (1-based); the last tab stays.
func (e *Editor) closeTab(n int) {
	if len(e.m.Tabs) < 2 || n < 1 || n > len(e.m.Tabs) {
		return
	}
	if n == e.m.TabIndex {
		next := n + 1
		if next > len(e.m.Tabs) {
			next = n - 1
		}
		e.selectTab(next)
	}
	e.m.Tabs = slices.Delete(e.m.Tabs, n-1, n)
	if e.m.TabIndex > n {
		e.m.TabIndex--
	}
}

// --- state ---------------------------------------------------------------------

// stateObject is the agent's state message (playground--state).
func (e *Editor) stateObject() *protocol.State {
	b := e.current()
	line, col := lineCol(b.Text, b.Point)
	st := &protocol.State{
		Envelope: protocol.Envelope{T: protocol.EventState},
		Buf:      b.Name, Mode: b.Mode, ModeName: "", Minors: []string{},
		Modified: b.Modified, Readonly: b.ReadOnly, Narrowed: b.Narrowed, Overwrite: b.Overwrite,
		Line: line, Col: col, Point: b.Point,
		Layout: cloneLayout(e.m.Layout), Frame: e.m.LearnerFrame, Frames: 1,
		Tabs:       protocol.Tabs{Count: len(e.m.Tabs), Current: e.tabName(e.m.TabIndex - 1)},
		Isearch:    e.isearch != nil,
		Recursive:  e.m.Recursion,
		Minibuffer: e.minibufferObject(),
	}
	if b.Path != "" {
		f := e.abs(b.Path)
		st.File = &f
	}
	if b.Overwrite {
		st.Minors = append(st.Minors, "overwrite-mode")
	}
	for _, w := range e.m.Windows {
		pw := protocol.Window{ID: w.ID, Buf: w.Buf, Selected: w.Selected}
		if wb := e.buffer(w.Buf); wb != nil {
			pw.Mode = wb.Mode
		}
		if w.Side != "" {
			s := w.Side
			pw.Side = &s
		}
		st.Windows = append(st.Windows, pw)
	}
	if b.ReadOnly {
		reason := "read-only-mode"
		switch b.Mode {
		case modeDired:
			reason = "dired-mode"
		case modeHelp, modeCompilation, modeMessages:
			reason = "special-mode"
		}
		st.ReadOnlyReason = &reason
	}
	if e.m.Menu != "" {
		st.Waiting = &protocol.Waiting{Kind: protocol.WaitingTransient, What: "the " + e.m.Menu + " menu", Leave: "C-g"}
	}
	return st
}

func (e *Editor) minibufferObject() *protocol.Minibuffer {
	if e.m.Minibuffer == nil {
		return nil
	}
	mb := *e.m.Minibuffer
	return &mb
}

// keyEvent reports a command; buf defaults to the selected window's buffer.
func (e *Editor) keyEvent(keys, cmd string, buf *Buffer) {
	if buf == nil {
		buf = e.current()
	}
	e.emit(&protocol.KeyEvent{Envelope: protocol.Envelope{T: protocol.EventKey}, Keys: keys, Cmd: cmd,
		Buf: buf.Name, Mode: buf.Mode, Frame: e.m.LearnerFrame})
}

// minibufferBuffer stands for the minibuffer in key events.
var minibufferBuffer = &Buffer{Name: minibufferName, Mode: modeMinibuffer}
