package checkpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// RefPrefix is the namespace of every checkpoint ref.
const RefPrefix = "refs/playground/"

// ManualPrefix is the namespace of learner checkpoints (projects).
const ManualPrefix = RefPrefix + "manual/"

// PlaygroundDir is the workspace-relative directory sessiond owns.
const PlaygroundDir = ".playground"

// KeepDirs are never removed by a restore (build output and playground
// state).
var KeepDirs = []string{"target", "_build", ".venv", "node_modules", ".playground"}

// ExcludePatterns go into .git/info/exclude when sessiond initialises the
// workspace.
// .playground itself is ignored by .playground/.gitignore ("*") rather than
// here: git add refuses a pathspec that names a path ignored by
// info/exclude, even as an exclusion.
var ExcludePatterns = []string{
	"target/",
	"_build/",
	".venv/",
	"node_modules/",
	"/emacs.d/elpa/",
	"/emacs.d/eln-cache/",
	"/emacs.d/var/",
	"/emacs.d/auto-save-list/",
	"/emacs.d/tree-sitter/",
	"/emacs.d/transient/",
	"/emacs.d/url/",
	"/emacs.d/history",
	"/emacs.d/recentf",
	"/emacs.d/places",
	"/emacs.d/tramp",
	"/emacs.d/eshell/",
	".#*",
}

// SafeDirectoryEnv marks every directory safe for git through
// command-line-equivalent configuration. The workspace is usually owned by
// another uid than the learner (a bind mount from the host, a tmpfs created
// by root), and git refuses such repositories ("dubious ownership") unless
// safe.directory says otherwise; the session container has a single user,
// so the protection buys nothing there. Supervised processes (Emacs, Magit,
// the learner's shells) get the same variables.
var SafeDirectoryEnv = []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=safe.directory", "GIT_CONFIG_VALUE_0=*"}

// NoOptionalLocksEnv keeps sessiond's background git (checkpoints, shell
// conditions such as `git status --porcelain`) from taking optional locks:
// git status otherwise refreshes the index under index.lock, and a git add
// or git commit the learner (Magit) runs at that moment fails with "Unable
// to create index.lock: File exists".
var NoOptionalLocksEnv = []string{"GIT_OPTIONAL_LOCKS=0"}

// Identity used for checkpoint commits (environment only; the learner's git
// configuration is not touched).
var identityEnv = []string{
	"GIT_AUTHOR_NAME=Emacs Playground", "GIT_AUTHOR_EMAIL=playground@localhost",
	"GIT_COMMITTER_NAME=Emacs Playground", "GIT_COMMITTER_EMAIL=playground@localhost",
}

// Runner runs git. Tests substitute a recorder or use ExecRunner against a
// temporary repository.
type Runner interface {
	// Run executes git with args in dir with extra environment entries and
	// returns stdout. A non-zero exit is an error that includes stderr.
	Run(ctx context.Context, dir string, env []string, args ...string) ([]byte, error)
}

// ExecRunner runs the git binary.
type ExecRunner struct{ Bin string }

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	bin := r.Bin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.Env = append(cmd.Env, SafeDirectoryEnv...)
	cmd.Env = append(cmd.Env, NoOptionalLocksEnv...)
	cmd.Env = append(cmd.Env, env...)
	var out, errb bytes.Buffer
	cmd.Stdout = &limitedBuffer{b: &out, max: 8 << 20}
	cmd.Stderr = &limitedBuffer{b: &errb, max: 64 << 10}
	if err := cmd.Run(); err != nil {
		return out.Bytes(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

type limitedBuffer struct {
	b   *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.b.Len(); room > 0 {
		if len(p) > room {
			l.b.Write(p[:room])
		} else {
			l.b.Write(p)
		}
	}
	return len(p), nil
}

// Meta is stored next to each ref.
type Meta struct {
	Label     string               `json:"label"`
	Ref       string               `json:"ref"`
	CreatedAt int64                `json:"createdAt"`
	Step      *protocol.StepRef    `json:"step,omitempty"`
	Spec      *protocol.WindowSpec `json:"spec,omitempty"`
	// Buffers is the state of the file buffers the commit (files as on
	// disk) does not have, by workspace-relative path; a restore puts it
	// back after the agent's reset-step.
	Buffers map[string]BufferState `json:"buffers,omitempty"`
	// Globals is the printed alist of Emacs session variables (kill ring,
	// histories, keyboard macro) when the checkpoint was taken.
	Globals string `json:"globals,omitempty"`
	// EmptyDirs are the workspace's empty directories (workspace-relative),
	// which a commit cannot hold; a restore recreates them (lesson 2.6
	// moves files into a docs folder made in the step before).
	EmptyDirs []string `json:"emptyDirs,omitempty"`
	// Nested are the nested git repositories, kept whole (nested.go).
	Nested []NestedRepo `json:"nested,omitempty"`
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
	// Undo is the printed buffer-undo-list, reduced to readable entries.
	Undo string `json:"undo,omitempty"`
}

// Manager takes, lists and restores checkpoints of one workspace. Operations
// are serialised.
type Manager struct {
	Workspace string
	Git       Runner
	Now       func() time.Time
	mu        sync.Mutex
}

// New returns a manager running the given git binary.
func New(workspace, gitBin string) *Manager {
	return &Manager{Workspace: workspace, Git: ExecRunner{Bin: gitBin}, Now: time.Now}
}

// StepRef returns refs/playground/<lesson>/<step>.
func StepRef(lessonID, stepID string) string { return RefPrefix + lessonID + "/" + stepID }

// ManualRef returns refs/playground/manual/<slug of label>.
func ManualRef(label string) string { return ManualPrefix + Slug(label) }

// Slug derives a ref-safe name from a free-text label: lower case, runs of
// anything outside [a-z0-9._-] become one dash, at most 60 bytes, never
// empty.
func Slug(label string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(label) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		s = "checkpoint"
	}
	return s
}

// ValidRef reports whether ref is a checkpoint ref sessiond may restore.
func ValidRef(ref string) bool {
	if !strings.HasPrefix(ref, RefPrefix) || len(ref) == len(RefPrefix) {
		return false
	}
	rest := ref[len(RefPrefix):]
	for _, seg := range strings.Split(rest, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return !strings.ContainsAny(rest, " ~^:?*[\\\x7f") && !strings.Contains(rest, "..") && !strings.Contains(rest, "@{")
}

// IndexFile is the temporary index used by checkpoints.
func (m *Manager) IndexFile() string {
	return filepath.Join(m.Workspace, PlaygroundDir, "index.tmp")
}

// Command is one git invocation of a plan.
type Command struct {
	Env  []string
	Args []string
}

// TakeCommands returns the commands that record a checkpoint (backend.md
// §3.9), with "$tree" and "$parent" placeholders resolved by the caller:
// the commit-tree step receives the tree from write-tree and the parent when
// the ref already exists.
func TakeCommands(indexFile, ref, message, tree, parent string) []Command {
	idx := "GIT_INDEX_FILE=" + indexFile
	commit := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		commit = append(commit, "-p", parent)
	}
	return []Command{
		{Env: []string{idx}, Args: []string{"add", "-A", "--", ".", ":!" + PlaygroundDir}},
		{Env: []string{idx}, Args: []string{"write-tree"}},
		{Env: append([]string{idx}, identityEnv...), Args: commit},
		{Args: []string{"update-ref", ref, "$commit"}},
	}
}

// RestorePrepCommands run before RestoreCommands so the restore only writes
// files that differ from the checkpoint. They load the checkpoint's tree
// into the temporary index (indexFile, which the caller has just refreshed
// from the working tree) and refresh its stat data: git restore then keeps
// index entries whose blob already matches and skips files whose stat data
// is current. The end state is the one RestoreCommands alone produce, since
// the temporary index is reset to the checkpoint either way. With -q,
// update-index does not fail on files that differ.
func RestorePrepCommands(indexFile, ref string) []Command {
	idx := "GIT_INDEX_FILE=" + indexFile
	return []Command{
		{Env: []string{idx}, Args: []string{"read-tree", ref}},
		{Env: []string{idx}, Args: []string{"update-index", "-q", "--refresh"}},
	}
}

// RestoreCommands returns the commands that restore ref (backend.md §3.9).
// Like TakeCommands they use the temporary index, never the learner's: the
// course files stay untracked in the learner's repository (no "A" in git
// status, no Git@ in the mode line). git clean judges against the
// temporary index, which then holds the checkpoint, so it removes every
// file the checkpoint does not have (unless it is in KeepDirs or ignored).
func RestoreCommands(indexFile, ref string) []Command {
	idx := "GIT_INDEX_FILE=" + indexFile
	clean := []string{"clean", "-fd"}
	for _, d := range KeepDirs {
		clean = append(clean, "-e", d)
	}
	return []Command{
		{Env: []string{idx}, Args: []string{"restore", "--source=" + ref, "--worktree", "--staged", "--", "."}},
		{Env: []string{idx}, Args: clean},
	}
}

func (m *Manager) git(ctx context.Context, env []string, args ...string) (string, error) {
	out, err := m.run(ctx, env, args...)
	return strings.TrimSpace(string(out)), err
}

// GitTimeout bounds one git command of the manager.
const GitTimeout = 2 * time.Minute

// StaleLockAge is how old a git lock file must be before the manager
// removes it. The manager serialises its own git commands, so a lock that
// outlives one of them was left by a git process that died holding it.
const StaleLockAge = 10 * time.Second

// lockRe finds the lock file in git's "Unable to create '….lock': File
// exists" error.
var lockRe = regexp.MustCompile(`Unable to create '([^']+\.lock)': File exists`)

// run runs one git command for the manager. The command is detached from
// the caller's cancellation (bounded by GitTimeout instead): a request that
// ends while git writes the index must not kill git with the lock held, or
// every later checkpoint and restore fails with "index.lock: File exists".
// When a lock file left by an earlier git process blocks the command, run
// removes it if it is older than StaleLockAge (else waits for it to go,
// up to two seconds) and runs the command once more.
func (m *Manager) run(ctx context.Context, env []string, args ...string) ([]byte, error) {
	return m.runIn(ctx, m.Workspace, env, args...)
}

// runIn is run with dir as the working directory.
func (m *Manager) runIn(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), GitTimeout)
	defer cancel()
	out, err := m.Git.Run(ctx, dir, env, args...)
	if err == nil {
		return out, nil
	}
	match := lockRe.FindStringSubmatch(err.Error())
	if match == nil {
		return out, err
	}
	lock := match[1]
	if !filepath.IsAbs(lock) {
		lock = filepath.Join(dir, lock)
	}
	if !m.clearLock(lock) {
		return out, err
	}
	return m.Git.Run(ctx, dir, env, args...)
}

// clearLock waits up to two seconds for the git lock file to go away and
// removes it when it is older than StaleLockAge. It reports whether the
// lock is gone.
func (m *Manager) clearLock(lock string) bool {
	now := m.Now
	if now == nil {
		now = time.Now
	}
	for i := 0; ; i++ {
		fi, err := os.Stat(lock)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil {
			return false
		}
		if now().Sub(fi.ModTime()) >= StaleLockAge {
			rerr := os.Remove(lock)
			return rerr == nil || errors.Is(rerr, os.ErrNotExist)
		}
		if i >= 20 {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Init makes the workspace a git repository if it is not one, ignores
// .playground and writes the exclude patterns. It is idempotent.
func (m *Manager) Init(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensurePlaygroundDir(); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(m.Workspace, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := m.git(ctx, nil, "init", "-q"); err != nil {
			return err
		}
	}
	if err := m.resetLearnerIndex(ctx); err != nil {
		return err
	}
	return m.writeExclude()
}

// IndexResetMarker, under .playground, records that resetLearnerIndex ran.
const IndexResetMarker = "learner-index-reset"

// resetLearnerIndex resets the learner's index to HEAD (empty without a
// commit) once per workspace (IndexResetMarker), so course files are
// untracked in the learner's repository whatever staged them; checkpoints
// themselves only use the temporary index. The working tree is not
// touched. Caller holds m.mu.
func (m *Manager) resetLearnerIndex(ctx context.Context) error {
	marker := filepath.Join(m.Workspace, PlaygroundDir, IndexResetMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	args := []string{"read-tree", "--empty"}
	if _, err := m.git(ctx, nil, "rev-parse", "-q", "--verify", "HEAD^{tree}"); err == nil {
		args = []string{"read-tree", "HEAD"}
	}
	if _, err := m.git(ctx, nil, args...); err != nil {
		return err
	}
	return os.WriteFile(marker, nil, 0o644)
}

// ensurePlaygroundDir creates .playground with a .gitignore that ignores
// everything in it.
func (m *Manager) ensurePlaygroundDir() error {
	dir := filepath.Join(m.Workspace, PlaygroundDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if b, err := os.ReadFile(gi); err == nil && strings.TrimSpace(string(b)) == "*" {
		return nil
	}
	return os.WriteFile(gi, []byte("*\n"), 0o644)
}

func (m *Manager) writeExclude() error {
	p := filepath.Join(m.Workspace, ".git", "info", "exclude")
	cur, _ := os.ReadFile(p)
	have := map[string]bool{}
	for _, l := range strings.Split(string(cur), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, pat := range ExcludePatterns {
		if !have[pat] {
			add = append(add, pat)
		}
	}
	if len(add) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.Write(cur)
	if len(cur) > 0 && !bytes.HasSuffix(cur, []byte("\n")) {
		b.WriteByte('\n')
	}
	if !have["# emacs-playground"] {
		b.WriteString("# emacs-playground\n")
	}
	for _, pat := range add {
		b.WriteString(pat + "\n")
	}
	return os.WriteFile(p, []byte(b.String()), 0o644)
}

// Take records a checkpoint at ref with label and saves the metadata.
func (m *Manager) Take(ctx context.Context, ref, label string, step *protocol.StepRef, spec *protocol.WindowSpec) (Meta, error) {
	return m.TakeWith(ctx, Meta{Ref: ref, Label: label, Step: step, Spec: spec})
}

// TakeWith is Take with the metadata given whole (Ref, Label, Step, Spec,
// Buffers, Globals); CreatedAt and EmptyDirs are set here.
func (m *Manager) TakeWith(ctx context.Context, meta Meta) (Meta, error) {
	ref, label := meta.Ref, meta.Label
	if !ValidRef(ref) {
		return Meta{}, fmt.Errorf("checkpoint: invalid ref %q", ref)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ensurePlaygroundDir(); err != nil {
		return Meta{}, err
	}
	parent, _ := m.commitOf(ctx, ref)
	repos := m.nestedRepos()
	cmds := TakeCommands(m.IndexFile(), ref, "checkpoint "+label, "$tree", parent)
	add := withExcludes(cmds[0], repos)
	if _, err := m.git(ctx, add.Env, add.Args...); err != nil {
		return Meta{}, err
	}
	tree, err := m.git(ctx, cmds[1].Env, cmds[1].Args...)
	if err != nil {
		return Meta{}, err
	}
	nested, err := m.snapshotNested(ctx, repos, label)
	if err != nil {
		return Meta{}, err
	}
	cmds = TakeCommands(m.IndexFile(), ref, "checkpoint "+label, tree, parent)
	commitCmd := cmds[2]
	for _, n := range nested {
		// Extra parents keep the nested snapshots reachable.
		commitCmd.Args = append(commitCmd.Args, "-p", n.Worktree, "-p", n.GitDir)
	}
	commit, err := m.git(ctx, commitCmd.Env, commitCmd.Args...)
	if err != nil {
		return Meta{}, err
	}
	meta.Nested = nested
	if _, err := m.git(ctx, nil, "update-ref", ref, commit); err != nil {
		return Meta{}, err
	}
	meta.CreatedAt = m.Now().UnixMilli()
	meta.EmptyDirs = m.emptyDirs()
	return meta, m.writeMeta(meta)
}

// commitOf returns the commit ref points to; an error when there is none.
func (m *Manager) commitOf(ctx context.Context, ref string) (string, error) {
	return m.git(ctx, nil, "rev-parse", "-q", "--verify", ref+"^{commit}")
}

func (m *Manager) metaPath(ref string) string {
	rel := strings.TrimPrefix(ref, RefPrefix)
	return filepath.Join(m.Workspace, PlaygroundDir, "checkpoints", filepath.FromSlash(rel)+".json")
}

func (m *Manager) writeMeta(meta Meta) error {
	p := m.metaPath(meta.Ref)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ReadMeta returns the metadata saved for ref.
func (m *Manager) ReadMeta(ref string) (Meta, error) {
	b, err := os.ReadFile(m.metaPath(ref))
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	if err := json.Unmarshal(b, &meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// Exists reports whether ref exists.
func (m *Manager) Exists(ctx context.Context, ref string) bool {
	if !ValidRef(ref) {
		return false
	}
	_, err := m.commitOf(ctx, ref)
	return err == nil
}

// List returns every checkpoint, newest first, with the files a restore
// would change.
func (m *Manager) List(ctx context.Context) ([]protocol.CheckpointInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out, err := m.git(ctx, nil, "for-each-ref", "--format=%(refname)%09%(creatordate:unix)%09%(subject)", RefPrefix)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return []protocol.CheckpointInfo{}, nil
	}
	repos := m.nestedRepos()
	if err := m.stageWorktree(ctx, repos); err != nil {
		return nil, err
	}
	var items []protocol.CheckpointInfo
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		info := protocol.CheckpointInfo{Ref: f[0], Label: strings.TrimPrefix(f[2], "checkpoint ")}
		var secs int64
		fmt.Sscan(f[1], &secs)
		info.CreatedAt = secs * 1000
		meta, merr := m.ReadMeta(f[0])
		if merr == nil {
			info.Label = meta.Label
			info.CreatedAt = meta.CreatedAt
			info.Step = meta.Step
		}
		files, err := m.diffFiles(ctx, f[0])
		if err != nil {
			return nil, err
		}
		// Files under a nested repository are the nested diff's.
		files = slices.DeleteFunc(files, func(p string) bool { return underAny(p, repos) })
		for _, n := range meta.Nested {
			files = append(files, m.nestedDiff(ctx, n)...)
		}
		info.Files = files
		items = append(items, info)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

// stageWorktree refreshes the temporary index from the working tree,
// leaving out the nested repositories in skip. Caller holds m.mu.
func (m *Manager) stageWorktree(ctx context.Context, skip []string) error {
	cmds := TakeCommands(m.IndexFile(), "", "", "", "")
	add := withExcludes(cmds[0], skip)
	_, err := m.git(ctx, add.Env, add.Args...)
	return err
}

// underAny reports whether p is one of dirs or below one.
func underAny(p string, dirs []string) bool {
	for _, d := range dirs {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	return false
}

// diffFiles lists paths that differ between ref and the staged working
// tree. Caller holds m.mu and has called stageWorktree.
func (m *Manager) diffFiles(ctx context.Context, ref string) ([]string, error) {
	out, err := m.run(ctx, []string{"GIT_INDEX_FILE=" + m.IndexFile()},
		"diff-index", "--cached", "--name-only", "-z", "--no-renames", ref, "--")
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, path.Clean(p))
		}
	}
	return files, nil
}

// Restore resets the workspace to ref and returns the workspace-relative
// paths of the files that were restored or removed, and the metadata saved
// with the ref (Spec may be nil for checkpoints taken without the agent).
func (m *Manager) Restore(ctx context.Context, ref string) ([]string, Meta, error) {
	if !ValidRef(ref) {
		return nil, Meta{}, fmt.Errorf("checkpoint: invalid ref %q", ref)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.commitOf(ctx, ref); err != nil {
		return nil, Meta{}, fmt.Errorf("checkpoint %s: %w", ref, os.ErrNotExist)
	}
	meta, err := m.ReadMeta(ref)
	if err != nil {
		meta = Meta{Ref: ref, Label: strings.TrimPrefix(ref, RefPrefix)}
	}
	// Repositories made since the checkpoint lose their .git and become
	// ordinary files; the checkpoint's own nested repositories stay out of
	// the workspace restore and are restored whole afterwards.
	dropped := m.dropNewRepos(m.nestedRepos(), meta.Nested)
	skip := nestedPaths(meta.Nested)
	if err := m.stageWorktree(ctx, skip); err != nil {
		return nil, Meta{}, err
	}
	files, err := m.diffFiles(ctx, ref)
	if err != nil {
		return nil, Meta{}, err
	}
	files = append(files, dropped...)
	// Best effort: without it the restore rewrites every file (new mtimes,
	// a file event per path, buffers that look changed on disk).
	for _, c := range RestorePrepCommands(m.IndexFile(), ref) {
		if _, err := m.git(ctx, c.Env, c.Args...); err != nil {
			break
		}
	}
	for _, c := range RestoreCommands(m.IndexFile(), ref) {
		c = withExcludes(c, skip)
		if _, err := m.git(ctx, c.Env, c.Args...); err != nil {
			return nil, Meta{}, err
		}
	}
	nfiles, err := m.restoreNested(ctx, meta.Nested)
	files = append(files, nfiles...)
	if err != nil {
		return files, meta, err
	}
	// git has no empty directories: git clean removed the ones that
	// existed at the checkpoint if anything was put into them since.
	m.makeDirs(meta.EmptyDirs)
	return files, meta, nil
}

// skipDir reports whether the empty-directory walk leaves the directory
// rel (slash-separated, workspace-relative) out: git and playground state,
// build output (KeepDirs) and the package directories of emacs.d.
func skipDir(rel string) bool {
	if rel == ".git" || rel == PlaygroundDir {
		return true
	}
	base := path.Base(rel)
	for _, d := range KeepDirs {
		if base == d {
			return true
		}
	}
	for _, p := range ExcludePatterns {
		if strings.HasPrefix(p, "/") && strings.HasSuffix(p, "/") && strings.TrimSuffix(strings.TrimPrefix(p, "/"), "/") == rel {
			return true
		}
	}
	return false
}

// maxEmptyDirs bounds the empty directories recorded with a checkpoint.
const maxEmptyDirs = 1000

// emptyDirs lists the empty directories of the workspace (workspace-
// relative, slash-separated), which a commit cannot hold.
func (m *Manager) emptyDirs() []string {
	var out []string
	_ = filepath.WalkDir(m.Workspace, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == m.Workspace {
			return nil
		}
		rel, rerr := filepath.Rel(m.Workspace, p)
		if rerr != nil {
			return filepath.SkipDir
		}
		rel = filepath.ToSlash(rel)
		if skipDir(rel) {
			return filepath.SkipDir
		}
		if ents, err := os.ReadDir(p); err == nil && len(ents) == 0 && len(out) < maxEmptyDirs {
			out = append(out, rel)
		}
		return nil
	})
	return out
}

// makeDirs recreates recorded empty directories inside the workspace.
func (m *Manager) makeDirs(dirs []string) {
	if len(dirs) == 0 {
		return
	}
	root, err := os.OpenRoot(m.Workspace)
	if err != nil {
		return
	}
	defer root.Close()
	for _, rel := range dirs {
		if rel == "" || path.IsAbs(rel) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		_ = root.MkdirAll(filepath.FromSlash(rel), 0o755)
	}
}
