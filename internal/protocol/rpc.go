package protocol

import "encoding/json"

// RPCOp names a session RPC: POST /s/{id}/rpc/{op} (api.md §3.4).
type RPCOp string

// Session RPC operations.
const (
	OpKeys         RPCOp = "keys"
	OpEval         RPCOp = "eval"
	OpRescue       RPCOp = "rescue"
	OpScenario     RPCOp = "scenario"
	OpCheck        RPCOp = "check"
	OpHint         RPCOp = "hint"
	OpCheckpoint   RPCOp = "checkpoint"
	OpCheckpoints  RPCOp = "checkpoints"
	OpRestore      RPCOp = "restore"
	OpMode         RPCOp = "mode"
	OpModules      RPCOp = "modules"
	OpRestartEmacs RPCOp = "restart-emacs"
	OpStuck        RPCOp = "stuck"
	OpStatus       RPCOp = "status"
	OpDescribe     RPCOp = "describe"
	OpDropStep     RPCOp = "drop-step"
)

// RPCResponse is the success body of every RPC: {"ok":true,"result":…}.
// Errors are problem+json (Problem) with a non-2xx status.
type RPCResponse[T any] struct {
	OK     bool `json:"ok"`
	Result T    `json:"result"`
}

// RawRPCResponse is RPCResponse with an undecoded result.
type RawRPCResponse = RPCResponse[json.RawMessage]

// Empty is the request or result of operations that carry nothing ({}).
type Empty struct{}

// KeysRequest sends keys to the frame. Exactly one of Keys and Text is set.
// Not idempotent.
type KeysRequest struct {
	// Keys is a key sequence in Emacs notation ("C-x C-f", "M-x", "<f3>"),
	// validated with ParseKeys. sessiond sends it with tmux send-keys
	// (terminal) or xdotool (gui).
	Keys string `json:"keys,omitempty"`
	// Text is typed literally (tmux send-keys -l, xdotool type).
	Text string `json:"text,omitempty"`
	// Frame selects the frame; default is the current mode.
	Frame FrameKind `json:"frame,omitempty"`
}

// EvalRequest evaluates an Emacs Lisp form in the daemon (author mode,
// tests). The gateway only proxies it when features.authorMode is on.
type EvalRequest struct {
	Form string `json:"form"`
	// TimeoutMs defaults to 2000, maximum 10000.
	TimeoutMs int `json:"timeoutMs,omitempty"`
}

// EvalResult is the value as produced by json-serialize; values that cannot
// be serialised become their printed representation (a string).
type EvalResult struct {
	Value any `json:"value"`
}

// RescueResult lists what playground-rescue did, as short sentences
// ("cancelled the prompt", "closed the extra windows").
type RescueResult struct {
	Did []string `json:"did"`
}

// ScenarioRequest loads a lesson's scenario (architecture.md §3.3).
type ScenarioRequest struct {
	LessonID string `json:"lessonId"`
	// StepID starts at a given step; default: the first step not yet
	// complete according to sessiond's state, else the first step.
	StepID string `json:"stepId,omitempty"`
	// Reset forces fixture extraction over learner files (setup.reset).
	Reset bool `json:"reset,omitempty"`
}

// ScenarioResult reports the loaded step.
type ScenarioResult struct {
	Step StepRef `json:"step"`
	// Index and Count locate the step in the scenario.
	Index int `json:"index"`
	Count int `json:"count"`
	// Modules is the module set now enabled.
	Modules []string `json:"modules"`
	// Restarting is true when loading changed the module set and the daemon
	// is restarting (the step becomes active after the restart).
	Restarting bool `json:"restarting"`
}

// CheckResult forces an evaluation of the active step now. When no step is
// active the RPC fails with ErrConflict.
type CheckResult struct {
	LessonID   string       `json:"lessonId"`
	StepID     string       `json:"stepId"`
	Conditions []CondResult `json:"conditions"`
	Complete   bool         `json:"complete"`
}

// HintRequest records that the learner revealed a hint (progress counts it).
type HintRequest struct {
	StepID string `json:"stepId"`
	// Level is the hint level now shown (1 for the first hint).
	Level int `json:"level"`
}

// CheckpointRequest takes a manual checkpoint (projects).
type CheckpointRequest struct {
	// Label is free text; sessiond derives a ref-safe name from it.
	Label string `json:"label"`
}

// CheckpointResult names the ref created.
type CheckpointResult struct {
	Ref       string `json:"ref"`
	CreatedAt int64  `json:"createdAt"`
}

// CheckpointInfo is one checkpoint in a listing.
type CheckpointInfo struct {
	Label     string `json:"label"`
	Ref       string `json:"ref"`
	CreatedAt int64  `json:"createdAt"`
	// Files lists workspace-relative paths whose content differs between the
	// checkpoint and the working tree: what a restore would overwrite or
	// delete.
	Files []string `json:"files"`
	// Step is set for step checkpoints.
	Step *StepRef `json:"step,omitempty"`
}

// CheckpointsResult lists checkpoints, newest first.
type CheckpointsResult struct {
	Items []CheckpointInfo `json:"items"`
}

// RestoreRequest restores a checkpoint by label or ref (exactly one). An
// empty request restores the active step's checkpoint. Not idempotent.
type RestoreRequest struct {
	Label string `json:"label,omitempty"`
	Ref   string `json:"ref,omitempty"`
}

// RestoreResult reports how many files were restored or removed.
type RestoreResult struct {
	RestoredFiles int `json:"restoredFiles"`
}

// ModeRequest switches the frame shown to the browser.
type ModeRequest struct {
	Mode FrameKind `json:"mode"`
}

// ModeResult echoes the mode now active.
type ModeResult struct {
	Mode FrameKind `json:"mode"`
}

// ModulesRequest replaces the enabled module set.
type ModulesRequest struct {
	Enabled []string `json:"enabled"`
}

// ModulesResult tells whether the daemon restarts.
type ModulesResult struct {
	Restarting bool `json:"restarting"`
}

// StuckRPCRequest asks sessiond for its recent events (it also files the
// report with the gateway when called by the browser directly).
type StuckRPCRequest struct {
	Note string `json:"note"`
}

// StuckRPCResult returns the last 200 events, oldest first, as raw JSON
// frames exactly as sent on /events.
type StuckRPCResult struct {
	Events []json.RawMessage `json:"events"`
}

// EmacsStatus is the detailed Emacs description in StatusResult.
type EmacsStatus struct {
	Version  string `json:"version"`
	PID      int    `json:"pid"`
	UptimeMs int64  `json:"uptimeMs"`
	// Agent is true while the agent socket is connected.
	Agent bool `json:"agent"`
	// Responsive is false after missed heartbeats.
	Responsive bool `json:"responsive"`
}

// StatusResult is the result of rpc status and the body of GET /status.
type StatusResult struct {
	SessionID string      `json:"sessionId"`
	Emacs     EmacsStatus `json:"emacs"`
	Frames    Frames      `json:"frames"`
	Mode      FrameKind   `json:"mode"`
	Step      *StepRef    `json:"step"`
	Modules   []string    `json:"modules"`
	// Versions are the toolchain versions from images/session/versions.env.
	Versions map[string]string `json:"versions,omitempty"`
	// ActiveWebSockets counts open /pty, /events and /vnc connections.
	ActiveWebSockets int `json:"activeWebSockets"`
}

// DescribeRequest asks what a key sequence runs (ticker glosses for commands
// missing from tables.gloss).
type DescribeRequest struct {
	Keys string `json:"keys"`
}

// DescribeResult is the agent's playground-describe answer.
type DescribeResult struct {
	// Cmd is the command name, or "" when the key is unbound.
	Cmd string `json:"cmd"`
	// Doc is the first line of the docstring.
	Doc string `json:"doc"`
}

// DropStepRequest tells sessiond to forget the active step if it belongs to
// LessonID (sent by the gateway after POST /progress/reset).
type DropStepRequest struct {
	LessonID string `json:"lessonId"`
}

// RPCSpec pairs an operation with its request and result types, for
// generators and the typed client in the browser.
type RPCSpec struct {
	Op RPCOp
	// Request and Result are zero values of the request and result types.
	Request any
	Result  any
	// Idempotent is false for keys and restore.
	Idempotent bool
	// Internal ops are called by the gateway, not by the browser.
	Internal bool
}

// RPCSpecs lists every session RPC.
var RPCSpecs = []RPCSpec{
	{Op: OpKeys, Request: KeysRequest{}, Result: Empty{}},
	{Op: OpEval, Request: EvalRequest{}, Result: EvalResult{}, Idempotent: true},
	{Op: OpRescue, Request: Empty{}, Result: RescueResult{}, Idempotent: true},
	{Op: OpScenario, Request: ScenarioRequest{}, Result: ScenarioResult{}, Idempotent: true},
	{Op: OpCheck, Request: Empty{}, Result: CheckResult{}, Idempotent: true},
	{Op: OpHint, Request: HintRequest{}, Result: Empty{}, Idempotent: true},
	{Op: OpCheckpoint, Request: CheckpointRequest{}, Result: CheckpointResult{}, Idempotent: true},
	{Op: OpCheckpoints, Request: Empty{}, Result: CheckpointsResult{}, Idempotent: true},
	{Op: OpRestore, Request: RestoreRequest{}, Result: RestoreResult{}},
	{Op: OpMode, Request: ModeRequest{}, Result: ModeResult{}, Idempotent: true},
	{Op: OpModules, Request: ModulesRequest{}, Result: ModulesResult{}, Idempotent: true},
	{Op: OpRestartEmacs, Request: Empty{}, Result: Empty{}, Idempotent: true},
	{Op: OpStuck, Request: StuckRPCRequest{}, Result: StuckRPCResult{}, Idempotent: true},
	{Op: OpStatus, Request: Empty{}, Result: StatusResult{}, Idempotent: true},
	{Op: OpDescribe, Request: DescribeRequest{}, Result: DescribeResult{}, Idempotent: true},
	{Op: OpDropStep, Request: DropStepRequest{}, Result: Empty{}, Idempotent: true, Internal: true},
}
