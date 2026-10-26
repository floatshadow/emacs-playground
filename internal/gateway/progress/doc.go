// Package progress aggregates step progress into lesson progress (api.md
// §2.4, backend.md §2.9) and files stuck reports (api.md §2.5).
//
// Service.Apply takes protocol.ProgressReport from sessiond
// (POST /internal/sessions/{id}/progress): hintsUsed and resets are
// absolute totals for the step; a done step is never downgraded by a later
// in-progress report (only POST /progress/reset clears it); completedAt is
// the report's at, else now. Steps of lessons the bundle knows are checked
// against the lesson's scenario.
//
// Service.Get answers GET /api/v1/progress: a lesson is done when every
// step of its scenario is done, in-progress when any step has a record,
// otherwise absent.
//
// Stuck files "I'm stuck" reports. The browser's POST /api/v1/stuck makes
// the gateway ask sessiond for its last events (rpc stuck) and sessiond may
// also file the same report through /internal/sessions/{id}/stuck; reports
// with the same user, lesson, step and note within 30 seconds are merged so
// one click stores one report. Reports are rate limited per user.
package progress
