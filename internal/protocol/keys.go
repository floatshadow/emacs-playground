package protocol

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Modifiers is a set of Emacs key modifiers.
type Modifiers uint8

// Emacs modifiers, in the order key-description prints them (A- C- H- M-
// S- s-).
const (
	ModAlt Modifiers = 1 << iota
	ModCtrl
	ModHyper
	ModMeta
	ModShift
	ModSuper
)

var modOrder = []struct {
	m      Modifiers
	prefix byte
}{
	{ModAlt, 'A'}, {ModCtrl, 'C'}, {ModHyper, 'H'}, {ModMeta, 'M'}, {ModShift, 'S'}, {ModSuper, 's'},
}

// NamedKeys are the base key names Emacs prints without angle brackets.
var NamedKeys = map[string]bool{
	"RET": true, "TAB": true, "SPC": true, "ESC": true, "DEL": true, "LFD": true, "NUL": true,
}

// Key is one key event in Emacs notation, e.g. C-x, M-%, <f3>, C-<left>,
// RET, a.
type Key struct {
	Mods Modifiers
	// Base is a single character ("x", "%"), a named key ("RET"), or a
	// function key name without brackets ("f3", "left") when Func is true.
	Base string
	// Func is true for function keys written <name>.
	Func bool
}

// String renders the key in canonical Emacs notation.
func (k Key) String() string {
	var b strings.Builder
	for _, mo := range modOrder {
		if k.Mods&mo.m != 0 {
			b.WriteByte(mo.prefix)
			b.WriteByte('-')
		}
	}
	if k.Func {
		b.WriteString("<" + k.Base + ">")
	} else {
		b.WriteString(k.Base)
	}
	return b.String()
}

// ParseKey parses one key token: optional modifier prefixes followed by a
// single character, a named key (RET TAB SPC ESC DEL LFD NUL), or a
// function key in angle brackets (<f3>, <left>, <deletechar>). F1 to F35
// written bare are accepted as aliases for <f1> to <f35>.
func ParseKey(tok string) (Key, error) {
	if tok == "" {
		return Key{}, fmt.Errorf("empty key")
	}
	var k Key
	rest := tok
	for len(rest) >= 3 && rest[1] == '-' {
		var m Modifiers
		switch rest[0] {
		case 'A':
			m = ModAlt
		case 'C':
			m = ModCtrl
		case 'H':
			m = ModHyper
		case 'M':
			m = ModMeta
		case 'S':
			m = ModShift
		case 's':
			m = ModSuper
		default:
			return Key{}, fmt.Errorf("key %q: unknown modifier %q", tok, rest[:2])
		}
		if k.Mods&m != 0 {
			return Key{}, fmt.Errorf("key %q: repeated modifier %q", tok, rest[:2])
		}
		k.Mods |= m
		rest = rest[2:]
	}
	switch {
	case len(rest) > 2 && rest[0] == '<' && rest[len(rest)-1] == '>':
		name := rest[1 : len(rest)-1]
		for _, r := range name {
			if !(r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
				return Key{}, fmt.Errorf("key %q: bad function key name %q", tok, name)
			}
		}
		k.Base, k.Func = name, true
	case NamedKeys[rest]:
		k.Base = rest
	case isFunctionAlias(rest):
		k.Base, k.Func = "f"+rest[1:], true
	default:
		r, n := utf8.DecodeRuneInString(rest)
		if n != len(rest) || r == utf8.RuneError || unicode.IsSpace(r) {
			return Key{}, fmt.Errorf("key %q: %q is not a single character, a named key or <function-key>", tok, rest)
		}
		k.Base = rest
	}
	return k, nil
}

func isFunctionAlias(s string) bool {
	if len(s) < 2 || len(s) > 3 || s[0] != 'F' {
		return false
	}
	n := 0
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n >= 1 && n <= 35
}

// ParseKeys parses a whitespace-separated key sequence ("C-x C-f",
// "C-x 4 f", "M-g M-g").
func ParseKeys(seq string) ([]Key, error) {
	fields := strings.Fields(seq)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty key sequence")
	}
	keys := make([]Key, 0, len(fields))
	for _, f := range fields {
		k, err := ParseKey(f)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// FormatKeys renders keys in canonical notation separated by spaces.
func FormatKeys(keys []Key) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k.String()
	}
	return strings.Join(parts, " ")
}

// NormalizeKeys parses and re-renders a key sequence canonically
// ("M-C-x" becomes "C-M-x", "F3" becomes "<f3>").
func NormalizeKeys(seq string) (string, error) {
	keys, err := ParseKeys(seq)
	if err != nil {
		return "", err
	}
	return FormatKeys(keys), nil
}
