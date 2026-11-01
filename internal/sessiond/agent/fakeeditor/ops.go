package fakeeditor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// Replies are built as the agent builds them, field for field (the
// protocol types omit empty fields the agent always sends).

type fileReply struct {
	Path    string `json:"path"`
	Point   int    `json:"point"`
	Visible bool   `json:"visible"`
}

type snapshotReply struct {
	Files          []fileReply `json:"files"`
	Layout         string      `json:"layout"`
	WindowState    string      `json:"windowState"`
	Selected       string      `json:"selected"`
	Tab            string      `json:"tab"`
	Tabs           []string    `json:"tabs"`
	TabIndex       int         `json:"tabIndex"`
	SpecialBuffers string      `json:"specialBuffers"`
	TakenAt        int64       `json:"takenAt"`
}

type resetReply struct {
	Reverted int `json:"reverted"`
	Killed   int `json:"killed"`
}

type openReply struct {
	Opened int `json:"opened"`
}

type setupReply struct {
	OK bool `json:"ok"`
}

type setModulesReply struct {
	Written string `json:"written"`
}

// Handle answers one agent call (agent.FakeHandler).
func (e *Editor) Handle(op protocol.AgentOp, args json.RawMessage) (any, error) {
	e.mu.Lock()
	e.calls = appendLog(e.calls, Call{Op: op, Args: slices.Clone(args)})
	f := e.faults[op]
	e.mu.Unlock()
	if f.Hook != nil {
		f.Hook()
	}
	for deadline := time.Now().Add(time.Minute); e.isSilent() && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if f.Err != nil {
		return nil, f.Err
	}
	e.mu.Lock()
	v, err := e.op(op, args)
	e.flush()
	return v, err
}

func (e *Editor) isSilent() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.silent
}

// op runs op with e.mu held.
func (e *Editor) op(op protocol.AgentOp, raw json.RawMessage) (any, error) {
	switch op {
	case protocol.AgentOpCheck:
		var a protocol.AgentCheckArgs
		_ = json.Unmarshal(raw, &a)
		res := protocol.AgentCheckResult{Results: make([]protocol.Truth, len(a.Forms))}
		for i, form := range a.Forms {
			res.Results[i] = e.check(form)
		}
		return res, nil
	case protocol.AgentOpEval:
		var a protocol.AgentEvalArgs
		_ = json.Unmarshal(raw, &a)
		return map[string]any{"value": e.eval(a.Form)}, nil
	case protocol.AgentOpRescue:
		did := e.rescue()
		e.stateLocked()
		return map[string]any{"did": did}, nil
	case protocol.AgentOpCaptureEditor:
		var a protocol.AgentCaptureEditorArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		return e.captureEditor(&a), nil
	case protocol.AgentOpRestoreEditor:
		var a protocol.AgentRestoreEditorArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		r := e.restoreEditor(&a)
		e.stateLocked()
		return r, nil
	case protocol.AgentOpModifiedFiles:
		paths := e.m.ModifiedFiles()
		if paths == nil {
			paths = []string{}
		}
		return map[string]any{"paths": paths}, nil
	case protocol.AgentOpFlushBuffers:
		return map[string]any{"saved": e.flushBuffers()}, nil
	case protocol.AgentOpVisit:
		var a protocol.AgentVisitArgs
		_ = json.Unmarshal(raw, &a)
		rel, ok := e.rel(a.Path)
		if !ok || rel == "" {
			return nil, fmt.Errorf("No path to visit")
		}
		b := e.visit(rel)
		e.switchTo(b)
		e.stateLocked()
		return map[string]any{"buffer": b.Name}, nil
	case protocol.AgentOpLeaveDisplay:
		return map[string]any{"closed": true, "detail": ""}, nil
	case protocol.AgentOpResumeLoop, protocol.AgentOpResetDebug:
		return map[string]any{}, nil
	case protocol.AgentOpSnapshot:
		return e.snapshot(), nil
	case protocol.AgentOpOpen:
		var spec protocol.WindowSpec
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, err
		}
		n := e.openSpec(&spec)
		e.stateLocked()
		return openReply{Opened: n}, nil
	case protocol.AgentOpResetStep:
		var a protocol.AgentResetStepArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		r := e.resetStep(&a.Spec)
		e.stateLocked()
		return r, nil
	case protocol.AgentOpSetup:
		var a protocol.AgentSetupArgs
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, err
		}
		if err := e.setup(&a); err != nil {
			return nil, err
		}
		e.stateLocked()
		return setupReply{OK: true}, nil
	case protocol.AgentOpSetModules:
		var a protocol.AgentSetModulesArgs
		_ = json.Unmarshal(raw, &a)
		return e.setModules(a.Enabled)
	case protocol.AgentOpFrame:
		return map[string]any{"frame": "F2"}, nil
	case protocol.AgentOpDescribe:
		var a struct {
			Keys string `json:"keys"`
			Cmd  string `json:"cmd"`
		}
		_ = json.Unmarshal(raw, &a)
		return e.describe(a.Keys, a.Cmd), nil
	}
	return nil, fmt.Errorf("unknown op %s", op)
}

// --- check and eval ------------------------------------------------------------

var (
	bufferFormName     = regexp.MustCompile(`^\(let \(\(pg-b \(or \(get-buffer ("(?:[^"\\]|\\.)*")\)`)
	bufferFormMode     = regexp.MustCompile(`\(derived-mode-p \(intern ("(?:[^"\\]|\\.)*")\)\)`)
	bufferFormContains = regexp.MustCompile(`\(search-forward ("(?:[^"\\]|\\.)*") nil t\)`)
)

// elispUnquote reads a string literal as steps.ElispString writes it.
func elispUnquote(lit string) string {
	s := lit[1 : len(lit)-1]
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(s)
}

func (e *Editor) check(form string) protocol.Truth {
	if fn := e.Forms[form]; fn != nil {
		return fn(&e.m)
	}
	switch form {
	case "t":
		return protocol.True
	case "nil":
		return protocol.False
	}
	if c, ok := parseBufferForm(form); ok {
		return e.bufferCond(c)
	}
	return protocol.Unknown
}

// parseBufferForm recognises steps.BufferForm's output by reading its
// parts back and rebuilding it.
func parseBufferForm(form string) (*protocol.BufferCond, bool) {
	m := bufferFormName.FindStringSubmatch(form)
	if m == nil {
		return nil, false
	}
	c := &protocol.BufferCond{Name: elispUnquote(m[1])}
	if mm := bufferFormMode.FindStringSubmatch(form); mm != nil {
		c.Mode = elispUnquote(mm[1])
	}
	if mm := bufferFormContains.FindStringSubmatch(form); mm != nil {
		c.Contains = elispUnquote(mm[1])
	}
	switch {
	case strings.Contains(form, "(not (process-live-p"):
		c.Process = protocol.ProcessNone
	case strings.Contains(form, "(process-live-p"):
		c.Process = protocol.ProcessRunning
	}
	switch {
	case strings.Contains(form, "(not (buffer-modified-p))"):
		f := false
		c.Modified = &f
	case strings.Contains(form, "(buffer-modified-p)"):
		t := true
		c.Modified = &t
	}
	return c, steps.BufferForm(c) == form
}

func (e *Editor) bufferCond(c *protocol.BufferCond) protocol.Truth {
	b := e.buffer(c.Name)
	if b == nil {
		for _, x := range e.m.Buffers {
			if strings.HasPrefix(x.Name, c.Name+"<") && strings.HasSuffix(x.Name, ">") {
				b = x
				break
			}
		}
	}
	ok := b != nil &&
		(c.Mode == "" || b.Mode == c.Mode) &&
		(c.Contains == "" || strings.Contains(b.Text, c.Contains)) &&
		c.Process != protocol.ProcessRunning &&
		(c.Modified == nil || *c.Modified == b.Modified)
	if ok {
		return protocol.True
	}
	return protocol.False
}

// eval answers registered forms (author-mode eval, setup.eval) and null
// for anything else: sessiond builds no Lisp.
func (e *Editor) eval(form string) any {
	if fn := e.Evals[form]; fn != nil {
		return fn(&e.m)
	}
	return nil
}

// flushBuffers is playground--op-flush-buffers: modified file buffers are
// saved unless read-only or changed on disk.
func (e *Editor) flushBuffers() int {
	n := 0
	for _, b := range e.m.Buffers {
		if b.Path != "" && b.Modified && !b.ReadOnly {
			if disk, ok := e.readFile(b.Path); ok && disk != b.Saved {
				continue
			}
			if e.writeFile(b.Path, b.Text) == nil {
				b.Saved, b.Modified = b.Text, false
				n++
			}
		}
	}
	return n
}

// --- rescue ----------------------------------------------------------------------

// leaveTopLevel closes what an op at top level throws out of: the hydra,
// prompts, recursive edits, a pending prefix, a search.
func (e *Editor) leaveTopLevel() (hydra, prompt, recursive, search bool) {
	if e.m.Hydra != "" {
		e.m.Hydra, hydra = "", true
	}
	if e.prompt != nil {
		e.emit(&protocol.MinibufferEvent{Envelope: protocol.Envelope{T: protocol.EventMinibuffer}, Open: false, Prompt: e.prompt.text, Depth: 1, Kind: protocol.MinibufferPrompt})
		e.prompt, e.m.Minibuffer, prompt = nil, nil, true
	}
	if e.m.Recursion > 0 {
		e.m.Recursion, recursive = 0, true
	}
	if e.isearch != nil {
		e.emit(&protocol.MinibufferEvent{Envelope: protocol.Envelope{T: protocol.EventMinibuffer}, Open: false, Prompt: "I-search: ", Kind: protocol.MinibufferIsearch})
		e.isearch, e.m.Minibuffer, search = nil, nil, true
	}
	e.pending, e.reading = nil, nil
	return
}

// rescue is playground--op-rescue: the actions in the agent's order.
func (e *Editor) rescue() []string {
	did := []string{}
	name := e.m.Hydra
	hydra, prompt, recursive, search := e.leaveTopLevel()
	if hydra {
		did = append(did, "closed the "+name+" hydra")
	}
	if prompt {
		did = append(did, "cancelled the prompt")
	}
	if recursive {
		did = append(did, "left the recursive edit")
	}
	if search {
		did = append(did, "stopped the search")
	}
	keep := e.mainWindow()
	var seen []string
	for _, w := range e.m.Windows {
		if slices.Contains(seen, w.Buf) {
			continue
		}
		seen = append(seen, w.Buf)
		b := e.buffer(w.Buf)
		if b == nil {
			continue
		}
		if b.Overwrite {
			b.Overwrite = false
			did = append(did, "turned off overwrite mode")
		}
		if b.Narrowed {
			b.Narrowed = false
			did = append(did, "widened "+b.Name)
		}
	}
	if others := len(e.m.Windows) - 1; others > 0 {
		e.selectWindow(keep)
		e.onlyWindow()
		s := "s"
		if others == 1 {
			s = ""
		}
		did = append(did, fmt.Sprintf("closed %d other window%s", others, s))
	} else {
		e.selectWindow(keep)
	}
	if b := e.current(); b.Mode == modeHelp || b.Mode == modeMessages {
		e.switchTo(e.otherBuffer(b.Name))
		did = append(did, "put away "+b.Name)
	}
	return did
}

// mainWindow is the window rescue keeps: the selected one when it visits a
// file, else one visiting a file, else the selected one.
func (e *Editor) mainWindow() int {
	sel := e.selected()
	if b := e.buffer(sel.Buf); b != nil && b.Path != "" && sel.Side == "" {
		return sel.ID
	}
	for _, w := range e.m.Windows {
		if b := e.buffer(w.Buf); b != nil && b.Path != "" && w.Side == "" {
			return w.ID
		}
	}
	return sel.ID
}

// --- snapshot, open, reset-step (restore-editor resets with resetStep) ------------------

func (e *Editor) snapshot() snapshotReply {
	sel := e.selected()
	if b := e.buffer(sel.Buf); b != nil {
		sel.Point = b.Point
	}
	r := snapshotReply{Files: []fileReply{}, Tabs: []string{}}
	for _, b := range e.m.Buffers {
		if b.Path == "" {
			continue
		}
		f := fileReply{Path: b.Path, Point: b.Point}
		for _, w := range e.m.Windows {
			if w.Buf == b.Name {
				f.Point, f.Visible = w.Point, true
				break
			}
		}
		r.Files = append(r.Files, f)
	}
	r.Layout = layoutString(e.m.Layout, func(id int) string { return e.leafNameOf(e.window(id).Buf) })
	ws := windowState{Layout: cloneLayout(e.m.Layout), Selected: sel.ID}
	for _, w := range e.m.Windows {
		sw := stateWindow{ID: w.ID, Buf: w.Buf, Point: w.Point}
		if b := e.buffer(w.Buf); b != nil {
			sw.Path = b.Path
		}
		ws.Windows = append(ws.Windows, sw)
	}
	r.WindowState = ws.String()
	r.Selected = e.leafNameOf(sel.Buf)
	r.Tab = e.tabName(e.m.TabIndex - 1)
	for _, t := range e.m.Tabs {
		r.Tabs = append(r.Tabs, t.Name)
	}
	r.TabIndex = e.m.TabIndex
	r.SpecialBuffers = printSpecial(e.m.Buffers)
	r.TakenAt = e.now().UnixMilli()
	return r
}

// openSpec visits the spec's files at their points and applies its tab,
// windows and selection; it returns the number of files visited.
func (e *Editor) openSpec(spec *protocol.WindowSpec) int {
	n := 0
	for _, f := range spec.Files {
		rel, ok := e.rel(f.Path)
		if !ok || rel == "" {
			continue
		}
		if _, exists := e.readFile(rel); !exists {
			continue
		}
		b := e.visit(rel)
		n++
		if f.Point > 0 {
			b.Point = clampPoint(b.Text, f.Point)
		}
	}
	e.selectSpecTab(spec)
	if ws, ok := parseWindowState(spec.WindowState); ok {
		e.applyWindowState(ws)
	} else if spec.Layout != "" {
		if p, err := parsePattern(spec.Layout); err == nil {
			e.applyPattern(p)
		}
	}
	if spec.Selected != "" {
		e.selectLeaf(spec.Selected)
	}
	return n
}

func (e *Editor) selectSpecTab(spec *protocol.WindowSpec) {
	if spec.TabIndex >= 1 && len(spec.Tabs) == len(e.m.Tabs) && spec.TabIndex <= len(e.m.Tabs) {
		e.selectTab(spec.TabIndex)
		return
	}
	if spec.Tab != "" {
		for i, t := range e.m.Tabs {
			if t.Name == spec.Tab {
				e.selectTab(i + 1)
				return
			}
		}
	}
}

// resetStep is playground--reset-step-now.
func (e *Editor) resetStep(spec *protocol.WindowSpec) resetReply {
	e.leaveTopLevel()
	var r resetReply
	for _, b := range slices.Clone(e.m.Buffers) {
		switch {
		case b.Path == "":
			if b.Mode == modeDired && !e.isDir(b.Dir) {
				e.killBuffer(b)
				r.Killed++
			}
		default:
			disk, exists := e.readFile(b.Path)
			switch {
			case !exists:
				e.killBuffer(b)
				r.Killed++
			case b.Modified || disk != b.Saved:
				e.revert(b, disk)
				r.Reverted++
			}
		}
	}
	if len(spec.Files) > 0 {
		keep := map[string]bool{}
		for _, f := range spec.Files {
			if rel, ok := e.rel(f.Path); ok {
				keep[rel] = true
			}
		}
		for _, b := range slices.Clone(e.m.Buffers) {
			if b.Path != "" && !b.Modified && !keep[b.Path] {
				e.killBuffer(b)
				r.Killed++
			}
		}
	}
	e.syncSpecial(spec.SpecialBuffers)
	r.Killed += e.killLaterRuns(spec)
	e.syncTabs(spec.Tabs)
	e.openSpec(spec)
	return r
}

func (e *Editor) revert(b *Buffer, disk string) {
	b.Text, b.Saved, b.Modified, b.Undo = disk, disk, false, nil
	b.Point = clampPoint(disk, b.Point)
	for i := range e.m.Windows {
		if e.m.Windows[i].Buf == b.Name {
			e.m.Windows[i].Point = clampPoint(disk, e.m.Windows[i].Point)
		}
	}
}

// syncSpecial makes the help buffers those printed in special (the fake's
// own printSpecial format; anything else leaves them alone).
func (e *Editor) syncSpecial(special string) {
	want, ok := parseSpecial(special)
	if !ok {
		return
	}
	for _, b := range slices.Clone(e.m.Buffers) {
		if b.Mode == modeHelp {
			if item, listed := want[b.Name]; !listed || item != b.Item {
				e.killBuffer(b)
			}
		}
	}
	for name, item := range want {
		if e.buffer(name) == nil {
			e.addBuffer(&Buffer{Name: name, Mode: modeHelp, ReadOnly: true, Item: item, Text: item + "\n"})
		}
	}
}

// killLaterRuns kills build output started after the spec was taken,
// unless one of its windows shows it.
func (e *Editor) killLaterRuns(spec *protocol.WindowSpec) int {
	if spec.TakenAt <= 0 {
		return 0
	}
	shown := map[string]bool{}
	if ws, ok := parseWindowState(spec.WindowState); ok {
		for _, w := range ws.Windows {
			shown[w.Buf] = true
		}
	} else if p, err := parsePattern(spec.Layout); err == nil {
		var walk func(pattern)
		walk = func(p pattern) {
			if p.split == "" {
				shown[p.name] = true
			}
			for _, c := range p.children {
				walk(c)
			}
		}
		walk(p)
	}
	n := 0
	for _, b := range slices.Clone(e.m.Buffers) {
		if b.Mode == modeCompilation && b.Started > spec.TakenAt && !shown[b.Name] {
			e.killBuffer(b)
			n++
		}
	}
	return n
}

// syncTabs makes the tabs those want lists (playground--sync-tabs).
func (e *Editor) syncTabs(want []string) {
	if len(want) == 0 {
		return
	}
	var named []string
	for _, w := range want {
		if w != "" {
			named = append(named, w)
		}
	}
	unnamed := len(want) - len(named)
	for i := 0; i < len(e.m.Tabs); {
		t := e.m.Tabs[i]
		if t.Name != "" && !slices.Contains(named, t.Name) {
			if len(e.m.Tabs) > 1 {
				e.closeTab(i + 1)
				continue
			}
			e.m.Tabs[i].Name = ""
		}
		i++
	}
	countUnnamed := func() int {
		n := 0
		for _, t := range e.m.Tabs {
			if t.Name == "" {
				n++
			}
		}
		return n
	}
	lastUnnamed := func() int {
		other, current := 0, 0
		for i, t := range e.m.Tabs {
			if t.Name == "" {
				if i+1 == e.m.TabIndex {
					current = i + 1
				} else {
					other = i + 1
				}
			}
		}
		if other > 0 {
			return other
		}
		return current
	}
	for len(e.m.Tabs) > 1 && countUnnamed() > unnamed {
		e.closeTab(lastUnnamed())
	}
	for _, name := range named {
		if slices.ContainsFunc(e.m.Tabs, func(t Tab) bool { return t.Name == name }) {
			continue
		}
		if countUnnamed() > unnamed {
			e.m.Tabs[lastUnnamed()-1].Name = name
			continue
		}
		e.newTab()
		e.currentTab().Name = name
	}
}

// --- setup ------------------------------------------------------------------------------

// setup is playground--setup-now.
func (e *Editor) setup(a *protocol.AgentSetupArgs) error {
	e.leaveTopLevel()
	// Buffers whose files the fixtures just rewrote or removed.
	for _, b := range slices.Clone(e.m.Buffers) {
		if b.Path == "" {
			continue
		}
		if disk, ok := e.readFile(b.Path); !ok {
			e.killBuffer(b)
		} else if disk != b.Saved {
			e.revert(b, disk)
		}
	}
	keep := map[string]bool{}
	for _, p := range a.Open {
		if rel, ok := e.rel(p); ok {
			keep[rel] = true
		}
	}
	if a.Reset {
		// The lesson starts with one tab named after its buffer.
		e.m.Tabs, e.m.TabIndex = []Tab{{}}, 1
		e.clearDesk(keep)
	}
	var opened []*Buffer
	for _, p := range a.Open {
		if rel, ok := e.rel(p); ok && rel != "" {
			opened = append(opened, e.visit(rel))
		}
	}
	if a.Reset {
		for _, b := range opened {
			b.Point = 1
		}
	}
	switch {
	case strings.HasSuffix(a.Layout, ".eld"):
		return fmt.Errorf("Layout file %s not found", a.Layout)
	case a.Layout != "" && a.Layout != "single":
		if a.Compile != "" && strings.Contains(a.Layout, "*compilation*") && e.buffer("*compilation*") == nil {
			e.addBuffer(&Buffer{Name: "*compilation*", Mode: modeCompilation, ReadOnly: true})
		}
		p, err := parsePattern(a.Layout)
		if err != nil {
			return err
		}
		e.applyPattern(p)
	case len(opened) > 0:
		e.onlyWindow()
		e.switchTo(opened[0])
	}
	if a.Reset {
		for i := range e.m.Windows {
			if slices.ContainsFunc(opened, func(b *Buffer) bool { return b.Name == e.m.Windows[i].Buf }) {
				e.m.Windows[i].Point = 1
			}
		}
	}
	if a.Eval != "" {
		if fn := e.Evals[a.Eval]; fn != nil {
			fn(&e.m)
		}
	}
	if a.Compile != "" {
		b := e.buffer("*compilation*")
		if b == nil {
			b = e.addBuffer(&Buffer{Name: "*compilation*", Mode: modeCompilation, ReadOnly: true})
		}
		b.Text, b.Point, b.Started = "-*- mode: compilation -*-\n"+a.Compile+"\n", 1, e.now().UnixMilli()
		e.displayBuffer(b)
	}
	return nil
}

// clearDesk kills the unmodified buffers a fresh lesson could trip over
// (playground--clear-desk).
func (e *Editor) clearDesk(keep map[string]bool) {
	for _, b := range slices.Clone(e.m.Buffers) {
		if b.Modified {
			continue
		}
		switch {
		case b.Path != "":
			if !keep[b.Path] {
				e.killBuffer(b)
			}
		case b.Mode == modeDired:
			e.killBuffer(b)
		case b.Mode == modeFundamental && b.Text == "" && !strings.HasPrefix(b.Name, "*") && !strings.HasPrefix(b.Name, " "):
			e.killBuffer(b)
		}
	}
}

// --- the other ops ----------------------------------------------------------------------------

var moduleNameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

func (e *Editor) setModules(enabled []string) (any, error) {
	for _, m := range enabled {
		if !moduleNameRe.MatchString(m) {
			return nil, fmt.Errorf("Bad module name %q", m)
		}
	}
	p := e.ModulesPath
	if p == "" {
		p = filepath.Join(e.Workspace, "emacs.d", "modules.el")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, []byte(scenario.RenderModulesEl(enabled)), 0o644); err != nil {
		return nil, err
	}
	return setModulesReply{Written: p}, nil
}

func (e *Editor) describe(keys, cmd string) protocol.DescribeResult {
	if keys == "" && cmd != "" {
		return protocol.DescribeResult{Cmd: cmd, Doc: commandDocs[cmd]}
	}
	if c, ok := e.binding(keys); ok {
		return protocol.DescribeResult{Cmd: c, Doc: commandDocs[c]}
	}
	if e.isPrefix(keys) {
		return protocol.DescribeResult{Doc: keys + " is a prefix key"}
	}
	return protocol.DescribeResult{}
}
