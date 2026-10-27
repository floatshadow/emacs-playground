package pty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/inputtap"
)

// bash substitutes for `tmux attach` on hosts without tmux.
func bash() *exec.Cmd {
	cmd := exec.Command("bash", "--noprofile", "--norc", "-i")
	cmd.Env = []string{"TERM=xterm-256color", "PS1=$ ", "HOME=/tmp", "PATH=/usr/bin:/bin"}
	return cmd
}

type client struct {
	t   *testing.T
	c   *websocket.Conn
	ctx context.Context
	mu  sync.Mutex
	out bytes.Buffer
	ctl []protocol.PtyControl
	err error
	end chan struct{}
}

func dial(t *testing.T, url string) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	cl := &client{t: t, c: c, ctx: ctx, end: make(chan struct{})}
	go func() {
		defer close(cl.end)
		for {
			typ, data, err := c.Read(ctx)
			cl.mu.Lock()
			if err != nil {
				cl.err = err
				cl.mu.Unlock()
				return
			}
			if typ == websocket.MessageBinary {
				cl.out.Write(data)
			} else {
				var ctl protocol.PtyControl
				_ = json.Unmarshal(data, &ctl)
				cl.ctl = append(cl.ctl, ctl)
			}
			cl.mu.Unlock()
		}
	}()
	return cl
}

func (cl *client) send(s string) {
	if err := cl.c.Write(cl.ctx, websocket.MessageBinary, []byte(s)); err != nil {
		cl.t.Fatal(err)
	}
}

func (cl *client) control(v protocol.PtyControl) {
	b, _ := json.Marshal(v)
	if err := cl.c.Write(cl.ctx, websocket.MessageText, b); err != nil {
		cl.t.Fatal(err)
	}
}

func (cl *client) waitOutput(sub string) {
	cl.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		cl.mu.Lock()
		ok := strings.Contains(cl.out.String(), sub)
		cl.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.t.Fatalf("output never contained %q:\n%s", sub, cl.out.String())
}

func (cl *client) hasControl(t protocol.PtyControlType) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	for _, c := range cl.ctl {
		if c.T == t {
			return true
		}
	}
	return false
}

func (cl *client) closeStatus() websocket.StatusCode {
	cl.t.Helper()
	select {
	case <-cl.end:
	case <-time.After(10 * time.Second):
		cl.t.Fatal("connection did not close")
	}
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return websocket.CloseStatus(cl.err)
}

func TestPtyWithBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	var mu sync.Mutex
	var prefixes []string
	tap := inputtap.NewTap(func(ev *protocol.PrefixEvent) {
		mu.Lock()
		prefixes = append(prefixes, ev.Keys)
		mu.Unlock()
	})
	var active sync.WaitGroup
	open := 0
	var omu sync.Mutex
	h := &Handler{Spawn: bash, Tap: tap, DefaultCols: 120, DefaultRows: 36, Track: func(d int) {
		omu.Lock()
		open += d
		omu.Unlock()
	}}
	srv := httptest.NewServer(h)
	defer srv.Close()
	defer active.Wait()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	a := dial(t, url+"?cols=100&rows=30")
	a.send("stty size\r")
	a.waitOutput("30 100")
	if !a.hasControl(protocol.PtyAttached) {
		t.Fatal("no attached frame")
	}
	a.control(protocol.PtyControl{T: protocol.PtyResize, Cols: 132, Rows: 40})
	time.Sleep(50 * time.Millisecond)
	a.send("stty size; echo $((6*7))\r")
	a.waitOutput("40 132")
	a.waitOutput("42")
	a.control(protocol.PtyControl{T: protocol.PtyPing})
	deadline := time.Now().Add(5 * time.Second)
	for !a.hasControl(protocol.PtyPong) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !a.hasControl(protocol.PtyPong) {
		t.Fatal("no pong")
	}
	// Input is tapped: C-x is a pending prefix (then C-g clears it in bash
	// harmlessly).
	a.send("\x18")
	a.send("\x07")
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if strings.Join(prefixes, "|") != "C-x|" {
		t.Fatalf("prefix events %q", prefixes)
	}
	mu.Unlock()

	// A second viewer replaces the first.
	b := dial(t, url)
	if st := a.closeStatus(); st != protocol.CloseReplaced {
		t.Fatalf("first viewer close status %v", st)
	}
	if !a.hasControl(protocol.PtyReplaced) {
		t.Fatal("first viewer did not get replaced frame")
	}
	b.send("echo second\r")
	b.waitOutput("second")
	if !h.Active() {
		t.Fatal("viewer not active")
	}

	// The command exiting closes with 4001.
	b.send("exit\r")
	if st := b.closeStatus(); st != protocol.CloseEmacsUnavailable {
		t.Fatalf("exit close status %v", st)
	}
	time.Sleep(50 * time.Millisecond)
	if h.Active() {
		t.Fatal("viewer still registered")
	}
	omu.Lock()
	if open != 0 {
		t.Fatalf("open count %d", open)
	}
	omu.Unlock()
}

func TestPtyClientCloseKillsProcess(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	h := &Handler{Spawn: bash, DefaultCols: 80, DefaultRows: 24}
	srv := httptest.NewServer(h)
	defer srv.Close()
	a := dial(t, "ws"+strings.TrimPrefix(srv.URL, "http"))
	a.send("echo pid=$$.\r")
	a.waitOutput(".\r\n")
	a.mu.Lock()
	out := a.out.String()
	a.mu.Unlock()
	i := strings.LastIndex(out, "pid=")
	var pid int
	if _, err := fmt.Sscanf(out[i:], "pid=%d.", &pid); err != nil || pid <= 0 {
		t.Fatalf("pid from %q: %v", out, err)
	}
	_ = a.c.Close(websocket.StatusNormalClosure, "")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("bash still running after the client closed")
}

func TestPtyNotReadyAndDetached(t *testing.T) {
	h := &Handler{Spawn: bash, Ready: func() error { return errors.New("tmux is not running") }}
	srv := httptest.NewServer(h)
	defer srv.Close()
	a := dial(t, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if st := a.closeStatus(); st != protocol.CloseEmacsUnavailable {
		t.Fatalf("not ready: %v", st)
	}

	// An attach command that reports being detached (tmux prints this when
	// another client attaches with -d) closes as replaced.
	h2 := &Handler{Spawn: func() *exec.Cmd {
		return exec.Command("sh", "-c", "printf '[detached (from session emacs)]\\r\\n'")
	}}
	srv2 := httptest.NewServer(h2)
	defer srv2.Close()
	b := dial(t, "ws"+strings.TrimPrefix(srv2.URL, "http"))
	if st := b.closeStatus(); st != protocol.CloseReplaced {
		t.Fatalf("detached: %v", st)
	}

	// Shutdown closes with 4003.
	done := make(chan struct{})
	h3 := &Handler{Spawn: bash, Done: done}
	srv3 := httptest.NewServer(h3)
	defer srv3.Close()
	c := dial(t, "ws"+strings.TrimPrefix(srv3.URL, "http"))
	c.send("echo up\r")
	c.waitOutput("up")
	close(done)
	if st := c.closeStatus(); st != protocol.CloseSessionStopping {
		t.Fatalf("shutdown: %v", st)
	}
}
