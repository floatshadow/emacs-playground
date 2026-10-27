package vnc

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/inputtap"
)

// Handler serves GET /vnc.
type Handler struct {
	// Addr is the Xvnc address, e.g. 127.0.0.1:5901.
	Addr string
	// Active reports whether the GUI mode is on.
	Active func() bool
	// Tap observes client-to-server bytes; nil disables tapping.
	Tap *inputtap.Tap
	// Track counts open WebSockets.
	Track func(int)
	// Done closes bridges with protocol.CloseSessionStopping.
	Done <-chan struct{}
	Log  *slog.Logger

	mu    sync.Mutex
	conns map[*bridge]struct{}
}

// bridge is one open /vnc connection. The first close request wins: when
// the learner switches back to the terminal, CloseAll records 4002 before
// the supervisor stops Xvnc, so the display going away afterwards does not
// change the close code the browser sees.
type bridge struct {
	once   sync.Once
	stop   chan struct{}
	code   int
	reason string
}

func (b *bridge) close(code int, reason string) {
	b.once.Do(func() {
		b.code, b.reason = code, reason
		close(b.stop)
	})
}

// CloseAll asks every open bridge to close with protocol.CloseGUINotActive
// (the learner switched back to the terminal frame). It returns at once;
// each bridge performs its close handshake in its own goroutine. Call it
// before stopping Xvnc.
func (h *Handler) CloseAll() {
	h.mu.Lock()
	conns := h.conns
	h.conns = nil
	h.mu.Unlock()
	for b := range conns {
		b.close(protocol.CloseGUINotActive, "gui_not_active")
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // the gateway authenticated and checked Origin
		Subprotocols:       []string{"binary"},
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer c.CloseNow()
	if h.Track != nil {
		h.Track(1)
		defer h.Track(-1)
	}
	log := h.Log
	if log == nil {
		log = slog.Default()
	}
	if h.Active != nil && !h.Active() {
		_ = httpx.Close(c, protocol.CloseGUINotActive, "gui_not_active")
		return
	}
	var d net.Dialer
	dctx, dcancel := context.WithTimeout(r.Context(), 3*time.Second)
	tcp, err := d.DialContext(dctx, "tcp", h.Addr)
	dcancel()
	if err != nil {
		log.Warn("vnc dial failed", "addr", h.Addr, "err", err)
		_ = httpx.Close(c, protocol.CloseEmacsUnavailable, "the GUI display is not available")
		return
	}
	defer tcp.Close()

	b := &bridge{stop: make(chan struct{})}
	h.mu.Lock()
	if h.conns == nil {
		h.conns = map[*bridge]struct{}{}
	}
	h.conns[b] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.conns, b)
		h.mu.Unlock()
	}()
	// The mode may have changed between the check above and the
	// registration, in which case CloseAll missed this bridge.
	if h.Active != nil && !h.Active() {
		b.close(protocol.CloseGUINotActive, "gui_not_active")
	}

	// The NetConn's context is cancelled only after the close handshake:
	// cancelling it closes the WebSocket without a close frame.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ws := websocket.NetConn(ctx, c, websocket.MessageBinary)
	var src io.Reader = ws
	if h.Tap != nil {
		src = io.TeeReader(ws, h.Tap.NewRFBStream())
	}
	clientDone := make(chan struct{})
	displayDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(tcp, src)
		close(clientDone)
	}()
	go func() {
		_, _ = io.Copy(ws, tcp)
		close(displayDone)
	}()
	select {
	case <-b.stop:
	case <-clientDone:
		// The browser went away or sent a close frame.
		b.close(int(websocket.StatusNormalClosure), "")
	case <-displayDone:
		if h.Active != nil && !h.Active() {
			b.close(protocol.CloseGUINotActive, "gui_not_active")
		} else {
			// Xvnc exited while the GUI mode is on; the supervisor
			// restarts it and the browser may reconnect.
			b.close(protocol.CloseEmacsUnavailable, "the GUI display closed")
		}
	case <-h.Done:
		b.close(protocol.CloseSessionStopping, "session stopping")
	case <-r.Context().Done():
		b.close(int(websocket.StatusGoingAway), "")
	}
	_ = tcp.Close()
	_ = httpx.Close(c, b.code, b.reason)
}
