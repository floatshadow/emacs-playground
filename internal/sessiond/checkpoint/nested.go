package checkpoint

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Nested git repositories (setup.repos, a learner's git init): the
// workspace repository would record one as a gitlink and restore none of
// its files, so a checkpoint keeps each one whole instead, as two commits
// in the workspace repository: its working tree (its own ignore rules
// apply, so build output stays out) and its .git directory (history,
// branches and the staging area; lock files excluded). The checkpoint
// commit lists both as extra parents so they stay reachable; the outer
// tree leaves the repository out.

// NestedRepo is one nested repository captured by a checkpoint.
type NestedRepo struct {
	// Path is workspace-relative.
	Path string `json:"path"`
	// Worktree and GitDir are commits in the workspace repository.
	Worktree string `json:"worktree"`
	GitDir   string `json:"gitDir"`
}

// maxNestedRepos bounds the nested repositories a checkpoint captures.
const maxNestedRepos = 50

// nestedRepos lists the workspace's nested repositories (directories with a
// .git directory), workspace-relative and sorted, outside build output,
// .playground and emacs.d's package directories. A repository inside a
// nested one is part of it.
func (m *Manager) nestedRepos() []string {
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
		if skipDir(rel) || path.Base(rel) == ".git" {
			return filepath.SkipDir
		}
		if st, err := os.Lstat(filepath.Join(p, ".git")); err == nil && st.IsDir() {
			if len(out) < maxNestedRepos {
				out = append(out, rel)
			}
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// excludeSpecs are pathspecs leaving the given repositories out.
func excludeSpecs(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, ":(exclude,literal)"+p)
	}
	return out
}

// withExcludes appends exclusion pathspecs to a command whose pathspec
// list ends its arguments (after "--").
func withExcludes(c Command, paths []string) Command {
	if len(paths) == 0 {
		return c
	}
	for _, a := range c.Args {
		if a == "--" {
			c.Args = append(append([]string(nil), c.Args...), excludeSpecs(paths)...)
			return c
		}
	}
	return c
}

// nestedEnv points git at the workspace repository with worktree as the
// working tree and its own temporary index.
func (m *Manager) nestedEnv(worktree, index string) []string {
	return []string{
		"GIT_DIR=" + filepath.Join(m.Workspace, ".git"),
		"GIT_WORK_TREE=" + worktree,
		"GIT_INDEX_FILE=" + index,
	}
}

func (m *Manager) nestedIndex(rel, part string) string {
	return filepath.Join(m.Workspace, PlaygroundDir, "nested-"+Slug(rel)+"-"+part+".tmp")
}

// gitDirSpec excludes lock files from a .git snapshot.
var gitDirSpec = []string{"--", ".", ":(exclude,glob)**/*.lock"}

// snapshotTree stages worktree into its temporary index and returns a
// commit of it. Caller holds m.mu.
func (m *Manager) snapshotTree(ctx context.Context, worktree, index, message string, spec []string) (string, error) {
	env := m.nestedEnv(worktree, index)
	if _, err := m.runIn(ctx, worktree, env, "read-tree", "--empty"); err != nil {
		return "", err
	}
	if _, err := m.runIn(ctx, worktree, env, append([]string{"add", "-A"}, spec...)...); err != nil {
		return "", err
	}
	tree, err := m.runIn(ctx, worktree, env, "write-tree")
	if err != nil {
		return "", err
	}
	commit, err := m.runIn(ctx, worktree, append(env, identityEnv...), "commit-tree", strings.TrimSpace(string(tree)), "-m", message)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(commit)), nil
}

// snapshotNested captures every nested repository. Caller holds m.mu.
func (m *Manager) snapshotNested(ctx context.Context, repos []string, label string) ([]NestedRepo, error) {
	var out []NestedRepo
	for _, rel := range repos {
		wt := filepath.Join(m.Workspace, filepath.FromSlash(rel))
		w, err := m.snapshotTree(ctx, wt, m.nestedIndex(rel, "wt"), "checkpoint "+label+" "+rel, []string{"--", "."})
		if err != nil {
			return nil, err
		}
		g, err := m.snapshotTree(ctx, filepath.Join(wt, ".git"), m.nestedIndex(rel, "git"), "checkpoint "+label+" "+rel+"/.git", gitDirSpec)
		if err != nil {
			return nil, err
		}
		out = append(out, NestedRepo{Path: rel, Worktree: w, GitDir: g})
	}
	return out, nil
}

// restoreTree makes worktree match commit (files the commit lacks are
// removed; with ignored, ignored files too) and returns the paths it
// changed, relative to worktree. Caller holds m.mu.
func (m *Manager) restoreTree(ctx context.Context, worktree, index, commit string, spec []string, ignored bool) ([]string, error) {
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		return nil, err
	}
	env := m.nestedEnv(worktree, index)
	var changed []string
	if _, err := m.runIn(ctx, worktree, env, append([]string{"add", "-A"}, spec...)...); err == nil {
		if out, err := m.runIn(ctx, worktree, env, "diff-index", "--cached", "--name-only", "-z", "--no-renames", commit, "--"); err == nil {
			for _, p := range strings.Split(string(out), "\x00") {
				if p != "" {
					changed = append(changed, path.Clean(p))
				}
			}
		}
	}
	for _, args := range [][]string{
		{"read-tree", commit},
		{"update-index", "-q", "--refresh"},
	} {
		if _, err := m.runIn(ctx, worktree, env, args...); err != nil {
			break
		}
	}
	if _, err := m.runIn(ctx, worktree, env, "restore", "--source="+commit, "--worktree", "--staged", "--", "."); err != nil {
		return changed, err
	}
	clean := []string{"clean", "-fd"}
	if ignored {
		clean[1] = "-fdx"
	} else {
		for _, d := range KeepDirs {
			clean = append(clean, "-e", d)
		}
	}
	_, err := m.runIn(ctx, worktree, env, clean...)
	return changed, err
}

// restoreNested puts the checkpoint's nested repositories back and removes
// the .git of repositories made since (their files are then ordinary
// workspace files the outer restore handles). It returns the
// workspace-relative paths it changed. Caller holds m.mu.
func (m *Manager) restoreNested(ctx context.Context, nested []NestedRepo) ([]string, error) {
	var changed []string
	for _, n := range nested {
		if !validRel(n.Path) {
			continue
		}
		wt := filepath.Join(m.Workspace, filepath.FromSlash(n.Path))
		if _, err := m.restoreTree(ctx, filepath.Join(wt, ".git"), m.nestedIndex(n.Path, "git"), n.GitDir, gitDirSpec, true); err != nil {
			return changed, err
		}
		files, err := m.restoreTree(ctx, wt, m.nestedIndex(n.Path, "wt"), n.Worktree, []string{"--", "."}, false)
		for _, f := range files {
			changed = append(changed, n.Path+"/"+f)
		}
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// dropNewRepos removes the .git directory of nested repositories that the
// checkpoint does not have. Caller holds m.mu.
func (m *Manager) dropNewRepos(current []string, nested []NestedRepo) []string {
	keep := map[string]bool{}
	for _, n := range nested {
		keep[n.Path] = true
	}
	var dropped []string
	for _, rel := range current {
		if keep[rel] || !validRel(rel) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.Workspace, filepath.FromSlash(rel), ".git")); err == nil {
			dropped = append(dropped, rel+"/.git")
		}
	}
	return dropped
}

// nestedDiff lists the files of a nested repository's working tree that
// differ from its checkpoint, workspace-relative. Caller holds m.mu.
func (m *Manager) nestedDiff(ctx context.Context, n NestedRepo) []string {
	wt := filepath.Join(m.Workspace, filepath.FromSlash(n.Path))
	if _, err := os.Stat(wt); err != nil {
		return []string{n.Path}
	}
	env := m.nestedEnv(wt, m.nestedIndex(n.Path, "wt"))
	if _, err := m.runIn(ctx, wt, env, "add", "-A", "--", "."); err != nil {
		return nil
	}
	out, err := m.runIn(ctx, wt, env, "diff-index", "--cached", "--name-only", "-z", "--no-renames", n.Worktree, "--")
	if err != nil {
		return nil
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, n.Path+"/"+path.Clean(p))
		}
	}
	return files
}

func validRel(rel string) bool {
	return rel != "" && !path.IsAbs(rel) && path.Clean(rel) == rel && rel != ".." && !strings.HasPrefix(rel, "../")
}

func nestedPaths(nested []NestedRepo) []string {
	out := make([]string, 0, len(nested))
	for _, n := range nested {
		out = append(out, n.Path)
	}
	return out
}
