// Package vnc bridges the /vnc WebSocket to Xvnc on 127.0.0.1:<PG_VNC_PORT>,
// passing client-to-server bytes through the RFB key decoder (backend.md
// §3.11).
//
// TODO(sessiond, backend.md §3.11).
package vnc
