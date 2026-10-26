package inputtap

import (
	"encoding/binary"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zsy/emacs-playground/internal/protocol"
)

var t0 = time.Unix(1761472000, 0)

func keysString(ks []protocol.Key) string { return protocol.FormatKeys(ks) }

func TestTermDecoderLegacy(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ctrl letters", "\x18\x06", "C-x C-f"},
		{"printable", "ab", "a b"},
		{"space ret tab del", " \r\t\x7f", "SPC RET TAB DEL"},
		{"C-@ C-h C-j", "\x00\x08\x0a", "C-@ C-h C-j"},
		{"C-_ C-\\ C-]", "\x1f\x1c\x1d", "C-_ C-\\ C-]"},
		{"meta x", "\x1bx", "M-x"},
		{"meta ctrl f", "\x1b\x06", "C-M-f"},
		{"meta percent", "\x1b%", "M-%"},
		{"esc esc", "\x1b\x1b", "M-ESC"},
		{"utf8", "é中", "é 中"},
		{"meta utf8", "\x1bé", "M-é"},
		{"arrows", "\x1b[A\x1b[B\x1b[C\x1b[D", "<up> <down> <right> <left>"},
		{"ss3 arrows and f1-f4", "\x1bOA\x1bOP\x1bOQ\x1bOR\x1bOS", "<up> <f1> <f2> <f3> <f4>"},
		{"ctrl left", "\x1b[1;5D", "C-<left>"},
		{"meta shift up", "\x1b[1;4A", "M-S-<up>"},
		{"home end", "\x1b[H\x1b[F\x1b[1~\x1b[4~", "<home> <end> <home> <end>"},
		{"tilde keys", "\x1b[2~\x1b[3~\x1b[5~\x1b[6~", "<insert> <deletechar> <prior> <next>"},
		{"function keys", "\x1b[15~\x1b[24~\x1b[24;5~", "<f5> <f12> C-<f12>"},
		{"backtab", "\x1b[Z", "<backtab>"},
		{"modifyOtherKeys C-;", "\x1b[27;5;59~", "C-;"},
		{"modifyOtherKeys C-RET", "\x1b[27;5;13~", "C-RET"},
		{"modifyOtherKeys C-S-a", "\x1b[27;6;65~", "C-S-a"},
		{"focus events ignored", "\x1b[I\x1b[Ox", "x"},
		{"sgr mouse ignored", "\x1b[<0;10;5M\x1b[<0;10;5mq", "q"},
		{"x10 mouse ignored", "\x1b[M !!q", "q"},
		{"terminal replies ignored", "\x1b[?1u\x1b[?62;22c", ""},
		{"bracketed paste skipped", "a\x1b[200~\x18\x06 pasted\x1b[201~b", "a b"},
	}
	for _, c := range cases {
		var d TermDecoder
		got := keysString(d.Feed([]byte(c.in), t0))
		if got != c.want {
			t.Errorf("%s: %q → %q, want %q", c.name, c.in, got, c.want)
		}
		if d.Pending() {
			t.Errorf("%s: unexpected pending bytes", c.name)
		}
	}
}

func TestTermDecoderKitty(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"\x1b[97;5u", "C-a"},
		{"\x1b[120;3u", "M-x"},
		{"\x1b[120;7u", "C-M-x"},
		{"\x1b[59;5u", "C-;"},
		{"\x1b[13;5u", "C-RET"},
		{"\x1b[13u", "RET"},
		{"\x1b[27u", "ESC"},
		{"\x1b[9;2u", "<backtab>"},
		{"\x1b[127;3u", "M-DEL"},
		{"\x1b[115;6u", "C-S-s"},
		{"\x1b[97:65;6u", "C-S-a"},
		{"\x1b[37:37;8u", "C-M-%"}, // with alternate key reporting
		{"\x1b[53:37;8u", "C-M-%"}, // shift+5 reported as % via shifted key
		{"\x1b[97;2u", "A"},
		{"\x1b[49:33;2u", "!"},
		{"\x1b[97;5:3u", ""},    // release
		{"\x1b[97;5:2u", "C-a"}, // repeat
		{"\x1b[57441;2u", ""},   // left shift alone
		{"\x1b[57376u", "<f25>"},
		{"\x1b[57399u", "<kp-0>"},
		{"\x1b[97;9u", "s-a"},
		{"\x1b[97;17u", "H-a"},
		{"\x1b[97;33u", "M-a"},
		{"\x1b[1;5P", "C-<f1>"},
		{"\x1b[1;5R", "C-<f3>"},
		{"\x1b[12;40R", ""}, // cursor position report
		{"\x1b[32;5u", "C-SPC"},
		{"\x1b[960u", "π"},
	}
	for _, c := range cases {
		d := TermDecoder{Kitty: true}
		if got := keysString(d.Feed([]byte(c.in), t0)); got != c.want {
			t.Errorf("%q → %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTermDecoderEscTiming(t *testing.T) {
	var d TermDecoder
	// ESC alone waits.
	if ks := d.Feed([]byte("\x1b"), t0); len(ks) != 0 || !d.Pending() {
		t.Fatalf("lone ESC should be pending, got %v", ks)
	}
	// A following byte within 50 ms makes M-x.
	if got := keysString(d.Feed([]byte("x"), t0.Add(20*time.Millisecond))); got != "M-x" {
		t.Fatalf("got %q", got)
	}
	// ESC then nothing: Flush after the timeout reports ESC.
	d.Feed([]byte("\x1b"), t0)
	if ks := d.Flush(t0.Add(10 * time.Millisecond)); ks != nil {
		t.Fatalf("flushed too early: %v", ks)
	}
	if got := keysString(d.Flush(t0.Add(60 * time.Millisecond))); got != "ESC" {
		t.Fatalf("flush got %q", got)
	}
	// ESC, then a byte after the timeout: ESC and x separately.
	d.Feed([]byte("\x1b"), t0)
	if got := keysString(d.Feed([]byte("x"), t0.Add(80*time.Millisecond))); got != "ESC x" {
		t.Fatalf("late byte got %q", got)
	}
	// A CSI sequence split across chunks.
	if ks := d.Feed([]byte("\x1b[1;"), t0); len(ks) != 0 {
		t.Fatalf("partial CSI produced %v", ks)
	}
	if got := keysString(d.Feed([]byte("5A"), t0.Add(time.Millisecond))); got != "C-<up>" {
		t.Fatalf("split CSI got %q", got)
	}
	// A split UTF-8 character.
	b := []byte("中")
	if ks := d.Feed(b[:1], t0); len(ks) != 0 {
		t.Fatal("partial utf8")
	}
	if got := keysString(d.Feed(b[1:], t0)); got != "中" {
		t.Fatalf("split utf8 got %q", got)
	}
	// Kitty mode reports a lone ESC at once.
	k := TermDecoder{Kitty: true}
	if got := keysString(k.Feed([]byte("\x1b"), t0)); got != "ESC" {
		t.Fatalf("kitty lone ESC got %q", got)
	}
}

func TestModeTracker(t *testing.T) {
	var m ModeTracker
	m.Feed([]byte("hello\x1b[>1u"))
	if !m.Kitty() || m.Flags() != 1 {
		t.Fatalf("push: %d", m.Flags())
	}
	m.Feed([]byte("\x1b[=3;2u"))
	if m.Flags() != 3 {
		t.Fatalf("or: %d", m.Flags())
	}
	m.Feed([]byte("\x1b[=2;3u"))
	if m.Flags() != 1 {
		t.Fatalf("and-not: %d", m.Flags())
	}
	// Split across chunks.
	m.Feed([]byte("text\x1b["))
	m.Feed([]byte(">5u more"))
	if m.Flags() != 5 {
		t.Fatalf("split push: %d", m.Flags())
	}
	m.Feed([]byte("\x1b[<u"))
	if m.Flags() != 1 {
		t.Fatalf("pop: %d", m.Flags())
	}
	m.Feed([]byte("\x1b[<5u"))
	if m.Kitty() {
		t.Fatalf("pop past bottom: %d", m.Flags())
	}
	m.Feed([]byte("\x1b[>4;2m"))
	if m.ModifyOtherKeys != 2 {
		t.Fatalf("modifyOtherKeys %d", m.ModifyOtherKeys)
	}
	m.Feed([]byte("\x1b[>4m"))
	if m.ModifyOtherKeys != 0 {
		t.Fatalf("modifyOtherKeys reset %d", m.ModifyOtherKeys)
	}
	// SGR colour sequences are not mode changes.
	m.Feed([]byte("\x1b[38;2;1;2;3m\x1b[0m"))
	if m.Kitty() || m.ModifyOtherKeys != 0 {
		t.Fatal("colours changed modes")
	}
}

// rfb builds a client-to-server stream: handshake then messages.
func rfbHandshake() []byte { return []byte("RFB 003.008\n\x01\x01") }

func keyEvent(down bool, ks uint32) []byte {
	b := make([]byte, 8)
	b[0] = 4
	if down {
		b[1] = 1
	}
	binary.BigEndian.PutUint32(b[4:], ks)
	return b
}

func press(ks ...uint32) []byte {
	var out []byte
	for _, k := range ks {
		out = append(out, keyEvent(true, k)...)
	}
	for i := len(ks) - 1; i >= 0; i-- {
		out = append(out, keyEvent(false, ks[i])...)
	}
	return out
}

func TestRFBDecoder(t *testing.T) {
	setEnc := []byte{2, 0, 0, 2, 0, 0, 0, 7, 0xff, 0xff, 0xff, 0x21}
	fbReq := []byte{3, 1, 0, 0, 0, 0, 0, 100, 0, 100}
	pointer := []byte{5, 0, 0, 10, 0, 10}
	cut := append([]byte{6, 0, 0, 0, 0, 0, 0, 3}, "abc"...)
	extCut := append([]byte{6, 0, 0, 0, 0xff, 0xff, 0xff, 0xfc}, "wxyz"...) // length -4
	pixfmt := make([]byte, 20)
	fence := []byte{248, 0, 0, 0, 0, 0, 0, 0, 2, 'h', 'i'}
	desk := append([]byte{251, 0, 0x06, 0x40, 0x03, 0x84, 1, 0}, make([]byte, 16)...)
	qemuKey := func(down bool, ks uint32) []byte {
		b := make([]byte, 12)
		b[0], b[1] = 255, 0
		if down {
			b[3] = 1
		}
		binary.BigEndian.PutUint32(b[4:], ks)
		return b
	}

	cases := []struct {
		name string
		msgs [][]byte
		want string
	}{
		{"C-x C-f", [][]byte{press(xkControlL, 'x'), press(xkControlL, 'f')}, "C-x C-f"},
		{"M-x", [][]byte{press(xkAltL, 'x')}, "M-x"},
		{"C-M-%", [][]byte{press(xkControlL, xkAltL, xkShiftL, '%')}, "C-M-%"},
		{"shifted letter", [][]byte{press(xkShiftL, 'A')}, "A"},
		{"C-S-a", [][]byte{press(xkControlR, xkShiftR, 'A')}, "C-S-a"},
		{"named keys", [][]byte{press(0xff0d), press(0xff09), press(0xff08), press(0xff1b), press(' ')}, "RET TAB DEL ESC SPC"},
		{"function keys", [][]byte{press(0xffbe), press(0xffc9), press(xkControlL, 0xff51), press(xkShiftL, 0xff52)}, "<f1> <f12> C-<left> S-<up>"},
		{"super hyper", [][]byte{press(xkSuperL, 'a'), press(xkHyperL, 'b')}, "s-a H-b"},
		{"unicode keysym", [][]byte{press(0x010003c0), press(0xe9)}, "π é"},
		{"other messages interleaved", [][]byte{setEnc, pixfmt, fbReq, pointer, cut, extCut, fence, desk, press(xkControlL, 'g')}, "C-g"},
		{"qemu extended key", [][]byte{qemuKey(true, xkControlL), qemuKey(true, 'x'), qemuKey(false, 'x'), qemuKey(false, xkControlL)}, "C-x"},
	}
	for _, c := range cases {
		var d RFBDecoder
		stream := rfbHandshake()
		for _, m := range c.msgs {
			stream = append(stream, m...)
		}
		// Feed in uneven chunks to exercise buffering.
		var got []protocol.Key
		for i := 0; i < len(stream); i += 3 {
			end := i + 3
			if end > len(stream) {
				end = len(stream)
			}
			got = append(got, d.Feed(stream[i:end])...)
		}
		if s := keysString(got); s != c.want {
			t.Errorf("%s: got %q want %q", c.name, s, c.want)
		}
		if d.Disabled() {
			t.Errorf("%s: decoder disabled", c.name)
		}
	}

	// RFB 3.3 has no security byte from the client.
	var d RFBDecoder
	got := d.Feed(append([]byte("RFB 003.003\n\x01"), press('q')...))
	if keysString(got) != "q" {
		t.Fatalf("3.3: %q", keysString(got))
	}
	// VNC authentication response is skipped.
	d = RFBDecoder{}
	stream := append([]byte("RFB 003.008\n\x02"), make([]byte, 16)...)
	stream = append(stream, 1)
	got = d.Feed(append(stream, press('z')...))
	if keysString(got) != "z" {
		t.Fatalf("vnc auth: %q", keysString(got))
	}
	// Unknown message types disable decoding without panicking.
	d = RFBDecoder{}
	d.Feed(append(rfbHandshake(), 99, 1, 2, 3))
	if !d.Disabled() || d.Feed(press('a')) != nil {
		t.Fatal("unknown message should disable the decoder")
	}
}

func TestPrefixTracker(t *testing.T) {
	type step struct {
		key  string
		emit bool
		keys string
	}
	cases := []struct {
		name  string
		steps []step
	}{
		{"C-x then C-f", []step{{"C-x", true, "C-x"}, {"C-f", false, ""}}},
		{"C-x 4 f", []step{{"C-x", true, "C-x"}, {"4", true, "C-x 4"}, {"f", false, ""}}},
		{"C-x r t", []step{{"C-x", true, "C-x"}, {"r", true, "C-x r"}, {"t", false, ""}}},
		{"C-g cancels", []step{{"C-x", true, "C-x"}, {"C-g", true, ""}, {"C-g", false, ""}}},
		{"M-g M-g", []step{{"M-g", true, "M-g"}, {"M-g", false, ""}}},
		{"ESC prefix", []step{{"ESC", true, "ESC"}, {"g", true, "M-g"}, {"g", false, ""}}},
		{"ESC x", []step{{"ESC", true, "ESC"}, {"x", false, ""}}},
		{"plain keys", []step{{"a", false, ""}, {"C-f", false, ""}}},
		{"C-c C-c", []step{{"C-c", true, "C-c"}, {"C-c", false, ""}}},
		{"C-h k", []step{{"C-h", true, "C-h"}, {"k", false, ""}, {"C-x", true, "C-x"}}},
	}
	for _, c := range cases {
		p := NewPrefixTracker(nil)
		for i, s := range c.steps {
			k, err := protocol.ParseKey(s.key)
			if err != nil {
				t.Fatal(err)
			}
			keys, emit := p.Key(k)
			if emit != s.emit || keys != s.keys {
				t.Errorf("%s step %d (%s): got (%q, %v) want (%q, %v)", c.name, i, s.key, keys, emit, s.keys, s.emit)
			}
		}
	}
}

func TestTapStreams(t *testing.T) {
	var mu sync.Mutex
	var got []string
	tap := NewTap(func(ev *protocol.PrefixEvent) {
		mu.Lock()
		got = append(got, ev.Keys+"|"+string(ev.Source))
		mu.Unlock()
	})
	s := tap.NewTermStream()
	defer s.Close()
	s.Input([]byte("\x18"))
	s.Input([]byte("4"))
	s.Input([]byte("f"))
	// Lone ESC is reported by the timer.
	s.Input([]byte("\x1b"))
	time.Sleep(3 * MetaTimeout)
	mu.Lock()
	joined := strings.Join(got, ",")
	mu.Unlock()
	if joined != "C-x|tap,C-x 4|tap,ESC|tap" {
		t.Fatalf("events %q", joined)
	}
	if tap.Pending() != "ESC" {
		t.Fatalf("pending %q", tap.Pending())
	}
	tap.Reset()
	if tap.Pending() != "" {
		t.Fatal("reset")
	}
	// Output enables kitty: CSI u input decodes.
	s.Output([]byte("\x1b[>1u"))
	if !s.Kitty() {
		t.Fatal("kitty not detected")
	}
	s.Input([]byte("\x1b[99;5u")) // C-c
	r := tap.NewRFBStream()
	_, _ = r.Write(rfbHandshake())
	_, _ = r.Write(press(xkControlL, 'g'))
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(got[3:], ",") != "C-c|tap,|tap" {
		t.Fatalf("after kitty/rfb: %v", got)
	}
}
