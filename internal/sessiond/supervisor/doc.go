// Package supervisor keeps the Emacs daemon, the tmux server, the terminal
// frame, Xvnc and the GUI frame alive with per-child restart policies
// (backend.md §3.3, architecture.md §5.2), and tracks agent heartbeats.
//
// TODO(sessiond, backend.md §3.3).
package supervisor
