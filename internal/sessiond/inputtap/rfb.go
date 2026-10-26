package inputtap

import (
	"encoding/binary"
	"unicode"

	"github.com/zsy/emacs-playground/internal/protocol"
)

// RFBDecoder passively parses the client-to-server half of an RFB 3.3, 3.7
// or 3.8 stream (RFC 6143) and reports key presses. It never alters or
// blocks the stream; when it meets a message it cannot size (an unknown
// extension or security type) it stops decoding for the rest of the
// connection.
type RFBDecoder struct {
	state    rfbState
	buf      []byte
	minor    int
	disabled bool
	mods     map[uint32]bool
}

type rfbState int

const (
	rfbVersion rfbState = iota
	rfbSecurity
	rfbVNCAuth
	rfbClientInit
	rfbMessages
)

// Disabled reports whether the decoder gave up on the stream.
func (d *RFBDecoder) Disabled() bool { return d.disabled }

// Feed consumes client-to-server bytes and returns key presses.
func (d *RFBDecoder) Feed(p []byte) []protocol.Key {
	if d.disabled {
		return nil
	}
	if d.mods == nil {
		d.mods = map[uint32]bool{}
	}
	d.buf = append(d.buf, p...)
	var out []protocol.Key
	for !d.disabled {
		n, keys, ok := d.step(d.buf)
		if !ok {
			break
		}
		out = append(out, keys...)
		d.buf = d.buf[n:]
	}
	if d.disabled {
		d.buf = nil
	}
	// Keep the buffer from growing without bound on huge cut-text messages:
	// step consumes them as soon as they are complete, so only partial
	// messages remain here.
	if cap(d.buf) > 1<<20 && len(d.buf) < 1<<10 {
		d.buf = append([]byte(nil), d.buf...)
	}
	return out
}

// step consumes one unit from b; ok is false when b is incomplete.
func (d *RFBDecoder) step(b []byte) (int, []protocol.Key, bool) {
	switch d.state {
	case rfbVersion:
		if len(b) < 12 {
			return 0, nil, false
		}
		// "RFB 003.008\n"
		if string(b[:4]) != "RFB " {
			d.disabled = true
			return 0, nil, false
		}
		d.minor = int(b[8]-'0')*100 + int(b[9]-'0')*10 + int(b[10]-'0')
		if d.minor < 7 {
			// 3.3: the server picks the security type; no client byte.
			d.state = rfbClientInit
		} else {
			d.state = rfbSecurity
		}
		return 12, nil, true
	case rfbSecurity:
		if len(b) < 1 {
			return 0, nil, false
		}
		switch b[0] {
		case 1: // None
			d.state = rfbClientInit
		case 2: // VNC authentication: 16-byte response
			d.state = rfbVNCAuth
		default:
			d.disabled = true
			return 0, nil, false
		}
		return 1, nil, true
	case rfbVNCAuth:
		if len(b) < 16 {
			return 0, nil, false
		}
		d.state = rfbClientInit
		return 16, nil, true
	case rfbClientInit:
		if len(b) < 1 {
			return 0, nil, false
		}
		d.state = rfbMessages
		return 1, nil, true
	}
	return d.message(b)
}

func (d *RFBDecoder) message(b []byte) (int, []protocol.Key, bool) {
	if len(b) < 1 {
		return 0, nil, false
	}
	need := func(n int) bool { return len(b) >= n }
	switch b[0] {
	case 0: // SetPixelFormat
		if !need(20) {
			return 0, nil, false
		}
		return 20, nil, true
	case 2: // SetEncodings
		if !need(4) {
			return 0, nil, false
		}
		n := 4 + 4*int(binary.BigEndian.Uint16(b[2:4]))
		if !need(n) {
			return 0, nil, false
		}
		return n, nil, true
	case 3: // FramebufferUpdateRequest
		if !need(10) {
			return 0, nil, false
		}
		return 10, nil, true
	case 4: // KeyEvent
		if !need(8) {
			return 0, nil, false
		}
		down := b[1] != 0
		ks := binary.BigEndian.Uint32(b[4:8])
		return 8, d.key(ks, down), true
	case 5: // PointerEvent
		if !need(6) {
			return 0, nil, false
		}
		return 6, nil, true
	case 6: // ClientCutText; a negative length is the extended clipboard
		if !need(8) {
			return 0, nil, false
		}
		l := int64(int32(binary.BigEndian.Uint32(b[4:8])))
		if l < 0 {
			l = -l
		}
		n := 8 + int(l)
		if !need(n) {
			return 0, nil, false
		}
		return n, nil, true
	case 150: // EnableContinuousUpdates
		if !need(10) {
			return 0, nil, false
		}
		return 10, nil, true
	case 248: // ClientFence
		if !need(9) {
			return 0, nil, false
		}
		n := 9 + int(b[8])
		if !need(n) {
			return 0, nil, false
		}
		return n, nil, true
	case 250: // xvp
		if !need(4) {
			return 0, nil, false
		}
		return 4, nil, true
	case 251: // SetDesktopSize
		if !need(8) {
			return 0, nil, false
		}
		n := 8 + 16*int(b[6])
		if !need(n) {
			return 0, nil, false
		}
		return n, nil, true
	case 253: // gii
		if !need(4) {
			return 0, nil, false
		}
		n := 4 + int(binary.BigEndian.Uint16(b[2:4]))
		if !need(n) {
			return 0, nil, false
		}
		return n, nil, true
	case 255: // QEMU client message
		if !need(2) {
			return 0, nil, false
		}
		switch b[1] {
		case 0: // extended key event
			if !need(12) {
				return 0, nil, false
			}
			down := binary.BigEndian.Uint16(b[2:4]) != 0
			ks := binary.BigEndian.Uint32(b[4:8])
			return 12, d.key(ks, down), true
		case 1: // audio
			if !need(4) {
				return 0, nil, false
			}
			if binary.BigEndian.Uint16(b[2:4]) == 2 {
				if !need(10) {
					return 0, nil, false
				}
				return 10, nil, true
			}
			return 4, nil, true
		}
	}
	d.disabled = true
	return 0, nil, false
}

// key updates modifier state and returns the key for a press.
func (d *RFBDecoder) key(ks uint32, down bool) []protocol.Key {
	if isModifierKeysym(ks) {
		if down {
			d.mods[ks] = true
		} else {
			delete(d.mods, ks)
		}
		return nil
	}
	if !down {
		return nil
	}
	k, ok := KeysymKey(ks, d.modifiers())
	if !ok {
		return nil
	}
	return []protocol.Key{k}
}

func (d *RFBDecoder) modifiers() protocol.Modifiers {
	var m protocol.Modifiers
	for ks := range d.mods {
		switch ks {
		case xkShiftL, xkShiftR:
			m |= protocol.ModShift
		case xkControlL, xkControlR:
			m |= protocol.ModCtrl
		case xkAltL, xkAltR, xkMetaL, xkMetaR:
			m |= protocol.ModMeta
		case xkSuperL, xkSuperR:
			m |= protocol.ModSuper
		case xkHyperL, xkHyperR:
			m |= protocol.ModHyper
		}
	}
	return m
}

// KeysymKey converts an X keysym pressed with modifiers to an Emacs key as
// a GUI frame would see it: shift is folded into characters (the keysym is
// already shifted), C-<uppercase> becomes C-S-<lowercase>, and function keys
// keep S-.
func KeysymKey(ks uint32, mods protocol.Modifiers) (protocol.Key, bool) {
	if name, ok := keysymNames[ks]; ok {
		switch name {
		case "RET", "TAB", "DEL", "ESC":
			return protocol.Key{Mods: mods &^ protocol.ModShift, Base: name}, true
		case "backtab":
			return protocol.Key{Mods: mods &^ protocol.ModShift, Base: name, Func: true}, true
		}
		return protocol.Key{Mods: mods, Base: name, Func: true}, true
	}
	r, ok := keysymRune(ks)
	if !ok {
		return protocol.Key{}, false
	}
	mods &^= protocol.ModShift
	if r == ' ' {
		return protocol.Key{Mods: mods, Base: "SPC"}, true
	}
	if mods&protocol.ModCtrl != 0 && unicode.IsUpper(r) {
		r = unicode.ToLower(r)
		mods |= protocol.ModShift
	}
	if !unicode.IsPrint(r) {
		return protocol.Key{}, false
	}
	return protocol.Key{Mods: mods, Base: string(r)}, true
}
