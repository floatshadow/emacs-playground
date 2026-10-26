// Package agent listens on the agent Unix socket, decodes JSON lines
// (protocol.DecodeAgentMessage), keeps at most one agent connection (a new
// one replaces the old, as after a daemon restart), answers pings, and
// correlates serialised RPC calls with per-call timeouts (backend.md §3.5,
// api.md §7).
//
// Calls go through one goroutine so Emacs never receives a second call
// before it replied to the first. Each call carries timeoutMs for the
// agent's with-timeout; sessiond waits that long plus a small grace before
// reporting ErrTimeout.
//
// Fake is a minimal in-process agent used by unit tests and by sessiond's
// -fake mode (bash instead of Emacs): it says hello, pings, and answers calls
// through a handler function.
package agent
