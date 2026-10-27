package curriculum

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Source file and directory names (CURRICULUM.md Part B.1).
const (
	IndexFile      = "index.yaml"
	LessonFile     = "lesson.md"
	ProjectFile    = "project.md"
	ScenarioFile   = "scenario.yaml"
	TablesDir      = "tables"
	AssetsDir      = "assets"
	ProjectsPrefix = "projects/"
)

// Table file base names (without .yaml or .<locale>.yaml).
const (
	TableExits    = "exits"
	TableHabits   = "habits"
	TableGloss    = "gloss"
	TableCheats   = "cheats"
	TableGlossary = "glossary"
)

// TableNames lists the five tables.
var TableNames = []string{TableExits, TableHabits, TableGloss, TableCheats, TableGlossary}

// SourceIndex is curriculum/index.yaml: the shape of protocol.CurriculumIndex
// without format, version, build time and tables.
type SourceIndex struct {
	Parts    []protocol.Part       `json:"parts"`
	Projects []protocol.ProjectRef `json:"projects"`
}

// Lesson is one loaded lesson or project directory.
type Lesson struct {
	// ID is the lesson directory relative to the curriculum root.
	ID string
	// Dir is the lesson directory on disk.
	Dir string
	// Project is true for projects/<dir> (project.md instead of lesson.md).
	Project bool
	// Ref is the lesson's entry in index.yaml.
	Ref protocol.LessonRef
	// Markdown maps a locale to the lesson text without front matter. The
	// default locale is always present.
	Markdown map[string]string
	// Front maps a locale to the parsed front matter.
	Front map[string]protocol.LessonFront
	// Scenario is the parsed scenario.yaml with ids assigned.
	Scenario protocol.Scenario
	// Assets are paths relative to the lesson's assets/ directory, sorted.
	Assets []string
	// FixtureDir is the absolute setup.workspace directory, or "".
	FixtureDir string
}

// MarkdownFile returns lesson.md or project.md.
func (l *Lesson) MarkdownFile() string {
	if l.Project {
		return ProjectFile
	}
	return LessonFile
}

// Source is a loaded curriculum source tree.
type Source struct {
	// Dir is the curriculum root.
	Dir   string
	Index SourceIndex
	// Lessons by id, for every lesson whose directory loaded.
	Lessons map[string]*Lesson
	// Order lists lesson ids in course order: parts, then projects that no
	// part references.
	Order []string
	// Tables by locale; DefaultLocale is always present.
	Tables map[string]*protocol.Tables
	// Locales are the non-default locales found in lesson texts and tables,
	// sorted.
	Locales []string
}

// Lesson returns the lesson with id, or nil.
func (s *Source) Lesson(id string) *Lesson { return s.Lessons[id] }

var (
	idSegRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	localeRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

// Load reads and validates the curriculum tree at dir. It always returns a
// Source (possibly partial) together with every diagnostic found.
func Load(dir string) (*Source, Diagnostics) {
	var diags Diagnostics
	src := &Source{
		Dir:     dir,
		Lessons: map[string]*Lesson{},
		Tables:  map[string]*protocol.Tables{},
	}
	ir := reporter{diags: &diags, file: IndexFile}
	if err := DecodeYAMLFile(filepath.Join(dir, IndexFile), &src.Index); err != nil {
		ir.errorf("", "%v", err)
		return src, diags
	}
	locales := map[string]bool{}
	validateIndex(src, ir)

	// Lessons in course order, then projects not referenced by a part.
	seen := map[string]bool{}
	for pi, p := range src.Index.Parts {
		for li, ref := range p.Lessons {
			if seen[ref.ID] || ref.ID == "" {
				continue
			}
			seen[ref.ID] = true
			src.Order = append(src.Order, ref.ID)
			loadLessonRef(src, ref, ir, partWhere(pi, li), locales, &diags)
		}
	}
	for i, pr := range src.Index.Projects {
		if seen[pr.ID] || pr.ID == "" {
			continue
		}
		seen[pr.ID] = true
		src.Order = append(src.Order, pr.ID)
		ref := protocol.LessonRef{ID: pr.ID, N: pr.N, Title: pr.Title, Minutes: pr.Minutes, VSCode: pr.VSCode, Tags: pr.Tags, Status: pr.Status}
		loadLessonRef(src, ref, ir, "projects["+itoa(i)+"]", locales, &diags)
	}
	checkProjectTasks(src, ir)
	findUnlistedLessons(src, seen, ir)

	loadTables(src, locales, &diags)
	for l := range locales {
		if l != protocol.DefaultLocale {
			src.Locales = append(src.Locales, l)
		}
	}
	sort.Strings(src.Locales)

	// Cross checks that need the tables.
	glossary := src.Tables[protocol.DefaultLocale].Glossary
	for _, id := range src.Order {
		if l := src.Lessons[id]; l != nil {
			r := reporter{diags: &diags, file: path.Join(id, l.MarkdownFile())}
			checkJargon(l.Markdown[protocol.DefaultLocale], glossary, r)
		}
	}
	return src, diags
}

func partWhere(pi, li int) string { return "parts[" + itoa(pi) + "].lessons[" + itoa(li) + "]" }

func loadLessonRef(src *Source, ref protocol.LessonRef, ir reporter, where string, locales map[string]bool, diags *Diagnostics) {
	dir := filepath.Join(src.Dir, filepath.FromSlash(ref.ID))
	st, err := os.Stat(dir)
	exists := err == nil && st.IsDir()
	if ref.Status == protocol.Planned {
		if exists {
			ir.warnf(where, "lesson %s is planned but its directory exists; mark it ready to ship it", ref.ID)
		}
		return
	}
	if !exists {
		ir.errorf(where, "lesson %s is %s but has no directory %s", ref.ID, ref.Status, ref.ID)
		return
	}
	l, ok := loadLesson(src.Dir, ref, locales, diags)
	if ok {
		src.Lessons[ref.ID] = l
	}
}

// loadLesson parses one lesson directory. ok is false when the lesson could
// not be used at all (missing or unparsable scenario or text).
func loadLesson(root string, ref protocol.LessonRef, locales map[string]bool, diags *Diagnostics) (*Lesson, bool) {
	l := &Lesson{
		ID:       ref.ID,
		Dir:      filepath.Join(root, filepath.FromSlash(ref.ID)),
		Project:  strings.HasPrefix(ref.ID, ProjectsPrefix),
		Ref:      ref,
		Markdown: map[string]string{},
		Front:    map[string]protocol.LessonFront{},
	}
	ok := true
	mdName := l.MarkdownFile()
	mr := reporter{diags: diags, file: path.Join(ref.ID, mdName)}
	data, err := os.ReadFile(filepath.Join(l.Dir, mdName))
	if err != nil {
		mr.errorf("", "missing %s", mdName)
		ok = false
	} else {
		front, body, err := SplitFrontMatter(data)
		if err != nil {
			mr.errorf("front matter", "%v", err)
		}
		l.Front[protocol.DefaultLocale] = front
		l.Markdown[protocol.DefaultLocale] = body
		validateFront(front, mr)
		validateMarkdown(body, mr)
	}
	// Localised texts: lesson.<locale>.md.
	base := strings.TrimSuffix(mdName, ".md")
	entries, _ := os.ReadDir(l.Dir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, base+".") || !strings.HasSuffix(name, ".md") || name == mdName {
			continue
		}
		loc := strings.TrimSuffix(strings.TrimPrefix(name, base+"."), ".md")
		lr := reporter{diags: diags, file: path.Join(ref.ID, name)}
		if !localeRe.MatchString(loc) {
			lr.errorf("", "%q is not a locale tag (want lesson.zh-CN.md)", loc)
			continue
		}
		data, err := os.ReadFile(filepath.Join(l.Dir, name))
		if err != nil {
			lr.errorf("", "%v", err)
			continue
		}
		front, body, err := SplitFrontMatter(data)
		if err != nil {
			lr.errorf("front matter", "%v", err)
		}
		l.Front[loc] = front
		l.Markdown[loc] = body
		validateMarkdown(body, lr)
		locales[loc] = true
	}

	sr := reporter{diags: diags, file: path.Join(ref.ID, ScenarioFile)}
	if err := DecodeYAMLFile(filepath.Join(l.Dir, ScenarioFile), &l.Scenario); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			sr.errorf("", "missing %s", ScenarioFile)
		} else {
			sr.errorf("", "%v", err)
		}
		return l, false
	}
	l.Scenario.AssignIDs()
	validateScenario(l, sr)
	collectLocales(&l.Scenario, locales)

	// Assets.
	adir := filepath.Join(l.Dir, AssetsDir)
	if st, err := os.Stat(adir); err == nil && st.IsDir() {
		ar := reporter{diags: diags, file: path.Join(ref.ID, AssetsDir)}
		_ = filepath.WalkDir(adir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				ar.errorf("", "%v", err)
				return nil
			}
			rel, _ := filepath.Rel(adir, p)
			if d.Type()&fs.ModeSymlink != 0 {
				ar.errorf(filepath.ToSlash(rel), "symlinks are not allowed in assets")
				return nil
			}
			if d.Type().IsRegular() {
				l.Assets = append(l.Assets, filepath.ToSlash(rel))
			}
			return nil
		})
		sort.Strings(l.Assets)
	}
	checkImages(l, mr)
	return l, ok
}

// collectLocales records every locale used in localised scenario strings.
func collectLocales(s *protocol.Scenario, locales map[string]bool) {
	add := func(t protocol.LocalText) {
		for k := range t {
			locales[k] = true
		}
	}
	add(s.Title)
	var cond func(c *protocol.Condition)
	cond = func(c *protocol.Condition) {
		add(c.Text)
		add(c.Hint)
		for i := range c.All {
			cond(&c.All[i])
		}
		for i := range c.Any {
			cond(&c.Any[i])
		}
		if c.Not != nil {
			cond(c.Not)
		}
	}
	for i := range s.Steps {
		st := &s.Steps[i]
		add(st.Goal)
		add(st.Detail)
		for j := range st.Conditions {
			cond(&st.Conditions[j])
		}
		for _, r := range st.Stuck {
			add(r.Say)
			add(r.Title)
		}
	}
}

// findUnlistedLessons warns about lesson directories that index.yaml does
// not mention.
func findUnlistedLessons(src *Source, listed map[string]bool, ir reporter) {
	dirs := map[string]bool{}
	for _, p := range src.Index.Parts {
		if p.ID != "" && !strings.HasPrefix(p.ID, "projects") {
			dirs[p.ID] = true
		}
	}
	dirs["projects"] = true
	for d := range dirs {
		entries, err := os.ReadDir(filepath.Join(src.Dir, d))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			id := d + "/" + e.Name()
			if listed[id] {
				continue
			}
			if _, err := os.Stat(filepath.Join(src.Dir, d, e.Name(), ScenarioFile)); err == nil {
				ir.warnf("", "lesson directory %s is not listed in index.yaml", id)
			}
		}
	}
}

// loadTables reads tables/<name>.yaml and tables/<name>.<locale>.yaml.
func loadTables(src *Source, locales map[string]bool, diags *Diagnostics) {
	tdir := filepath.Join(src.Dir, TablesDir)
	def := &protocol.Tables{}
	src.Tables[protocol.DefaultLocale] = def
	for _, name := range TableNames {
		file := path.Join(TablesDir, name+".yaml")
		r := reporter{diags: diags, file: file}
		if err := decodeTable(filepath.Join(tdir, name+".yaml"), name, def); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				r.warnf("", "missing table %s", file)
			} else {
				r.errorf("", "%v", err)
			}
		}
	}
	normaliseTables(def)
	validateTables(def, diags, "")

	entries, _ := os.ReadDir(tdir)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		base := strings.TrimSuffix(name, ".yaml")
		table, loc, ok := strings.Cut(base, ".")
		if !ok {
			if !contains(TableNames, base) {
				reporter{diags: diags, file: path.Join(TablesDir, name)}.warnf("", "unknown table file")
			}
			continue
		}
		r := reporter{diags: diags, file: path.Join(TablesDir, name)}
		if !contains(TableNames, table) {
			r.warnf("", "unknown table %q", table)
			continue
		}
		if !localeRe.MatchString(loc) {
			r.errorf("", "%q is not a locale tag (want exits.zh-CN.yaml)", loc)
			continue
		}
		t := src.Tables[loc]
		if t == nil {
			// Start from a copy of the default tables; the locale file
			// replaces whole tables.
			c := *def
			t = &c
			src.Tables[loc] = t
		}
		if err := decodeTable(filepath.Join(tdir, name), table, t); err != nil {
			r.errorf("", "%v", err)
		}
		locales[loc] = true
	}
	for loc, t := range src.Tables {
		if loc != protocol.DefaultLocale {
			normaliseTables(t)
			validateTables(t, diags, "."+loc)
		}
	}
}

func decodeTable(file, name string, t *protocol.Tables) error {
	switch name {
	case TableExits:
		var v []protocol.ExitRow
		if err := DecodeYAMLFile(file, &v); err != nil {
			return err
		}
		t.Exits = v
	case TableHabits:
		var v []protocol.HabitRow
		if err := DecodeYAMLFile(file, &v); err != nil {
			return err
		}
		t.Habits = v
	case TableGloss:
		var v map[string]string
		if err := DecodeYAMLFile(file, &v); err != nil {
			return err
		}
		t.Gloss = v
	case TableCheats:
		var v map[string][]protocol.CheatRow
		if err := DecodeYAMLFile(file, &v); err != nil {
			return err
		}
		t.Cheats = v
	case TableGlossary:
		var v map[string]string
		if err := DecodeYAMLFile(file, &v); err != nil {
			return err
		}
		t.Glossary = v
	}
	return nil
}

// normaliseTables replaces nil tables with empty ones so that the JSON has
// [] and {} rather than null.
func normaliseTables(t *protocol.Tables) {
	if t.Exits == nil {
		t.Exits = []protocol.ExitRow{}
	}
	if t.Habits == nil {
		t.Habits = []protocol.HabitRow{}
	}
	if t.Gloss == nil {
		t.Gloss = map[string]string{}
	}
	if t.Cheats == nil {
		t.Cheats = map[string][]protocol.CheatRow{}
	}
	if t.Glossary == nil {
		t.Glossary = map[string]string{}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func itoa(i int) string { return strconv.Itoa(i) }
