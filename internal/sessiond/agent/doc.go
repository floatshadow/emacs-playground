// Package agent listens on the agent Unix socket, decodes JSON lines
// (protocol.DecodeAgentMessage), keeps at most one agent connection, answers
// pings, and correlates serialised RPC calls with per-call timeouts
// (backend.md §3.5, api.md §7).
//
// TODO(sessiond, backend.md §3.5).
package agent
