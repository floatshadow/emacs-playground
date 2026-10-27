package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Scenario is the parsed scenario.yaml of a lesson or project
// (CURRICULUM.md Part B.3, api.md §6). The gateway serves it inside
// LessonBundle; sessiond fetches it from /internal/curriculum/scenarios.
//
// YAML is decoded by converting it to JSON (goccy/go-yaml YAMLToJSON) and
// unmarshalling into these types, so the JSON tags below are also the YAML
// keys.
type Scenario struct {
	// ID is the lesson id: the lesson directory relative to the curriculum
	// root, e.g. "02-buffers-and-files/03-opening-files" or
	// "projects/p6-polyglot".
	ID string `json:"id"`
	// Title is the lesson title.
	Title LocalText `json:"title"`
	// Minutes is the rough duration.
	Minutes int `json:"minutes"`
	// Mode is the frame the lesson needs: terminal (default), gui or any.
	Mode LessonMode `json:"mode,omitempty"`
	// Tags: remote, gui, project.
	Tags []string `json:"tags,omitempty"`
	// Modules is the full module set enabled while the lesson runs, in load
	// order.
	Modules []string `json:"modules"`
	// VSCode names the VS Code habit the lesson replaces.
	VSCode string `json:"vscode,omitempty"`
	// Preview configures the preview pane (optional).
	Preview *PreviewSpec `json:"preview,omitempty"`
	// Remote requests the SSH box (optional).
	Remote *RemoteSpec `json:"remote,omitempty"`
	// Setup prepares the workspace and Emacs when the scenario loads.
	Setup Setup `json:"setup"`
	// Steps are the lesson's goals in order.
	Steps []Step `json:"steps"`
}

// LessonMode is the frame a lesson runs in.
type LessonMode string

// Lesson modes.
const (
	LessonTerminal LessonMode = "terminal"
	LessonGUI      LessonMode = "gui"
	LessonAny      LessonMode = "any"
)

// Lesson tags.
const (
	TagRemote  = "remote"
	TagGUI     = "gui"
	TagProject = "project"
)

// PreviewSpec configures the preview pane.
type PreviewSpec struct {
	// Tabs are workspace-relative files opened as preview tabs.
	Tabs []string `json:"tabs,omitempty"`
	// Follow maps a source file to the artefact shown while that source is
	// the current buffer, e.g. {"docs/report.typ": "docs/report.pdf"}.
	Follow map[string]string `json:"follow,omitempty"`
}

// RemoteSpec requests the SSH box.
type RemoteSpec struct {
	Box bool `json:"box"`
}

// Setup prepares the workspace and Emacs for a scenario.
type Setup struct {
	// Workspace is the fixture directory relative to the lesson directory,
	// e.g. "fixtures/hello-rust". Empty: no fixtures.
	Workspace string `json:"workspace,omitempty"`
	// Root is the directory under /work the fixtures extract to, e.g.
	// "hello" for /work/hello.
	Root string `json:"root,omitempty"`
	// Reset restores Root to the fixtures: learner edits are overwritten and
	// files the fixtures do not contain are removed (build directories such
	// as target/ and .venv/ are kept). Emacs reverts the affected buffers
	// and clears unmodified leftovers (AgentSetupArgs.Reset).
	Reset bool `json:"reset,omitempty"`
	// Open lists workspace-relative files to visit, e.g. "hello/README.md".
	Open []string `json:"open,omitempty"`
	// Layout is "single", a layout pattern such as
	// "(vsplit hello/src/main.rs *compilation*)", or a window-state file
	// relative to the lesson directory (ending in .eld).
	Layout string `json:"layout,omitempty"`
	// Eval is an Emacs Lisp form run after Open and Layout.
	Eval string `json:"eval,omitempty"`
	// Compile is a command run once so *compilation* exists; null or empty
	// for none. It runs in the project root (project.el) of the first Open
	// file, or that file's directory when it is in no project.
	Compile string `json:"compile,omitempty"`
}

// Step is one goal of a lesson.
type Step struct {
	// ID is unique within the scenario, [a-z0-9-]+.
	ID string `json:"id"`
	// Goal is the one-line goal shown in the step list.
	Goal LocalText `json:"goal"`
	// Detail is a longer explanation (Markdown, keys keyified).
	Detail LocalText `json:"detail,omitempty"`
	// Demo is a demo string that completes the step when replayed
	// (ParseDemo); required for every step (ADR-017).
	Demo string `json:"demo,omitempty"`
	// Conditions are ordered: the hint shown is the first unmet one's.
	Conditions []Condition `json:"conditions"`
	// Stuck rules produce server-side nudges.
	Stuck []StuckRule `json:"stuck,omitempty"`
	// Checkpoint takes a checkpoint when the step starts. Default true; set
	// false explicitly to skip (a pointer so absence means default).
	Checkpoint *bool `json:"checkpoint,omitempty"`
}

// TakesCheckpoint reports whether a checkpoint is taken when the step
// starts (default true).
func (s *Step) TakesCheckpoint() bool { return s.Checkpoint == nil || *s.Checkpoint }

// ConditionKind names the kind of a Condition (CURRICULUM.md Part B.3).
type ConditionKind string

// Condition kinds.
const (
	CondEvent  ConditionKind = "event"
	CondElisp  ConditionKind = "elisp"
	CondFile   ConditionKind = "file"
	CondShell  ConditionKind = "shell"
	CondLayout ConditionKind = "layout"
	CondState  ConditionKind = "state"
	CondAll    ConditionKind = "all"
	CondAny    ConditionKind = "any"
	CondNot    ConditionKind = "not"
)

// Condition is one predicate of a step. Exactly one kind field (Event,
// Elisp, File, Shell, Layout, State, All, Any, Not) is set. Text and Hint
// are used on top-level conditions; nested conditions inside all, any and
// not only need the kind field.
type Condition struct {
	// ID overrides the derived id ConditionID(stepID, index).
	ID string `json:"id,omitempty"`
	// Text is the short description shown in the step's condition list.
	Text LocalText `json:"text,omitempty"`
	// Hint is shown when this is the first unmet condition and the learner
	// asked for a hint.
	Hint LocalText `json:"hint,omitempty"`

	Event  *EventCond  `json:"event,omitempty"`
	Elisp  string      `json:"elisp,omitempty"`
	File   *FileCond   `json:"file,omitempty"`
	Shell  *ShellCond  `json:"shell,omitempty"`
	Layout string      `json:"layout,omitempty"`
	State  StateCond   `json:"state,omitempty"`
	All    []Condition `json:"all,omitempty"`
	Any    []Condition `json:"any,omitempty"`
	Not    *Condition  `json:"not,omitempty"`
}

// Kind returns the condition's kind, or "" when no kind field is set.
// When several are set (an authoring error caught by validation) the first
// in declaration order wins.
func (c *Condition) Kind() ConditionKind {
	if kinds := c.Kinds(); len(kinds) > 0 {
		return kinds[0]
	}
	return ""
}

// Kinds returns every kind field that is set, for validation (exactly one
// is allowed).
func (c *Condition) Kinds() []ConditionKind {
	var out []ConditionKind
	if c.Event != nil {
		out = append(out, CondEvent)
	}
	if c.Elisp != "" {
		out = append(out, CondElisp)
	}
	if c.File != nil {
		out = append(out, CondFile)
	}
	if c.Shell != nil {
		out = append(out, CondShell)
	}
	if c.Layout != "" {
		out = append(out, CondLayout)
	}
	if c.State != nil {
		out = append(out, CondState)
	}
	if c.All != nil {
		out = append(out, CondAll)
	}
	if c.Any != nil {
		out = append(out, CondAny)
	}
	if c.Not != nil {
		out = append(out, CondNot)
	}
	return out
}

// ConditionID derives the id of the top-level condition at 0-based index i
// of step stepID: "<stepId>/<i+1>" (1-based in the id, so the first
// condition of step "open" is "open/1").
func ConditionID(stepID string, i int) string { return stepID + "/" + strconv.Itoa(i+1) }

// StuckRuleID derives the id of the stuck rule at 0-based index i of step
// stepID: "<stepId>/stuck/<i+1>".
func StuckRuleID(stepID string, i int) string { return stepID + "/stuck/" + strconv.Itoa(i+1) }

// AssignIDs fills empty Condition.ID and StuckRule.ID fields of top-level
// conditions and stuck rules with their derived ids. The gateway calls it
// when building the bundle so the browser and sessiond agree.
func (s *Scenario) AssignIDs() {
	for si := range s.Steps {
		st := &s.Steps[si]
		for i := range st.Conditions {
			if st.Conditions[i].ID == "" {
				st.Conditions[i].ID = ConditionID(st.ID, i)
			}
		}
		for i := range st.Stuck {
			if st.Stuck[i].ID == "" {
				st.Stuck[i].ID = StuckRuleID(st.ID, i)
			}
		}
	}
}

// StepIndex returns the index of the step with id, or -1.
func (s *Scenario) StepIndex(id string) int {
	for i := range s.Steps {
		if s.Steps[i].ID == id {
			return i
		}
	}
	return -1
}

// EventSince selects the window an event condition counts commands in.
type EventSince string

// Event condition windows.
const (
	SinceStep   EventSince = "step"
	SinceLesson EventSince = "lesson"
)

// EventCond is true when Command ran at least Min times (default 1) since
// the step (default) or the lesson started.
type EventCond struct {
	Command string     `json:"command"`
	Min     int        `json:"min,omitempty"`
	Since   EventSince `json:"since,omitempty"`
}

// MinOrDefault returns Min, or 1 when unset.
func (e *EventCond) MinOrDefault() int {
	if e.Min <= 0 {
		return 1
	}
	return e.Min
}

// FileCond is a predicate on a workspace file. Path is workspace-relative.
// Exactly one predicate is set: Exists, Contains, NotContains, Regex, or
// JSONPath with Equals. Files are read through os.Root, capped at 1 MB.
type FileCond struct {
	Path string `json:"path"`
	// Exists: true requires the file to exist, false requires it not to.
	Exists *bool `json:"exists,omitempty"`
	// Contains requires the substring.
	Contains string `json:"contains,omitempty"`
	// NotContains requires the file to exist and not contain the substring.
	NotContains string `json:"not_contains,omitempty"`
	// Regex is an RE2 regular expression that must match somewhere.
	Regex string `json:"regex,omitempty"`
	// JSONPath is a dotted path into a JSON file ("package.version",
	// "items.0.name"); Equals is compared with JSON equality.
	JSONPath string `json:"json_path,omitempty"`
	Equals   any    `json:"equals,omitempty"`
}

// ShellCond runs `sh -c Run` as the learner; exit status 0 is true.
type ShellCond struct {
	Run string `json:"run"`
	// Cwd is workspace-relative; default the scenario's setup root.
	Cwd string `json:"cwd,omitempty"`
	// Timeout in seconds, default 5; a timeout yields unknown.
	Timeout int `json:"timeout,omitempty"`
}

// TimeoutOrDefault returns Timeout, or 5 when unset.
func (s *ShellCond) TimeoutOrDefault() int {
	if s.Timeout <= 0 {
		return 5
	}
	return s.Timeout
}

// StateCond maps State field paths to expected values, e.g.
// {"narrowed": true} or {"minibuffer.kind": "prompt"}. Values use the
// Predicate operators (see Predicate); all entries must hold.
type StateCond = Predicate

// StuckRule emits a nudge when its When holds for the active step.
type StuckRule struct {
	// ID overrides the derived id StuckRuleID(stepID, index).
	ID   string    `json:"id,omitempty"`
	When StuckWhen `json:"when"`
	// Say is the nudge body.
	Say LocalText `json:"say"`
	// Title is the nudge title; default: the first sentence of Say.
	Title LocalText `json:"title,omitempty"`
	// Kind is the nudge tone, default calm.
	Kind NudgeKind `json:"kind,omitempty"`
}

// StuckWhen is a conjunction of time and key-stream thresholds; every set
// field must hold. Evaluated by sessiond on its 2 s ticker.
type StuckWhen struct {
	// MinibufferSeconds: a minibuffer has been open this long.
	MinibufferSeconds int `json:"minibuffer_seconds,omitempty"`
	// IdleSeconds: no key event for this long.
	IdleSeconds int `json:"idle_seconds,omitempty"`
	// StepSeconds: the step has been active this long.
	StepSeconds int `json:"step_seconds,omitempty"`
	// QuitsInWindow: at least Count quits (keyboard-quit,
	// abort-minibuffers, abort-recursive-edit) within Seconds.
	QuitsInWindow *CountWindow `json:"quits_in_window,omitempty"`
	// UnboundRepeats: the same unbound key sequence this many times in a
	// row.
	UnboundRepeats int `json:"unbound_repeats,omitempty"`
}

// CountWindow is "Count occurrences within Seconds".
type CountWindow struct {
	Count   int `json:"count"`
	Seconds int `json:"seconds"`
}

// DefaultLocale is the locale of plain strings in lessons and tables.
const DefaultLocale = "en"

// LocalText is a possibly localised string. In YAML and JSON it is either a
// plain string (the default locale, "en") or a map from locale to string
// ({"en": "...", "zh-CN": "..."}). It always marshals to the plain string
// when only "en" is present, else to the map.
//
// Schema generators must special-case this type; the TypeScript form is
// `type LocalText = string | Record<string, string>`.
type LocalText map[string]string

// Text returns a LocalText holding s in the default locale.
func Text(s string) LocalText {
	if s == "" {
		return nil
	}
	return LocalText{DefaultLocale: s}
}

// Get returns the text for locale, falling back to the base language
// ("zh" for "zh-CN"), then "en", then any entry in sorted locale order.
func (t LocalText) Get(locale string) string {
	if len(t) == 0 {
		return ""
	}
	if s, ok := t[locale]; ok {
		return s
	}
	if i := strings.IndexByte(locale, '-'); i > 0 {
		if s, ok := t[locale[:i]]; ok {
			return s
		}
	}
	if s, ok := t[DefaultLocale]; ok {
		return s
	}
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return t[keys[0]]
}

// String returns the default-locale text.
func (t LocalText) String() string { return t.Get(DefaultLocale) }

// IsZero reports whether the text is empty in every locale.
func (t LocalText) IsZero() bool {
	for _, v := range t {
		if v != "" {
			return false
		}
	}
	return true
}

// MarshalJSON implements json.Marshaler.
func (t LocalText) MarshalJSON() ([]byte, error) {
	if len(t) == 0 {
		return []byte(`""`), nil
	}
	if len(t) == 1 {
		if s, ok := t[DefaultLocale]; ok {
			return json.Marshal(s)
		}
	}
	return json.Marshal(map[string]string(t))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *LocalText) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*t = nil
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = Text(s)
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("localised text: want a string or a map of locale to string: %w", err)
	}
	*t = LocalText(m)
	return nil
}
