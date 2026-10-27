package events

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

func echo(s string) protocol.Event { return &protocol.EchoEvent{Text: s} }

func decode(t *testing.T, data []byte) protocol.Event {
	t.Helper()
	ev, err := protocol.DecodeEvent(data)
	if err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return ev
}

func TestPublishStampsAndRing(t *testing.T) {
	b := NewBus(nil)
	b.Now = func() time.Time { return time.UnixMilli(1761472000123) }
	for i := 1; i <= RingSize+20; i++ {
		f := b.Publish(echo("e"))
		if f.Seq != int64(i) {
			t.Fatalf("seq %d want %d", f.Seq, i)
		}
	}
	ev := decode(t, b.Recent(1)[0]).(*protocol.EchoEvent)
	if ev.Seq != RingSize+20 || ev.Ts != 1761472000123 || ev.T != protocol.EventEcho {
		t.Fatalf("envelope %+v", ev.Envelope)
	}
	if got := len(b.Recent(1000)); got != RingSize {
		t.Fatalf("recent %d", got)
	}
	if got := len(b.Recent(StuckWindow)); got != StuckWindow {
		t.Fatalf("stuck window %d", got)
	}
	// Oldest kept is seq 21.
	first := decode(t, b.Recent(RingSize)[0])
	if first.Env().Seq != 21 {
		t.Fatalf("oldest %d", first.Env().Seq)
	}
}

func TestReplay(t *testing.T) {
	b := NewBus(nil)
	for i := 0; i < 10; i++ {
		b.Publish(echo("x"))
	}
	b.Publish(&protocol.State{Buf: "main.rs"})
	cases := []struct {
		after    int64
		replayed bool
		n        int
	}{
		{-1, false, 0}, // no ?after
		{0, true, 11},  // everything is still in the ring
		{5, true, 6},
		{11, true, 0},  // up to date
		{12, false, 0}, // from the future (sessiond restarted)
	}
	for _, c := range cases {
		s, snap := b.Subscribe(c.after)
		if snap.Replayed != c.replayed || len(snap.Frames) != c.n {
			t.Errorf("after=%d: replayed=%v n=%d", c.after, snap.Replayed, len(snap.Frames))
		}
		if snap.State.Seq != 11 {
			t.Errorf("after=%d: state seq %d", c.after, snap.State.Seq)
		}
		for i, f := range snap.Frames {
			if f.Seq != c.after+int64(i)+1 {
				t.Errorf("after=%d: frame %d seq %d", c.after, i, f.Seq)
			}
		}
		s.Close()
	}
	// Too old: the ring no longer covers seq 1.
	for i := 0; i < RingSize; i++ {
		b.Publish(echo("y"))
	}
	_, snap := b.Subscribe(3)
	if snap.Replayed || snap.Frames != nil {
		t.Fatalf("expected no replay for a gap older than the ring")
	}
	_, snap = b.Subscribe(b.Seq() - RingSize)
	if !snap.Replayed || len(snap.Frames) != RingSize {
		t.Fatalf("edge of ring: replayed=%v n=%d", snap.Replayed, len(snap.Frames))
	}
}

func TestSlowSubscriberGetsGap(t *testing.T) {
	b := NewBus(nil)
	s, _ := b.Subscribe(-1)
	defer s.Close()
	fast, _ := b.Subscribe(-1)
	defer fast.Close()
	go func() {
		for range fast.C {
		}
	}()
	for i := 0; i < SubscriberBuffer+10; i++ {
		b.Publish(echo("x"))
	}
	if s.Gaps != 1 {
		t.Fatalf("gaps %d", s.Gaps)
	}
	f := <-s.C
	gap, ok := decode(t, f.Data).(*protocol.GapEvent)
	if !ok || f.Seq != 0 {
		t.Fatalf("first frame after overflow should be an unsequenced gap, got %s", f.Data)
	}
	if gap.From != 1 || gap.To != SubscriberBuffer+1 {
		t.Fatalf("gap %d..%d", gap.From, gap.To)
	}
	// Live delivery resumes after the gap.
	var next []int64
	for len(s.C) > 0 {
		next = append(next, (<-s.C).Seq)
	}
	if len(next) != 9 || next[0] != SubscriberBuffer+2 {
		t.Fatalf("after gap: %v", next)
	}
}

func TestHandlerHelloStateReplay(t *testing.T) {
	b := NewBus(nil)
	b.Publish(&protocol.KeyEvent{Keys: "C-x C-f", Cmd: "find-file"})
	b.Publish(&protocol.State{Buf: "main.rs"})
	b.Publish(&protocol.KeyEvent{Keys: "C-n", Cmd: "next-line"})
	done := make(chan struct{})
	h := &Handler{Bus: b, Done: done, Hello: func(replayed bool) *protocol.HelloEvent {
		return &protocol.HelloEvent{Protocol: protocol.Version, SessionID: "local", Mode: protocol.FrameTerminal}
	}}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	read := func(c *websocket.Conn) protocol.Event {
		t.Helper()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return decode(t, data)
	}

	// Replay after seq 1.
	c, _, err := websocket.Dial(ctx, ws+"?after=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	hello := read(c).(*protocol.HelloEvent)
	if !hello.Replayed || hello.T != protocol.EventHello || hello.Seq != 0 {
		t.Fatalf("hello %+v", hello)
	}
	if st := read(c).(*protocol.State); st.Seq != 2 {
		t.Fatalf("state seq %d", st.Seq)
	}
	if ev := read(c); ev.Env().Seq != 2 {
		t.Fatalf("replay 1: %d", ev.Env().Seq)
	}
	if ev := read(c); ev.Env().Seq != 3 {
		t.Fatalf("replay 2: %d", ev.Env().Seq)
	}
	// Live event; a client ping is ignored.
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"t":"ping"}`))
	b.Publish(echo("live"))
	if ev := read(c).(*protocol.EchoEvent); ev.Text != "live" || ev.Seq != 4 {
		t.Fatalf("live %+v", ev)
	}

	// Fresh connection: no replay.
	c2, _, err := websocket.Dial(ctx, ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hello := read(c2).(*protocol.HelloEvent); hello.Replayed {
		t.Fatal("fresh connection must not be replayed")
	}
	if _, ok := read(c2).(*protocol.State); !ok {
		t.Fatal("second frame must be the state")
	}

	// Shutdown closes with 4003.
	close(done)
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != protocol.CloseSessionStopping {
		t.Fatalf("close status %v", err)
	}
	if _, _, err := websocket.Dial(ctx, ws+"?after=x", nil); err == nil {
		t.Fatal("bad after should fail the upgrade")
	}
}

func TestLogWriterRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.log")
	w := NewLogWriter(path, nil)
	w.MaxSize = 100
	b := NewBus(nil)
	b.AddSink(w.Sink)
	for i := 0; i < 10; i++ {
		b.Publish(echo("0123456789"))
	}
	w.Close()
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cur) > 100 || len(old) > 100 || len(old) == 0 {
		t.Fatalf("sizes %d %d", len(cur), len(old))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(cur)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
	}
}
