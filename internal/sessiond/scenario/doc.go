// Package scenario loads a lesson (backend.md §3.12, architecture.md §3.3):
// it fetches the scenario and the fixture tarball from the gateway (both
// cached by ETag under /work/.playground/cache, and used from the cache when
// the gateway is unreachable), extracts fixtures into /work/<setup.root>
// without overwriting learner files unless setup.reset, ensures the module
// set (rewriting modules.el and restarting the daemon with buffer restore
// when it changes), runs the agent's setup (open, layout, eval, compile),
// takes the step's checkpoint and activates the step in the engine.
//
// Loading the lesson that is already active is idempotent: a page reload
// does not re-run setup or overwrite the step's checkpoint. The checkpoint
// of a step is taken when it does not exist yet or when fixtures were
// re-extracted with reset, so "reset step" returns to the state the learner
// first saw.
//
// Module unlocking is monotonic (emacs.md §4): the enabled set becomes the
// current set plus the scenario's modules; rpc modules replaces it exactly.
package scenario
