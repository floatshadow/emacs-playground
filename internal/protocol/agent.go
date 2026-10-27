package protocol

import (
	"encoding/json"
	"fmt"
)

// Agent protocol (api.md §7): newline-delimited JSON over the Unix socket
// PG_AGENT_SOCKET (default /run/playground/agent.sock). One object per line,
// UTF-8, no line longer than MaxAgentLine.

// MaxAgentLine is the maximum length of one agent protocol line in bytes.
const MaxAgentLine = 1 << 20

// AgentMsgType is the "t" of an agent protocol message.
type AgentMsgType string

// Agent to sessiond message types. "key", "state", "echo" and "minibuffer"
// reuse the event structs (KeyEvent, State, EchoEvent, MinibufferEvent)
// without seq and ts; sessiond stamps and forwards them.
const (
	AgentHello      AgentMsgType = "hello"
	AgentKey        AgentMsgType = "key"
	AgentState      AgentMsgType = "state"
	AgentEcho       AgentMsgType = "echo"
	AgentMinibuffer AgentMsgType = "minibuffer"
	AgentFrame      AgentMsgType = "frame"
	AgentPing       AgentMsgType = "ping"
	AgentReply      AgentMsgType = "reply"
)

// sessiond to agent message types.
const (
	AgentCall AgentMsgType = "call"
	AgentPong AgentMsgType = "pong"
)

// AgentHelloMsg is the agent's first line after connecting.
type AgentHelloMsg struct {
	T AgentMsgType `json:"t"`
	// Protocol is Version.
	Protocol int `json:"protocol"`
	// Agent is the playground-agent.el version, e.g. "0.1.0".
	Agent string `json:"agent"`
	// Emacs is emacs-version, e.g. "31.1".
	Emacs string `json:"emacs"`
	// PID is (emacs-pid).
	PID int `json:"pid"`
	// Modules is playground-modules as loaded (may differ from modules.el
	// when a module failed to load).
	Modules []string `json:"modules,omitempty"`
	// ModuleErrors maps module name to its load error message.
	ModuleErrors map[string]string `json:"moduleErrors,omitempty"`
}

// FrameEventKind is the event of an agent "frame" message.
type FrameEventKind string

// Frame message events.
const (
	FrameOpened FrameEventKind = "opened"
	FrameClosed FrameEventKind = "closed"
)

// AgentFrameMsg reports a frame created or deleted
// (server-after-make-frame-hook, delete-frame-functions). sessiond turns it
// into EmacsEvent frame-opened or frame-closed.
type AgentFrameMsg struct {
	T     AgentMsgType   `json:"t"`
	Event FrameEventKind `json:"event"`
	Kind  FrameKind      `json:"kind"`
}

// AgentPingMsg is sent by the agent every 2 s; sessiond answers with pong.
// Five missed pings mark Emacs unresponsive.
type AgentPingMsg struct {
	T AgentMsgType `json:"t"`
}

// AgentReplyMsg answers an AgentCallMsg with the same ID.
type AgentReplyMsg struct {
	T  AgentMsgType `json:"t"`
	ID int64        `json:"id"`
	OK bool         `json:"ok"`
	// Value is the op's result (see the Agent*Result types).
	Value json.RawMessage `json:"value,omitempty"`
	// Error is the error message when OK is false.
	Error string `json:"error,omitempty"`
}

// AgentCallMsg asks the agent to run an op. sessiond serialises calls: the
// agent never receives a second call before replying to the first.
type AgentCallMsg struct {
	T  AgentMsgType `json:"t"`
	ID int64        `json:"id"`
	Op AgentOp      `json:"op"`
	// Args is the op's argument object (see the Agent*Args types); {} when
	// the op takes none.
	Args json.RawMessage `json:"args"`
	// TimeoutMs bounds the op inside Emacs (with-timeout).
	TimeoutMs int `json:"timeoutMs"`
}

// AgentPongMsg answers AgentPingMsg.
type AgentPongMsg struct {
	T AgentMsgType `json:"t"`
}

// AgentOp names an op the agent implements (emacs.md §10.3).
type AgentOp string

// Agent ops.
const (
	AgentOpEval       AgentOp = "eval"
	AgentOpCheck      AgentOp = "check"
	AgentOpRescue     AgentOp = "rescue"
	AgentOpResetStep  AgentOp = "reset-step"
	AgentOpSnapshot   AgentOp = "snapshot"
	AgentOpSetModules AgentOp = "set-modules"
	AgentOpOpen       AgentOp = "open"
	AgentOpFrame      AgentOp = "frame"
	AgentOpDescribe   AgentOp = "describe"
	AgentOpSetup      AgentOp = "setup"
)

// AgentEvalArgs: eval {form} → AgentEvalResult.
type AgentEvalArgs struct {
	Form string `json:"form"`
}

// AgentEvalResult carries the json-serialize'd value.
type AgentEvalResult struct {
	Value any `json:"value"`
}

// AgentCheckArgs: check {forms} → AgentCheckResult. Each form is evaluated
// with condition-case and a 1 s with-timeout.
type AgentCheckArgs struct {
	Forms []string `json:"forms"`
}

// AgentCheckResult has one Truth per form, in order.
type AgentCheckResult struct {
	Results []Truth `json:"results"`
}

// AgentRescueResult: rescue → {did}.
type AgentRescueResult struct {
	Did []string `json:"did"`
}

// AgentResetStepArgs: reset-step {spec}.
type AgentResetStepArgs struct {
	Spec WindowSpec `json:"spec"`
}

// AgentSetModulesArgs: set-modules {enabled}; the agent writes modules.el
// and replies; sessiond then restarts the daemon.
type AgentSetModulesArgs struct {
	Enabled []string `json:"enabled"`
}

// AgentOpenArgs: open {files, layout} applies a WindowSpec (also used after
// a daemon restart).
type AgentOpenArgs = WindowSpec

// AgentFrameArgs: frame {kind:"gui", display?} opens a frame on the X
// display as an alternative to emacsclient -c.
type AgentFrameArgs struct {
	Kind    FrameKind `json:"kind"`
	Display string    `json:"display,omitempty"`
}

// AgentDescribeArgs: describe {keys} → DescribeResult.
type AgentDescribeArgs struct {
	Keys string `json:"keys"`
}

// AgentSetupArgs: setup runs a scenario's setup inside Emacs in order:
// open the files, apply the layout, evaluate the form, run the compile
// command (CURRICULUM.md Part B.3). Paths are workspace-relative.
type AgentSetupArgs struct {
	Open    []string `json:"open,omitempty"`
	Layout  string   `json:"layout,omitempty"`
	Eval    string   `json:"eval,omitempty"`
	Compile string   `json:"compile,omitempty"`
	// Workspace is the absolute workspace root (/work) so the agent can
	// resolve relative paths.
	Workspace string `json:"workspace"`
	// Reset is true when the fixtures were extracted afresh (setup.reset or
	// a reset request): the agent then clears the desk before opening, by
	// killing the unmodified buffers a lesson's keys could trip over
	// (other workspace files, Dired and buffer lists with stale marks,
	// stray empty buffers made by C-x b with a new name). Modified buffers
	// are kept. With Root, the agent also forgets the cursor positions
	// save-place kept for files under it, so a replayed lesson opens its
	// files at the top rather than where the last attempt left them.
	Reset bool `json:"reset,omitempty"`
	// Root is the workspace-relative directory the fixtures were extracted
	// to (setup.root, e.g. "hello"); empty when the scenario has none.
	Root string `json:"root,omitempty"`
}

// WindowSpec is the buffer and window spec saved with each checkpoint and
// periodically for restart restore (backend.md §3.9, emacs.md §7). It is
// the result of the snapshot op and the argument of open and reset-step.
type WindowSpec struct {
	// Files are the file-visiting buffers, most recently used first.
	Files []FileSpec `json:"files"`
	// Layout is a simple layout description used when WindowState is empty:
	// "single" or a layout pattern over workspace-relative paths, e.g.
	// "(vsplit hello/src/main.rs *compilation*)".
	Layout string `json:"layout,omitempty"`
	// WindowState is the printed (window-state-get nil t) of the selected
	// frame; opaque to sessiond; preferred over Layout when present.
	WindowState string `json:"windowState,omitempty"`
	// Selected is the workspace-relative path or buffer name of the selected
	// window's buffer.
	Selected string `json:"selected,omitempty"`
	// Tab is the current tab-bar tab name.
	Tab string `json:"tab,omitempty"`
	// Tabs are the names of the frame's tab-bar tabs in order, "" for a
	// tab without an explicit name (its name follows its buffer). A
	// reset-step closes the named tabs the list does not have and brings
	// back the named ones it misses, so a step's demo leaves no tab
	// behind. Absent when the snapshot was taken without tab-bar.
	Tabs []string `json:"tabs,omitempty"`
	// TabIndex is the 1-based position of the current tab in Tabs.
	TabIndex int `json:"tabIndex,omitempty"`
	// SpecialBuffers is the printed list of the non-file buffers the agent
	// can make again (help buffers: name and the help command that made
	// them); opaque to sessiond. A reset-step makes the missing ones again
	// and kills the help buffers it does not list, so a demo that killed
	// or opened help buffers leaves the step as it started (lesson 2.5).
	SpecialBuffers string `json:"specialBuffers,omitempty"`
	// TakenAt is when the snapshot was taken (ms).
	TakenAt int64 `json:"takenAt,omitempty"`
}

// FileSpec is one file-visiting buffer in a WindowSpec.
type FileSpec struct {
	// Path is relative to the workspace when inside it, else absolute.
	Path string `json:"path"`
	// Point is the 1-based buffer position.
	Point int `json:"point"`
	// Visible is true when the buffer is shown in a window.
	Visible bool `json:"visible,omitempty"`
}

// Truth is a three-valued condition result: true, false or unknown. JSON:
// true, false or the string "unknown" (null also decodes as unknown).
type Truth int8

// Truth values.
const (
	False Truth = iota
	True
	Unknown
)

// String returns "true", "false" or "unknown".
func (t Truth) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	}
	return "unknown"
}

// MarshalJSON implements json.Marshaler.
func (t Truth) MarshalJSON() ([]byte, error) {
	switch t {
	case True:
		return []byte("true"), nil
	case False:
		return []byte("false"), nil
	}
	return []byte(`"unknown"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *Truth) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true":
		*t = True
	case "false":
		*t = False
	case `"unknown"`, "null":
		*t = Unknown
	default:
		return fmt.Errorf("truth: want true, false or \"unknown\", got %s", b)
	}
	return nil
}

// AgentMessage is a decoded line from the agent: one of *AgentHelloMsg,
// *KeyEvent, *State, *EchoEvent, *MinibufferEvent, *AgentFrameMsg,
// *AgentPingMsg, *AgentReplyMsg.
type AgentMessage any

// DecodeAgentMessage decodes one line sent by the agent.
func DecodeAgentMessage(line []byte) (AgentMessage, error) {
	var head struct {
		T AgentMsgType `json:"t"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return nil, fmt.Errorf("agent message: %w", err)
	}
	var m AgentMessage
	switch head.T {
	case AgentHello:
		m = &AgentHelloMsg{}
	case AgentKey:
		m = &KeyEvent{}
	case AgentState:
		m = &State{}
	case AgentEcho:
		m = &EchoEvent{}
	case AgentMinibuffer:
		m = &MinibufferEvent{}
	case AgentFrame:
		m = &AgentFrameMsg{}
	case AgentPing:
		m = &AgentPingMsg{}
	case AgentReply:
		m = &AgentReplyMsg{}
	default:
		return nil, fmt.Errorf("agent message: unknown type %q", head.T)
	}
	if err := json.Unmarshal(line, m); err != nil {
		return nil, fmt.Errorf("agent message %q: %w", head.T, err)
	}
	return m, nil
}

// NewAgentCall builds a call message with args encoded as JSON (nil args
// become {}).
func NewAgentCall(id int64, op AgentOp, args any, timeoutMs int) (*AgentCallMsg, error) {
	raw := json.RawMessage(`{}`)
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("agent call %s: %w", op, err)
		}
		raw = b
	}
	return &AgentCallMsg{T: AgentCall, ID: id, Op: op, Args: raw, TimeoutMs: timeoutMs}, nil
}
