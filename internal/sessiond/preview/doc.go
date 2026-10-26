// Package preview serves workspace files through os.Root with no-store,
// nosniff and a sandboxing CSP for active content, JSON directory listings
// (protocol.PreviewListing), and watches the workspace recursively with
// fsnotify, emitting file events coalesced per path at 200 ms (backend.md
// §3.10, architecture.md §3.6).
//
// The .playground directory (session state, caches, SSH keys for the
// remote box) is never served.
package preview
