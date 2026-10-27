package inputtap

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

type prefixLog struct {
	mu  sync.Mutex
	got []string
}

func (l *prefixLog) emit(ev *protocol.PrefixEvent) {
	l.mu.Lock()
	l.got = append(l.got, ev.Keys)
	l.mu.Unlock()
}

func (l *prefixLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.got, ",")
}

// TestLateEscAfterCommand is B4: ESC ESC ESC 150 ms apart. tmux hands each
// ESC to Emacs at once; the tap reports a lone ESC only when its timer
// fires, so the third one used to arrive after the agent's
// keyboard-escape-quit and left "ESC is a prefix" pending for good.
func TestLateEscAfterCommand(t *testing.T) {
	log := &prefixLog{}
	tap := NewTap(log.emit)
	s := tap.NewTermStream()
	defer s.Close()

	s.Input([]byte("\x1b"))
	time.Sleep(3 * MetaTimeout)
	if log.String() != "ESC" {
		t.Fatalf("the first lone ESC is a prefix: %q", log.String())
	}
	s.Input([]byte("\x1b"))
	time.Sleep(3 * MetaTimeout)
	// The third ESC: Emacs runs keyboard-escape-quit before the tap's timer.
	s.Input([]byte("\x1b"))
	time.Sleep(MetaTimeout / 5)
	tap.Reset() // the agent's key event
	time.Sleep(3 * MetaTimeout)
	if log.String() != "ESC" || tap.Pending() != "" {
		t.Fatalf("a prefix followed the command that used the ESC: %q, pending %q", log.String(), tap.Pending())
	}

	// A lone ESC typed after the command is a prefix again.
	s.Input([]byte("\x1b"))
	time.Sleep(3 * MetaTimeout)
	if log.String() != "ESC,ESC" || tap.Pending() != "ESC" {
		t.Fatalf("a fresh lone ESC: %q, pending %q", log.String(), tap.Pending())
	}

	// A stale ESC reported by the next input (rather than the timer) is
	// judged the same way.
	tap.Reset()
	s.Close()
	s2 := tap.NewTermStream()
	defer s2.Close()
	s2.Input([]byte("\x1b"))
	s2.Close() // no timer: the next Input finds the ESC stale
	s2.done = false
	time.Sleep(MetaTimeout / 5)
	tap.Reset()
	time.Sleep(2 * MetaTimeout)
	s2.Input([]byte("\x18"))
	if log.String() != "ESC,ESC,C-x" {
		t.Fatalf("stale ESC before C-x: %q", log.String())
	}
}
