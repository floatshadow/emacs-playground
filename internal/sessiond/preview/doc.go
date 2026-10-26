// Package preview serves workspace files through os.Root with no-store,
// nosniff and a sandboxing CSP for HTML, JSON directory listings
// (protocol.PreviewListing), and watches the workspace recursively with
// fsnotify, emitting file events coalesced per path at 200 ms (backend.md
// §3.10).
//
// TODO(sessiond, backend.md §3.10).
package preview
