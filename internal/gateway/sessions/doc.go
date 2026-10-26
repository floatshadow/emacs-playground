// Package sessions owns the session registry and lifecycle (backend.md
// §2.3-§2.4, architecture.md §5.1): Manager (Ensure, Get, Stop, Touch,
// Reap), Backend implementations staticBackend (local mode, one fixed
// session at local.sessionUrl) and dockerBackend (server mode, through
// internal/docker; experimental in slice 1), the keyed per-user mutex, the
// reaper and registry rebuild from container labels.
//
// TODO(gateway, backend.md §2.3-§2.4).
package sessions
