package events

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"

	"github.com/zsy/emacs-playground/internal/httpx"
	"github.com/zsy/emacs-playground/internal/protocol"
)

// HelloFunc builds the hello event for a new connection. replayed tells
// whether replayed events follow.
type HelloFunc func(replayed bool) *protocol.HelloEvent

// Handler serves GET /events (api.md §3.2): the first frame is hello, then
// the latest full state, then replayed events when ?after= was given and the
// ring buffer still covers the gap, then live events.
type Handler struct {
	Bus   *Bus
	Hello HelloFunc
	// Track is called with +1 when a connection opens and -1 when it closes
	// (active WebSocket count for /status and the gateway heartbeat).
	Track func(delta int)
	// Done is closed when the server shuts down; connections then close with
	// protocol.CloseSessionStopping.
	Done <-chan struct{}
	Log  *slog.Logger
	// PingInterval overrides httpx.PingInterval (tests).
	PingInterval time.Duration
}

const writeTimeout = 5 * time.Second

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	after := int64(-1)
	if v := r.URL.Query().Get(protocol.QueryAfter); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			httpx.WriteError(w, r, httpx.BadRequest("after must be a non-negative integer"))
			return
		}
		after = n
	}
	c, err := httpx.AcceptWebSocket(w, r, httpx.AcceptOptions{TrustProxy: true, ReadLimit: 64 << 10})
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

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub, snap := h.Bus.Subscribe(after)
	defer sub.Close()

	hello := h.Hello(snap.Replayed)
	hello.Replayed = snap.Replayed
	protocol.Stamp(hello, 0, time.Now().UnixMilli())
	if err := httpx.WriteJSONText(ctx, c, hello, writeTimeout); err != nil {
		return
	}
	if snap.State.Seq != 0 {
		if err := httpx.WriteText(ctx, c, snap.State.Data, writeTimeout); err != nil {
			return
		}
	}
	for _, f := range snap.Frames {
		if err := httpx.WriteText(ctx, c, f.Data, writeTimeout); err != nil {
			return
		}
	}

	// Read loop: clients may send {"t":"ping"}; everything is discarded, but
	// reading is required to process control frames and notice closes.
	go func() {
		defer cancel()
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	go httpx.KeepAlive(ctx, c, h.PingInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-h.Done:
			_ = httpx.Close(c, protocol.CloseSessionStopping, "session stopping")
			return
		case f := <-sub.C:
			if err := httpx.WriteText(ctx, c, f.Data, writeTimeout); err != nil {
				if !errors.Is(err, context.Canceled) && websocket.CloseStatus(err) == -1 {
					log.Debug("events write failed", "err", err)
				}
				return
			}
		}
	}
}
