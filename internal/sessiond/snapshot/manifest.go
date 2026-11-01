package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
)

// ManifestVersion is the version Capture writes. Version 0 (no version
// field) has the same fields and is read unchanged.
const ManifestVersion = 1

// Manifest is what a checkpoint keeps beside its commit, in
// .playground/checkpoints/<ref without refs/playground/>.json. The ref is
// published after the manifest is written, so a ref names a checkpoint
// only when its manifest exists and names the ref's commit (a ref without
// one predates the snapshot service and is invisible; a manifest of
// another commit is a recapture that stopped before the ref moved).
type Manifest struct {
	Version   int               `json:"version,omitempty"`
	Label     string            `json:"label"`
	Ref       string            `json:"ref"`
	CreatedAt int64             `json:"createdAt"`
	Step      *protocol.StepRef `json:"step,omitempty"`
	// Commit is the commit the ref publishes; absent in version 0.
	Commit string `json:"commit,omitempty"`
	// Spec, Buffers and Globals are the editor state (EditorState), flat
	// for version 0; absent when the checkpoint was taken without an
	// agent.
	Spec    *protocol.WindowSpec   `json:"spec,omitempty"`
	Buffers map[string]BufferState `json:"buffers,omitempty"`
	Globals string                 `json:"globals,omitempty"`
	// EmptyDirs and Nested are what the commit cannot hold
	// (checkpoint.Tree).
	EmptyDirs []string                `json:"emptyDirs,omitempty"`
	Nested    []checkpoint.NestedRepo `json:"nested,omitempty"`
}

// Editor returns the manifest's editor state.
func (m Manifest) Editor() EditorState {
	return EditorState{Spec: m.Spec, Buffers: m.Buffers, Globals: m.Globals}
}

// BufferState is what a checkpoint keeps of one file buffer besides the
// file: a step can start with the previous step's unsaved edit and rely on
// its undo history (lesson 1.3 undoes a deletion within a region).
type BufferState struct {
	// Modified is true when the buffer had unsaved changes.
	Modified bool `json:"modified,omitempty"`
	// Text is the unsaved text (HasText), absent for unmodified or large
	// buffers.
	Text    string `json:"text,omitempty"`
	HasText bool   `json:"hasText,omitempty"`
	// Undo is the printed buffer-undo-list, reduced to readable entries;
	// opaque to sessiond.
	Undo string `json:"undo,omitempty"`
}

// EditorState is the editor state a checkpoint keeps: the window spec, the
// file buffers' unsaved text and undo by workspace-relative path, and the
// printed session variables (GlobalVars).
type EditorState struct {
	Spec    *protocol.WindowSpec
	Buffers map[string]BufferState
	Globals string
}

// manifestPath is where the manifest of ref lives.
func manifestPath(workspace, ref string) string {
	rel := strings.TrimPrefix(ref, checkpoint.RefPrefix)
	return filepath.Join(workspace, checkpoint.PlaygroundDir, "checkpoints", filepath.FromSlash(rel)+".json")
}

// writeJSON writes v to p atomically (a temporary file renamed over p).
func writeJSON(p string, v any, indent bool) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	var b []byte
	var err error
	if indent {
		b, err = json.MarshalIndent(v, "", "  ")
	} else {
		b, err = json.Marshal(v)
	}
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
