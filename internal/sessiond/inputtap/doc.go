// Package inputtap decodes terminal input bytes (legacy and kitty keyboard
// protocol) and RFB KeyEvent messages into Emacs key names
// (protocol.Key.String) and emits prefix events for pending prefix keys
// (backend.md §3.6, ADR-013).
//
// TODO(sessiond, backend.md §3.6).
package inputtap
