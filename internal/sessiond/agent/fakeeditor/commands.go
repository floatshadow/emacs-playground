package fakeeditor

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// defaultBindings is the global keymap (a subset of Emacs's, as in
// web/dev/mock-emacs.ts).
func defaultBindings() map[string]string {
	return map[string]string{
		"C-x C-f": "find-file", "C-x C-s": "save-buffer", "C-x d": "dired", "C-x b": "switch-to-buffer",
		"C-x 0": "delete-window", "C-x 1": "delete-other-windows", "C-x 2": "split-window-below",
		"C-x 3": "split-window-right", "C-x o": "other-window",
		"C-x t 2": "tab-new", "C-x t r": "tab-rename", "C-x t o": "tab-next", "C-x t 0": "tab-close",
		"C-x u": "undo", "C-/": "undo", "C-_": "undo",
		"C-x h": "mark-whole-buffer", "C-SPC": "set-mark-command", "C-@": "set-mark-command",
		"C-x n n": "narrow-to-region", "C-x n w": "widen", "<insert>": "overwrite-mode",
		"C-h k": "describe-key", "C-h f": "describe-function",
		"C-g": "keyboard-quit", "C-s": "isearch-forward",
		"C-a": "move-beginning-of-line", "C-e": "move-end-of-line", "C-f": "forward-char", "C-b": "backward-char",
		"C-n": "next-line", "C-p": "previous-line", "<left>": "left-char", "<right>": "right-char",
		"<up>": "previous-line", "<down>": "next-line", "<home>": "move-beginning-of-line", "<end>": "move-end-of-line",
		"M-<": "beginning-of-buffer", "M->": "end-of-buffer",
		"C-k": "kill-line", "C-y": "yank", "C-d": "delete-char", "DEL": "delete-backward-char",
		"RET": "newline", "TAB": "indent-for-tab-command",
	}
}

// modeBindings are the local keymaps that take precedence in a mode.
var modeBindings = map[string]map[string]string{
	modeDired: {"RET": "dired-find-file", "q": "quit-window", "n": "next-line", "p": "previous-line", "g": "revert-buffer"},
	modeHelp:  {"q": "quit-window"},
}

// commandDocs are the first doc lines describe reports.
var commandDocs = map[string]string{
	"find-file":            "Edit file FILENAME.",
	"save-buffer":          "Save current buffer in visited file if modified.",
	"split-window-below":   "Split WINDOW into two windows, one above the other.",
	"split-window-right":   "Split WINDOW into two side-by-side windows.",
	"other-window":         "Select another window in cyclic ordering of windows.",
	"delete-other-windows": "Make WINDOW fill its frame.",
	"kill-line":            "Kill the rest of the current line; if no nonblanks there, kill thru newline.",
	"yank":                 "Reinsert (\"paste\") the last stretch of killed text.",
	"undo":                 "Undo some previous changes.",
	"dired":                "\"Edit\" directory DIRNAME--delete, rename, print, etc. some files in it.",
	"keyboard-quit":        "Signal a ‘quit’ condition.",
}

// prompt is an open minibuffer and what accepting it does.
type prompt struct {
	text    string
	exitCmd string
	done    func(e *Editor, input string)
}

// keyReader collects a key sequence for a command reading one (C-h k).
type keyReader struct {
	keys []protocol.Key
	done func(e *Editor, seq string)
}

// isearchState is a running incremental search.
type isearchState struct {
	start  int
	search string
}

// Press runs the keys (Emacs notation, "C-x C-f") as typed: each complete
// key sequence runs its command, which reports a key event and the state.
func (e *Editor) Press(keys string) error {
	ks, err := protocol.ParseKeys(keys)
	if err != nil {
		return err
	}
	e.mu.Lock()
	for _, k := range ks {
		e.key(k)
	}
	e.flush()
	return nil
}

// Type types text literally (tmux send-keys -l): one self-inserting key per
// character.
func (e *Editor) Type(text string) {
	e.mu.Lock()
	for _, r := range text {
		var k protocol.Key
		switch r {
		case ' ':
			k = protocol.Key{Base: "SPC"}
		case '\t':
			k = protocol.Key{Base: "TAB"}
		case '\n', '\r':
			k = protocol.Key{Base: "RET"}
		default:
			k = protocol.Key{Base: string(r)}
		}
		e.key(k)
	}
	e.flush()
}

// printable returns the character a key inserts, if it self-inserts.
func printable(k protocol.Key) (string, bool) {
	if k.Mods != 0 || k.Func {
		return "", false
	}
	if k.Base == "SPC" {
		return " ", true
	}
	if utf8.RuneCountInString(k.Base) == 1 {
		return k.Base, true
	}
	return "", false
}

// key handles one key.
func (e *Editor) key(k protocol.Key) {
	switch {
	case e.reading != nil:
		e.readKey(k)
		return
	case e.isearch != nil:
		if e.isearchKey(k) {
			return
		}
	case e.prompt != nil:
		e.promptKey(k)
		return
	}
	e.pending = append(e.pending, k)
	seq := keyString(e.pending)
	if cmd, ok := e.binding(seq); ok {
		e.pending = nil
		e.run(cmd, seq)
		return
	}
	if e.isPrefix(seq) {
		return
	}
	e.pending = nil
	if len(seq) > 0 && strings.Contains(seq, " ") && k.String() == "C-g" {
		e.done(seq, protocol.UndefinedCommand, nil)
		return
	}
	if ch, ok := printable(k); ok && !strings.Contains(seq, " ") {
		e.selfInsert(k.String(), ch)
		return
	}
	e.done(seq, protocol.UndefinedCommand, nil)
}

func keyString(ks []protocol.Key) string {
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = k.String()
	}
	return strings.Join(parts, " ")
}

// binding looks seq up in the current buffer's mode map, then globally.
func (e *Editor) binding(seq string) (string, bool) {
	if m := modeBindings[e.current().Mode]; m != nil {
		if c, ok := m[seq]; ok {
			return c, true
		}
	}
	c, ok := e.Bindings[seq]
	return c, ok
}

func (e *Editor) isPrefix(seq string) bool {
	for k := range e.Bindings {
		if strings.HasPrefix(k, seq+" ") {
			return true
		}
	}
	return false
}

// run runs command cmd invoked with keys.
func (e *Editor) run(cmd, keys string) {
	if c := e.Commands[cmd]; c != nil {
		c(e, keys)
		return
	}
	e.done(keys, cmd, nil)
}

// done finishes a command the way post-command-hook does: the key event,
// then the state.
func (e *Editor) done(keys, cmd string, buf *Buffer) {
	e.keyEvent(keys, cmd, buf)
	e.finish(cmd)
}

// finish ends a command whose key event already went out.
func (e *Editor) finish(cmd string) {
	if cmd != "next-line" && cmd != "previous-line" {
		e.goal = -1
	}
	if cmd == "self-insert-command" {
		e.selfRun++
	} else {
		e.selfRun = 0
	}
	e.last = cmd
	e.stateLocked()
}

// --- the minibuffer ------------------------------------------------------------

// openPrompt opens a minibuffer for the command cmd typed as keys: the
// command is reported when its prompt opens (as the agent does), then the
// minibuffer event and the state.
func (e *Editor) openPrompt(keys, cmd, text, initial, exitCmd string, done func(*Editor, string)) {
	e.keyEvent(keys, cmd, nil)
	e.prompt = &prompt{text: text, exitCmd: exitCmd, done: done}
	e.m.Minibuffer = &protocol.Minibuffer{Depth: 1, Prompt: text, Contents: initial, Kind: protocol.MinibufferPrompt}
	e.emit(&protocol.MinibufferEvent{Envelope: protocol.Envelope{T: protocol.EventMinibuffer}, Open: true, Prompt: text, Depth: 1, Kind: protocol.MinibufferPrompt})
	e.last = cmd
	e.stateLocked()
}

// closePrompt closes the minibuffer, reporting the command that left it.
func (e *Editor) closePrompt(keys, cmd string) *prompt {
	p := e.prompt
	e.keyEvent(keys, cmd, minibufferBuffer)
	e.emit(&protocol.MinibufferEvent{Envelope: protocol.Envelope{T: protocol.EventMinibuffer}, Open: false, Prompt: p.text, Depth: 1, Kind: protocol.MinibufferPrompt})
	e.prompt, e.m.Minibuffer = nil, nil
	return p
}

func (e *Editor) promptKey(k protocol.Key) {
	mb := e.m.Minibuffer
	switch ks := k.String(); ks {
	case "RET":
		input := mb.Contents
		p := e.closePrompt(ks, e.prompt.exitCmd)
		p.done(e, input)
		e.finish(e.last)
	case "C-g":
		e.closePrompt(ks, "abort-minibuffers")
		e.finish("abort-minibuffers")
	case "DEL":
		if r := []rune(mb.Contents); len(r) > 0 {
			mb.Contents = string(r[:len(r)-1])
		}
		e.done(ks, "delete-backward-char", minibufferBuffer)
	case "TAB":
		e.done(ks, "minibuffer-complete", minibufferBuffer)
	default:
		if ch, ok := printable(k); ok {
			mb.Contents += ch
			e.done(ks, "self-insert-command", minibufferBuffer)
			return
		}
		e.done(ks, protocol.UndefinedCommand, minibufferBuffer)
	}
}

// dirInput is a directory as the minibuffer shows it (absolute, with a slash).
func (e *Editor) dirInput(rel string) string {
	return strings.TrimSuffix(e.abs(rel), "/") + "/"
}

// resolve turns minibuffer input into a workspace-relative path.
func (e *Editor) resolve(input, dir string) (string, bool) {
	if !strings.HasPrefix(input, "/") {
		input = path.Join(e.abs(dir), input)
	}
	return e.rel(input)
}

// --- key-reading commands --------------------------------------------------------

func (e *Editor) readKey(k protocol.Key) {
	r := e.reading
	r.keys = append(r.keys, k)
	seq := keyString(r.keys)
	if _, ok := e.binding(seq); !ok && e.isPrefix(seq) {
		return
	}
	e.reading = nil
	r.done(e, seq)
}

// --- isearch -----------------------------------------------------------------------

// isearchKey handles k during a search; false when the search ended and k
// still has to run.
func (e *Editor) isearchKey(k protocol.Key) bool {
	is := e.isearch
	ks := k.String()
	closeSearch := func() {
		e.emit(&protocol.MinibufferEvent{Envelope: protocol.Envelope{T: protocol.EventMinibuffer}, Open: false, Prompt: "I-search: ", Kind: protocol.MinibufferIsearch})
		e.isearch, e.m.Minibuffer = nil, nil
	}
	switch {
	case ks == "RET":
		closeSearch()
		e.done(ks, "isearch-exit", nil)
		return true
	case ks == "C-g":
		closeSearch()
		e.setPoint(is.start)
		e.done(ks, "isearch-abort", nil)
		return true
	case ks == "C-s":
		e.isearchMove(true)
		e.done(ks, "isearch-repeat-forward", nil)
		return true
	}
	if ch, ok := printable(k); ok {
		is.search += ch
		e.m.Minibuffer.Contents = is.search
		e.isearchMove(false)
		e.done(ks, "isearch-printing-char", nil)
		return true
	}
	closeSearch()
	return false
}

func (e *Editor) isearchMove(next bool) {
	b := e.current()
	from := e.isearch.start
	if next {
		from = b.Point
	}
	r := []rune(b.Text)
	i := strings.Index(string(r[min(from-1, len(r)):]), e.isearch.search)
	if i >= 0 {
		e.setPoint(from + utf8.RuneCountInString(string(r[min(from-1, len(r)):])[:i]) + utf8.RuneCountInString(e.isearch.search))
	}
}

// --- editing ---------------------------------------------------------------------------

// change replaces the current buffer's text, recording undo; amalgamate
// joins runs of self-insertion into one undo step, as Emacs does.
func (e *Editor) change(b *Buffer, text string, point int, amalgamate bool) {
	if !(amalgamate && e.last == "self-insert-command" && e.selfRun < 20 && e.selfRun > 0) {
		b.Undo = append(b.Undo, Edit{Text: b.Text, Point: b.Point})
	}
	b.Text = text
	if b.Path != "" {
		b.Modified = b.Text != b.Saved
	} else {
		b.Modified = true
	}
	e.setPoint(point)
}

func (e *Editor) selfInsert(keys, ch string) {
	b := e.current()
	if b.ReadOnly {
		e.done(keys, "self-insert-command", nil)
		return
	}
	text := b.Text
	if b.Overwrite {
		if _, end := lineBounds(text, b.Point); b.Point < end {
			text, _ = deleteRange(text, b.Point, b.Point+1)
		}
	}
	e.change(b, insertAt(text, b.Point, ch), b.Point+runeLen(ch), true)
	e.done(keys, "self-insert-command", nil)
}

func (e *Editor) killRing() []string {
	kr, _ := e.m.Globals["kill-ring"].([]string)
	return kr
}

// --- commands ----------------------------------------------------------------------------

func defaultCommands() map[string]Command {
	cmds := map[string]Command{
		"find-file": func(e *Editor, keys string) {
			dir := e.current().Dir
			e.openPrompt(keys, "find-file", "Find file: ", e.dirInput(dir), "minibuffer-complete-and-exit",
				func(e *Editor, input string) {
					rel, ok := e.resolve(input, dir)
					if !ok {
						return
					}
					if e.isDir(rel) {
						e.switchTo(e.dired(rel))
						return
					}
					e.switchTo(e.visit(rel))
				})
		},
		"dired": func(e *Editor, keys string) {
			dir := e.current().Dir
			e.openPrompt(keys, "dired", "Dired (directory): ", e.dirInput(dir), "minibuffer-completion-exit",
				func(e *Editor, input string) {
					if rel, ok := e.resolve(input, dir); ok && e.isDir(rel) {
						e.switchTo(e.dired(rel))
					}
				})
		},
		"switch-to-buffer": func(e *Editor, keys string) {
			e.openPrompt(keys, "switch-to-buffer", "Switch to buffer: ", "", "minibuffer-complete-and-exit",
				func(e *Editor, input string) {
					if input == "" {
						input = e.otherBuffer(e.current().Name).Name
					}
					b := e.buffer(input)
					if b == nil {
						b = e.addBuffer(&Buffer{Name: input, Mode: modeFundamental, Dir: e.current().Dir})
					}
					e.switchTo(b)
				})
		},
		"describe-function": func(e *Editor, keys string) {
			e.openPrompt(keys, "describe-function", "Describe function: ", "", "minibuffer-complete-and-exit",
				func(e *Editor, input string) {
					e.help("(describe-function--helper "+input+" nil)", input+" is an interactive function.\n\n"+commandDocs[input]+"\n")
				})
		},
		"describe-key": func(e *Editor, keys string) {
			e.reading = &keyReader{done: func(e *Editor, seq string) {
				cmd, _ := e.binding(seq)
				if cmd == "" {
					cmd = "undefined"
				}
				e.help("(describe-key (("+fmt.Sprintf("%q", seq)+" . "+fmt.Sprintf("%q", seq)+")))", seq+" runs the command "+cmd+".\n")
				e.done(keys, "describe-key", nil)
			}}
		},
		"tab-rename": func(e *Editor, keys string) {
			e.openPrompt(keys, "tab-rename", "New name for tab (leave blank for automatic naming): ", "", "exit-minibuffer",
				func(e *Editor, input string) { e.currentTab().Name = input })
		},
		"save-buffer": func(e *Editor, keys string) {
			if b := e.current(); b.Path != "" && b.Modified {
				if err := e.writeFile(b.Path, b.Text); err == nil {
					b.Saved, b.Modified = b.Text, false
				}
			}
			e.done(keys, "save-buffer", nil)
		},
		"split-window-below": func(e *Editor, keys string) { e.split(protocol.VSplit); e.done(keys, "split-window-below", nil) },
		"split-window-right": func(e *Editor, keys string) { e.split(protocol.HSplit); e.done(keys, "split-window-right", nil) },
		"other-window": func(e *Editor, keys string) {
			ids := e.m.Layout.Leaves()
			i := indexOf(ids, e.selected().ID)
			e.selectWindow(ids[(i+1)%len(ids)])
			e.done(keys, "other-window", nil)
		},
		"delete-other-windows": func(e *Editor, keys string) { e.onlyWindow(); e.done(keys, "delete-other-windows", nil) },
		"delete-window": func(e *Editor, keys string) {
			e.deleteWindow(e.selected().ID)
			e.done(keys, "delete-window", nil)
		},
		"tab-new": func(e *Editor, keys string) { e.newTab(); e.done(keys, "tab-new", nil) },
		"tab-next": func(e *Editor, keys string) {
			e.selectTab(e.m.TabIndex%len(e.m.Tabs) + 1)
			e.done(keys, "tab-next", nil)
		},
		"tab-close": func(e *Editor, keys string) { e.closeTab(e.m.TabIndex); e.done(keys, "tab-close", nil) },
		"undo": func(e *Editor, keys string) {
			b := e.current()
			if n := len(b.Undo); n > 0 && !b.ReadOnly {
				u := b.Undo[n-1]
				b.Undo = b.Undo[:n-1]
				b.Text = u.Text
				if b.Path != "" {
					b.Modified = b.Text != b.Saved
				}
				e.setPoint(u.Point)
			}
			e.done(keys, "undo", nil)
		},
		"mark-whole-buffer": func(e *Editor, keys string) {
			b := e.current()
			b.Mark = runeLen(b.Text) + 1
			e.setPoint(1)
			e.done(keys, "mark-whole-buffer", nil)
		},
		"set-mark-command": func(e *Editor, keys string) {
			e.current().Mark = e.current().Point
			e.done(keys, "set-mark-command", nil)
		},
		"narrow-to-region": func(e *Editor, keys string) {
			b := e.current()
			if b.Mark > 0 {
				lo, hi := min(b.Mark, b.Point), max(b.Mark, b.Point)
				b.Narrowed = lo > 1 || hi < runeLen(b.Text)+1
			}
			e.done(keys, "narrow-to-region", nil)
		},
		"widen": func(e *Editor, keys string) { e.current().Narrowed = false; e.done(keys, "widen", nil) },
		"overwrite-mode": func(e *Editor, keys string) {
			b := e.current()
			b.Overwrite = !b.Overwrite
			e.done(keys, "overwrite-mode", nil)
		},
		"keyboard-quit": func(e *Editor, keys string) { e.done(keys, "keyboard-quit", nil) },
		"isearch-forward": func(e *Editor, keys string) {
			e.isearch = &isearchState{start: e.current().Point}
			e.m.Minibuffer = &protocol.Minibuffer{Prompt: "I-search: ", Kind: protocol.MinibufferIsearch}
			e.emit(&protocol.MinibufferEvent{Envelope: protocol.Envelope{T: protocol.EventMinibuffer}, Open: true, Prompt: "I-search: ", Kind: protocol.MinibufferIsearch})
			e.done(keys, "isearch-forward", nil)
		},
		"move-beginning-of-line": func(e *Editor, keys string) {
			start, _ := lineBounds(e.current().Text, e.current().Point)
			e.setPoint(start)
			e.done(keys, "move-beginning-of-line", nil)
		},
		"move-end-of-line": func(e *Editor, keys string) {
			_, end := lineBounds(e.current().Text, e.current().Point)
			e.setPoint(end)
			e.done(keys, "move-end-of-line", nil)
		},
		"forward-char":           func(e *Editor, keys string) { e.setPoint(e.current().Point + 1); e.done(keys, "forward-char", nil) },
		"backward-char":          func(e *Editor, keys string) { e.setPoint(e.current().Point - 1); e.done(keys, "backward-char", nil) },
		"right-char":             func(e *Editor, keys string) { e.setPoint(e.current().Point + 1); e.done(keys, "right-char", nil) },
		"left-char":              func(e *Editor, keys string) { e.setPoint(e.current().Point - 1); e.done(keys, "left-char", nil) },
		"beginning-of-buffer":    func(e *Editor, keys string) { e.setPoint(1); e.done(keys, "beginning-of-buffer", nil) },
		"end-of-buffer":          func(e *Editor, keys string) { e.setPoint(1 << 30); e.done(keys, "end-of-buffer", nil) },
		"next-line":              func(e *Editor, keys string) { e.moveLine(1); e.done(keys, "next-line", nil) },
		"previous-line":          func(e *Editor, keys string) { e.moveLine(-1); e.done(keys, "previous-line", nil) },
		"indent-for-tab-command": func(e *Editor, keys string) { e.done(keys, "indent-for-tab-command", nil) },
		"newline": func(e *Editor, keys string) {
			if b := e.current(); !b.ReadOnly {
				e.change(b, insertAt(b.Text, b.Point, "\n"), b.Point+1, false)
			}
			e.done(keys, "newline", nil)
		},
		"delete-backward-char": func(e *Editor, keys string) {
			if b := e.current(); !b.ReadOnly && b.Point > 1 {
				text, _ := deleteRange(b.Text, b.Point-1, b.Point)
				e.change(b, text, b.Point-1, false)
			}
			e.done(keys, "delete-backward-char", nil)
		},
		"delete-char": func(e *Editor, keys string) {
			if b := e.current(); !b.ReadOnly && b.Point <= runeLen(b.Text) {
				text, _ := deleteRange(b.Text, b.Point, b.Point+1)
				e.change(b, text, b.Point, false)
			}
			e.done(keys, "delete-char", nil)
		},
		"kill-line": func(e *Editor, keys string) {
			b := e.current()
			_, end := lineBounds(b.Text, b.Point)
			if end == b.Point && end <= runeLen(b.Text) {
				end++
			}
			if !b.ReadOnly && end > b.Point {
				text, killed := deleteRange(b.Text, b.Point, end)
				kr := e.killRing()
				if e.last == "kill-line" && len(kr) > 0 {
					kr[0] += killed
				} else {
					kr = append([]string{killed}, kr...)
				}
				e.m.Globals["kill-ring"] = kr
				e.change(b, text, b.Point, false)
			}
			e.done(keys, "kill-line", nil)
		},
		"yank": func(e *Editor, keys string) {
			b := e.current()
			if kr := e.killRing(); len(kr) > 0 && !b.ReadOnly {
				e.change(b, insertAt(b.Text, b.Point, kr[0]), b.Point+runeLen(kr[0]), false)
			}
			e.done(keys, "yank", nil)
		},
		"quit-window": func(e *Editor, keys string) {
			b := e.current()
			if len(e.m.Windows) > 1 && b.Mode == modeHelp {
				e.deleteWindow(e.selected().ID)
			} else {
				e.switchTo(e.otherBuffer(b.Name))
			}
			e.done(keys, "quit-window", nil)
		},
		"dired-find-file": func(e *Editor, keys string) {
			b := e.current()
			if name := direntAt(b); name != "" && name != "." {
				rel := path.Join(b.Dir, name)
				if name == ".." {
					rel = path.Dir(b.Dir)
				}
				if rel == "." {
					rel = ""
				}
				if e.isDir(rel) {
					e.switchTo(e.dired(rel))
				} else {
					e.switchTo(e.visit(rel))
				}
			}
			e.done(keys, "dired-find-file", nil)
		},
		"revert-buffer": func(e *Editor, keys string) {
			if b := e.current(); b.Mode == modeDired {
				b.Text = e.listing(b.Dir)
				b.Point = clampPoint(b.Text, b.Point)
			}
			e.done(keys, "revert-buffer", nil)
		},
	}
	return cmds
}

// moveLine moves point n lines, keeping the goal column across
// consecutive line moves; at the first or last line it does not move
// (Emacs signals beginning- or end-of-buffer).
func (e *Editor) moveLine(n int) {
	b := e.current()
	line, col := lineCol(b.Text, b.Point)
	if e.last == "next-line" || e.last == "previous-line" {
		if e.goal >= 0 {
			col = e.goal
		}
	}
	e.goal = col
	lines := strings.Split(b.Text, "\n")
	target := line + n
	if target < 1 || target > len(lines) {
		return
	}
	p := 1
	for i := 0; i < target-1; i++ {
		p += runeLen(lines[i]) + 1
	}
	e.setPoint(p + min(col, runeLen(lines[target-1])))
}

// help shows a *Help* buffer made by item (display-buffer, not selected).
func (e *Editor) help(item, text string) {
	b := e.buffer("*Help*")
	if b == nil {
		b = e.addBuffer(&Buffer{Name: "*Help*", Mode: modeHelp, ReadOnly: true})
	}
	b.Item, b.Text, b.Point = item, text, 1
	e.displayBuffer(b)
}

// dired returns the Dired buffer of directory rel, made when needed.
func (e *Editor) dired(rel string) *Buffer {
	for _, b := range e.m.Buffers {
		if b.Mode == modeDired && b.Dir == rel {
			return b
		}
	}
	name := path.Base(rel)
	if rel == "" {
		name = path.Base(e.Workspace)
	}
	if e.buffer(name) != nil {
		name += "<" + path.Base(path.Dir(e.abs(rel))) + ">"
	}
	text := e.listing(rel)
	b := e.addBuffer(&Buffer{Name: name, Dir: rel, Mode: modeDired, ReadOnly: true, Text: text})
	// Point on the first entry after . and ..
	lines := strings.SplitAfter(text, "\n")
	p := 1
	for i, l := range lines {
		if i >= 4 {
			break
		}
		p += runeLen(l)
	}
	b.Point = clampPoint(text, p+2)
	return b
}

// listing is a Dired listing: a header line, . and .., then the entries.
func (e *Editor) listing(rel string) string {
	var sb strings.Builder
	sb.WriteString("  " + e.dirInput(rel) + ":\n  total 0\n  .\n  ..\n")
	if r, err := e.openRoot(); err == nil {
		d := rel
		if d == "" {
			d = "."
		}
		if f, err := r.Open(d); err == nil {
			ents, _ := f.ReadDir(-1)
			f.Close()
			names := make([]string, 0, len(ents))
			for _, en := range ents {
				names = append(names, en.Name())
			}
			slices.Sort(names)
			for _, n := range names {
				sb.WriteString("  " + n + "\n")
			}
		}
	}
	return sb.String()
}

// direntAt is the name on point's line of a Dired listing.
func direntAt(b *Buffer) string {
	line, _ := lineCol(b.Text, b.Point)
	lines := strings.Split(b.Text, "\n")
	if line < 3 || line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1])
}
