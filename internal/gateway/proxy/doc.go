// Package proxy is the per-session reverse proxy for /s/{id}/… (backend.md
// §2.5): one cached httputil.ReverseProxy per session with Rewrite setting
// the target and protocol.HeaderUser and stripping cookies; WebSocket
// upgrades pass through; open WebSockets are counted per session for the
// reaper and limited to 6 per user.
//
// TODO(gateway, backend.md §2.5).
package proxy
