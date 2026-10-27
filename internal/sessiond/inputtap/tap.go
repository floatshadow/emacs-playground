package inputtap

import (
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// DefaultPrefixes is the static prefix list (backend.md §3.6).
var DefaultPrefixes = []string{
	"C-x", "C-c", "C-h", "M-g", "M-s", "M-o",
	"C-x 4", "C-x 5", "C-x t", "C-x r", "C-x n", "C-x p", "C-x 8",
	"ESC",
}

// PrefixTracker remembers the key sequence typed so far while it is a known
// prefix. It is not safe for concurrent use (Tap serialises).
type PrefixTracker struct {
	prefixes map[string]bool
	pending  []protocol.Key
}

// NewPrefixTracker returns a tracker for the given prefix sequences (Emacs
// notation); nil means DefaultPrefixes.
func NewPrefixTracker(prefixes []string) *PrefixTracker {
	if prefixes == nil {
		prefixes = DefaultPrefixes
	}
	m := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		if n, err := protocol.NormalizeKeys(p); err == nil {
			m[n] = true
		}
	}
	return &PrefixTracker{prefixes: m}
}

var keyCg = protocol.Key{Mods: protocol.ModCtrl, Base: "g"}

// Key feeds one key. It returns the prefix event payload and true when the
// pending prefix changed in a way the browser should hear about: a sequence
// that is (still) a prefix, or "" when C-g cancelled a pending prefix. A key
// that completes a sequence clears the pending prefix silently: the agent's
// key event is authoritative and arrives right after.
func (t *PrefixTracker) Key(k protocol.Key) (string, bool) {
	if k == keyCg {
		had := len(t.pending) > 0
		t.pending = t.pending[:0]
		return "", had
	}
	seq := append(append([]protocol.Key{}, t.pending...), k)
	s := canonical(seq)
	if t.prefixes[s] {
		t.pending = seq
		return s, true
	}
	t.pending = t.pending[:0]
	return "", false
}

// Pending returns the pending prefix ("" when none).
func (t *PrefixTracker) Pending() string {
	if len(t.pending) == 0 {
		return ""
	}
	return canonical(t.pending)
}

// Reset forgets the pending prefix (the agent reported a command).
func (t *PrefixTracker) Reset() { t.pending = t.pending[:0] }

// canonical renders a sequence the way key-description does for the
// prefixes we track: "ESC g" is "M-g".
func canonical(seq []protocol.Key) string {
	if len(seq) >= 2 && seq[0] == (protocol.Key{Base: "ESC"}) && seq[1].Mods&protocol.ModMeta == 0 {
		k := seq[1]
		k.Mods |= protocol.ModMeta
		rest := append([]protocol.Key{k}, seq[2:]...)
		return protocol.FormatKeys(rest)
	}
	return protocol.FormatKeys(seq)
}

// Tap decodes every input stream of one Emacs and publishes prefix events.
// It is safe for concurrent use.
type Tap struct {
	mu     sync.Mutex
	prefix *PrefixTracker
	emit   func(*protocol.PrefixEvent)
	// OnKey, when set, observes every decoded key (tests, debugging).
	OnKey func(protocol.Key)
	now   func() time.Time
	// lastCommand is when the agent last reported a command (Reset).
	lastCommand time.Time
}

// NewTap returns a tap that calls emit for prefix changes. emit must not
// block for long (it publishes to the event bus).
func NewTap(emit func(*protocol.PrefixEvent)) *Tap {
	return &Tap{prefix: NewPrefixTracker(nil), emit: emit, now: time.Now}
}

// Keys feeds decoded keys from any stream.
func (t *Tap) Keys(keys []protocol.Key) {
	if len(keys) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, k := range keys {
		if t.OnKey != nil {
			t.OnKey(k)
		}
		if s, ok := t.prefix.Key(k); ok && t.emit != nil {
			t.emit(&protocol.PrefixEvent{Keys: s, Source: protocol.PrefixFromTap})
		}
	}
}

// late handles keys a stream reports only after MetaTimeout (a lone ESC,
// an unterminated sequence) whose bytes arrived at arrived. tmux
// (escape-time 0) hands such a byte to Emacs at once, so when the agent has
// reported a command since the bytes arrived, Emacs has already used them
// (ESC ESC ESC is keyboard-escape-quit) and reporting them now would show a
// pending prefix after the command that consumed it. Such keys are
// dropped and the pending prefix is cleared; other keys go through Keys.
func (t *Tap) late(keys []protocol.Key, arrived time.Time) {
	if len(keys) == 0 {
		return
	}
	t.mu.Lock()
	if !t.lastCommand.IsZero() && arrived.Before(t.lastCommand) {
		if t.OnKey != nil {
			for _, k := range keys {
				t.OnKey(k)
			}
		}
		t.prefix.Reset()
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	t.Keys(keys)
}

// Reset clears the pending prefix (called on agent key events) and
// remembers when, for late keys.
func (t *Tap) Reset() {
	t.mu.Lock()
	t.prefix.Reset()
	t.lastCommand = t.now()
	t.mu.Unlock()
}

// Pending returns the pending prefix.
func (t *Tap) Pending() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.prefix.Pending()
}

// TermStream is the tap for one terminal connection: input bytes go through
// the decoder, output bytes through the mode tracker. A lone ESC is reported
// after MetaTimeout by a timer.
type TermStream struct {
	tap   *Tap
	mu    sync.Mutex
	dec   TermDecoder
	modes ModeTracker
	timer *time.Timer
	done  bool
}

// NewTermStream starts a stream for one /pty connection.
func (t *Tap) NewTermStream() *TermStream { return &TermStream{tap: t} }

// Input observes bytes the browser sends to the terminal.
func (s *TermStream) Input(p []byte) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.dec.Kitty = s.modes.Kitty()
	now := s.tap.now()
	stale, arrived := s.dec.TakeStale(now)
	keys := s.dec.Feed(p, now)
	if s.dec.Pending() {
		if s.timer == nil {
			s.timer = time.AfterFunc(MetaTimeout+5*time.Millisecond, s.flush)
		} else {
			s.timer.Reset(MetaTimeout + 5*time.Millisecond)
		}
	}
	s.mu.Unlock()
	s.tap.late(stale, arrived)
	s.tap.Keys(keys)
}

func (s *TermStream) flush() {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	arrived := s.dec.escAt
	keys := s.dec.Flush(s.tap.now())
	s.mu.Unlock()
	s.tap.late(keys, arrived)
}

// Output observes bytes the terminal sends to the browser.
func (s *TermStream) Output(p []byte) {
	s.mu.Lock()
	s.modes.Feed(p)
	s.mu.Unlock()
}

// Kitty reports whether the stream has seen the kitty protocol enabled.
func (s *TermStream) Kitty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modes.Kitty()
}

// Close stops the stream's timer.
func (s *TermStream) Close() {
	s.mu.Lock()
	s.done = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
}

// RFBStream is the tap for one /vnc connection.
type RFBStream struct {
	tap *Tap
	dec RFBDecoder
}

// NewRFBStream starts a stream for one /vnc connection.
func (t *Tap) NewRFBStream() *RFBStream { return &RFBStream{tap: t} }

// Write implements io.Writer so the stream can sit behind io.TeeReader on
// the client-to-server copy. It never fails.
func (s *RFBStream) Write(p []byte) (int, error) {
	s.tap.Keys(s.dec.Feed(p))
	return len(p), nil
}
