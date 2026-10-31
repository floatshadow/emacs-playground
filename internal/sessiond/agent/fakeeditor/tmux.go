package fakeeditor

import (
	"context"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/sessiond/supervisor"
)

// FakeTmux is the supervisor's tmux in -fake mode: send-keys reach the
// editor as the keys tmux would type into Emacs, so rpc keys, demos,
// skips and fast-forwards move the model.
type FakeTmux struct {
	supervisor.NoTmux
	Editor *Editor
}

var _ supervisor.Tmux = FakeTmux{}

// tmuxNames maps tmux key names back to Emacs notation
// (supervisor.TmuxKeys in reverse).
var tmuxNames = map[string]string{
	"Enter": "RET", "Tab": "TAB", "Space": "SPC", "Escape": "ESC", "BSpace": "DEL",
	"Up": "<up>", "Down": "<down>", "Left": "<left>", "Right": "<right>",
	"Home": "<home>", "End": "<end>", "PPage": "<prior>", "NPage": "<next>",
	"IC": "<insert>", "DC": "<deletechar>", "BTab": "<backtab>",
	"F1": "<f1>", "F2": "<f2>", "F3": "<f3>", "F4": "<f4>", "F5": "<f5>", "F6": "<f6>",
	"F7": "<f7>", "F8": "<f8>", "F9": "<f9>", "F10": "<f10>", "F11": "<f11>", "F12": "<f12>",
}

// emacsKey converts one tmux key ("C-x", "M-Enter", "C-Space", "\;").
func emacsKey(k string) string {
	mods := ""
	for {
		switch {
		case strings.HasPrefix(k, "C-") && len(k) > 2:
			mods, k = mods+"C-", k[2:]
			continue
		case strings.HasPrefix(k, "M-") && len(k) > 2:
			mods, k = mods+"M-", k[2:]
			continue
		case strings.HasPrefix(k, "S-") && len(k) > 2:
			mods, k = mods+"S-", k[2:]
			continue
		}
		break
	}
	if name, ok := tmuxNames[k]; ok {
		k = name
	}
	if k == `\;` {
		k = ";"
	}
	if strings.HasPrefix(k, "<") && mods != "" {
		return "<" + mods + k[1:]
	}
	return mods + k
}

// SendKeys implements supervisor.Tmux.
func (t FakeTmux) SendKeys(_ context.Context, literal bool, keys ...string) error {
	if literal {
		t.Editor.Type(strings.Join(keys, ""))
		return nil
	}
	ks := make([]string, len(keys))
	for i, k := range keys {
		ks[i] = emacsKey(k)
	}
	return t.Editor.Press(strings.Join(ks, " "))
}
