package inputtap

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zsy/emacs-playground/internal/protocol"
)

// MetaTimeout is how long a lone ESC waits for a following byte before it
// is reported as ESC rather than the start of M-<key> (legacy encoding).
const MetaTimeout = 50 * time.Millisecond

// maxPending bounds an unterminated escape sequence; longer ones are
// discarded.
const maxPending = 256

// TermDecoder is a state machine over terminal input bytes. It is not safe
// for concurrent use; one decoder per input stream.
type TermDecoder struct {
	buf   []byte
	escAt time.Time
	// Kitty is set when the output side has enabled the kitty keyboard
	// protocol: the ESC key then arrives as CSI 27 u, so a raw ESC byte that
	// is not followed by a sequence is reported at once.
	Kitty bool
	paste bool
}

// Pending reports whether bytes are buffered waiting for more input (a
// lone ESC or an incomplete sequence); call Flush after MetaTimeout.
func (d *TermDecoder) Pending() bool { return len(d.buf) > 0 }

// Feed decodes p, received at now, and returns the completed keys.
func (d *TermDecoder) Feed(p []byte, now time.Time) []protocol.Key {
	var out []protocol.Key
	if len(d.buf) > 0 && d.buf[0] == 0x1b && now.Sub(d.escAt) > MetaTimeout && !d.paste {
		// The previous chunk ended in ESC (or an incomplete sequence) and
		// nothing followed in time: report what we have.
		out = append(out, d.flushStale()...)
	}
	if len(d.buf) == 0 && len(p) > 0 {
		d.escAt = now
	}
	d.buf = append(d.buf, p...)
	keys, rest := d.decode(d.buf, false)
	out = append(out, keys...)
	if len(rest) > maxPending {
		rest = nil
	}
	if len(rest) > 0 && len(rest) != len(d.buf) {
		d.escAt = now
	}
	d.buf = append(d.buf[:0], rest...)
	return out
}

// Flush reports a pending lone ESC (or the prefix of an unterminated
// sequence) once MetaTimeout has passed since it arrived.
func (d *TermDecoder) Flush(now time.Time) []protocol.Key {
	if len(d.buf) == 0 || now.Sub(d.escAt) < MetaTimeout {
		return nil
	}
	return d.flushStale()
}

func (d *TermDecoder) flushStale() []protocol.Key {
	keys, _ := d.decode(d.buf, true)
	d.buf = d.buf[:0]
	return keys
}

// decode consumes as much of b as forms complete keys. With final set, an
// incomplete trailing sequence is resolved as if no more bytes will come.
func (d *TermDecoder) decode(b []byte, final bool) ([]protocol.Key, []byte) {
	var out []protocol.Key
	i := 0
	for i < len(b) {
		if d.paste {
			// Inside bracketed paste: skip until ESC [ 201 ~.
			j := indexOf(b[i:], "\x1b[201~")
			if j < 0 {
				// Keep a possible partial terminator.
				keep := len(b) - i
				if keep > 5 {
					keep = 5
				}
				return out, b[len(b)-keep:]
			}
			i += j + len("\x1b[201~")
			d.paste = false
			continue
		}
		c := b[i]
		if c != 0x1b {
			k, n, ok := decodeChar(b[i:], final)
			if !ok {
				return out, b[i:]
			}
			if n == 0 {
				n = 1
			}
			if k.Base != "" {
				out = append(out, k)
			}
			i += n
			continue
		}
		// ESC.
		if i+1 >= len(b) {
			if final || d.Kitty {
				out = append(out, protocol.Key{Base: "ESC"})
				i++
				continue
			}
			return out, b[i:]
		}
		switch b[i+1] {
		case '[':
			keys, n, ok := d.parseCSI(b[i:], final)
			if !ok {
				if final {
					// ESC [ with nothing after it in time: M-[.
					out = append(out, protocol.Key{Mods: protocol.ModMeta, Base: "["})
					i += 2
					continue
				}
				return out, b[i:]
			}
			out = append(out, keys...)
			i += n
		case 'O':
			if i+2 >= len(b) {
				if final {
					out = append(out, protocol.Key{Mods: protocol.ModMeta, Base: "O"})
					i += 2
					continue
				}
				return out, b[i:]
			}
			if k, ok := ss3Key(b[i+2]); ok {
				out = append(out, k)
				i += 3
				continue
			}
			out = append(out, protocol.Key{Mods: protocol.ModMeta, Base: "O"})
			i += 2
		default:
			// ESC followed by a key: Meta.
			k, n, ok := decodeChar(b[i+1:], final)
			if !ok {
				return out, b[i:]
			}
			if b[i+1] == 0x1b {
				k, n = protocol.Key{Base: "ESC"}, 1
			}
			if k.Base == "" {
				i += 1 + n
				continue
			}
			k.Mods |= protocol.ModMeta
			out = append(out, k)
			i += 1 + n
		}
	}
	return out, nil
}

func indexOf(b []byte, s string) int { return strings.Index(string(b), s) }

// decodeChar decodes one non-ESC key from the start of b: a C0 control, DEL,
// SPC, or one UTF-8 character. ok is false when b holds an incomplete UTF-8
// sequence and final is not set.
func decodeChar(b []byte, final bool) (protocol.Key, int, bool) {
	c := b[0]
	switch {
	case c == 0x00:
		return protocol.Key{Mods: protocol.ModCtrl, Base: "@"}, 1, true
	case c == 0x09:
		return protocol.Key{Base: "TAB"}, 1, true
	case c == 0x0d:
		return protocol.Key{Base: "RET"}, 1, true
	case c >= 0x01 && c <= 0x1a:
		return protocol.Key{Mods: protocol.ModCtrl, Base: string(rune('a' + c - 1))}, 1, true
	case c == 0x1b:
		return protocol.Key{Base: "ESC"}, 1, true
	case c == 0x1c:
		return protocol.Key{Mods: protocol.ModCtrl, Base: "\\"}, 1, true
	case c == 0x1d:
		return protocol.Key{Mods: protocol.ModCtrl, Base: "]"}, 1, true
	case c == 0x1e:
		return protocol.Key{Mods: protocol.ModCtrl, Base: "^"}, 1, true
	case c == 0x1f:
		return protocol.Key{Mods: protocol.ModCtrl, Base: "_"}, 1, true
	case c == 0x20:
		return protocol.Key{Base: "SPC"}, 1, true
	case c == 0x7f:
		return protocol.Key{Base: "DEL"}, 1, true
	case c < 0x80:
		return protocol.Key{Base: string(rune(c))}, 1, true
	}
	if !utf8.FullRune(b) {
		if !final {
			return protocol.Key{}, 0, false
		}
		return protocol.Key{}, len(b), true
	}
	r, n := utf8.DecodeRune(b)
	if r == utf8.RuneError || !unicode.IsPrint(r) {
		return protocol.Key{}, n, true
	}
	return protocol.Key{Base: string(r)}, n, true
}

func ss3Key(c byte) (protocol.Key, bool) {
	switch c {
	case 'A':
		return fkey("up"), true
	case 'B':
		return fkey("down"), true
	case 'C':
		return fkey("right"), true
	case 'D':
		return fkey("left"), true
	case 'H':
		return fkey("home"), true
	case 'F':
		return fkey("end"), true
	case 'P':
		return fkey("f1"), true
	case 'Q':
		return fkey("f2"), true
	case 'R':
		return fkey("f3"), true
	case 'S':
		return fkey("f4"), true
	case 'M':
		return fkey("kp-enter"), true
	}
	return protocol.Key{}, false
}

func fkey(name string) protocol.Key { return protocol.Key{Base: name, Func: true} }

// parseCSI parses ESC [ … final at the start of b. It returns the keys (none
// for sequences that are not key presses), the bytes consumed, and ok=false
// when the sequence is incomplete.
func (d *TermDecoder) parseCSI(b []byte, final bool) ([]protocol.Key, int, bool) {
	// b[0] = ESC, b[1] = '['.
	i := 2
	// Legacy X10 mouse: ESC [ M Cb Cx Cy.
	if i < len(b) && b[i] == 'M' {
		if len(b) < 6 {
			return nil, 0, false
		}
		return nil, 6, true
	}
	start := i
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x3f {
		i++
	}
	if i >= len(b) {
		return nil, 0, false
	}
	fin := b[i]
	if fin < 0x40 || fin > 0x7e {
		// Malformed: drop the introducer.
		return nil, 2, true
	}
	params := string(b[start:i])
	n := i + 1
	if params != "" && strings.ContainsRune("<=>?", rune(params[0])) {
		// Private sequences on input are terminal replies (CSI ? flags u,
		// device attributes) or SGR mouse reports (CSI < … M/m): not keys.
		return nil, n, true
	}
	ps := strings.Split(params, ";")
	num := func(k int, def int) int {
		if k >= len(ps) {
			return def
		}
		f := ps[k]
		if j := strings.IndexByte(f, ':'); j >= 0 {
			f = f[:j]
		}
		if f == "" {
			return def
		}
		v, err := strconv.Atoi(f)
		if err != nil {
			return def
		}
		return v
	}
	mods := modsFromParam(num(1, 1))
	switch fin {
	case 'u':
		return d.kittyKey(ps), n, true
	case '~':
		code := num(0, 0)
		switch code {
		case 200:
			d.paste = true
			return nil, n, true
		case 201:
			return nil, n, true
		case 27:
			// xterm modifyOtherKeys: CSI 27 ; mods ; code ~
			k, ok := codepointKey(num(2, 0), 0, modsFromParam(num(1, 1)))
			if !ok {
				return nil, n, true
			}
			return []protocol.Key{k}, n, true
		}
		name, ok := tildeKeys[code]
		if !ok {
			return nil, n, true
		}
		return []protocol.Key{withMods(fkey(name), mods)}, n, true
	case 'A', 'B', 'C', 'D', 'H', 'F', 'E':
		name := map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left", 'H': "home", 'F': "end", 'E': "begin"}[fin]
		return []protocol.Key{withMods(fkey(name), mods)}, n, true
	case 'P', 'Q', 'S':
		name := map[byte]string{'P': "f1", 'Q': "f2", 'S': "f4"}[fin]
		return []protocol.Key{withMods(fkey(name), mods)}, n, true
	case 'R':
		// CSI 1 ; m R is F3 with modifiers; CSI row ; col R is a cursor
		// position report.
		if num(0, 1) == 1 && len(ps) <= 2 {
			return []protocol.Key{withMods(fkey("f3"), mods)}, n, true
		}
		return nil, n, true
	case 'Z':
		return []protocol.Key{withMods(fkey("backtab"), mods&^protocol.ModShift)}, n, true
	}
	// Focus events (CSI I, CSI O) and anything else: not keys.
	return nil, n, true
}

var tildeKeys = map[int]string{
	1: "home", 2: "insert", 3: "deletechar", 4: "end", 5: "prior", 6: "next", 7: "home", 8: "end",
	11: "f1", 12: "f2", 13: "f3", 14: "f4", 15: "f5", 17: "f6", 18: "f7", 19: "f8", 20: "f9", 21: "f10",
	23: "f11", 24: "f12", 25: "f13", 26: "f14", 28: "f15", 29: "f16", 31: "f17", 32: "f18", 33: "f19", 34: "f20",
}

// Kitty and xterm modifier bits (value - 1).
const (
	kShift = 1 << iota
	kAlt
	kCtrl
	kSuper
	kHyper
	kMeta
	kCaps
	kNum
)

// modsFromParam converts a CSI modifier parameter (1 + bits) to Emacs
// modifiers. Alt and Meta both become M-.
func modsFromParam(p int) protocol.Modifiers {
	if p < 1 {
		p = 1
	}
	bits := p - 1
	var m protocol.Modifiers
	if bits&kShift != 0 {
		m |= protocol.ModShift
	}
	if bits&(kAlt|kMeta) != 0 {
		m |= protocol.ModMeta
	}
	if bits&kCtrl != 0 {
		m |= protocol.ModCtrl
	}
	if bits&kSuper != 0 {
		m |= protocol.ModSuper
	}
	if bits&kHyper != 0 {
		m |= protocol.ModHyper
	}
	return m
}

func withMods(k protocol.Key, m protocol.Modifiers) protocol.Key {
	k.Mods |= m
	return k
}

// kittyKey decodes CSI code[:shifted[:base]] ; mods[:event] ; text u.
func (d *TermDecoder) kittyKey(ps []string) []protocol.Key {
	codeField := strings.Split(ps[0], ":")
	code, err := strconv.Atoi(codeField[0])
	if err != nil {
		return nil
	}
	shifted := 0
	if len(codeField) > 1 && codeField[1] != "" {
		shifted, _ = strconv.Atoi(codeField[1])
	}
	mp, event := 1, 1
	if len(ps) > 1 {
		mf := strings.Split(ps[1], ":")
		if v, err := strconv.Atoi(mf[0]); err == nil {
			mp = v
		}
		if len(mf) > 1 {
			if v, err := strconv.Atoi(mf[1]); err == nil {
				event = v
			}
		}
	}
	if event == 3 {
		return nil // release
	}
	k, ok := codepointKey(code, shifted, modsFromParam(mp))
	if !ok {
		return nil
	}
	return []protocol.Key{k}
}

// kittyFunctional maps kitty's private-use functional key codes.
var kittyFunctional = func() map[int]string {
	m := map[int]string{
		57358: "", 57359: "", 57360: "", // lock keys
		57361: "print", 57362: "pause", 57363: "menu",
		57409: "kp-decimal", 57410: "kp-divide", 57411: "kp-multiply", 57412: "kp-subtract",
		57413: "kp-add", 57414: "kp-enter", 57415: "kp-equal", 57416: "kp-separator",
		57417: "kp-left", 57418: "kp-right", 57419: "kp-up", 57420: "kp-down",
		57421: "kp-prior", 57422: "kp-next", 57423: "kp-home", 57424: "kp-end",
		57425: "kp-insert", 57426: "kp-delete", 57427: "kp-begin",
	}
	for i := 0; i <= 22; i++ {
		m[57364+i] = "f" + strconv.Itoa(13+i)
	}
	for i := 0; i <= 9; i++ {
		m[57399+i] = "kp-" + strconv.Itoa(i)
	}
	// Media keys and modifier keys (57428..57454) produce nothing.
	for c := 57428; c <= 57454; c++ {
		m[c] = ""
	}
	return m
}()

// codepointKey turns a Unicode code point (or kitty functional code) with
// modifiers into an Emacs key, following Emacs conventions: shift folds into
// printable characters (S-a is A), C-S-<letter> keeps both.
func codepointKey(code, shifted int, mods protocol.Modifiers) (protocol.Key, bool) {
	switch code {
	case 9:
		if mods&protocol.ModShift != 0 {
			return withMods(fkey("backtab"), mods&^protocol.ModShift), true
		}
		return protocol.Key{Mods: mods, Base: "TAB"}, true
	case 13:
		return protocol.Key{Mods: mods, Base: "RET"}, true
	case 27:
		return protocol.Key{Mods: mods, Base: "ESC"}, true
	case 127, 8:
		return protocol.Key{Mods: mods, Base: "DEL"}, true
	case 32:
		return protocol.Key{Mods: mods, Base: "SPC"}, true
	}
	if name, ok := kittyFunctional[code]; ok {
		if name == "" {
			return protocol.Key{}, false
		}
		return withMods(fkey(name), mods), true
	}
	if code < 0x20 || code > unicode.MaxRune || (code >= 0xe000 && code <= 0xf8ff) {
		return protocol.Key{}, false
	}
	r := rune(code)
	if mods&protocol.ModShift != 0 {
		switch {
		case shifted > 0:
			r = rune(shifted)
			mods &^= protocol.ModShift
		case unicode.IsLetter(r) && mods&protocol.ModCtrl == 0:
			r = unicode.ToUpper(r)
			mods &^= protocol.ModShift
		case !unicode.IsLetter(r):
			mods &^= protocol.ModShift
		}
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
