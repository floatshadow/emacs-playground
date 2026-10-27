package curriculum

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Modules are the configuration modules of emacs.md §3, in their
// conventional load order.
var Modules = []string{
	"base", "agent", "editing", "completion", "files", "windows", "git", "terminals",
	"lsp", "lang-rust", "lang-ocaml", "lang-python", "writing", "org", "power",
	"remote", "ui", "input",
}

var knownTags = map[string]bool{protocol.TagRemote: true, protocol.TagGUI: true, protocol.TagProject: true}

var (
	lessonNRe  = regexp.MustCompile(`^([0-9]{1,2}\.[0-9]{1,2}|P[1-9][0-9]?)$`)
	stepIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	commandRe  = regexp.MustCompile(`^[^\s()"';]+$`)
	templateRe = regexp.MustCompile(`\{([A-Za-z0-9_.]+)\}`)
)

func validateIndex(src *Source, r reporter) {
	idx := &src.Index
	if len(idx.Parts) == 0 {
		r.errorf("parts", "no parts")
	}
	partIDs := map[string]bool{}
	lessonIDs := map[string]string{}
	for pi, p := range idx.Parts {
		w := "parts[" + itoa(pi) + "]"
		if p.N != pi {
			r.errorf(w+".n", "part numbers must run 0, 1, 2, … in order (got %d at position %d)", p.N, pi)
		}
		if !idSegRe.MatchString(p.ID) {
			r.errorf(w+".id", "%q is not a directory name ([a-z0-9-]+)", p.ID)
		}
		if partIDs[p.ID] {
			r.errorf(w+".id", "duplicate part id %q", p.ID)
		}
		partIDs[p.ID] = true
		if strings.TrimSpace(p.Title) == "" {
			r.errorf(w+".title", "required")
		}
		if p.Minutes <= 0 {
			r.errorf(w+".minutes", "must be positive")
		}
		if strings.TrimSpace(p.Outcome) == "" {
			r.warnf(w+".outcome", "missing outcome sentence")
		}
		for _, m := range p.Modules {
			if !slices.Contains(Modules, m) {
				r.errorf(w+".modules", "unknown module %q (emacs.md §3)", m)
			}
		}
		checkStatus(p.Status, w+".status", r)
		if p.SuggestedAfter != nil && (*p.SuggestedAfter < 0 || *p.SuggestedAfter >= p.N) {
			r.errorf(w+".suggestedAfter", "must name an earlier part")
		}
		allReady := len(p.Lessons) > 0
		for li, l := range p.Lessons {
			lw := partWhere(pi, li)
			checkLessonID(l.ID, lw+".id", r)
			if prev, dup := lessonIDs[l.ID]; dup {
				r.errorf(lw+".id", "lesson %s is also listed at %s", l.ID, prev)
			}
			lessonIDs[l.ID] = lw
			if !strings.HasPrefix(l.ID, ProjectsPrefix) && !strings.HasPrefix(l.ID, p.ID+"/") {
				r.errorf(lw+".id", "lesson id %q must start with the part directory %q", l.ID, p.ID+"/")
			}
			if !lessonNRe.MatchString(l.N) {
				r.errorf(lw+".n", "%q is not a display number like \"2.3\" or \"P6\"", l.N)
			} else if !strings.HasPrefix(l.N, "P") && !strings.HasPrefix(l.N, itoa(p.N)+".") {
				r.errorf(lw+".n", "%q does not belong to part %d", l.N, p.N)
			}
			if strings.TrimSpace(l.Title) == "" {
				r.errorf(lw+".title", "required")
			}
			if l.Minutes <= 0 {
				r.errorf(lw+".minutes", "must be positive")
			}
			for _, t := range l.Tags {
				if !knownTags[t] {
					r.errorf(lw+".tags", "unknown tag %q (gui, remote, project)", t)
				}
			}
			checkStatus(l.Status, lw+".status", r)
			if l.Status != protocol.Ready {
				allReady = false
			}
		}
		if p.Status == protocol.Ready && !allReady {
			r.errorf(w+".status", "part is ready but not every lesson is ready")
		}
	}
	projIDs := map[string]bool{}
	for i, pr := range idx.Projects {
		w := "projects[" + itoa(i) + "]"
		checkLessonID(pr.ID, w+".id", r)
		if !strings.HasPrefix(pr.ID, ProjectsPrefix) {
			r.errorf(w+".id", "project ids start with projects/")
		}
		if projIDs[pr.ID] {
			r.errorf(w+".id", "duplicate project %q", pr.ID)
		}
		projIDs[pr.ID] = true
		if !lessonNRe.MatchString(pr.N) || !strings.HasPrefix(pr.N, "P") {
			r.errorf(w+".n", "%q is not P1…P8", pr.N)
		}
		if strings.TrimSpace(pr.Title) == "" {
			r.errorf(w+".title", "required")
		}
		checkStatus(pr.Status, w+".status", r)
		taskIDs := map[string]bool{}
		for ti, t := range pr.Tasks {
			tw := w + ".tasks[" + itoa(ti) + "]"
			if !stepIDRe.MatchString(t.ID) {
				r.errorf(tw+".id", "%q is not a step id ([a-z0-9-]+)", t.ID)
			}
			if taskIDs[t.ID] {
				r.errorf(tw+".id", "duplicate task id %q", t.ID)
			}
			taskIDs[t.ID] = true
			if t.N != ti+1 {
				r.errorf(tw+".n", "tasks are numbered 1, 2, … in order")
			}
		}
	}
	// Projects referenced from a part must exist in projects.
	for pi, p := range idx.Parts {
		for li, l := range p.Lessons {
			if strings.HasPrefix(l.ID, ProjectsPrefix) && !projIDs[l.ID] {
				r.errorf(partWhere(pi, li)+".id", "project %s is not listed under projects", l.ID)
			}
		}
	}
}

func checkStatus(s protocol.Availability, where string, r reporter) {
	if s != protocol.Ready && s != protocol.Planned {
		r.errorf(where, "status must be ready or planned, got %q", s)
	}
}

func checkLessonID(id, where string, r reporter) {
	a, b, ok := strings.Cut(id, "/")
	if !ok || strings.Contains(b, "/") || !idSegRe.MatchString(a) || !idSegRe.MatchString(b) {
		r.errorf(where, "lesson id %q must be <part dir>/<lesson dir> with [a-z0-9-] names", id)
	}
}

// checkProjectTasks checks that a ready project's tasks are its scenario's
// steps.
func checkProjectTasks(src *Source, r reporter) {
	for i, pr := range src.Index.Projects {
		l := src.Lessons[pr.ID]
		if l == nil {
			continue
		}
		var steps []string
		for _, s := range l.Scenario.Steps {
			steps = append(steps, s.ID)
		}
		var tasks []string
		for _, t := range pr.Tasks {
			tasks = append(tasks, t.ID)
		}
		if strings.Join(steps, ",") != strings.Join(tasks, ",") {
			r.errorf("projects["+itoa(i)+"].tasks", "tasks %v do not match the scenario steps %v", tasks, steps)
		}
	}
}

// validateScenario checks one lesson's scenario.yaml.
func validateScenario(l *Lesson, r reporter) {
	s := &l.Scenario
	if s.ID != l.ID {
		r.errorf("id", "scenario id %q must equal the lesson directory %q", s.ID, l.ID)
	}
	if s.Title.IsZero() {
		r.errorf("title", "required")
	}
	if s.Minutes <= 0 {
		r.errorf("minutes", "must be positive")
	}
	switch s.Mode {
	case "", protocol.LessonTerminal, protocol.LessonGUI, protocol.LessonAny:
	default:
		r.errorf("mode", "must be terminal, gui or any, got %q", s.Mode)
	}
	for _, t := range s.Tags {
		if !knownTags[t] {
			r.errorf("tags", "unknown tag %q (gui, remote, project)", t)
		}
	}
	if s.Mode == protocol.LessonGUI && !slices.Contains(s.Tags, protocol.TagGUI) {
		r.warnf("tags", "mode gui lessons should carry the gui tag")
	}
	if len(s.Modules) == 0 {
		r.errorf("modules", "required (the full module set, e.g. [base, agent, editing])")
	}
	seenMod := map[string]bool{}
	for _, m := range s.Modules {
		if !slices.Contains(Modules, m) {
			r.errorf("modules", "unknown module %q (emacs.md §3)", m)
		}
		if seenMod[m] {
			r.errorf("modules", "module %q listed twice", m)
		}
		seenMod[m] = true
	}
	if len(s.Modules) > 0 && (!seenMod["base"] || !seenMod["agent"]) {
		r.errorf("modules", "every lesson needs base and agent")
	}
	if s.Preview != nil {
		for i, t := range s.Preview.Tabs {
			checkRelPath(t, "preview.tabs["+itoa(i)+"]", r)
		}
		for src, dst := range s.Preview.Follow {
			checkRelPath(src, "preview.follow", r)
			checkRelPath(dst, "preview.follow", r)
		}
	}
	if s.Remote != nil && s.Remote.Box && !slices.Contains(s.Tags, protocol.TagRemote) {
		r.warnf("remote", "lessons that use the box should carry the remote tag")
	}
	validateSetup(l, r)

	if len(s.Steps) == 0 {
		r.errorf("steps", "at least one step is required")
	}
	stepIDs := map[string]bool{}
	condIDs := map[string]string{}
	for i := range s.Steps {
		st := &s.Steps[i]
		w := "steps[" + itoa(i) + "]"
		if !stepIDRe.MatchString(st.ID) {
			r.errorf(w+".id", "%q is not a step id ([a-z0-9-]+)", st.ID)
		}
		if stepIDs[st.ID] {
			r.errorf(w+".id", "duplicate step id %q", st.ID)
		}
		stepIDs[st.ID] = true
		if st.Goal.IsZero() {
			r.errorf(w+".goal", "required")
		}
		if strings.TrimSpace(st.Demo) == "" {
			r.errorf(w+".demo", "required: a demo that completes the step (CURRICULUM.md B.3)")
		} else if _, err := protocol.ParseDemo(st.Demo); err != nil {
			r.errorf(w+".demo", "%v", err)
		}
		if len(st.Conditions) == 0 {
			r.errorf(w+".conditions", "at least one condition is required")
		}
		for j := range st.Conditions {
			c := &st.Conditions[j]
			cw := w + ".conditions[" + itoa(j) + "]"
			if prev, dup := condIDs[c.ID]; dup {
				r.errorf(cw+".id", "condition id %q is also used at %s", c.ID, prev)
			}
			condIDs[c.ID] = cw
			if c.Text.IsZero() {
				r.warnf(cw+".text", "top-level conditions should have text (shown in the step list)")
			}
			validateCondition(c, cw, true, r)
		}
		for j, rule := range st.Stuck {
			sw := w + ".stuck[" + itoa(j) + "]"
			validateStuck(rule, sw, r)
			if prev, dup := condIDs[rule.ID]; dup {
				r.errorf(sw+".id", "id %q is also used at %s", rule.ID, prev)
			}
			condIDs[rule.ID] = sw
		}
	}
}

func validateSetup(l *Lesson, r reporter) {
	su := &l.Scenario.Setup
	if su.Workspace != "" {
		if checkRelPath(su.Workspace, "setup.workspace", r) {
			dir := filepath.Join(l.Dir, filepath.FromSlash(su.Workspace))
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				r.errorf("setup.workspace", "directory %s not found in the lesson", su.Workspace)
			} else {
				l.FixtureDir = dir
				validateFixtures(dir, su.Workspace, r)
			}
		}
		if su.Root == "" {
			r.warnf("setup.root", "fixtures extract into /work itself; set root to keep lessons apart")
		}
	}
	if su.Root != "" {
		checkRelPath(su.Root, "setup.root", r)
	}
	if su.Reset && su.Workspace == "" {
		r.errorf("setup.reset", "reset needs setup.workspace")
	}
	for i, o := range su.Open {
		if !checkRelPath(o, "setup.open["+itoa(i)+"]", r) {
			continue
		}
		// A file opened from the fixtures should exist in them.
		if rel, ok := strings.CutPrefix(o, strings.TrimSuffix(su.Root, "/")+"/"); ok && l.FixtureDir != "" && su.Root != "" {
			if _, err := os.Stat(filepath.Join(l.FixtureDir, filepath.FromSlash(rel))); err != nil {
				r.warnf("setup.open["+itoa(i)+"]", "%s is not in the fixtures (it must be created by an earlier lesson)", o)
			}
		}
	}
	if su.Layout != "" && su.Layout != "single" {
		if strings.HasSuffix(su.Layout, ".eld") {
			if checkRelPath(su.Layout, "setup.layout", r) {
				if _, err := os.Stat(filepath.Join(l.Dir, filepath.FromSlash(su.Layout))); err != nil {
					r.errorf("setup.layout", "window-state file %s not found", su.Layout)
				}
			}
		} else if _, err := protocol.ParseLayoutPattern(su.Layout); err != nil {
			r.errorf("setup.layout", "%v", err)
		}
	}
	if su.Eval != "" {
		if err := checkElisp(su.Eval); err != nil {
			r.errorf("setup.eval", "%v", err)
		}
	}
}

// validateFixtures allows only regular files and directories.
func validateFixtures(dir, rel string, r reporter) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			r.errorf("setup.workspace", "%v", err)
			return nil
		}
		name, _ := filepath.Rel(dir, p)
		where := path.Join(rel, filepath.ToSlash(name))
		switch {
		case d.IsDir():
		case d.Type().IsRegular():
			if info, err := d.Info(); err == nil && info.Size() > 10<<20 {
				r.warnf(where, "fixture file larger than 10 MB")
			}
		case d.Type()&fs.ModeSymlink != 0:
			r.errorf(where, "symlinks are not allowed in fixtures")
		default:
			r.errorf(where, "only regular files and directories are allowed in fixtures")
		}
		return nil
	})
}

// checkRelPath requires a clean relative slash path inside its root.
func checkRelPath(p, where string, r reporter) bool {
	if p == "" {
		r.errorf(where, "empty path")
		return false
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		r.errorf(where, "%q must be a relative path with / separators", p)
		return false
	}
	c := path.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") || c != strings.TrimSuffix(p, "/") {
		r.errorf(where, "%q must be a clean path without ..", p)
		return false
	}
	return true
}

func validateCondition(c *protocol.Condition, w string, top bool, r reporter) {
	kinds := c.Kinds()
	switch len(kinds) {
	case 0:
		r.errorf(w, "no condition kind (event, elisp, file, shell, layout, state, all, any, not)")
		return
	case 1:
	default:
		r.errorf(w, "exactly one condition kind allowed, found %v", kinds)
		return
	}
	if !top && (c.ID != "" || !c.Hint.IsZero()) {
		r.warnf(w, "id and hint are only used on top-level conditions")
	}
	switch kinds[0] {
	case protocol.CondEvent:
		e := c.Event
		if !commandRe.MatchString(e.Command) {
			r.errorf(w+".event.command", "%q is not a command name", e.Command)
		}
		if e.Min < 0 {
			r.errorf(w+".event.min", "must not be negative")
		}
		switch e.Since {
		case "", protocol.SinceStep, protocol.SinceLesson:
		default:
			r.errorf(w+".event.since", "must be step or lesson, got %q", e.Since)
		}
	case protocol.CondElisp:
		if err := checkElisp(c.Elisp); err != nil {
			r.errorf(w+".elisp", "%v", err)
		}
	case protocol.CondFile:
		f := c.File
		checkRelPath(f.Path, w+".file.path", r)
		n := 0
		if f.Exists != nil {
			n++
		}
		if f.Contains != "" {
			n++
		}
		if f.NotContains != "" {
			n++
		}
		if f.Regex != "" {
			n++
			if _, err := regexp.Compile(f.Regex); err != nil {
				r.errorf(w+".file.regex", "%v", err)
			}
		}
		if f.JSONPath != "" {
			n++
			if f.Equals == nil {
				r.errorf(w+".file.equals", "json_path needs equals")
			}
		} else if f.Equals != nil {
			r.errorf(w+".file.equals", "equals is only used with json_path")
		}
		if n != 1 {
			r.errorf(w+".file", "exactly one of exists, contains, not_contains, regex, json_path is required (found %d)", n)
		}
	case protocol.CondShell:
		sh := c.Shell
		if strings.TrimSpace(sh.Run) == "" {
			r.errorf(w+".shell.run", "required")
		}
		if sh.Cwd != "" {
			checkRelPath(sh.Cwd, w+".shell.cwd", r)
		}
		if sh.Timeout < 0 || sh.Timeout > 60 {
			r.errorf(w+".shell.timeout", "must be between 1 and 60 seconds")
		}
	case protocol.CondLayout:
		if _, err := protocol.ParseLayoutPattern(c.Layout); err != nil {
			r.errorf(w+".layout", "%v", err)
		}
	case protocol.CondState:
		if err := protocol.ValidatePredicate(c.State); err != nil {
			r.errorf(w+".state", "%v", err)
		}
		for p := range c.State {
			if !knownStatePath(p) {
				r.errorf(w+".state", "%q does not start with a state field (api.md §5)", p)
			}
		}
	case protocol.CondAll, protocol.CondAny:
		list := c.All
		name := "all"
		if kinds[0] == protocol.CondAny {
			list, name = c.Any, "any"
		}
		if len(list) == 0 {
			r.errorf(w+"."+name, "needs at least one condition")
		}
		for i := range list {
			validateCondition(&list[i], w+"."+name+"["+itoa(i)+"]", false, r)
		}
	case protocol.CondNot:
		validateCondition(c.Not, w+".not", false, r)
	}
}

func validateStuck(rule protocol.StuckRule, w string, r reporter) {
	wh := rule.When
	if wh.MinibufferSeconds == 0 && wh.IdleSeconds == 0 && wh.StepSeconds == 0 && wh.QuitsInWindow == nil && wh.UnboundRepeats == 0 {
		r.errorf(w+".when", "needs at least one of minibuffer_seconds, idle_seconds, step_seconds, quits_in_window, unbound_repeats")
	}
	if wh.MinibufferSeconds < 0 || wh.IdleSeconds < 0 || wh.StepSeconds < 0 || wh.UnboundRepeats < 0 {
		r.errorf(w+".when", "thresholds must not be negative")
	}
	if q := wh.QuitsInWindow; q != nil && (q.Count <= 0 || q.Seconds <= 0) {
		r.errorf(w+".when.quits_in_window", "count and seconds must be positive")
	}
	if rule.Say.IsZero() {
		r.errorf(w+".say", "required")
	}
	switch rule.Kind {
	case "", protocol.NudgeCalm, protocol.NudgeAsk:
	default:
		r.errorf(w+".kind", "must be calm or ask, got %q", rule.Kind)
	}
}

// stateFields are the top-level keys of the State document predicates and
// templates resolve against (protocol.StateDoc).
var stateFields = func() map[string]bool {
	m := map[string]bool{"otherWindows": true, "selectedWindow": true}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type)
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name != "" && name != "-" {
				m[name] = true
			}
		}
	}
	walk(reflect.TypeOf(protocol.State{}))
	return m
}()

func knownStatePath(p string) bool {
	root, _, _ := strings.Cut(p, ".")
	return stateFields[root]
}

// exitSessionFields are the fields of the virtual root "session" that exit
// rows may use besides state fields (protocol.ExitSession).
var exitSessionFields = func() map[string]bool {
	m := map[string]bool{}
	t := reflect.TypeOf(protocol.ExitSession{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		m[name] = true
	}
	return m
}()

// knownExitPath accepts state paths and session.<field> (frontend.md §6).
func knownExitPath(p string) bool {
	if root, rest, ok := strings.Cut(p, "."); ok && root == protocol.ExitSessionRoot {
		field, _, _ := strings.Cut(rest, ".")
		return exitSessionFields[field]
	}
	return knownStatePath(p)
}

// validateTables checks the advice tables of one locale.
func validateTables(t *protocol.Tables, diags *Diagnostics, suffix string) {
	er := reporter{diags: diags, file: path.Join(TablesDir, TableExits+suffix+".yaml")}
	ids := map[string]bool{}
	for i, row := range t.Exits {
		w := "[" + itoa(i) + "]"
		if !idSegRe.MatchString(row.ID) {
			er.errorf(w+".id", "%q is not an id ([a-z0-9-]+)", row.ID)
		}
		if ids[row.ID] {
			er.errorf(w+".id", "duplicate id %q", row.ID)
		}
		ids[row.ID] = true
		if len(row.When) == 0 {
			er.errorf(w+".when", "required")
		}
		if err := protocol.ValidatePredicate(row.When); err != nil {
			er.errorf(w+".when", "%v", err)
		}
		for p := range row.When {
			if !knownExitPath(p) {
				er.errorf(w+".when", "%q does not start with a state field or session.{prefix,emacs,mode,echo} (api.md §5, frontend.md §6)", p)
			}
		}
		if strings.TrimSpace(row.Why) == "" || strings.TrimSpace(row.How) == "" {
			er.errorf(w, "why and how are required")
		}
		for _, tmpl := range []string{row.Why, row.How} {
			for _, m := range templateRe.FindAllStringSubmatch(tmpl, -1) {
				if !knownExitPath(m[1]) {
					er.errorf(w, "template placeholder {%s} does not start with a state field", m[1])
				}
			}
		}
	}
	hr := reporter{diags: diags, file: path.Join(TablesDir, TableHabits+suffix+".yaml")}
	keys := map[string]bool{}
	for i, row := range t.Habits {
		w := "[" + itoa(i) + "]"
		k, err := protocol.ParseKeys(row.Key)
		if err != nil {
			hr.errorf(w+".key", "%v", err)
		} else if len(k) != 1 {
			hr.errorf(w+".key", "habits are single keys, got %q", row.Key)
		}
		if keys[row.Key] {
			hr.errorf(w+".key", "duplicate key %q", row.Key)
		}
		keys[row.Key] = true
		if strings.TrimSpace(row.VSCode) == "" || strings.TrimSpace(row.Say) == "" {
			hr.errorf(w, "vscode and say are required")
		}
	}
	gr := reporter{diags: diags, file: path.Join(TablesDir, TableGloss+suffix+".yaml")}
	for cmd, g := range t.Gloss {
		if !commandRe.MatchString(cmd) {
			gr.errorf(cmd, "not a command name")
		}
		if strings.TrimSpace(g) == "" {
			gr.errorf(cmd, "empty gloss")
		}
	}
	cr := reporter{diags: diags, file: path.Join(TablesDir, TableCheats+suffix+".yaml")}
	for mode, rows := range t.Cheats {
		for i, row := range rows {
			w := mode + "[" + itoa(i) + "]"
			if strings.TrimSpace(row[0]) == "" || strings.TrimSpace(row[1]) == "" {
				cr.errorf(w, "rows are [keys, description]")
				continue
			}
			if _, err := protocol.ParseKeys(row[0]); err != nil {
				cr.warnf(w, "%q does not parse as keys (%v); it is shown verbatim", row[0], err)
			}
		}
	}
	yr := reporter{diags: diags, file: path.Join(TablesDir, TableGlossary+suffix+".yaml")}
	for term, text := range t.Glossary {
		if strings.TrimSpace(text) == "" {
			yr.errorf(term, "empty definition")
		}
	}
}

// checkElisp does a light syntax check: balanced parentheses and brackets
// outside strings, character literals and comments.
func checkElisp(form string) error {
	if strings.TrimSpace(form) == "" {
		return fmt.Errorf("empty form")
	}
	depth := 0
	inStr := false
	for i := 0; i < len(form); i++ {
		c := form[i]
		switch {
		case inStr:
			if c == '\\' {
				i++
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == ';':
			for i < len(form) && form[i] != '\n' {
				i++
			}
		case c == '?' && (i == 0 || strings.IndexByte(" \t\n('`,[", form[i-1]) >= 0):
			// Character literal: ?a, ?\(, ?\C-x.
			if i+1 < len(form) && form[i+1] == '\\' {
				i += 2
			} else {
				i++
			}
		case c == '\\':
			i++
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
			if depth < 0 {
				return fmt.Errorf("unbalanced %q at offset %d", c, i)
			}
		}
	}
	if inStr {
		return fmt.Errorf("unterminated string")
	}
	if depth != 0 {
		return fmt.Errorf("%d unclosed parenthesis", depth)
	}
	return nil
}
