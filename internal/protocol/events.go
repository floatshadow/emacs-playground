package protocol

import (
	"encoding/json"
	"fmt"
)

// EventType is the "t" discriminator of an event (api.md §4).
type EventType string

// Event types on /s/{id}/events.
const (
	EventHello      EventType = "hello"
	EventKey        EventType = "key"
	EventPrefix     EventType = "prefix"
	EventState      EventType = "state"
	EventEcho       EventType = "echo"
	EventMinibuffer EventType = "minibuffer"
	EventEmacs      EventType = "emacs"
	EventFile       EventType = "file"
	EventCheck      EventType = "check"
	EventStep       EventType = "step"
	EventNudge      EventType = "nudge"
	EventCheckpoint EventType = "checkpoint"
	EventMode       EventType = "mode"
	EventModules    EventType = "modules"
	EventGap        EventType = "gap"
	EventError      EventType = "error"
)

// Envelope is the common part of every event: {"t", "seq", "ts"}. It is
// embedded in each event struct so that the fields appear flat on the wire.
//
// Seq is assigned by sessiond's event bus: monotonic per sessiond process,
// starting at 1. Ts is milliseconds since the Unix epoch, assigned by
// sessiond when the event enters the bus. Messages from the agent carry
// neither (they are omitted when zero).
type Envelope struct {
	T   EventType `json:"t"`
	Seq int64     `json:"seq,omitempty"`
	Ts  int64     `json:"ts,omitempty"`
}

// Env returns the envelope; it is promoted to every event struct.
func (e *Envelope) Env() *Envelope { return e }

// Event is implemented by pointers to every event struct in this file and by
// *State.
type Event interface {
	// Type returns the event's discriminator constant.
	Type() EventType
	// Env returns the embedded envelope for stamping.
	Env() *Envelope
}

// Stamp sets the envelope of ev: T from ev.Type(), and seq and ts. Producers
// call it (through the bus) before encoding.
func Stamp(ev Event, seq, ts int64) {
	e := ev.Env()
	e.T = ev.Type()
	e.Seq = seq
	e.Ts = ts
}

// MarshalEvent encodes ev, filling in T when the producer left it empty.
func MarshalEvent(ev Event) ([]byte, error) {
	if e := ev.Env(); e.T == "" {
		e.T = ev.Type()
	}
	return json.Marshal(ev)
}

// NewEvent returns a pointer to a zero event struct for t, or nil for an
// unknown type.
func NewEvent(t EventType) Event {
	switch t {
	case EventHello:
		return &HelloEvent{}
	case EventKey:
		return &KeyEvent{}
	case EventPrefix:
		return &PrefixEvent{}
	case EventState:
		return &State{}
	case EventEcho:
		return &EchoEvent{}
	case EventMinibuffer:
		return &MinibufferEvent{}
	case EventEmacs:
		return &EmacsEvent{}
	case EventFile:
		return &FileEvent{}
	case EventCheck:
		return &CheckEvent{}
	case EventStep:
		return &StepEvent{}
	case EventNudge:
		return &NudgeEvent{}
	case EventCheckpoint:
		return &CheckpointEvent{}
	case EventMode:
		return &ModeEvent{}
	case EventModules:
		return &ModulesEvent{}
	case EventGap:
		return &GapEvent{}
	case EventError:
		return &ErrorEvent{}
	}
	return nil
}

// EventTypes lists every event type in catalog order (for generators and
// exhaustive tests).
var EventTypes = []EventType{
	EventHello, EventKey, EventPrefix, EventState, EventEcho, EventMinibuffer,
	EventEmacs, EventFile, EventCheck, EventStep, EventNudge, EventCheckpoint,
	EventMode, EventModules, EventGap, EventError,
}

// UnknownEvent preserves an event whose type this build does not know.
type UnknownEvent struct {
	Envelope
	Raw json.RawMessage `json:"-"`
}

// Type implements Event.
func (u *UnknownEvent) Type() EventType { return u.T }

// MarshalJSON re-emits the original bytes.
func (u *UnknownEvent) MarshalJSON() ([]byte, error) { return u.Raw, nil }

// DecodeEvent decodes one event frame. Unknown types decode to
// *UnknownEvent rather than failing, so that old consumers survive new
// producers.
func DecodeEvent(data []byte) (Event, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("event envelope: %w", err)
	}
	ev := NewEvent(env.T)
	if ev == nil {
		raw := make(json.RawMessage, len(data))
		copy(raw, data)
		return &UnknownEvent{Envelope: env, Raw: raw}, nil
	}
	if err := json.Unmarshal(data, ev); err != nil {
		return nil, fmt.Errorf("event %q: %w", env.T, err)
	}
	return ev, nil
}

// EmacsInfo is the short Emacs description in hello and session info.
type EmacsInfo struct {
	// Version is emacs-version, e.g. "31.1".
	Version string `json:"version"`
	// UptimeMs is the daemon's uptime in milliseconds.
	UptimeMs int64 `json:"uptimeMs"`
}

// Frames reports which frames currently exist.
type Frames struct {
	Terminal bool `json:"terminal"`
	GUI      bool `json:"gui"`
}

// StepRef identifies a step of a lesson. Lesson ids contain one slash
// ("02-buffers-and-files/03-opening-files"); step ids are unique within the
// lesson ("open").
type StepRef struct {
	LessonID string `json:"lessonId"`
	StepID   string `json:"stepId"`
}

// HelloEvent is always the first frame on /events (api.md §3.2).
type HelloEvent struct {
	Envelope
	// Protocol is Version.
	Protocol int `json:"protocol"`
	// SessionID is PG_SESSION_ID.
	SessionID string `json:"sessionId"`
	// Mode is the frame kind the browser should show.
	Mode FrameKind `json:"mode"`
	// Emacs describes the daemon.
	Emacs EmacsInfo `json:"emacs"`
	// Frames reports which frames exist.
	Frames Frames `json:"frames"`
	// Step is the active step, or null when no scenario is loaded.
	Step *StepRef `json:"step"`
	// Modules is the enabled configuration module set, in load order.
	Modules []string `json:"modules"`
	// Replayed is true when the hello precedes replayed events (the client
	// connected with ?after= and the gap was still in the ring buffer).
	Replayed bool `json:"replayed"`
}

// Type implements Event.
func (*HelloEvent) Type() EventType { return EventHello }

// UndefinedCommand is the cmd the agent reports for an unbound key sequence;
// the ticker renders such entries with kind "unbound".
const UndefinedCommand = "undefined"

// KeyEvent reports one completed command (agent, post-command-hook). The
// same struct is the agent's "key" message without seq and ts.
type KeyEvent struct {
	Envelope
	// Keys is (key-description (this-command-keys-vector)), e.g. "C-x C-f".
	Keys string `json:"keys"`
	// Cmd is the command symbol name; "<lambda>" for anonymous commands;
	// UndefinedCommand for unbound keys.
	Cmd string `json:"cmd"`
	// Buf is the current buffer name after the command.
	Buf string `json:"buf"`
	// Mode is that buffer's major mode symbol.
	Mode string `json:"mode"`
	// Frame is the kind of frame the command ran in.
	Frame FrameKind `json:"frame"`
	// PrefixArg is the printed prefix argument the command received, if any.
	PrefixArg string `json:"prefixArg,omitempty"`
	// Mx is true when the command was run by name through M-x (or another
	// extended command reader); Keys is then the reader's keys, e.g. "M-x".
	Mx bool `json:"mx,omitempty"`
}

// Type implements Event.
func (*KeyEvent) Type() EventType { return EventKey }

// PrefixSource says who reported a pending prefix.
type PrefixSource string

// Prefix sources.
const (
	PrefixFromTap   PrefixSource = "tap"
	PrefixFromAgent PrefixSource = "agent"
)

// PrefixEvent reports a pending prefix key sequence (backend.md §3.6). It is
// advisory: the next KeyEvent supersedes it. Keys == "" means "no prefix
// pending any more" (sent by the tap when a sequence ends without an agent
// key event, for example after C-g).
type PrefixEvent struct {
	Envelope
	Keys   string       `json:"keys"`
	Source PrefixSource `json:"source"`
}

// Type implements Event.
func (*PrefixEvent) Type() EventType { return EventPrefix }

// EchoEvent is an echo-area message the agent did not filter as noise.
type EchoEvent struct {
	Envelope
	Text string `json:"text"`
}

// Type implements Event.
func (*EchoEvent) Type() EventType { return EventEcho }

// MinibufferEvent reports a minibuffer (or isearch) opening or closing.
type MinibufferEvent struct {
	Envelope
	Open   bool           `json:"open"`
	Prompt string         `json:"prompt"`
	Depth  int            `json:"depth"`
	Kind   MinibufferKind `json:"kind"`
}

// Type implements Event.
func (*MinibufferEvent) Type() EventType { return EventMinibuffer }

// EmacsEventKind is the lifecycle event reported by sessiond's supervisor.
type EmacsEventKind string

// Emacs lifecycle events.
const (
	EmacsFrameClosed     EmacsEventKind = "frame-closed"
	EmacsFrameOpened     EmacsEventKind = "frame-opened"
	EmacsDaemonRestarted EmacsEventKind = "daemon-restarted"
	EmacsSuspended       EmacsEventKind = "suspended"
	EmacsResumed         EmacsEventKind = "resumed"
	EmacsUnresponsive    EmacsEventKind = "unresponsive"
	EmacsResponsive      EmacsEventKind = "responsive"
	EmacsRestarting      EmacsEventKind = "restarting"
	EmacsStopping        EmacsEventKind = "stopping"
)

// EmacsEvent reports supervisor events (architecture.md §5.2, §6).
type EmacsEvent struct {
	Envelope
	Event EmacsEventKind `json:"event"`
	// Frame is set for frame-closed and frame-opened.
	Frame FrameKind `json:"frame,omitempty"`
	// Reason is a short human-readable cause ("C-x C-c", "exit status 1",
	// "module set changed").
	Reason string `json:"reason,omitempty"`
}

// Type implements Event.
func (*EmacsEvent) Type() EventType { return EventEmacs }

// FileEventKind is the kind of change in the workspace.
type FileEventKind string

// File change kinds.
const (
	FileCreated  FileEventKind = "created"
	FileModified FileEventKind = "modified"
	FileDeleted  FileEventKind = "deleted"
)

// FileEvent reports a workspace change, debounced per path at 200 ms.
type FileEvent struct {
	Envelope
	// Path is relative to the workspace root (/work), slash-separated.
	Path  string        `json:"path"`
	Event FileEventKind `json:"event"`
}

// Type implements Event.
func (*FileEvent) Type() EventType { return EventFile }

// CondResult is the result of one top-level condition of the active step.
type CondResult struct {
	// ID is the condition id (Condition.ID or ConditionID(stepID, index)).
	ID string `json:"id"`
	// OK is true when the condition holds.
	OK bool `json:"ok"`
	// Unknown is true when the condition could not be evaluated (elisp
	// timeout or error, shell timeout); OK is then false.
	Unknown bool `json:"unknown,omitempty"`
	// Detail is optional evaluator output for author mode (shell stdout
	// truncated to 4 KB, the elisp error message).
	Detail string `json:"detail,omitempty"`
}

// CheckEvent carries the latest evaluation of the active step.
type CheckEvent struct {
	Envelope
	LessonID   string       `json:"lessonId"`
	StepID     string       `json:"stepId"`
	Conditions []CondResult `json:"conditions"`
	// Complete is true when every condition holds.
	Complete bool `json:"complete"`
}

// Type implements Event.
func (*CheckEvent) Type() EventType { return EventCheck }

// StepStatus is the status reported by StepEvent.
type StepStatus string

// Step statuses (architecture.md §5.3).
const (
	StepLoaded   StepStatus = "loaded"
	StepComplete StepStatus = "complete"
	StepReset    StepStatus = "reset"
)

// StepEvent reports step transitions. After "complete" the engine advances
// and emits "loaded" for the next step; for the last step it sets
// LessonComplete instead.
type StepEvent struct {
	Envelope
	LessonID string     `json:"lessonId"`
	StepID   string     `json:"stepId"`
	Status   StepStatus `json:"status"`
	// Index is the 0-based position of the step in the scenario.
	Index int `json:"index"`
	// Count is the number of steps in the scenario.
	Count int `json:"count"`
	// LessonComplete is true on the "complete" event of the last step.
	LessonComplete bool `json:"lessonComplete,omitempty"`
}

// Type implements Event.
func (*StepEvent) Type() EventType { return EventStep }

// NudgeKind is the tone of a nudge card.
type NudgeKind string

// Nudge kinds. "habit" cards are created by the browser only.
const (
	NudgeCalm  NudgeKind = "calm"
	NudgeAsk   NudgeKind = "ask"
	NudgeHabit NudgeKind = "habit"
)

// NudgeEvent is a server-side nudge from a scenario stuck rule.
type NudgeEvent struct {
	Envelope
	// ID is stable per rule so the browser can dismiss it
	// (StuckRule.ID or "<stepId>/stuck/<n>").
	ID    string    `json:"id"`
	Kind  NudgeKind `json:"kind"`
	Title string    `json:"title"`
	Body  string    `json:"body"`
}

// Type implements Event.
func (*NudgeEvent) Type() EventType { return EventNudge }

// CheckpointEvent reports a checkpoint taken.
type CheckpointEvent struct {
	Envelope
	// Label is "<lessonId>/<stepId>" for step checkpoints or the learner's
	// label for manual ones.
	Label string `json:"label"`
	// Ref is the full git ref, e.g. "refs/playground/<lesson>/<step>".
	Ref       string `json:"ref"`
	CreatedAt int64  `json:"createdAt"`
}

// Type implements Event.
func (*CheckpointEvent) Type() EventType { return EventCheckpoint }

// ModeEvent reports a frame switch.
type ModeEvent struct {
	Envelope
	Mode FrameKind `json:"mode"`
}

// Type implements Event.
func (*ModeEvent) Type() EventType { return EventMode }

// ModulesEvent reports a module set change.
type ModulesEvent struct {
	Envelope
	Enabled []string `json:"enabled"`
	// Restarting is true while the daemon restarts to load the new set.
	Restarting bool `json:"restarting"`
}

// Type implements Event.
func (*ModulesEvent) Type() EventType { return EventModules }

// GapEvent tells a slow subscriber that events from From to To (inclusive)
// were dropped. The browser should call rpc check or reconnect.
type GapEvent struct {
	Envelope
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// Type implements Event.
func (*GapEvent) Type() EventType { return EventGap }

// ErrorEvent is an error the learner should see (fixture fetch failed,
// module load error).
type ErrorEvent struct {
	Envelope
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Type implements Event.
func (*ErrorEvent) Type() EventType { return EventError }
