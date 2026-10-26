// Package supervisor keeps the Emacs daemon, the tmux server, the terminal
// frame, Xvnc and the GUI frame alive with per-child restart policies
// (backend.md §3.3, architecture.md §5.2), and tracks agent heartbeats.
//
// Child runs one process in its own process group under a Policy
// (DaemonPolicy: backoff 1, 2, 4, 8 s, give up after 5 restarts in 10
// minutes; WhileActivePolicy for the GUI children). Supervisor.Start is
// steps 3 and 4 of the startup sequence; the frame watcher respawns the
// terminal pane after C-x C-c and restarts tmux when its server goes away;
// the heartbeat raises emacs unresponsive after five missed pings and sends
// Options.BusySignal (SIGUSR2, debug-on-event) to the daemon process;
// Interrupt is Rescue's C-g, followed by an error event offering a restart
// when Emacs is still busy RescueGrace later. SetMode starts Xvnc and the
// GUI frame on demand; SendKeys types through tmux send-keys or xdotool;
// RestartEmacs and crash recovery reopen the last snapshot's buffers.
//
// Commands and Tmux are interfaces over the real binaries (RealCommands,
// ExecTmux) with bash substitutes (FakeCommands, NoTmux) for tests and
// sessiond -fake.
package supervisor
