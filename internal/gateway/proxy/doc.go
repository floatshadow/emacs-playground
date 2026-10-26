// Package proxy is the per-session reverse proxy for /s/{id}/… (backend.md
// §2.5): one cached httputil.ReverseProxy per session whose Rewrite sets the
// target, strips the /s/{id} prefix, sets protocol.HeaderUser and the
// request id, and removes the Cookie and Authorization headers; Set-Cookie
// from sessiond is dropped. WebSocket upgrades pass through ReverseProxy
// (101 Switching Protocols); the handler counts open WebSockets per session
// (Sessions.Opened and Closed, for the reaper) and enforces a limit of 6
// per user.
//
// Only the session's owner gets through (any user for the shared local
// session). Sessions that are not ready or idle answer 503
// session_unavailable. POST /rpc/eval is refused unless author mode is on,
// and internal ops (drop-step) are refused from browsers.
package proxy
