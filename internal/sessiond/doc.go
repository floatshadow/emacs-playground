// Package sessiond wires the per-session daemon (backend.md §3): the startup
// sequence (§3.2), the HTTP mux on PG_LISTEN (/pty, /events, /vnc,
// /rpc/{op}, /preview/, /healthz, /status), RPC dispatch (api.md §3.4) and
// graceful shutdown (backend.md §5).
//
// TODO(sessiond, backend.md §3.1-§3.2): server.go with New(cfg) and Run.
package sessiond
