package protocol

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
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

// DemoSender delivers one element of a demo: Keys in Emacs notation or Text
// typed literally (a KeysRequest without Frame).
type DemoSender func(ctx context.Context, req KeysRequest) error

// PlayDemo plays a demo string through send with playctl's pacing: one
// send per key followed by DemoChordDelayMs, typed text in runs followed by
// DemoCharDelayMs per character (a newline is sent as RET and a tab as TAB),
// and ~N pauses as written. Speed scales the chord and character delays
// (2 is twice as fast; zero or less means 1). playctl curriculum play uses
// it over rpc keys, sessiond's "Skip this step" and "Go to this step" over
// tmux. It stops at the first send error or when ctx ends.
func PlayDemo(ctx context.Context, demo string, speed float64, send DemoSender) error {
	toks, err := ParseDemo(demo)
	if err != nil {
		return err
	}
	if speed <= 0 {
		speed = 1
	}
	scaled := func(ms int) time.Duration { return time.Duration(float64(ms)/speed) * time.Millisecond }
	sleep := func(d time.Duration) error {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	}
	for _, t := range toks {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch t.Kind {
		case DemoKey:
			if err := send(ctx, KeysRequest{Keys: t.Key}); err != nil {
				return fmt.Errorf("keys %s: %w", t.Key, err)
			}
			if err := sleep(scaled(DemoChordDelayMs)); err != nil {
				return err
			}
		case DemoText:
			for _, chunk := range SplitDemoText(t.Text) {
				req := KeysRequest{Text: chunk}
				switch chunk {
				case "\n":
					req = KeysRequest{Keys: "RET"}
				case "\t":
					req = KeysRequest{Keys: "TAB"}
				}
				if err := send(ctx, req); err != nil {
					return fmt.Errorf("text %q: %w", chunk, err)
				}
				if err := sleep(scaled(DemoCharDelayMs * len([]rune(chunk)))); err != nil {
					return err
				}
			}
		case DemoPause:
			if err := sleep(time.Duration(t.PauseMs) * time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

// SplitDemoText splits typed demo text into runs and single "\n" or "\t"
// elements.
func SplitDemoText(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\t' {
			if i > start {
				out = append(out, s[start:i])
			}
			out = append(out, s[i:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
