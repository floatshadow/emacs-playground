// Package store persists users, invites, sessions, progress, stuck reports
// and the audit log (backend.md §2.7): a SQLite implementation
// (modernc.org/sqlite, WAL, one writer, embedded migrations) for server mode
// and jsonStore over <dataDir>/local.json (atomic writes) for local mode,
// both behind:
//
//	type Store interface {
//	    Users() UserRepo
//	    Sessions() SessionRepo
//	    Progress() ProgressRepo
//	    Stuck() StuckRepo
//	    Invites() InviteRepo
//	    Audit(ctx context.Context, e AuditEvent) error
//	}
//
// TODO(gateway, backend.md §2.7): jsonStore in slice 1; SQLite store with
// the schema in backend.md §2.7.
package store
