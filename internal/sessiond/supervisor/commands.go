package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// TmuxConfigPaths are tried in order for the tmux configuration (the
// session image installs the first; emacs.md §5).
var TmuxConfigPaths = []string{"/opt/playground/tmux.conf", "/etc/tmux.conf"}

// Env returns the environment shared by Emacs, tmux and the frames: the
// process environment plus the runtime directory (so the Emacs server
// socket lives in <RunDir>/emacs, emacs.md §1), the agent socket, the
// workspace, and terminal settings.
func Env(cfg *protocol.SessiondConfig) []string {
	env := os.Environ()
	set := map[string]string{
		"XDG_RUNTIME_DIR":  cfg.RunDir,
		"PG_AGENT_SOCKET":  cfg.AgentSocket,
		"PG_WORKSPACE":     cfg.Workspace,
		"PG_SESSION_ID":    cfg.SessionID,
		"COLORTERM":        "truecolor",
		"LANG":             envOr("LANG", "C.UTF-8"),
		"PG_EMACS_SERVER":  cfg.EmacsServer,
		"PG_PLAYGROUND":    "1",
		"PG_SESSION_TOKEN": "",
		"PG_PROXY_SECRET":  "",
	}
	// git refuses a workspace owned by another uid; see
	// checkpoint.SafeDirectoryEnv.
	if _, ok := os.LookupEnv("GIT_CONFIG_COUNT"); !ok {
		set["GIT_CONFIG_COUNT"] = "1"
		set["GIT_CONFIG_KEY_0"] = "safe.directory"
		set["GIT_CONFIG_VALUE_0"] = "*"
	}
	out := env[:0:0]
	for _, kv := range env {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if _, ok := set[k]; ok {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range set {
		if v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// InitDirectory is the live Emacs configuration in the workspace.
func InitDirectory(cfg *protocol.SessiondConfig) string {
	return filepath.Join(cfg.Workspace, "emacs.d")
}

// RealCommands builds the session image's process commands from cfg.
func RealCommands(cfg *protocol.SessiondConfig) Commands {
	env := Env(cfg)
	w, h := 1600, 900
	fmt.Sscanf(cfg.VNCGeometry, "%dx%d", &w, &h)
	display := cfg.Display
	return Commands{
		Daemon: func() *exec.Cmd {
			c := exec.Command(cfg.EmacsBin, "--fg-daemon="+cfg.EmacsServer, "--init-directory="+InitDirectory(cfg))
			c.Env = env
			c.Dir = cfg.Workspace
			return c
		},
		Xvnc: func() *exec.Cmd {
			// The geometry is the initial size only: the browser asks for
			// its pane's size (noVNC resizeSession, SetDesktopSize), which
			// Xvnc accepts and applies through RandR (B13).
			c := exec.Command(cfg.XvncBin, display,
				"-geometry", fmt.Sprintf("%dx%d", w, h), "-depth", "24",
				"-rfbport", fmt.Sprint(cfg.VNCPort), "-localhost", "-SecurityTypes", "None",
				"-dpi", "110", "-AlwaysShared", "-AcceptSetDesktopSize=1", "-desktop", "playground")
			c.Env = env
			return c
		},
		GUIFrame: func() *exec.Cmd {
			// No (fullscreen . fullboth): without a window manager it does
			// nothing. The agent sizes GUI frames to their display, also
			// when RandR changes it (emacs.md §10).
			c := exec.Command(cfg.EmacsclientBin, "-c", "-s", cfg.EmacsServer,
				"-F", fmt.Sprintf("((width . %d) (height . %d))", w/9, h/18))
			c.Env = append(append([]string{}, env...), "DISPLAY="+display)
			c.Dir = cfg.Workspace
			return c
		},
		Xdotool: func(args ...string) *exec.Cmd {
			c := exec.Command(cfg.XdotoolBin, args...)
			c.Env = append(append([]string{}, env...), "DISPLAY="+display)
			return c
		},
	}
}

// RealTmux builds the tmux manager for cfg.
func RealTmux(cfg *protocol.SessiondConfig) *ExecTmux {
	cols, rows := 120, 36
	fmt.Sscanf(cfg.TermSize, "%dx%d", &cols, &rows)
	conf := ""
	for _, p := range TmuxConfigPaths {
		if _, err := os.Stat(p); err == nil {
			conf = p
			break
		}
	}
	return &ExecTmux{
		Bin: cfg.TmuxBin, Socket: cfg.TmuxSocket, Config: conf, Cols: cols, Rows: rows,
		FrameCommand: []string{cfg.EmacsclientBin, "-t", "-s", cfg.EmacsServer},
		Env:          append(Env(cfg), "TERM=xterm-256color"),
	}
}

// AttachCommand returns the /pty command: tmux attach with -d so a new
// viewer detaches the previous one (backend.md §3.4).
func AttachCommand(cfg *protocol.SessiondConfig) func() *exec.Cmd {
	env := append(Env(cfg), "TERM=xterm-256color")
	return func() *exec.Cmd {
		c := exec.Command(cfg.TmuxBin, "-u", "-L", cfg.TmuxSocket, "attach-session", "-d", "-t", SessionName)
		c.Env = env
		return c
	}
}

// FakeCommands substitute bash for Emacs and the GUI processes (tests and
// sessiond -fake on hosts without Emacs, tmux or Xvnc).
func FakeCommands(bash string) Commands {
	if bash == "" {
		bash = "bash"
	}
	sleeper := func(name string) func() *exec.Cmd {
		return func() *exec.Cmd {
			return exec.Command(bash, "-c", "exec -a "+name+" sleep 2147483647")
		}
	}
	return Commands{
		Daemon:   sleeper("fake-emacs"),
		Xvnc:     sleeper("fake-xvnc"),
		GUIFrame: sleeper("fake-gui-frame"),
		Xdotool:  func(args ...string) *exec.Cmd { return exec.Command(bash, "-c", "true") },
	}
}

// FakeAttach returns a /pty command that runs an interactive bash.
func FakeAttach(bash, workspace string) func() *exec.Cmd {
	if bash == "" {
		bash = "bash"
	}
	return func() *exec.Cmd {
		c := exec.Command(bash, "--noprofile", "--norc", "-i")
		c.Dir = workspace
		c.Env = append(os.Environ(), "TERM=xterm-256color", "PS1=fake-emacs$ ")
		return c
	}
}
