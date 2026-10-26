// Package events is the event bus (backend.md §3.7): monotonic seq,
// protocol.Stamp, a 500-event ring buffer for ?after= replay, subscribers
// with 256-entry buffers that receive a gap event on overflow, and the
// /events WebSocket handler (hello first, then a full state).
//
// Producers (the agent reader, the input tap, the supervisor, the file
// watcher and the step engine) call Bus.Publish. Every event is encoded
// once; subscribers, the replay buffer, the stuck-report window (Bus.Recent)
// and the event log (LogWriter, deployment.md §9.4) share the encoded frame.
package events
