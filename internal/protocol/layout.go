package protocol

import (
	"fmt"
	"path"
	"strings"
)

// LayoutPattern is a parsed layout expression as used by layout conditions
// and setup.layout (CURRICULUM.md Part B.3):
//
//	single                          exactly one window, any buffer
//	main.rs                         exactly one window showing main.rs
//	(vsplit main.rs *compilation*)  main.rs above *compilation*
//	(hsplit * (vsplit a b))         any window left of a above b
//
// vsplit stacks children top to bottom, hsplit left to right (SplitKind).
// "*" matches any single window. Names may be double-quoted when they
// contain spaces or parentheses. A name containing "/" is compared by its
// base name. Buffer names are compared by uniquify base name: "main.rs<hello>"
// matches "main.rs".
type LayoutPattern struct {
	// Split is empty for a leaf.
	Split SplitKind
	// Name is the leaf's buffer name, "*" for any, or "" with Single.
	Name string
	// Single is the bare word single.
	Single   bool
	Children []LayoutPattern
}

// String renders the pattern back to its s-expression form.
func (p LayoutPattern) String() string {
	if p.Single {
		return "single"
	}
	if p.Split == "" {
		if strings.ContainsAny(p.Name, " ()\"") {
			return fmt.Sprintf("%q", p.Name)
		}
		return p.Name
	}
	parts := []string{string(p.Split)}
	for _, c := range p.Children {
		parts = append(parts, c.String())
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// ParseLayoutPattern parses a layout expression.
func ParseLayoutPattern(s string) (LayoutPattern, error) {
	toks, err := lexLayout(s)
	if err != nil {
		return LayoutPattern{}, err
	}
	if len(toks) == 0 {
		return LayoutPattern{}, fmt.Errorf("layout pattern: empty")
	}
	if len(toks) == 1 && toks[0] == "single" {
		return LayoutPattern{Single: true}, nil
	}
	p, rest, err := parseLayoutToks(toks)
	if err != nil {
		return LayoutPattern{}, err
	}
	if len(rest) != 0 {
		return LayoutPattern{}, fmt.Errorf("layout pattern: trailing tokens %v", rest)
	}
	return p, nil
}

func lexLayout(s string) ([]string, error) {
	var toks []string
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '"':
			j := i + 1
			var b strings.Builder
			for j < len(s) && s[j] != '"' {
				if s[j] == '\\' && j+1 < len(s) {
					j++
				}
				b.WriteByte(s[j])
				j++
			}
			if j >= len(s) {
				return nil, fmt.Errorf("layout pattern: unterminated string")
			}
			// Mark quoted names so "vsplit" in quotes stays a name.
			toks = append(toks, "\x00"+b.String())
			i = j + 1
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t\n\r()\"", rune(s[j])) {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks, nil
}

func parseLayoutToks(toks []string) (LayoutPattern, []string, error) {
	if len(toks) == 0 {
		return LayoutPattern{}, nil, fmt.Errorf("layout pattern: unexpected end")
	}
	t := toks[0]
	switch t {
	case ")":
		return LayoutPattern{}, nil, fmt.Errorf("layout pattern: unexpected )")
	case "(":
		if len(toks) < 2 {
			return LayoutPattern{}, nil, fmt.Errorf("layout pattern: unexpected end after (")
		}
		kind := SplitKind(toks[1])
		if kind != VSplit && kind != HSplit {
			return LayoutPattern{}, nil, fmt.Errorf("layout pattern: want vsplit or hsplit after (, got %q", strings.TrimPrefix(toks[1], "\x00"))
		}
		p := LayoutPattern{Split: kind}
		rest := toks[2:]
		for {
			if len(rest) == 0 {
				return LayoutPattern{}, nil, fmt.Errorf("layout pattern: missing )")
			}
			if rest[0] == ")" {
				rest = rest[1:]
				break
			}
			c, r, err := parseLayoutToks(rest)
			if err != nil {
				return LayoutPattern{}, nil, err
			}
			p.Children = append(p.Children, c)
			rest = r
		}
		if len(p.Children) < 2 {
			return LayoutPattern{}, nil, fmt.Errorf("layout pattern: %s needs at least two children", kind)
		}
		return p, rest, nil
	default:
		name := strings.TrimPrefix(t, "\x00")
		if t == "single" {
			return LayoutPattern{}, nil, fmt.Errorf("layout pattern: single is only valid on its own")
		}
		return LayoutPattern{Name: name}, toks[1:], nil
	}
}

// BufferBaseName strips a uniquify suffix ("main.rs<hello>" → "main.rs").
func BufferBaseName(name string) string {
	if strings.HasSuffix(name, ">") {
		if i := strings.LastIndexByte(name, '<'); i > 0 {
			return name[:i]
		}
	}
	return name
}

// MatchLayout reports whether layout l, whose leaves show the buffers given
// by bufOf(windowID), matches pattern p. Nested combinations with the same
// orientation as their parent are flattened on both sides first.
func MatchLayout(p LayoutPattern, l Layout, bufOf func(id int) string) bool {
	l = flattenLayout(l)
	p = flattenPattern(p)
	if p.Single {
		return !l.IsZero() && l.IsLeaf()
	}
	return matchLayout(p, l, bufOf)
}

func matchLayout(p LayoutPattern, l Layout, bufOf func(int) string) bool {
	if p.Split == "" {
		if !l.IsLeaf() || l.IsZero() {
			return false
		}
		if p.Name == "*" {
			return true
		}
		want := p.Name
		if strings.Contains(want, "/") {
			want = path.Base(want)
		}
		return BufferBaseName(bufOf(l.Window)) == want
	}
	if l.Split != p.Split || len(l.Children) != len(p.Children) {
		return false
	}
	for i := range p.Children {
		if !matchLayout(p.Children[i], l.Children[i], bufOf) {
			return false
		}
	}
	return true
}

func flattenLayout(l Layout) Layout {
	if l.IsLeaf() {
		return l
	}
	out := Layout{Split: l.Split}
	for _, c := range l.Children {
		c = flattenLayout(c)
		if c.Split == l.Split {
			out.Children = append(out.Children, c.Children...)
		} else {
			out.Children = append(out.Children, c)
		}
	}
	if len(out.Children) == 1 {
		return out.Children[0]
	}
	return out
}

func flattenPattern(p LayoutPattern) LayoutPattern {
	if p.Split == "" {
		return p
	}
	out := LayoutPattern{Split: p.Split}
	for _, c := range p.Children {
		c = flattenPattern(c)
		if c.Split == p.Split {
			out.Children = append(out.Children, c.Children...)
		} else {
			out.Children = append(out.Children, c)
		}
	}
	return out
}
