package inputtap

import "strconv"

// X keysyms (X11/keysymdef.h) that RFB KeyEvent messages carry, mapped to
// Emacs key names. Latin-1 keysyms (0x20..0xff) and Unicode keysyms
// (0x01000000 | code point) map to the character itself; the table below
// covers the function and keypad keys Emacs names.

// Modifier keysyms.
const (
	xkShiftL         = 0xffe1
	xkShiftR         = 0xffe2
	xkControlL       = 0xffe3
	xkControlR       = 0xffe4
	xkCapsLock       = 0xffe5
	xkShiftLck       = 0xffe6
	xkMetaL          = 0xffe7
	xkMetaR          = 0xffe8
	xkAltL           = 0xffe9
	xkAltR           = 0xffea
	xkSuperL         = 0xffeb
	xkSuperR         = 0xffec
	xkHyperL         = 0xffed
	xkHyperR         = 0xffee
	xkISOLevel3Shift = 0xfe03
	xkModeSwitch     = 0xff7e
	xkNumLock        = 0xff7f
)

// keysymNames maps function keysyms to an Emacs key: plain names (RET, TAB,
// DEL, ESC) are written as is, function keys without angle brackets.
var keysymNames = func() map[uint32]string {
	m := map[uint32]string{
		0xff08: "DEL",     // BackSpace
		0xff09: "TAB",     // Tab
		0xfe20: "backtab", // ISO_Left_Tab
		0xff0d: "RET",     // Return
		0xff1b: "ESC",     // Escape
		0xffff: "delete",  // Delete
		0xff50: "home",
		0xff51: "left",
		0xff52: "up",
		0xff53: "right",
		0xff54: "down",
		0xff55: "prior",
		0xff56: "next",
		0xff57: "end",
		0xff58: "begin",
		0xff60: "select",
		0xff61: "print",
		0xff62: "execute",
		0xff63: "insert",
		0xff65: "undo",
		0xff66: "redo",
		0xff67: "menu",
		0xff68: "find",
		0xff69: "cancel",
		0xff6a: "help",
		0xff6b: "break",
		0xff13: "pause",
		0xff14: "scroll-lock",
		0xff8d: "kp-enter",
		0xff80: "kp-space",
		0xff89: "kp-tab",
		0xff95: "kp-home",
		0xff96: "kp-left",
		0xff97: "kp-up",
		0xff98: "kp-right",
		0xff99: "kp-down",
		0xff9a: "kp-prior",
		0xff9b: "kp-next",
		0xff9c: "kp-end",
		0xff9d: "kp-begin",
		0xff9e: "kp-insert",
		0xff9f: "kp-delete",
		0xffbd: "kp-equal",
		0xffaa: "kp-multiply",
		0xffab: "kp-add",
		0xffac: "kp-separator",
		0xffad: "kp-subtract",
		0xffae: "kp-decimal",
		0xffaf: "kp-divide",
	}
	for i := uint32(0); i <= 9; i++ {
		m[0xffb0+i] = "kp-" + strconv.Itoa(int(i))
	}
	for i := uint32(0); i < 35; i++ {
		m[0xffbe+i] = "f" + strconv.Itoa(int(i)+1)
	}
	return m
}()

// isModifierKeysym reports keysyms that only change modifier state.
func isModifierKeysym(ks uint32) bool {
	switch ks {
	case xkShiftL, xkShiftR, xkControlL, xkControlR, xkCapsLock, xkShiftLck,
		xkMetaL, xkMetaR, xkAltL, xkAltR, xkSuperL, xkSuperR, xkHyperL, xkHyperR,
		xkISOLevel3Shift, xkModeSwitch, xkNumLock:
		return true
	}
	return false
}

// keysymRune returns the character of a printable keysym.
func keysymRune(ks uint32) (rune, bool) {
	switch {
	case ks >= 0x20 && ks <= 0x7e, ks >= 0xa0 && ks <= 0xff:
		return rune(ks), true
	case ks >= 0x01000100 && ks <= 0x0110ffff:
		return rune(ks - 0x01000000), true
	}
	return 0, false
}
