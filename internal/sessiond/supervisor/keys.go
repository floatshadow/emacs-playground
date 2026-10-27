package supervisor

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// TmuxKeys converts an Emacs key sequence to tmux send-keys arguments.
func TmuxKeys(seq string) ([]string, error) {
	keys, err := protocol.ParseKeys(seq)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		s, err := tmuxKey(k)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

var tmuxNamed = map[string]string{
	"RET": "Enter", "TAB": "Tab", "SPC": "Space", "ESC": "Escape", "DEL": "BSpace",
}

var tmuxFunc = map[string]string{
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End", "prior": "PPage", "next": "NPage",
	"insert": "IC", "delete": "DC", "deletechar": "DC", "backtab": "BTab",
	"kp-0": "KP0", "kp-1": "KP1", "kp-2": "KP2", "kp-3": "KP3", "kp-4": "KP4",
	"kp-5": "KP5", "kp-6": "KP6", "kp-7": "KP7", "kp-8": "KP8", "kp-9": "KP9",
	"kp-divide": "KP/", "kp-multiply": "KP*", "kp-subtract": "KP-", "kp-add": "KP+",
	"kp-decimal": "KP.", "kp-enter": "KPEnter",
}

func tmuxKey(k protocol.Key) (string, error) {
	var b strings.Builder
	if k.Mods&protocol.ModCtrl != 0 {
		b.WriteString("C-")
	}
	if k.Mods&(protocol.ModMeta|protocol.ModAlt) != 0 {
		b.WriteString("M-")
	}
	if k.Mods&protocol.ModShift != 0 {
		b.WriteString("S-")
	}
	if k.Mods&(protocol.ModSuper|protocol.ModHyper) != 0 {
		return "", fmt.Errorf("key %s: super and hyper cannot be sent to a terminal", k)
	}
	switch {
	case k.Func:
		name, ok := tmuxFunc[k.Base]
		if !ok && len(k.Base) >= 2 && k.Base[0] == 'f' {
			var n int
			if _, err := fmt.Sscanf(k.Base, "f%d", &n); err == nil && n >= 1 && n <= 12 {
				name, ok = fmt.Sprintf("F%d", n), true
			}
		}
		if !ok {
			return "", fmt.Errorf("key %s cannot be sent to a terminal", k)
		}
		b.WriteString(name)
	case tmuxNamed[k.Base] != "":
		b.WriteString(tmuxNamed[k.Base])
	case k.Base == "LFD":
		if b.Len() > 0 {
			return "", fmt.Errorf("key %s cannot be sent to a terminal", k)
		}
		b.WriteString("C-j")
	case k.Base == "NUL":
		b.WriteString("C-Space")
	default:
		b.WriteString(k.Base)
	}
	s := b.String()
	// A trailing ";" separates tmux commands; escape it.
	if strings.HasSuffix(s, ";") {
		s = s[:len(s)-1] + `\;`
	}
	return s, nil
}

// XdotoolKeys converts an Emacs key sequence to xdotool key arguments.
func XdotoolKeys(seq string) ([]string, error) {
	keys, err := protocol.ParseKeys(seq)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		s, err := xdotoolKey(k)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

var xNamed = map[string]string{
	"RET": "Return", "TAB": "Tab", "SPC": "space", "ESC": "Escape", "DEL": "BackSpace", "LFD": "Linefeed",
}

var xFunc = map[string]string{
	"up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End", "prior": "Prior", "next": "Next",
	"insert": "Insert", "delete": "Delete", "deletechar": "Delete", "backtab": "ISO_Left_Tab",
	"menu": "Menu", "pause": "Pause", "print": "Print", "kp-enter": "KP_Enter",
}

var xPunct = map[rune]string{
	' ': "space", '!': "exclam", '"': "quotedbl", '#': "numbersign", '$': "dollar", '%': "percent",
	'&': "ampersand", '\'': "apostrophe", '(': "parenleft", ')': "parenright", '*': "asterisk",
	'+': "plus", ',': "comma", '-': "minus", '.': "period", '/': "slash", ':': "colon",
	';': "semicolon", '<': "less", '=': "equal", '>': "greater", '?': "question", '@': "at",
	'[': "bracketleft", '\\': "backslash", ']': "bracketright", '^': "asciicircum",
	'_': "underscore", '`': "grave", '{': "braceleft", '|': "bar", '}': "braceright", '~': "asciitilde",
}

func xdotoolKey(k protocol.Key) (string, error) {
	var parts []string
	if k.Mods&protocol.ModCtrl != 0 {
		parts = append(parts, "ctrl")
	}
	if k.Mods&(protocol.ModMeta|protocol.ModAlt) != 0 {
		parts = append(parts, "alt")
	}
	if k.Mods&protocol.ModShift != 0 {
		parts = append(parts, "shift")
	}
	if k.Mods&protocol.ModSuper != 0 {
		parts = append(parts, "super")
	}
	if k.Mods&protocol.ModHyper != 0 {
		parts = append(parts, "hyper")
	}
	var name string
	switch {
	case k.Func:
		if n, ok := xFunc[k.Base]; ok {
			name = n
		} else if len(k.Base) >= 2 && k.Base[0] == 'f' {
			name = "F" + k.Base[1:]
		} else if strings.HasPrefix(k.Base, "kp-") {
			name = "KP_" + k.Base[3:]
		} else {
			return "", fmt.Errorf("key %s cannot be sent to the GUI frame", k)
		}
	case xNamed[k.Base] != "":
		name = xNamed[k.Base]
	default:
		r, _ := utf8.DecodeRuneInString(k.Base)
		switch {
		case xPunct[r] != "":
			name = xPunct[r]
		case r < 0x80:
			name = k.Base
		default:
			name = fmt.Sprintf("U%04X", r)
		}
	}
	parts = append(parts, name)
	return strings.Join(parts, "+"), nil
}
