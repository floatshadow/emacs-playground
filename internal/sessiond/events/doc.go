// Package events is the event bus (backend.md §3.7): monotonic seq,
// protocol.Stamp, a 500-event ring buffer for ?after= replay, subscribers
// with 256-entry buffers that receive a gap event on overflow, and the
// /events WebSocket handler (hello first, then a full state).
//
// TODO(sessiond, backend.md §3.7).
package events
