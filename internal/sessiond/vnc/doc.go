// Package vnc bridges the /vnc WebSocket to Xvnc on 127.0.0.1:<PG_VNC_PORT>
// (backend.md §3.11, api.md §3.3): binary frames carry the RFB stream in
// both directions, and client-to-server bytes also pass through the input
// tap's RFB decoder so prefix feedback works in the GUI frame. The bridge is
// only open while the session mode is gui; otherwise the socket closes with
// protocol.CloseGUINotActive (4002), and switching back to the terminal
// closes open bridges with 4002 before Xvnc stops (Handler.CloseAll). If
// Xvnc exits while GUI mode is on, the bridge closes with 4001. The
// "binary" subprotocol that noVNC and websockify use is negotiated when
// offered.
package vnc
