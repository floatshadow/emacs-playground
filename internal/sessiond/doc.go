// Package sessiond wires the per-session daemon (backend.md §3): the startup
// sequence (§3.2), the HTTP mux on PG_LISTEN (/pty, /events, /vnc,
// /rpc/{op}, /preview/, /healthz, /status), RPC dispatch (api.md §3.4) and
// graceful shutdown (backend.md §5).
//
// Startup: create the run directory and open the agent socket; initialise
// the workspace (git, .playground, the pristine config copied to
// /work/emacs.d, modules.el with base and agent); start the HTTP server
// (healthz answers 503 "Emacs is starting" until the agent says hello, so
// the gateway and Compose can poll from the first second); start the Emacs
// daemon and wait for hello (60 s, restart once); start tmux and the
// terminal frame; resume the step saved in /work/.playground/state.json.
//
// Options.Fake substitutes bash for Emacs, tmux and Xvnc and runs an
// in-process fake agent, so the whole daemon runs on hosts without them
// (development and the pty and supervisor tests).
package sessiond
