package protocol

// CurriculumIndex is curriculum.json, produced by `playctl curriculum build`
// from curriculum/index.yaml, every scenario.yaml and the tables; served as
// GET /api/v1/curriculum (api.md §2.2).
//
// curriculum/index.yaml uses the same shape without Version, Format,
// BuiltAt and Tables; lesson metadata (minutes, mode, tags, vscode) is filled
// in from each lesson's scenario.yaml at build time when the lesson exists.
type CurriculumIndex struct {
	// Format is CurriculumFormat.
	Format int `json:"format"`
	// Version identifies the bundle build, e.g. "2026.10.26+fe00636".
	Version string `json:"version"`
	// BuiltAt is the build time in ms.
	BuiltAt int64 `json:"builtAt,omitempty"`
	// Locale is the locale of the strings in this index.
	Locale   string       `json:"locale,omitempty"`
	Parts    []Part       `json:"parts"`
	Projects []ProjectRef `json:"projects"`
	Tables   Tables       `json:"tables"`
}

// Availability says whether content exists yet.
type Availability string

// Availability values. Planned parts and lessons are listed in the course
// map but have no lesson directory; the gateway returns 404 for them.
const (
	Ready   Availability = "ready"
	Planned Availability = "planned"
)

// Part is one part of the course (CURRICULUM.md Part A).
type Part struct {
	// N is the part number, 0 to 14.
	N int `json:"n"`
	// ID is the part directory name, e.g. "02-buffers-and-files".
	ID      string `json:"id"`
	Title   string `json:"title"`
	Minutes int    `json:"minutes"`
	// Outcome is one sentence: what the learner can do after the part.
	Outcome string `json:"outcome"`
	// Modules are the configuration modules this part turns on (the delta
	// over earlier parts; Part 0 lists base and agent).
	Modules []string `json:"modules"`
	// Status is ready when every lesson exists, planned otherwise.
	Status Availability `json:"status"`
	// SuggestedAfter is the part number recommended first (course map shows
	// "suggested after part N"; order is not enforced). Absent for part 0.
	SuggestedAfter *int `json:"suggestedAfter,omitempty"`
	// Lessons in order. For Part 14 these reference the projects.
	Lessons []LessonRef `json:"lessons"`
}

// LessonRef is a lesson entry in the index.
type LessonRef struct {
	// ID is "<part dir>/<lesson dir>", e.g.
	// "02-buffers-and-files/03-opening-files", or "projects/<dir>".
	ID string `json:"id"`
	// N is the display number, "2.3" or "P6".
	N       string     `json:"n"`
	Title   string     `json:"title"`
	Minutes int        `json:"minutes"`
	Mode    LessonMode `json:"mode,omitempty"`
	Tags    []string   `json:"tags,omitempty"`
	// VSCode is the VS Code habit the lesson replaces.
	VSCode string       `json:"vscode,omitempty"`
	Status Availability `json:"status"`
	// Steps is the number of steps (0 for planned lessons).
	Steps int `json:"steps,omitempty"`
}

// ProjectRef is a project entry (CURRICULUM.md Part 14).
type ProjectRef struct {
	// ID is "projects/<dir>", e.g. "projects/p6-polyglot".
	ID string `json:"id"`
	// N is "P1" to "P8".
	N         string        `json:"n"`
	Title     string        `json:"title"`
	Minutes   int           `json:"minutes"`
	Languages []string      `json:"languages"`
	VSCode    string        `json:"vscode,omitempty"`
	Tags      []string      `json:"tags,omitempty"`
	Status    Availability  `json:"status"`
	Tasks     []ProjectTask `json:"tasks"`
}

// ProjectTask is one task of a project; it is a step of the project's
// scenario with the same id.
type ProjectTask struct {
	ID    string `json:"id"`
	N     int    `json:"n"`
	Title string `json:"title"`
}

// LessonBundle is GET /api/v1/curriculum/lessons/{lessonId}.
type LessonBundle struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Locale is the locale of Markdown (the best match for the request).
	Locale string `json:"locale"`
	// Markdown is lesson.md (or project.md) without front matter.
	Markdown string `json:"markdown"`
	// Front is the parsed front matter.
	Front LessonFront `json:"front"`
	// Scenario is the parsed scenario with ids assigned (AssignIDs).
	Scenario Scenario `json:"scenario"`
	// Assets are paths relative to the lesson's assets/ directory, served at
	// /api/v1/curriculum/assets/{lessonId}/{path}.
	Assets []string `json:"assets"`
}

// LessonFront is the YAML front matter of lesson.md.
type LessonFront struct {
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Minutes int    `json:"minutes,omitempty"`
}

// Tables are the advice tables (CURRICULUM.md Part B.4), shipped in the
// curriculum index for the browser's advice engine.
type Tables struct {
	// Exits: layer 1, evaluated in order against the State object.
	Exits []ExitRow `json:"exits"`
	// Habits: layer 2, VS Code muscle memory translations.
	Habits []HabitRow `json:"habits"`
	// Gloss maps a command name to a short gloss for the ticker.
	Gloss map[string]string `json:"gloss"`
	// Cheats maps a major mode to cheat-sheet rows; key CheatsCommon holds
	// rows shown in every mode.
	Cheats map[string][]CheatRow `json:"cheats"`
	// Glossary maps a term to its tooltip text for :jargon[term].
	Glossary map[string]string `json:"glossary"`
}

// CheatsCommon is the Tables.Cheats key for rows shown in every mode.
const CheatsCommon = "common"

// ExitRow is one row of tables/exits.yaml.
type ExitRow struct {
	ID string `json:"id"`
	// When must hold against the State document (Predicate semantics).
	When Predicate `json:"when"`
	// Why is a template ("A prompt is open: {minibuffer.prompt}"); {path}
	// placeholders resolve against the State document (see Predicate and
	// ExpandTemplate).
	Why string `json:"why"`
	// How is the one exit, a template with keys keyified by the browser.
	How string `json:"how"`
	// Flag is an optional short chip ("Narrow", "Def", "Ovwrt").
	Flag string `json:"flag,omitempty"`
}

// HabitRow is one row of tables/habits.yaml.
type HabitRow struct {
	// Key is the single key that VS Code users press by habit, e.g. "C-z".
	Key string `json:"key"`
	// VSCode is what they meant ("undo").
	VSCode string `json:"vscode"`
	// Say is the card body.
	Say string `json:"say"`
}

// CheatRow is [keys, description]; YAML `- [M-., go to definition]`.
type CheatRow [2]string

// Built bundle layout: the contract between `playctl curriculum build -o DIR`
// and the gateway's curriculum.dir (backend.md §2.8, §4).
//
//	DIR/curriculum.json                         CurriculumIndex, default locale
//	DIR/curriculum.<locale>.json                CurriculumIndex for another locale (optional)
//	DIR/lessons/<lessonId>/bundle.json          LessonBundle, default locale
//	DIR/lessons/<lessonId>/bundle.<locale>.json LessonBundle for another locale (optional)
//	DIR/lessons/<lessonId>/scenario.json        Scenario with ids assigned (served to sessiond)
//	DIR/lessons/<lessonId>/assets/<path>        lesson images
//	DIR/fixtures/<lessonId>.tar.zst             fixture tarball
//
// <lessonId> contains a slash, so lesson directories are nested
// (lessons/02-buffers-and-files/03-opening-files/bundle.json).
//
// Fixture tarballs are tar archives (PAX or USTAR) compressed with zstd. They
// hold the contents of the scenario's setup.workspace directory with paths
// relative to it; sessiond extracts them into /work/<setup.root>. Only
// regular files and directories are allowed (no symlinks, devices or
// absolute or ".." paths); file modes are preserved. A lesson without
// setup.workspace has no tarball and GET /internal/curriculum/fixtures/{id}
// answers 204 No Content. Tarballs are built reproducibly (sorted entries,
// zero mtimes, uid/gid 0) so their sha256 is a stable ETag.
const (
	BundleIndexFile   = "curriculum.json"
	BundleLessonsDir  = "lessons"
	BundleFixturesDir = "fixtures"
	BundleLessonFile  = "bundle.json"
	BundleScenario    = "scenario.json"
	BundleAssetsDir   = "assets"
	FixtureExt        = ".tar.zst"
)

// BundleIndexName returns the index file name for locale ("" or
// DefaultLocale gives curriculum.json).
func BundleIndexName(locale string) string {
	if locale == "" || locale == DefaultLocale {
		return BundleIndexFile
	}
	return "curriculum." + locale + ".json"
}

// BundleLessonName returns the lesson bundle file name for locale.
func BundleLessonName(locale string) string {
	if locale == "" || locale == DefaultLocale {
		return BundleLessonFile
	}
	return "bundle." + locale + ".json"
}

// FixturePath returns the slash-separated path of a lesson's fixture
// tarball relative to the bundle directory.
func FixturePath(lessonID string) string {
	return BundleFixturesDir + "/" + lessonID + FixtureExt
}

// LessonDir returns the slash-separated directory of a lesson's bundle files
// relative to the bundle directory.
func LessonDir(lessonID string) string { return BundleLessonsDir + "/" + lessonID }
