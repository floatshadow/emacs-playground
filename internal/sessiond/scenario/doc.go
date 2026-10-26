// Package scenario loads a lesson (backend.md §3.12, architecture.md §3.3):
// fetches the scenario and fixture tarball from the gateway (cached by ETag
// under /work/.playground/cache), ensures the module set, extracts fixtures
// without overwriting learner files unless setup.reset, runs the agent's
// setup, takes the checkpoint and activates the step.
//
// TODO(sessiond, backend.md §3.12).
package scenario
