// Package httpx holds the HTTP plumbing shared by the gateway and sessiond:
// the problem+json error type and handler adapter (errors.go), JSON helpers
// (json.go), middleware for request ids, logging, panic recovery and Origin
// checks (middleware.go), and WebSocket helpers over github.com/coder/websocket
// (websocket.go). It has no knowledge of either service.
package httpx
