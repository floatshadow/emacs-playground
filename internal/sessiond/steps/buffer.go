package steps

import (
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// BufferForm turns a buffer condition into the elisp form the agent's
// check evaluates: non-nil when a buffer named c.Name (or with a uniquify
// suffix, "main.rs<hello>") exists and the other fields hold.
func BufferForm(c *protocol.BufferCond) string {
	name := ElispString(c.Name)
	var tests []string
	if c.Mode != "" {
		tests = append(tests, "(derived-mode-p (intern "+ElispString(c.Mode)+"))")
	}
	if c.Contains != "" {
		tests = append(tests, "(save-excursion (save-restriction (widen) (goto-char (point-min)) (search-forward "+ElispString(c.Contains)+" nil t)))")
	}
	switch c.Process {
	case protocol.ProcessRunning:
		tests = append(tests, "(process-live-p (get-buffer-process (current-buffer)))")
	case protocol.ProcessNone:
		tests = append(tests, "(not (process-live-p (get-buffer-process (current-buffer))))")
	}
	if c.Modified != nil {
		if *c.Modified {
			tests = append(tests, "(buffer-modified-p)")
		} else {
			tests = append(tests, "(not (buffer-modified-p))")
		}
	}
	tests = append(tests, "t")
	return "(let ((pg-b (or (get-buffer " + name + ") (seq-find (lambda (b) (let ((n (buffer-name b))) (and (string-prefix-p (concat " + name + " \"<\") n) (string-suffix-p \">\" n)))) (buffer-list))))) " +
		"(and pg-b (with-current-buffer pg-b (and " + strings.Join(tests, " ") + "))))"
}

// ElispString renders s as an Emacs Lisp string literal.
func ElispString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
