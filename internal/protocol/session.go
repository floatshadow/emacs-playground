package protocol

// Session endpoint paths served by sessiond on PG_LISTEN and reached through
// the gateway at /s/{sessionId} + path (api.md §3).
const (
	PathPty     = "/pty"
	PathEvents  = "/events"
	PathVNC     = "/vnc"
	PathRPC     = "/rpc/" // + op
	PathPreview = "/preview/"
	PathHealthz = "/healthz"
	PathStatus  = "/status"
)

// WebSocket close codes used by sessiond (api.md §3.1, §3.3).
const (
	// CloseReplaced: another /pty viewer took over.
	CloseReplaced = 4000
	// CloseEmacsUnavailable: tmux or Emacs could not be attached.
	CloseEmacsUnavailable = 4001
	// CloseGUINotActive: /vnc was opened while mode is terminal.
	CloseGUINotActive = 4002
	// CloseSessionStopping: the session is shutting down (SIGTERM).
	CloseSessionStopping = 4003
)

// PtyControlType is the "t" of a text control frame on /pty. Binary frames
// carry terminal bytes in both directions.
type PtyControlType string

// PTY control frame types.
const (
	// Client to server.
	PtyResize PtyControlType = "resize"
	PtyPing   PtyControlType = "ping"
	// Server to client.
	PtyAttached PtyControlType = "attached"
	PtyReplaced PtyControlType = "replaced"
	PtyPong     PtyControlType = "pong"
)

// PtyControl is a text frame on /pty, e.g. {"t":"resize","cols":120,"rows":36},
// {"t":"ping"}, {"t":"attached"}, {"t":"replaced"}.
type PtyControl struct {
	T    PtyControlType `json:"t"`
	Cols int            `json:"cols,omitempty"`
	Rows int            `json:"rows,omitempty"`
}

// EventsClientMessage is a text frame a client may send on /events
// ({"t":"ping"}). Anything else is ignored.
type EventsClientMessage struct {
	T string `json:"t"`
}

// Query parameters.
const (
	// QueryAfter on /events: replay events with seq greater than this.
	QueryAfter = "after"
	// QueryCols and QueryRows on /pty: initial terminal size.
	QueryCols = "cols"
	QueryRows = "rows"
)

// Headers.
const (
	// HeaderUser is set by the gateway proxy on requests to sessiond: the
	// authenticated user id.
	HeaderUser = "X-Playground-User"
	// HeaderRequestID carries the request id across services.
	HeaderRequestID = "X-Request-Id"
	// HeaderProxySecret carries the session's proxy secret on every request
	// the gateway sends to sessiond (proxy, RPC, status). sessiond started
	// with PG_PROXY_SECRET answers 401 to requests without it, except
	// GET /healthz, so one learner's container cannot drive another
	// learner's sessiond (deployment.md §9.1).
	HeaderProxySecret = "X-Playground-Proxy"
)

// HealthStatus is the body of GET /healthz on sessiond and the gateway.
type HealthStatus struct {
	OK bool `json:"ok"`
	// Reason explains a 503 ("agent not connected", "starting").
	Reason string `json:"reason,omitempty"`
}

// PreviewEntry is one item of a directory listing from GET /preview/{dir}/.
type PreviewEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
	// Mtime is the modification time in ms since the epoch.
	Mtime int64 `json:"mtime"`
}

// PreviewListing is the JSON body of GET /preview/{dir}/.
type PreviewListing struct {
	// Path is the listed directory relative to the workspace, "" for the root.
	Path    string         `json:"path"`
	Entries []PreviewEntry `json:"entries"`
}
