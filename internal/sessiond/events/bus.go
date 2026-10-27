package events

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Sizes from backend.md §3.7.
const (
	// RingSize is the number of events kept for ?after= replay.
	RingSize = 500
	// SubscriberBuffer is the per-subscriber queue length; a subscriber that
	// falls this far behind gets a gap event instead of blocking producers.
	SubscriberBuffer = 256
	// StuckWindow is the number of recent events attached to stuck reports
	// (api.md §2.5).
	StuckWindow = 200
)

// Frame is one encoded event. Seq is 0 for frames that are not part of the
// sequenced stream (per-subscriber gap events).
type Frame struct {
	Seq  int64
	T    protocol.EventType
	Data []byte
}

// Bus fans out events to subscribers. It is safe for concurrent use.
type Bus struct {
	mu        sync.Mutex
	seq       int64
	ring      [RingSize]Frame
	head      int // index of the oldest frame
	n         int // frames in the ring
	subs      map[*Subscription]struct{}
	lastState *Frame
	sinks     []func(Frame)

	// Now is the clock used for ts; tests replace it.
	Now func() time.Time
	log *slog.Logger
}

// NewBus returns an empty bus.
func NewBus(log *slog.Logger) *Bus {
	if log == nil {
		log = slog.Default()
	}
	return &Bus{subs: map[*Subscription]struct{}{}, Now: time.Now, log: log}
}

// AddSink registers f to receive every sequenced frame, in order, while the
// bus lock is held. f must not block (LogWriter queues).
func (b *Bus) AddSink(f func(Frame)) {
	b.mu.Lock()
	b.sinks = append(b.sinks, f)
	b.mu.Unlock()
}

// Publish stamps ev with the next seq and the current time, encodes it once
// and delivers it to every subscriber, the ring buffer and the sinks. It
// never blocks on slow subscribers. It returns the frame.
func (b *Bus) Publish(ev protocol.Event) Frame {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	protocol.Stamp(ev, b.seq, b.Now().UnixMilli())
	data, err := json.Marshal(ev)
	if err != nil {
		// Event structs always marshal; this is a programming error.
		b.log.Error("event encode failed", "type", ev.Type(), "err", err)
		b.seq--
		return Frame{}
	}
	f := Frame{Seq: b.seq, T: ev.Type(), Data: data}
	b.push(f)
	if f.T == protocol.EventState {
		fc := f
		b.lastState = &fc
	}
	for s := range b.subs {
		s.deliver(f, b.Now)
	}
	for _, sink := range b.sinks {
		sink(f)
	}
	return f
}

func (b *Bus) push(f Frame) {
	if b.n < RingSize {
		b.ring[(b.head+b.n)%RingSize] = f
		b.n++
		return
	}
	b.ring[b.head] = f
	b.head = (b.head + 1) % RingSize
}

// Seq returns the seq of the last published event (0 before the first).
func (b *Bus) Seq() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Recent returns up to n of the most recent sequenced frames, oldest first,
// as raw JSON (the stuck report window).
func (b *Bus) Recent(n int) []json.RawMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.n {
		n = b.n
	}
	out := make([]json.RawMessage, 0, n)
	for i := b.n - n; i < b.n; i++ {
		out = append(out, json.RawMessage(b.ring[(b.head+i)%RingSize].Data))
	}
	return out
}

// framesAfter returns ring frames with seq > after and whether the ring
// covers the whole range (after+1 .. seq). Caller holds b.mu.
func (b *Bus) framesAfter(after int64) ([]Frame, bool) {
	if after < 0 || after > b.seq {
		return nil, false
	}
	if after == b.seq {
		return nil, true
	}
	if b.n == 0 {
		return nil, false
	}
	oldest := b.ring[b.head].Seq
	if after+1 < oldest {
		return nil, false
	}
	out := make([]Frame, 0, b.seq-after)
	for i := 0; i < b.n; i++ {
		f := b.ring[(b.head+i)%RingSize]
		if f.Seq > after {
			out = append(out, f)
		}
	}
	return out, true
}

// Subscription receives frames on C. A slow subscriber's queue is reset and
// replaced by one gap frame (seq 0) covering the dropped range.
type Subscription struct {
	C   chan Frame
	bus *Bus
	// Gaps counts overflows (tests and logs).
	Gaps int
}

// Snapshot is what a new /events connection starts from.
type Snapshot struct {
	// Replayed is true when the ring buffer covered every event after the
	// requested seq; Frames then holds them in order.
	Replayed bool
	Frames   []Frame
	// State is the latest state frame (zero Seq when none yet).
	State Frame
}

// Subscribe registers a subscriber atomically with the replay snapshot, so
// that no event is lost or duplicated between replay and live delivery.
// after < 0 means "no replay requested".
func (b *Bus) Subscribe(after int64) (*Subscription, Snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := &Subscription{C: make(chan Frame, SubscriberBuffer), bus: b}
	var snap Snapshot
	if after >= 0 {
		snap.Frames, snap.Replayed = b.framesAfter(after)
		if !snap.Replayed {
			snap.Frames = nil
		}
	}
	if b.lastState != nil {
		snap.State = *b.lastState
	}
	b.subs[s] = struct{}{}
	return s, snap
}

// Close unregisters the subscription. C is not closed (producers never
// block on it, so there is nothing to wake).
func (s *Subscription) Close() {
	s.bus.mu.Lock()
	delete(s.bus.subs, s)
	s.bus.mu.Unlock()
}

// deliver is called with the bus lock held.
func (s *Subscription) deliver(f Frame, now func() time.Time) {
	select {
	case s.C <- f:
		return
	default:
	}
	// Overflow: reset the queue and replace it with a gap frame covering
	// everything that was queued plus f.
	from := f.Seq
drain:
	for {
		select {
		case old := <-s.C:
			if old.Seq != 0 && old.Seq < from {
				from = old.Seq
			}
		default:
			break drain
		}
	}
	s.Gaps++
	gap := &protocol.GapEvent{From: from, To: f.Seq}
	protocol.Stamp(gap, 0, now().UnixMilli())
	data, _ := json.Marshal(gap)
	select {
	case s.C <- Frame{T: protocol.EventGap, Data: data}:
	default:
	}
}
