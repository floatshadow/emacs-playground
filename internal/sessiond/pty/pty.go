package pty

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	cpty "github.com/creack/pty"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/inputtap"
)

// Size limits for resize requests.
const (
	minCols, maxCols = 10, 1000
	minRows, maxRows = 4, 500
)

// Handler serves GET /pty.
type Handler struct {
	// Spawn returns a fresh, unstarted command for one viewer.
	Spawn func() *exec.Cmd
	// Ready returns nil when the terminal can be attached (tmux is up).
	Ready func() error
	// Tap observes input and output; nil disables tapping.
	Tap *inputtap.Tap
	// DefaultCols and DefaultRows apply when the query has no size.
	DefaultCols, DefaultRows int
	// Track counts open WebSockets (+1/-1).
	Track func(int)
	// Done closes every viewer with protocol.CloseSessionStopping.
	Done <-chan struct{}
	Log  *slog.Logger
	// OnResize observes size changes (the supervisor keeps tmux's window
	// size in sync for other clients).
	OnResize func(cols, rows int)
	// OnDetach, when set, is called after an attached viewer's connection
	// ended (the page went away, reloaded or was replaced).
	OnDetach func()

	mu  sync.Mutex
	cur *viewer
}

type viewer struct {
	c        *websocket.Conn
	cmd      *exec.Cmd
	f        *os.File
	mu       sync.Mutex
	replaced bool
	cancel   context.CancelFunc
}

// Active reports whether a viewer is attached.
func (h *Handler) Active() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur != nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func queryInt(r *http.Request, k string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(k)); err == nil && v > 0 {
		return v
	}
	return def
}

func (h *Handler) log() *slog.Logger {
	if h.Log == nil {
		return slog.Default()
	}
	return h.Log
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := httpx.AcceptWebSocket(w, r, httpx.AcceptOptions{TrustProxy: true, ReadLimit: 1 << 20})
	if err != nil {
		return
	}
	defer c.CloseNow()
	if h.Track != nil {
		h.Track(1)
		defer h.Track(-1)
	}
	if h.Ready != nil {
		if err := h.Ready(); err != nil {
			_ = httpx.Close(c, protocol.CloseEmacsUnavailable, err.Error())
			return
		}
	}
	cols := clamp(queryInt(r, protocol.QueryCols, h.DefaultCols), minCols, maxCols)
	rows := clamp(queryInt(r, protocol.QueryRows, h.DefaultRows), minRows, maxRows)

	cmd := h.Spawn()
	f, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		h.log().Warn("pty start failed", "err", err)
		_ = httpx.Close(c, protocol.CloseEmacsUnavailable, "the terminal could not be attached")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	v := &viewer{c: c, cmd: cmd, f: f, cancel: cancel}
	if h.OnDetach != nil {
		defer h.OnDetach()
	}

	h.mu.Lock()
	old := h.cur
	h.cur = v
	h.mu.Unlock()
	if old != nil {
		// The close handshake may take a moment; do not hold up the new
		// viewer (tmux attach -d detaches the old client anyway).
		go old.replace()
	}
	defer func() {
		h.mu.Lock()
		if h.cur == v {
			h.cur = nil
		}
		h.mu.Unlock()
	}()
	if h.OnResize != nil {
		h.OnResize(cols, rows)
	}

	var stream *inputtap.TermStream
	if h.Tap != nil {
		stream = h.Tap.NewTermStream()
		defer stream.Close()
	}
	if err := httpx.WriteJSONText(ctx, c, protocol.PtyControl{T: protocol.PtyAttached}, 5*time.Second); err != nil {
		v.kill()
		_ = cmd.Wait()
		return
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// pty → WebSocket.
	outDone := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 32<<10)
		var tail []byte
		for {
			n, err := f.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				if stream != nil {
					stream.Output(chunk)
				}
				tail = append(tail, chunk...)
				if len(tail) > 256 {
					tail = tail[len(tail)-256:]
				}
				wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
				werr := c.Write(wctx, websocket.MessageBinary, chunk)
				wcancel()
				if werr != nil {
					cancel()
					outDone <- tail
					return
				}
			}
			if err != nil {
				outDone <- tail
				return
			}
		}
	}()

	// WebSocket → pty.
	go func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if stream != nil {
					stream.Input(data)
				}
				if _, err := f.Write(data); err != nil {
					return
				}
				continue
			}
			var ctl protocol.PtyControl
			if json.Unmarshal(data, &ctl) != nil {
				continue
			}
			switch ctl.T {
			case protocol.PtyResize:
				cc, rr := clamp(ctl.Cols, minCols, maxCols), clamp(ctl.Rows, minRows, maxRows)
				if err := cpty.Setsize(f, &cpty.Winsize{Cols: uint16(cc), Rows: uint16(rr)}); err != nil {
					h.log().Debug("pty resize", "err", err)
				}
				if h.OnResize != nil {
					h.OnResize(cc, rr)
				}
			case protocol.PtyPing:
				_ = httpx.WriteJSONText(ctx, c, protocol.PtyControl{T: protocol.PtyPong}, 5*time.Second)
			}
		}
	}()
	go httpx.KeepAlive(ctx, c, 0)

	select {
	case <-exited:
		// The attach command ended on its own: tmux detached us (another
		// client attached with -d) or the session went away.
		var tail []byte
		select {
		case tail = <-outDone:
		case <-time.After(500 * time.Millisecond):
		}
		_ = f.Close()
		if v.isReplaced() {
			return
		}
		if bytes.Contains(tail, []byte("[detached")) {
			_ = httpx.WriteJSONText(context.Background(), c, protocol.PtyControl{T: protocol.PtyReplaced}, time.Second)
			_ = httpx.Close(c, protocol.CloseReplaced, "attached elsewhere")
			return
		}
		_ = httpx.Close(c, protocol.CloseEmacsUnavailable, "the terminal exited")
	case <-ctx.Done():
		// Client went away or we were replaced.
		v.kill()
		_ = f.Close()
		waitOrKill(cmd, exited)
	case <-h.Done:
		v.kill()
		_ = f.Close()
		waitOrKill(cmd, exited)
		_ = httpx.Close(c, protocol.CloseSessionStopping, "session stopping")
	}
}

func waitOrKill(cmd *exec.Cmd, exited <-chan error) {
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-exited
	}
}

func (v *viewer) isReplaced() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.replaced
}

// replace tells the viewer another one took over and closes it.
func (v *viewer) replace() {
	v.mu.Lock()
	v.replaced = true
	v.mu.Unlock()
	_ = httpx.WriteJSONText(context.Background(), v.c, protocol.PtyControl{T: protocol.PtyReplaced}, time.Second)
	_ = httpx.Close(v.c, protocol.CloseReplaced, "replaced")
	v.cancel()
}

// kill hangs up the attach process (tmux detaches the client on SIGHUP).
func (v *viewer) kill() {
	if v.cmd.Process == nil {
		return
	}
	if err := v.cmd.Process.Signal(syscall.SIGHUP); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = v.cmd.Process.Kill()
	}
}
