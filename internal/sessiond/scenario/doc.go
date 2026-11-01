// Package scenario holds the lesson-loading helpers the session core's load
// op composes (backend.md §3.12, architecture.md §3.3): fetching the
// scenario and the fixture tarball from the gateway (both cached by ETag
// under /work/.playground/cache, and used from the cache when the gateway
// is unreachable), extracting fixtures into /work/<setup.root> without
// overwriting learner files unless setup.reset, setup.repos and setup.run,
// and the module set in emacs.d/modules.el.
//
// Module unlocking is monotonic (emacs.md §4): a load enables the current
// set plus the scenario's modules (Union); rpc modules replaces it exactly.
package scenario
