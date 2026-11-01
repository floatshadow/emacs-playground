// Package checkpoint is the git half of checkpoints: it records the
// workspace as commits under refs/playground/ with a temporary index,
// publishes, lists and restores them (backend.md §3.9, ADR-009). The
// snapshot service is its only caller and owns what goes with a commit
// (the manifest: label, step, editor state, nested repositories, empty
// directories) and the order: TakeTree, the manifest, then PublishRef.
//
// Taking a checkpoint never touches the learner's index or branch:
//
//	export GIT_INDEX_FILE=/work/.playground/index.tmp
//	git add -A -- . ':!.playground'
//	tree=$(git write-tree)
//	commit=$(git commit-tree "$tree" -m "checkpoint <lesson>/<step>" ${parent:+-p $parent})
//	git update-ref refs/playground/<lesson>/<step> "$commit"
//
// Restoring loads the ref's tree into the index first (git read-tree,
// update-index --refresh) so only differing files are rewritten, then runs
//
//	git restore --source=<ref> --worktree --staged -- .
//	git clean -fd -e target -e _build -e .venv -e node_modules -e .playground
//
// restores the nested repositories and recreates the recorded empty
// directories.
//
// Build output, the playground directory and the Emacs package and cache
// directories under emacs.d are listed in .git/info/exclude so checkpoints
// stay small and a restore never deletes installed packages.
package checkpoint
