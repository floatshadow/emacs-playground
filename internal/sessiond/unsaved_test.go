package sessiond

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
