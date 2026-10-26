package curriculum

import (
	"fmt"
	"strings"
)

// Severity of a diagnostic.
type Severity int

// Severities.
const (
	Error Severity = iota
	Warning
)

func (s Severity) String() string {
	if s == Warning {
		return "warning"
	}
	return "error"
}

// Diagnostic is one validation finding.
type Diagnostic struct {
	Severity Severity
	// File is the source file, relative to the curriculum directory.
	File string
	// Where locates the finding inside the file ("steps[1].conditions[0]").
	Where string
	Msg   string
}

func (d Diagnostic) String() string {
	var b strings.Builder
	b.WriteString(d.Severity.String())
	b.WriteString(": ")
	if d.File != "" {
		b.WriteString(d.File)
		b.WriteString(": ")
	}
	if d.Where != "" {
		b.WriteString(d.Where)
		b.WriteString(": ")
	}
	b.WriteString(d.Msg)
	return b.String()
}

// Diagnostics is a list of findings.
type Diagnostics []Diagnostic

// HasErrors reports whether any diagnostic is an error.
func (ds Diagnostics) HasErrors() bool { return ds.Count(Error) > 0 }

// Count returns the number of diagnostics with severity s.
func (ds Diagnostics) Count(s Severity) int {
	n := 0
	for _, d := range ds {
		if d.Severity == s {
			n++
		}
	}
	return n
}

// Err returns nil when there are no errors, else an error listing them.
func (ds Diagnostics) Err() error {
	if !ds.HasErrors() {
		return nil
	}
	var lines []string
	for _, d := range ds {
		if d.Severity == Error {
			lines = append(lines, d.String())
		}
	}
	return fmt.Errorf("curriculum: %d error(s):\n%s", len(lines), strings.Join(lines, "\n"))
}

// reporter accumulates diagnostics for one file.
type reporter struct {
	diags *Diagnostics
	file  string
}

func (r reporter) errorf(where, format string, a ...any) {
	*r.diags = append(*r.diags, Diagnostic{Severity: Error, File: r.file, Where: where, Msg: fmt.Sprintf(format, a...)})
}

func (r reporter) warnf(where, format string, a ...any) {
	*r.diags = append(*r.diags, Diagnostic{Severity: Warning, File: r.file, Where: where, Msg: fmt.Sprintf(format, a...)})
}
