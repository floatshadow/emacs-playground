// Package pty serves /pty (backend.md §3.4, api.md §3.1): each WebSocket
// gets `tmux -L pg attach-session -d -t emacs` on a pty
// (github.com/creack/pty), bytes flow both ways as binary frames, a text
// frame {"t":"resize","cols":…,"rows":…} calls Setsize, and {"t":"ping"} is
// answered with {"t":"pong"}. One viewer at a time: a new connection sends
// {"t":"replaced"} to the previous one and closes it with
// protocol.CloseReplaced (4000). When tmux cannot be attached the socket
// closes with protocol.CloseEmacsUnavailable (4001).
//
// Input bytes go through the input tap's terminal decoder before they reach
// the pty; output bytes go through its mode tracker (kitty keyboard protocol
// detection). The command is supplied by the caller, so tests and sessiond's
// -fake mode attach bash instead of tmux.
package pty
