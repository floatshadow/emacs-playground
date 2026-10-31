package fakeeditor

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// The window tree is a protocol.Layout: leaves are window ids, vsplit
// stacks its children top to bottom (C-x 2), hsplit puts them side by
// side (C-x 3). Like Emacs (window-combination-limit nil), splitting a
// window whose parent is split the same way adds a sibling to the parent.

func leaf(id int) protocol.Layout { return protocol.Layout{Window: id} }

// splitLeaf replaces leaf id of l with a split of kind holding id and
// then newID.
func splitLeaf(l protocol.Layout, id, newID int, kind protocol.SplitKind) protocol.Layout {
	if l.IsLeaf() {
		if l.Window == id {
			return protocol.Layout{Split: kind, Children: []protocol.Layout{leaf(id), leaf(newID)}}
		}
		return l
	}
	out := protocol.Layout{Split: l.Split}
	for _, c := range l.Children {
		if c.IsLeaf() && c.Window == id && l.Split == kind {
			out.Children = append(out.Children, leaf(id), leaf(newID))
			continue
		}
		out.Children = append(out.Children, splitLeaf(c, id, newID, kind))
	}
	return out
}

// removeLeaf drops leaf id from l; a split left with one child becomes
// that child, and a child split like its parent merges into it.
func removeLeaf(l protocol.Layout, id int) (protocol.Layout, bool) {
	if l.IsLeaf() {
		return l, l.Window != id
	}
	var kids []protocol.Layout
	for _, c := range l.Children {
		if nc, ok := removeLeaf(c, id); ok {
			if !nc.IsLeaf() && nc.Split == l.Split {
				kids = append(kids, nc.Children...)
			} else {
				kids = append(kids, nc)
			}
		}
	}
	switch len(kids) {
	case 0:
		return protocol.Layout{}, false
	case 1:
		return kids[0], true
	}
	return protocol.Layout{Split: l.Split, Children: kids}, true
}

// layoutString renders l as a layout pattern ("a.txt", "(vsplit a.txt
// *Help*)") naming each window by name(id), as playground--layout-string.
func layoutString(l protocol.Layout, name func(id int) string) string {
	if l.IsLeaf() {
		return name(l.Window)
	}
	parts := make([]string, len(l.Children))
	for i, c := range l.Children {
		parts[i] = layoutString(c, name)
	}
	return "(" + string(l.Split) + " " + strings.Join(parts, " ") + ")"
}

// leafName quotes a layout leaf as playground--leaf-name does.
func leafName(name string) string {
	if strings.ContainsAny(name, ` ()"`) {
		return strconv.Quote(name)
	}
	return name
}

// pattern is a parsed layout pattern: a leaf name or a split.
type pattern struct {
	split    protocol.SplitKind
	name     string
	children []pattern
}

// parsePattern reads a layout pattern ("single", "a.txt", "(vsplit a
// (hsplit b \"c d\"))").
func parsePattern(s string) (pattern, error) {
	toks, err := lexPattern(s)
	if err != nil {
		return pattern{}, err
	}
	p, rest, err := parsePatternToks(toks)
	if err != nil {
		return pattern{}, err
	}
	if len(rest) > 0 {
		return pattern{}, fmt.Errorf("layout %q: trailing %v", s, rest)
	}
	return p, nil
}

func lexPattern(s string) ([]string, error) {
	var toks []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			i++
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("layout %q: unterminated string", s)
			}
			u, err := strconv.Unquote(s[i : j+1])
			if err != nil {
				return nil, err
			}
			toks = append(toks, "\x00"+u)
			i = j + 1
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t\n()\"", rune(s[j])) {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks, nil
}

func parsePatternToks(toks []string) (pattern, []string, error) {
	if len(toks) == 0 {
		return pattern{}, nil, fmt.Errorf("layout: empty")
	}
	t := toks[0]
	switch {
	case t == "(":
		if len(toks) < 2 || (toks[1] != string(protocol.VSplit) && toks[1] != string(protocol.HSplit)) {
			return pattern{}, nil, fmt.Errorf("layout: bad split %v", toks)
		}
		p := pattern{split: protocol.SplitKind(toks[1])}
		rest := toks[2:]
		for len(rest) > 0 && rest[0] != ")" {
			c, r, err := parsePatternToks(rest)
			if err != nil {
				return pattern{}, nil, err
			}
			p.children = append(p.children, c)
			rest = r
		}
		if len(rest) == 0 {
			return pattern{}, nil, fmt.Errorf("layout: unbalanced")
		}
		return p, rest[1:], nil
	case t == ")":
		return pattern{}, nil, fmt.Errorf("layout: unexpected )")
	case strings.HasPrefix(t, "\x00"):
		return pattern{name: t[1:]}, toks[1:], nil
	}
	return pattern{name: t}, toks[1:], nil
}

// windowState is the fake's printed window state: opaque to sessiond,
// read back by open and reset-step. A state the fake did not print (a
// real Emacs one) falls back to the spec's layout pattern.
type windowState struct {
	Layout   protocol.Layout `json:"layout"`
	Windows  []stateWindow   `json:"windows"`
	Selected int             `json:"selected"`
}

type stateWindow struct {
	ID    int    `json:"id"`
	Buf   string `json:"buf"`
	Path  string `json:"path,omitempty"`
	Point int    `json:"point"`
}

const windowStatePrefix = "(pg-fake-window-state "

func (ws windowState) String() string {
	b, _ := json.Marshal(ws)
	return windowStatePrefix + strconv.Quote(string(b)) + ")"
}

func parseWindowState(s string) (windowState, bool) {
	var ws windowState
	body, ok := strings.CutPrefix(s, windowStatePrefix)
	if !ok || !strings.HasSuffix(body, ")") {
		return ws, false
	}
	u, err := strconv.Unquote(strings.TrimSuffix(body, ")"))
	if err != nil || json.Unmarshal([]byte(u), &ws) != nil || len(ws.Windows) == 0 {
		return ws, false
	}
	return ws, true
}

// help items are printed (NAME ITEM) lists; the fake reads back its own.
func printSpecial(bufs []*Buffer) string {
	var parts []string
	for _, b := range bufs {
		if b.Mode == modeHelp && b.Item != "" && !strings.HasPrefix(b.Name, " ") {
			parts = append(parts, "("+strconv.Quote(b.Name)+" "+b.Item+")")
		}
	}
	if len(parts) == 0 {
		return "nil"
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// parseSpecial reads printSpecial's output into name → item; ok is false
// for anything else.
func parseSpecial(s string) (map[string]string, bool) {
	out := map[string]string{}
	if s == "nil" {
		return out, true
	}
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return nil, false
	}
	rest := strings.TrimSpace(s[1 : len(s)-1])
	for rest != "" {
		if !strings.HasPrefix(rest, "(\"") {
			return nil, false
		}
		end := 2
		for end < len(rest) && rest[end] != '"' {
			if rest[end] == '\\' {
				end++
			}
			end++
		}
		if end >= len(rest) {
			return nil, false
		}
		name, err := strconv.Unquote(rest[1 : end+1])
		if err != nil {
			return nil, false
		}
		depth, i := 0, end+1
		for ; i < len(rest); i++ {
			if rest[i] == '(' {
				depth++
			} else if rest[i] == ')' {
				if depth == 0 {
					break
				}
				depth--
			}
		}
		if i >= len(rest) {
			return nil, false
		}
		out[name] = strings.TrimSpace(rest[end+1 : i])
		rest = strings.TrimSpace(rest[i+1:])
	}
	return out, true
}

// --- text positions (1-based, in runes) ---------------------------------

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// clampPoint keeps p inside text.
func clampPoint(text string, p int) int {
	return max(1, min(p, runeLen(text)+1))
}

// lineCol returns the 1-based line and 0-based column of point p.
func lineCol(text string, p int) (int, int) {
	r := []rune(text)
	line, col := 1, 0
	for i := 0; i < p-1 && i < len(r); i++ {
		if r[i] == '\n' {
			line++
			col = 0
		} else {
			col++
		}
	}
	return line, col
}

// lineBounds returns the points of the start and end (before the newline)
// of the line holding p.
func lineBounds(text string, p int) (int, int) {
	r := []rune(text)
	i := min(p-1, len(r))
	start := i
	for start > 0 && r[start-1] != '\n' {
		start--
	}
	end := i
	for end < len(r) && r[end] != '\n' {
		end++
	}
	return start + 1, end + 1
}

// insertAt inserts s at point p.
func insertAt(text string, p int, s string) string {
	r := []rune(text)
	i := min(max(p-1, 0), len(r))
	return string(r[:i]) + s + string(r[i:])
}

// deleteRange deletes the text between points from and to.
func deleteRange(text string, from, to int) (string, string) {
	r := []rune(text)
	from, to = min(max(from-1, 0), len(r)), min(max(to-1, 0), len(r))
	if from > to {
		from, to = to, from
	}
	return string(r[:from]) + string(r[to:]), string(r[from:to])
}

func indexOf(xs []int, x int) int { return slices.Index(xs, x) }
