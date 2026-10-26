// Package progress aggregates step progress into lesson progress (api.md
// §2.4, backend.md §2.9): applies protocol.ProgressReport from sessiond,
// answers GET /api/v1/progress, and resets a lesson (telling sessiond to
// drop the active step through rpc drop-step).
//
// TODO(gateway, backend.md §2.9).
package progress
