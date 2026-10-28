package scenario

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
	"github.com/floatshadow/emacs-playground/internal/sessiond/steps"
)

// Initial commits of setup.repos use a fixed identity and date, so the
// same fixtures always give the same commit id.
var initialCommitEnv = []string{
	"GIT_AUTHOR_NAME=Emacs Playground", "GIT_AUTHOR_EMAIL=playground@localhost",
	"GIT_COMMITTER_NAME=Emacs Playground", "GIT_COMMITTER_EMAIL=playground@localhost",
	"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
}

// Learner identity written into each new repository's config: the image
// has no global git identity, and Magit cannot commit without one.
const (
	LearnerName  = "Learner"
	LearnerEmail = "learner@playground.invalid"
)

// RepoDir resolves one setup.repos entry to a workspace-relative path,
// or an error when it leaves the setup root.
func RepoDir(root, dir string) (string, error) {
	rel := path.Clean(path.Join(strings.Trim(root, "/"), dir))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(dir) ||
		(root != "" && rel != strings.Trim(root, "/") && !strings.HasPrefix(rel, strings.Trim(root, "/")+"/")) {
		return "", fmt.Errorf("setup.repos: %q is not inside the setup root", dir)
	}
	return rel, nil
}

// prepareRepos makes each setup.repos directory a git repository with the
// fixtures as its initial commit when it has none; reset re-creates it.
func (l *Loader) prepareRepos(ctx context.Context, sc *protocol.Scenario, reset bool) error {
	if len(sc.Setup.Repos) == 0 {
		return nil
	}
	git := l.Git
	if git == nil {
		git = checkpoint.ExecRunner{}
	}
	for _, d := range sc.Setup.Repos {
		rel, err := RepoDir(sc.Setup.Root, d)
		if err != nil {
			return err
		}
		if err := InitRepo(ctx, git, filepath.Join(l.Workspace, filepath.FromSlash(rel)), reset); err != nil {
			return fmt.Errorf("setup.repos %s: %w", d, err)
		}
	}
	return nil
}

// InitRepo makes dir a git repository (branch main) whose first commit
// holds everything in it, unless it has a .git already; reset removes an
// existing .git first.
func InitRepo(ctx context.Context, git checkpoint.Runner, dir string, reset bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dotgit := filepath.Join(dir, ".git")
	if reset {
		if err := os.RemoveAll(dotgit); err != nil {
			return err
		}
	} else if _, err := os.Lstat(dotgit); err == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", LearnerName},
		{"config", "user.email", LearnerEmail},
		{"add", "-A"},
		{"commit", "-q", "--allow-empty", "--no-verify", "--no-gpg-sign", "-m", "Initial commit"},
	} {
		if _, err := git.Run(ctx, dir, initialCommitEnv, args...); err != nil {
			return err
		}
	}
	return nil
}

// runSetup runs setup.run in the setup root.
func (l *Loader) runSetup(ctx context.Context, sc *protocol.Scenario) error {
	if strings.TrimSpace(sc.Setup.Run) == "" {
		return nil
	}
	if l.Shell == nil {
		return fmt.Errorf("setup.run: no shell")
	}
	dir := filepath.Join(l.Workspace, filepath.FromSlash(strings.Trim(sc.Setup.Root, "/")))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	res := l.Shell.Run(ctx, dir, sc.Setup.Run, protocol.SetupRunTimeout*time.Second)
	switch {
	case res.TimedOut:
		return fmt.Errorf("setup.run timed out after %ds", protocol.SetupRunTimeout)
	case res.Err != nil:
		return fmt.Errorf("setup.run: %w", res.Err)
	case !res.OK:
		return fmt.Errorf("setup.run failed: %s", strings.TrimSpace(res.Output))
	}
	return nil
}

var _ steps.ShellRunner = steps.ExecShell{}
