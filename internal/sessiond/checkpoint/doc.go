// Package checkpoint records and restores git-based checkpoints under
// refs/playground/ with a temporary index, plus the protocol.WindowSpec
// saved next to each ref (backend.md §3.9, ADR-009).
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
// recreates the recorded empty directories, and leaves the agent's
// reset-step (and the buffer and session-variable restore) to the caller.
// Metadata (label, step, creation time, window spec, unsaved buffer state,
// session variables, empty directories) is stored in
// /work/.playground/checkpoints/<ref without refs/playground/>.json.
//
// Build output, the playground directory and the Emacs package and cache
// directories under emacs.d are listed in .git/info/exclude so checkpoints
// stay small and a restore never deletes installed packages.
package checkpoint
