// Package pty attaches `tmux -L pg attach-session -t emacs` on a pty
// (github.com/creack/pty) per /pty WebSocket, forwards bytes both ways,
// handles resize control frames, enforces a single viewer (close code
// protocol.CloseReplaced) and feeds the input tap (backend.md §3.4).
//
// TODO(sessiond, backend.md §3.4).
package pty
