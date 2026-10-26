package inputtap

import (
	"strconv"
	"strings"
)

// ModeTracker watches terminal output (what Emacs, through tmux, sends to the
// browser) for keyboard protocol changes:
//
//	CSI > flags u        push kitty flags
//	CSI < n u            pop n entries (default 1)
//	CSI = flags ; mode u set (1), or (2) or and-not (3) the current flags
//	CSI > 4 ; n m        xterm modifyOtherKeys level n (CSI > 4 m resets)
//
// Sequences split across chunks are handled with a small carry-over buffer.
type ModeTracker struct {
	stack []int
	cur   int
	// ModifyOtherKeys is the xterm modifyOtherKeys level (0, 1 or 2).
	ModifyOtherKeys int
	tail            []byte
}

// maxStack bounds the kitty flag stack (the protocol suggests a limit).
const maxStack = 32

// Kitty reports whether the kitty keyboard protocol is active (any flags).
func (m *ModeTracker) Kitty() bool { return m.cur != 0 }

// Flags returns the current kitty flags.
func (m *ModeTracker) Flags() int { return m.cur }

// Feed scans an output chunk.
func (m *ModeTracker) Feed(p []byte) {
	data := p
	if len(m.tail) > 0 {
		data = append(append([]byte{}, m.tail...), p...)
		m.tail = m.tail[:0]
	}
	for i := 0; i < len(data); i++ {
		if data[i] != 0x1b {
			continue
		}
		if i+1 >= len(data) {
			m.keep(data[i:])
			return
		}
		if data[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(data) && data[j] >= 0x20 && data[j] <= 0x3f {
			j++
		}
		if j >= len(data) {
			m.keep(data[i:])
			return
		}
		m.apply(string(data[i+2:j]), data[j])
		i = j
	}
}

func (m *ModeTracker) keep(b []byte) {
	if len(b) > 64 {
		return
	}
	m.tail = append(m.tail[:0], b...)
}

func (m *ModeTracker) apply(params string, fin byte) {
	if params == "" {
		return
	}
	switch {
	case fin == 'u' && params[0] == '>':
		n := atoiDefault(params[1:], 0)
		if len(m.stack) >= maxStack {
			m.stack = m.stack[1:]
		}
		m.stack = append(m.stack, m.cur)
		m.cur = n
	case fin == 'u' && params[0] == '<':
		n := atoiDefault(params[1:], 1)
		for ; n > 0; n-- {
			if len(m.stack) == 0 {
				m.cur = 0
				break
			}
			m.cur = m.stack[len(m.stack)-1]
			m.stack = m.stack[:len(m.stack)-1]
		}
	case fin == 'u' && params[0] == '=':
		parts := strings.Split(params[1:], ";")
		flags := atoiDefault(parts[0], 0)
		mode := 1
		if len(parts) > 1 {
			mode = atoiDefault(parts[1], 1)
		}
		switch mode {
		case 1:
			m.cur = flags
		case 2:
			m.cur |= flags
		case 3:
			m.cur &^= flags
		}
	case fin == 'm' && params[0] == '>':
		parts := strings.Split(params[1:], ";")
		if atoiDefault(parts[0], -1) != 4 {
			return
		}
		if len(parts) > 1 {
			m.ModifyOtherKeys = atoiDefault(parts[1], 0)
		} else {
			m.ModifyOtherKeys = 0
		}
	case fin == 'n' && strings.HasPrefix(params, ">4"):
		// CSI > 4 n disables modifyOtherKeys.
		m.ModifyOtherKeys = 0
	}
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
