package protocol

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// DemoTokenKind is the kind of a demo string token.
type DemoTokenKind string

// Demo token kinds.
const (
	DemoKey   DemoTokenKind = "key"
	DemoText  DemoTokenKind = "text"
	DemoPause DemoTokenKind = "pause"
)

// DemoToken is one element of a step's demo string.
type DemoToken struct {
	Kind DemoTokenKind `json:"kind"`
	// Key is the canonical key for DemoKey tokens.
	Key string `json:"key,omitempty"`
	// Text is typed literally for DemoText tokens.
	Text string `json:"text,omitempty"`
	// PauseMs is the pause for DemoPause tokens.
	PauseMs int `json:"pauseMs,omitempty"`
}

// Demo pacing used by the browser's DemoPlayer and playctl curriculum play
// (frontend.md §7).
const (
	DemoChordDelayMs = 170
	DemoCharDelayMs  = 70
)

// ParseDemo tokenises a demo string (CURRICULUM.md Part B.3):
//
//	C-x C-f "ma" TAB ~400 RET
//
// Tokens are separated by whitespace. A token starting with a double quote
// is text typed literally up to the closing quote; inside it \" is a quote,
// \\ a backslash, \n a newline (typed as RET) and \t a tab. ~N pauses N
// milliseconds. Anything else is a key (ParseKey).
func ParseDemo(s string) ([]DemoToken, error) {
	var out []DemoToken
	i := 0
	for i < len(s) {
		r := rune(s[i])
		if unicode.IsSpace(r) {
			i++
			continue
		}
		switch {
		case s[i] == '"':
			var b strings.Builder
			j := i + 1
			closed := false
			for j < len(s) {
				c := s[j]
				if c == '\\' && j+1 < len(s) {
					switch s[j+1] {
					case '"':
						b.WriteByte('"')
					case '\\':
						b.WriteByte('\\')
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						return nil, fmt.Errorf("demo: bad escape \\%c at offset %d", s[j+1], j)
					}
					j += 2
					continue
				}
				if c == '"' {
					closed = true
					j++
					break
				}
				b.WriteByte(c)
				j++
			}
			if !closed {
				return nil, fmt.Errorf("demo: unterminated string at offset %d", i)
			}
			if j < len(s) && !unicode.IsSpace(rune(s[j])) {
				return nil, fmt.Errorf("demo: missing space after string at offset %d", j)
			}
			if b.Len() == 0 {
				return nil, fmt.Errorf("demo: empty string at offset %d", i)
			}
			out = append(out, DemoToken{Kind: DemoText, Text: b.String()})
			i = j
		default:
			j := i
			for j < len(s) && !unicode.IsSpace(rune(s[j])) {
				j++
			}
			tok := s[i:j]
			if len(tok) > 1 && tok[0] == '~' {
				ms, err := strconv.Atoi(tok[1:])
				if err != nil || ms < 0 || ms > 60000 {
					return nil, fmt.Errorf("demo: bad pause %q (want ~0 to ~60000)", tok)
				}
				out = append(out, DemoToken{Kind: DemoPause, PauseMs: ms})
			} else {
				k, err := ParseKey(tok)
				if err != nil {
					return nil, fmt.Errorf("demo: %w", err)
				}
				out = append(out, DemoToken{Kind: DemoKey, Key: k.String()})
			}
			i = j
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("demo: empty")
	}
	return out, nil
}
