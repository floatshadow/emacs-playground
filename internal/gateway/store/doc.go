// Package store persists users, invites, sessions, progress, stuck reports
// and the audit log (backend.md §2.7) behind one interface with two
// implementations:
//
//   - OpenSQLite: modernc.org/sqlite (no cgo), WAL, busy_timeout 5 s, one
//     connection (so one writer), migrations embedded from migrations/*.sql
//     and recorded in schema_migrations. Server mode.
//   - OpenJSON: <dataDir>/local.json, human-readable, rewritten atomically
//     (temp file, fsync, rename) on every change. Local mode, so that a
//     local install has no database file and progress is easy to copy.
//
// Timestamps are milliseconds since the epoch. Missing rows are ErrNotFound.
package store
