// Package fakeeditor is a deterministic stand-in for the Emacs side of a
// session: a model of buffers, windows, tabs, help and build buffers, the
// minibuffer and the learner's frame, answering every agent op over the
// real agent socket (through agent.Fake) and reporting key, state and
// minibuffer events the way playground-agent.el does. FakeTmux routes the
// supervisor's send-keys to it, so rpc keys, demos, skips and
// fast-forwards move the model in sessiond -fake mode.
//
// The model follows batch Emacs without packages (no transient mark, the
// modes Emacs picks by itself) because that is what the contract fixtures
// in emacs.d/test/fixtures/agent are recorded from; fixtures_test.go
// replays them against the fake under the same shape rule the ERT replay
// applies to the real agent.
package fakeeditor

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
)

// Major modes the model uses.
const (
	modeScratch     = "lisp-interaction-mode"
	modeMessages    = "messages-buffer-mode"
	modeFundamental = "fundamental-mode"
	modeHelp        = "help-mode"
	modeDired       = "dired-mode"
	modeCompilation = "compilation-mode"
	modeMinibuffer  = "minibuffer-mode"
)

// minibufferName is the buffer key events name while a prompt has the focus.
const minibufferName = " *Minibuf-1*"

// Buffer is one buffer of the model.
type Buffer struct {
	Name string
	// Path is the workspace-relative file the buffer visits, "" for none.
	Path string
	// Dir is the buffer's default directory, workspace-relative ("" is the
	// workspace); a Dired buffer lists it.
	Dir  string
	Mode string
	Text string
	// Saved is the file's text as last visited, saved or reverted.
	Saved    string
	Point    int
	Modified bool
	ReadOnly bool
	// Mark is the mark's position, 0 when unset (there is no transient
	// mark: the region is never active).
	Mark     int
	Narrowed bool
	// Overwrite is overwrite-mode.
	Overwrite bool
	// Undo holds the text and point before each undoable change, newest last.
	Undo []Edit
	// Item is a help buffer's help command, printed ("(describe-function
	// find-file)"); reset-step makes the buffer again from it.
	Item string
	// Started is when a compilation buffer's run started (ms).
	Started int64
}

// Edit is one undo entry.
type Edit struct {
	Text  string
	Point int
}

// Window is one live window of the learner's frame.
type Window struct {
	ID       int
	Buf      string
	Point    int
	Selected bool
	Side     string
}

// Tab is a tab-bar tab; the current tab's windows are Model.Windows.
type Tab struct {
	// Name is the explicit name, "" for a tab named after its buffer.
	Name    string
	Windows []Window
	Layout  protocol.Layout
}

// Model is the editor state; Snapshot returns a copy.
type Model struct {
	// Buffers in buffer-list order, most recently selected first.
	Buffers []*Buffer
	// Windows in window-list order (the layout's leaves).
	Windows []Window
	Layout  protocol.Layout
	Tabs    []Tab
	// TabIndex is the current tab, 1-based.
	TabIndex int
	// Globals are session variables (kill-ring, …).
	Globals    map[string]any
	Minibuffer *protocol.Minibuffer
	// Menu is the open transient menu's name ("" for none).
	Menu string
	// Recursion is the depth of recursive edits (minibuffers not counted).
	Recursion    int
	LearnerFrame protocol.FrameKind
	// Modules are the modules the running Emacs loaded (read from
	// modules.el when it started).
	Modules []string
}

// Fault is what an op does instead of, or besides, answering.
type Fault struct {
	// Err fails the op with this message.
	Err error
	// Hook runs when the call arrives, before anything else (tests stop
	// sessiond from here).
	Hook func()
}

// Call is one call the editor received.
type Call struct {
	Op   protocol.AgentOp
	Args json.RawMessage
}

// Command runs a command on the model; keys are the keys that invoked it.
// It is called with the editor's lock held.
type Command func(e *Editor, keys string)

// Editor is the fake Emacs. Configure the exported fields before the
// editor is used; afterwards change them only through its methods.
type Editor struct {
	// Workspace is the absolute workspace directory (playground-agent-workspace).
	Workspace string
	// ModulesPath is the modules.el that set-modules writes and that a
	// start reads ("" for none).
	ModulesPath string
	// EmacsVersion is what hello reports (default "31.1").
	EmacsVersion string
	// Clock stamps snapshots and compilation runs (default time.Now).
	Clock func() time.Time
	// Forms decide check forms by their text; anything else is "unknown"
	// except t, nil and buffer conditions (steps.BufferForm), which are
	// read structurally. They run with the editor locked and may change
	// the model they get.
	Forms map[string]func(*Model) protocol.Truth
	// Evals answer eval forms by their text, as Forms; anything else is
	// null.
	Evals map[string]func(*Model) any
	// Bindings map key sequences to command names (global keymap).
	Bindings map[string]string
	// Commands implement command names.
	Commands map[string]Command

	mu      sync.Mutex
	m       Model
	faults  map[protocol.AgentOp]Fault
	calls   []Call
	sent    [][]byte
	outbox  []json.RawMessage
	pending []protocol.Key
	prompt  *prompt
	reading *keyReader
	isearch *isearchState
	// last is the previous command (undo amalgamation, goal column).
	last      string
	goal      int
	selfRun   int
	lastState string
	nextWin   int
	silent    bool
	offline   bool
	online    chan struct{}
	fake      *agent.Fake
	sendMu    sync.Mutex
	root      *os.Root
}

// New returns an editor for workspace in the state of a freshly started Emacs.
func New(workspace string) *Editor {
	e := &Editor{Workspace: workspace, online: make(chan struct{})}
	close(e.online)
	e.Bindings = defaultBindings()
	e.Commands = defaultCommands()
	e.Forms = map[string]func(*Model) protocol.Truth{}
	e.Evals = map[string]func(*Model) any{}
	e.restartLocked()
	return e
}

func (e *Editor) now() time.Time {
	if e.Clock != nil {
		return e.Clock()
	}
	return time.Now()
}

// Restart is a daemon (re)start: buffers, windows, tabs and session
// variables are gone; *scratch* fills the frame; modules.el is read.
func (e *Editor) Restart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.restartLocked()
}

func (e *Editor) restartLocked() {
	e.m = Model{
		Buffers: []*Buffer{
			{Name: "*scratch*", Mode: modeScratch, Point: 1},
			{Name: "*Messages*", Mode: modeMessages, Point: 1, ReadOnly: true},
		},
		Windows:      []Window{{ID: 1, Buf: "*scratch*", Point: 1, Selected: true}},
		Layout:       leaf(1),
		Tabs:         []Tab{{}},
		TabIndex:     1,
		Globals:      map[string]any{},
		LearnerFrame: protocol.FrameTerminal,
		Modules:      e.readModules(),
	}
	e.nextWin = 2
	e.pending, e.prompt, e.reading, e.isearch = nil, nil, nil, nil
	e.last, e.goal, e.selfRun, e.lastState = "", -1, 0, ""
}

func (e *Editor) readModules() []string {
	if e.ModulesPath == "" {
		return nil
	}
	b, err := os.ReadFile(e.ModulesPath)
	if err != nil {
		return nil
	}
	mods, _ := scenario.ParseModulesEl(string(b))
	return mods
}

// Hello is the hello message of the Emacs started with pid.
func (e *Editor) Hello(pid int) protocol.AgentHelloMsg {
	e.mu.Lock()
	defer e.mu.Unlock()
	v := e.EmacsVersion
	if v == "" {
		v = "31.1"
	}
	return protocol.AgentHelloMsg{T: protocol.AgentHello, Protocol: protocol.Version, Agent: protocol.AgentVersion,
		Emacs: v, PID: pid, Modules: slices.Clone(e.m.Modules)}
}

// Attach makes f the connection events go out on (f.Handle is e.Handle).
func (e *Editor) Attach(f *agent.Fake) {
	e.mu.Lock()
	e.fake = f
	silent := e.silent
	e.mu.Unlock()
	f.SetSilent(silent)
}

// Detach forgets the connection (the daemon exited).
func (e *Editor) Detach() {
	e.mu.Lock()
	e.fake = nil
	e.mu.Unlock()
}

// Snapshot returns a copy of the model.
func (e *Editor) Snapshot() Model {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.m.clone()
}

func (m Model) clone() Model {
	out := m
	out.Buffers = make([]*Buffer, len(m.Buffers))
	for i, b := range m.Buffers {
		c := *b
		c.Undo = slices.Clone(b.Undo)
		out.Buffers[i] = &c
	}
	out.Windows = slices.Clone(m.Windows)
	out.Layout = cloneLayout(m.Layout)
	out.Tabs = make([]Tab, len(m.Tabs))
	for i, t := range m.Tabs {
		out.Tabs[i] = Tab{Name: t.Name, Windows: slices.Clone(t.Windows), Layout: cloneLayout(t.Layout)}
	}
	out.Globals = maps.Clone(m.Globals)
	if m.Minibuffer != nil {
		mb := *m.Minibuffer
		out.Minibuffer = &mb
	}
	out.Modules = slices.Clone(m.Modules)
	return out
}

func cloneLayout(l protocol.Layout) protocol.Layout {
	out := protocol.Layout{Split: l.Split, Window: l.Window}
	for _, c := range l.Children {
		out.Children = append(out.Children, cloneLayout(c))
	}
	return out
}

// Buffer returns a copy of the buffer called name, or nil.
func (e *Editor) Buffer(name string) *Buffer {
	e.mu.Lock()
	defer e.mu.Unlock()
	if b := e.buffer(name); b != nil {
		c := *b
		return &c
	}
	return nil
}

// Update changes the model under the editor's lock (a timer or hook
// changing Emacs), then reports the state when it changed.
func (e *Editor) Update(fn func(m *Model)) {
	e.mu.Lock()
	fn(&e.m)
	e.stateLocked()
	e.flush()
}

// ModifiedFiles returns the workspace-relative files whose buffers have
// unsaved changes.
func (e *Editor) ModifiedFiles() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.m.ModifiedFiles()
}

// ModifiedFiles returns the workspace-relative files whose buffers have
// unsaved changes.
func (m *Model) ModifiedFiles() []string {
	out := []string{}
	for _, b := range m.Buffers {
		if b.Path != "" && b.Modified {
			out = append(out, b.Path)
		}
	}
	return out
}

// Calls returns the calls received so far (the last logLimit).
func (e *Editor) Calls() []Call {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.calls)
}

// CountCalls returns how many op calls were received.
func (e *Editor) CountCalls(op protocol.AgentOp) int {
	n := 0
	for _, c := range e.Calls() {
		if c.Op == op {
			n++
		}
	}
	return n
}

// Sent returns the messages the editor sent (key, state, minibuffer),
// encoded, oldest first (the last logLimit).
func (e *Editor) Sent() [][]byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.sent)
}

// --- faults --------------------------------------------------------------

// SetFault replaces op's fault.
func (e *Editor) SetFault(op protocol.AgentOp, f Fault) { e.setFault(op, func(x *Fault) { *x = f }) }

// Fail makes op fail with err until ClearFaults.
func (e *Editor) Fail(op protocol.AgentOp, err error) { e.setFault(op, func(f *Fault) { f.Err = err }) }

func (e *Editor) setFault(op protocol.AgentOp, fn func(*Fault)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.faults == nil {
		e.faults = map[protocol.AgentOp]Fault{}
	}
	f := e.faults[op]
	fn(&f)
	e.faults[op] = f
}

// ClearFaults removes every fault.
func (e *Editor) ClearFaults() {
	e.mu.Lock()
	e.faults = nil
	e.mu.Unlock()
}

// SetSilent stops (true) or resumes pings and replies: a hung Emacs.
func (e *Editor) SetSilent(v bool) {
	e.mu.Lock()
	e.silent = v
	f := e.fake
	e.mu.Unlock()
	if f != nil {
		f.SetSilent(v)
	}
}

// Disconnect drops the agent connection and keeps it down until Reconnect.
func (e *Editor) Disconnect() {
	e.mu.Lock()
	if !e.offline {
		e.offline = true
		e.online = make(chan struct{})
	}
	f := e.fake
	e.mu.Unlock()
	if f != nil {
		f.Drop()
	}
}

// Reconnect lets the agent connect again (it says hello anew).
func (e *Editor) Reconnect() {
	e.mu.Lock()
	if e.offline {
		e.offline = false
		close(e.online)
	}
	e.mu.Unlock()
}

// WaitOnline blocks while the editor is disconnected; false when ctx ends.
func (e *Editor) WaitOnline(ctx context.Context) bool {
	e.mu.Lock()
	ch := e.online
	e.mu.Unlock()
	select {
	case <-ch:
		return true
	case <-ctx.Done():
		return false
	}
}

// --- events ----------------------------------------------------------------

// logLimit bounds the logs of calls and sent messages (sessiond -fake
// runs for hours).
const logLimit = 4096

func appendLog[T any](log []T, v T) []T {
	if len(log) >= logLimit {
		log = slices.Delete(log, 0, len(log)-logLimit+1)
	}
	return append(log, v)
}

// emit queues a message; flush sends the queue.
func (e *Editor) emit(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	e.sent = appendLog(e.sent, b)
	e.outbox = append(e.outbox, json.RawMessage(b))
}

// flush sends the messages queued under the lock, in order, and releases
// the lock: sendMu is taken before mu is released so concurrent commands
// cannot reorder their events.
func (e *Editor) flush() {
	out := e.outbox
	e.outbox = nil
	f, offline := e.fake, e.offline
	e.sendMu.Lock()
	e.mu.Unlock()
	defer e.sendMu.Unlock()
	if f == nil || offline {
		return
	}
	for _, m := range out {
		_ = f.Send(m)
	}
}

// stateLocked reports the state unless it equals the last one sent.
func (e *Editor) stateLocked() {
	st := e.stateObject()
	b, _ := json.Marshal(st)
	if string(b) == e.lastState {
		return
	}
	e.lastState = string(b)
	e.sent = appendLog(e.sent, b)
	e.outbox = append(e.outbox, json.RawMessage(b))
}

// --- files -------------------------------------------------------------------

func (e *Editor) openRoot() (*os.Root, error) {
	if e.root != nil {
		return e.root, nil
	}
	r, err := os.OpenRoot(e.Workspace)
	if err != nil {
		return nil, err
	}
	e.root = r
	return r, nil
}

// Close releases the workspace root.
func (e *Editor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.root != nil {
		err := e.root.Close()
		e.root = nil
		return err
	}
	return nil
}

func (e *Editor) readFile(rel string) (string, bool) {
	r, err := e.openRoot()
	if err != nil {
		return "", false
	}
	b, err := r.ReadFile(rel)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (e *Editor) isDir(rel string) bool {
	if rel == "" {
		return true
	}
	r, err := e.openRoot()
	if err != nil {
		return false
	}
	st, err := r.Stat(rel)
	return err == nil && st.IsDir()
}

func (e *Editor) writeFile(rel, text string) error {
	r, err := e.openRoot()
	if err != nil {
		return err
	}
	if dir := path.Dir(rel); dir != "." {
		if err := r.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return r.WriteFile(rel, []byte(text), 0o644)
}

// rel turns a path typed or sent by sessiond into a workspace-relative
// one; ok is false outside the workspace.
func (e *Editor) rel(p string) (string, bool) {
	if filepath.IsAbs(p) {
		r, err := filepath.Rel(e.Workspace, p)
		if err != nil || r == ".." || strings.HasPrefix(r, "../") {
			return "", false
		}
		p = r
	}
	p = path.Clean(filepath.ToSlash(p))
	if p == "." {
		return "", true
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "", false
	}
	return p, true
}

func (e *Editor) abs(rel string) string { return filepath.Join(e.Workspace, filepath.FromSlash(rel)) }
