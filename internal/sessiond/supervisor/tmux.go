package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Tmux manages the tmux server that hosts the terminal frame.
type Tmux interface {
	// Start creates the server and the session running the terminal frame
	// command if the session does not exist, and installs the pane-died
	// hook. It is idempotent.
	Start(ctx context.Context) error
	// Alive reports whether the session exists.
	Alive(ctx context.Context) bool
	// PaneDead reports whether the frame's pane has exited.
	PaneDead(ctx context.Context) bool
	// RespawnPane restarts the terminal frame command in the pane.
	RespawnPane(ctx context.Context) error
	// WaitPaneDied blocks until the pane-died hook fires (nil) or the server
	// goes away (error).
	WaitPaneDied(ctx context.Context) error
	// SendKeys sends tmux key names, or literal text with literal set.
	SendKeys(ctx context.Context, literal bool, keys ...string) error
	// Kill stops the server.
	Kill(ctx context.Context) error
}

// waitChannel is the tmux wait-for channel signalled by the pane-died hook.
const waitChannel = "pg-pane-died"

// SessionName is the tmux session hosting the terminal frame.
const SessionName = "emacs"

// ExecTmux runs the tmux binary.
type ExecTmux struct {
	Bin    string
	Socket string
	// Config is a tmux.conf to load when the server starts ("" for none).
	Config string
	// Cols and Rows size the window until a client attaches.
	Cols, Rows int
	// FrameCommand is the pane command (emacsclient -t -s playground).
	FrameCommand []string
	// Env is the environment of the server (inherited by the pane).
	Env []string
}

func (t *ExecTmux) cmd(ctx context.Context, args ...string) *exec.Cmd {
	full := append([]string{"-u", "-L", t.Socket}, args...)
	c := exec.CommandContext(ctx, t.Bin, full...)
	c.Env = t.Env
	return c
}

func (t *ExecTmux) run(ctx context.Context, args ...string) (string, error) {
	c := t.cmd(ctx, args...)
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func shellQuote(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		if a != "" && strings.IndexFunc(a, func(r rune) bool {
			return !(r == '-' || r == '_' || r == '.' || r == '/' || r == '=' || r == ':' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		}) < 0 {
			q[i] = a
			continue
		}
		q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(q, " ")
}

// Start implements Tmux.
func (t *ExecTmux) Start(ctx context.Context) error {
	if t.Alive(ctx) {
		return t.configure(ctx)
	}
	args := []string{}
	if t.Config != "" {
		if _, err := os.Stat(t.Config); err == nil {
			args = append(args, "-f", t.Config)
		}
	}
	args = append(args, "new-session", "-d", "-s", SessionName,
		"-x", fmt.Sprint(t.Cols), "-y", fmt.Sprint(t.Rows), shellQuote(t.FrameCommand))
	if _, err := t.run(ctx, args...); err != nil {
		return err
	}
	return t.configure(ctx)
}

// EscapeTime is tmux's escape-time in milliseconds: how long tmux waits
// after an ESC from the browser for the rest of an escape sequence. With 0,
// a sequence split across two reads (a focus report ESC [ I) reaches Emacs
// as a lone ESC followed by text that self-inserts. It must stay well below
// the input tap's inputtap.MetaTimeout, which assumes Emacs has a lone ESC
// before the tap reports it.
const EscapeTime = 10

// configure applies the options sessiond relies on, whatever tmux.conf
// says: the pane stays after its command exits so it can be respawned, and
// the pane-died hook signals the wait channel.
func (t *ExecTmux) configure(ctx context.Context) error {
	cmds := [][]string{
		{"set-option", "-t", SessionName, "remain-on-exit", "on"},
		{"set-hook", "-t", SessionName, "pane-died", "wait-for -S " + waitChannel},
		{"set-option", "-g", "status", "off"},
		{"set-option", "-g", "prefix", "None"},
		{"set-option", "-g", "prefix2", "None"},
		{"set-option", "-s", "escape-time", fmt.Sprint(EscapeTime)},
	}
	for _, c := range cmds {
		if _, err := t.run(ctx, c...); err != nil {
			return err
		}
	}
	return nil
}

// Alive implements Tmux.
func (t *ExecTmux) Alive(ctx context.Context) bool {
	_, err := t.run(ctx, "has-session", "-t", SessionName)
	return err == nil
}

// PaneDead implements Tmux.
func (t *ExecTmux) PaneDead(ctx context.Context) bool {
	out, err := t.run(ctx, "display-message", "-p", "-t", SessionName, "#{pane_dead}")
	return err == nil && out == "1"
}

// RespawnPane implements Tmux.
func (t *ExecTmux) RespawnPane(ctx context.Context) error {
	_, err := t.run(ctx, "respawn-pane", "-k", "-t", SessionName, shellQuote(t.FrameCommand))
	return err
}

// WaitPaneDied implements Tmux.
func (t *ExecTmux) WaitPaneDied(ctx context.Context) error {
	c := t.cmd(ctx, "wait-for", waitChannel)
	var errb bytes.Buffer
	c.Stderr = &errb
	start := time.Now()
	err := c.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("tmux wait-for: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	if !t.Alive(ctx) {
		return errors.New("tmux server exited")
	}
	_ = start
	return nil
}

// SendKeys implements Tmux.
func (t *ExecTmux) SendKeys(ctx context.Context, literal bool, keys ...string) error {
	args := []string{"send-keys", "-t", SessionName}
	if literal {
		args = append(args, "-l", "--")
	}
	_, err := t.run(ctx, append(args, keys...)...)
	return err
}

// Kill implements Tmux.
func (t *ExecTmux) Kill(ctx context.Context) error {
	_, err := t.run(ctx, "kill-server")
	return err
}

// NoTmux stands in for tmux in -fake mode and tests: the terminal is
// whatever the pty spawns (bash), there is no pane to respawn.
type NoTmux struct{}

// Start implements Tmux.
func (NoTmux) Start(context.Context) error { return nil }

// Alive implements Tmux.
func (NoTmux) Alive(context.Context) bool { return true }

// PaneDead implements Tmux.
func (NoTmux) PaneDead(context.Context) bool { return false }

// RespawnPane implements Tmux.
func (NoTmux) RespawnPane(context.Context) error { return nil }

// WaitPaneDied implements Tmux: it blocks until ctx ends.
func (NoTmux) WaitPaneDied(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

// SendKeys implements Tmux.
func (NoTmux) SendKeys(context.Context, bool, ...string) error { return nil }

// Kill implements Tmux.
func (NoTmux) Kill(context.Context) error { return nil }
