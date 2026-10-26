// Package inputtap decodes what the learner types into Emacs key names,
// independently of Emacs, to drive instant prefix feedback (backend.md §3.6,
// ADR-013).
//
//   - TermDecoder: terminal input bytes (legacy encoding, CSI and SS3
//     sequences, xterm modifyOtherKeys, kitty keyboard protocol CSI u) into
//     protocol.Key values. Bracketed paste, mouse reports and terminal
//     replies are skipped.
//   - ModeTracker: watches terminal output for the kitty keyboard protocol
//     being pushed, set or popped (CSI > n u, CSI = n ; m u, CSI < n u) and
//     for modifyOtherKeys, so the decoder knows how ESC is encoded.
//   - RFBDecoder: the client-to-server RFB stream on /vnc; KeyEvent (type 4)
//     and QEMU extended key events with X keysyms mapped to Emacs keys and
//     modifier state tracked from the modifier keysyms.
//   - PrefixTracker: the static prefix list; reports a pending prefix after
//     each key and clears it on C-g.
//   - Tap ties them together for one Emacs: one PrefixTracker shared by every
//     input stream, one decoder per terminal or VNC connection, and an emit
//     callback that publishes protocol.PrefixEvent.
package inputtap
