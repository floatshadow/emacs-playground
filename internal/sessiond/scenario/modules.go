package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DefaultModules is the module set of a fresh workspace (backend.md §3.2).
var DefaultModules = []string{"base", "agent"}

var moduleName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateModules checks module names.
func ValidateModules(mods []string) error {
	seen := map[string]bool{}
	for _, m := range mods {
		if !moduleName.MatchString(m) {
			return fmt.Errorf("invalid module name %q", m)
		}
		if seen[m] {
			return fmt.Errorf("module %q listed twice", m)
		}
		seen[m] = true
	}
	return nil
}

var modulesList = regexp.MustCompile(`(?s)playground-modules\s+'\(([^)]*)\)`)

// ParseModulesEl reads the module list from modules.el content.
func ParseModulesEl(src string) ([]string, bool) {
	m := modulesList.FindStringSubmatch(src)
	if m == nil {
		return nil, false
	}
	return append([]string{}, strings.Fields(m[1])...), true
}

// RenderModulesEl renders modules.el for mods.
func RenderModulesEl(mods []string) string {
	return ";;; modules.el --- written by sessiond; edit through the course or the session page  -*- lexical-binding: t -*-\n" +
		";; The configuration modules loaded by init.el, in order (emacs.md §3, §4).\n" +
		"(setq playground-modules '(" + strings.Join(mods, " ") + "))\n"
}

// Modules reads and writes /work/emacs.d/modules.el.
type Modules struct {
	Path string
}

// Current returns the enabled modules (DefaultModules when the file is
// missing or unreadable).
func (m *Modules) Current() []string {
	b, err := os.ReadFile(m.Path)
	if err != nil {
		return slices.Clone(DefaultModules)
	}
	mods, ok := ParseModulesEl(string(b))
	if !ok {
		return slices.Clone(DefaultModules)
	}
	return mods
}

// Write replaces modules.el atomically.
func (m *Modules) Write(mods []string) error {
	if err := ValidateModules(mods); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		return err
	}
	tmp := m.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(RenderModulesEl(mods)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.Path)
}

// Union returns cur followed by the modules of want that cur lacks, in
// want's order (monotonic unlocking).
func Union(cur, want []string) []string {
	out := slices.Clone(cur)
	for _, w := range want {
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}
