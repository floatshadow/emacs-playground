package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// PingInterval is the WebSocket keepalive interval (backend.md §5).
const PingInterval = 20 * time.Second

// AcceptOptions configures AcceptWebSocket.
type AcceptOptions struct {
	// TrustProxy skips coder/websocket's Origin verification. sessiond sets
	// it: the gateway has already authenticated the user and checked Origin,
	// and the proxied request's Origin is the public URL, not sessiond's
	// host. The gateway itself never sets it.
	TrustProxy bool
	// OriginPatterns are additional allowed origin hosts (dev server).
	OriginPatterns []string
	// ReadLimit is the maximum message size; default 1 MB.
	ReadLimit int64
}

// AcceptWebSocket upgrades the request with compression disabled (binary
// terminal and VNC streams do not compress well and compression costs
// latency). On failure the library has already written an HTTP error.
func AcceptWebSocket(w http.ResponseWriter, r *http.Request, o AcceptOptions) (*websocket.Conn, error) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: o.TrustProxy,
		OriginPatterns:     o.OriginPatterns,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, err
	}
	limit := o.ReadLimit
	if limit <= 0 {
		limit = 1 << 20
	}
	c.SetReadLimit(limit)
	return c, nil
}

// KeepAlive pings c every interval until ctx ends or a ping fails; on
// failure it closes the connection. Run it in its own goroutine.
func KeepAlive(ctx context.Context, c *websocket.Conn, interval time.Duration) {
	if interval <= 0 {
		interval = PingInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, interval)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				_ = c.CloseNow()
				return
			}
		}
	}
}

// WriteJSONText writes v as one JSON text frame with a timeout.
func WriteJSONText(ctx context.Context, c *websocket.Conn, v any, timeout time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteText(ctx, c, b, timeout)
}

// WriteText writes b as one text frame with a timeout.
func WriteText(ctx context.Context, c *websocket.Conn, b []byte, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, b)
}

// Close closes c with a status code and reason (truncated to the 123-byte
// limit of close frames).
func Close(c *websocket.Conn, code int, reason string) error {
	if len(reason) > 120 {
		reason = reason[:120]
	}
	return c.Close(websocket.StatusCode(code), reason)
}
