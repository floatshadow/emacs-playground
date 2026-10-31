package sessiond

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/agent/fakeeditor"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
)

func TestBufferStateForms(t *testing.T) {
	capture := buffersForm("/work")
	for _, want := range []string{`(ws "/work/")`, "buffer-modified-p", "buffer-undo-list", "262144", "131072", "524288"} {
		if !strings.Contains(capture, want) {
			t.Fatalf("capture form lacks %q:\n%s", want, capture)
		}
	}
	bufs := map[string]checkpoint.BufferState{
		"editing/notes/journal.md": {Modified: true, HasText: true, Text: "# Journal\n\n## Wednesday \"quoted\" \\ back\n", Undo: `(nil ("x" . 3))`},
		"a.txt":                    {Undo: `(nil (1 . 2))`},
		"big.txt":                  {Modified: true},
	}
	restore := restoreBuffersForm("/work/", bufs, map[string]int{"editing/notes/journal.md": 12})
	for _, want := range []string{
		`(ws "/work/")`,
		`("a.txt" nil "(nil (1 . 2))" 0)`,
		`("big.txt" nil nil 0)`,
		`("editing/notes/journal.md" "# Journal` + "\n\n" + `## Wednesday \"quoted\" \\ back` + "\n" + `" "(nil (\"x\" . 3))" 12)`,
		"replace-buffer-contents", "(buffer-undo-list t)", "condition-case",
	} {
		if !strings.Contains(restore, want) {
			t.Fatalf("restore form lacks %q:\n%s", want, restore)
		}
	}
	// Sorted, so the form is stable.
	if strings.Index(restore, `"a.txt"`) > strings.Index(restore, `"editing/`) {
		t.Fatal("entries are not sorted")
	}
	// PG_TEST_FORMS_DIR=dir writes the capture forms for a check in real
	// Emacs (the session image).
	if dir := os.Getenv("PG_TEST_FORMS_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "capture.el"), []byte(buffersForm("/tmp/ws")), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "globals.el"), []byte(globalsForm()), 0o644)
		_ = os.WriteFile(filepath.Join(dir, "restore-globals.el"), []byte(restoreGlobalsForm("GLOBALS")), 0o644)
	}
}

func TestGlobalsForms(t *testing.T) {
	capture := globalsForm()
	for _, v := range GlobalVars {
		if !strings.Contains(capture, v) {
			t.Fatalf("capture form lacks %s", v)
		}
	}
	restore := restoreGlobalsForm(`((kill-ring "a \"b\"") (kmacro-counter . 3))`)
	if !strings.Contains(restore, `(read-from-string "((kill-ring \"a \\\"b\\\"\") (kmacro-counter . 3))")`) ||
		!strings.Contains(restore, "kill-ring-yank-pointer") {
		t.Fatalf("restore form:\n%s", restore)
	}
}

// The fake editor answers the checkpoint forms from its model: a capture
// followed by edits and a restore brings the unsaved text, undo, point and
// session variables back, and modified files lists the unsaved buffer.
func TestFakeEditorAnswersCheckpointForms(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := fakeeditor.New(ws)
	eval := func(form string) json.RawMessage {
		t.Helper()
		args, _ := json.Marshal(protocol.AgentEvalArgs{Form: form})
		v, err := ed.Handle(protocol.AgentOpEval, args)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(v)
		var r protocol.AgentEvalResult
		_ = json.Unmarshal(b, &r)
		out, _ := json.Marshal(r.Value)
		return out
	}
	if err := ed.Press("C-x C-f"); err != nil {
		t.Fatal(err)
	}
	ed.Type("a.txt")
	if err := ed.Press("RET C-k"); err != nil {
		t.Fatal(err)
	}
	if got := string(eval(modifiedFilesForm(ws))); got != `["a.txt"]` {
		t.Fatalf("modified files %s", got)
	}
	var bufs map[string][3]json.RawMessage
	if err := json.Unmarshal(eval(buffersForm(ws)), &bufs); err != nil || string(bufs["a.txt"][0]) != "true" || string(bufs["a.txt"][1]) != `"\n"` {
		t.Fatalf("buffers %v %v", bufs, err)
	}
	var printed string
	if err := json.Unmarshal(eval(globalsForm()), &printed); err != nil || !strings.Contains(printed, `"kill-ring":["alpha"]`) {
		t.Fatalf("globals %q %v", printed, err)
	}
	before := ed.Buffer("a.txt")
	var undo string
	_ = json.Unmarshal(bufs["a.txt"][2], &undo)
	state := map[string]checkpoint.BufferState{"a.txt": {Modified: true, HasText: true, Text: "\n", Undo: undo}}

	ed.Restart()
	if got := string(eval(restoreBuffersForm(ws, state, map[string]int{"a.txt": 1}))); got != "1" {
		t.Fatalf("restore buffers %s", got)
	}
	if got := string(eval(restoreGlobalsForm(printed))); got == "null" {
		t.Fatalf("restore globals %s", got)
	}
	after := ed.Buffer("a.txt")
	if after == nil || after.Text != "\n" || !after.Modified || len(after.Undo) != len(before.Undo) || after.Point != 1 {
		t.Fatalf("a.txt after the restore %+v (before %+v)", after, before)
	}
	if kr := ed.Snapshot().Globals["kill-ring"]; !reflect.DeepEqual(kr, []string{"alpha"}) {
		t.Fatalf("kill-ring after the restore %v", kr)
	}
}
