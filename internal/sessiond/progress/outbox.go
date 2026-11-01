// Package progress is sessiond's delivery of progress to the gateway
// (backend.md §3.8): one persistent outbox of coalesced,
// generation-stamped reports and one sender that posts them until the
// gateway accepts each. The gateway applies a report any number of times
// with the same result (statuses only rise, totals are absolute, a lap
// counts once per id), so a report sent twice is harmless and none is
// acknowledged beyond its HTTP answer.
package progress

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
)

// merge folds later into e as the gateway would apply both: the higher
// status wins and keeps its time, the latest totals win, lap fields stay
// once set, and the generation is the latest. Reports of two different
// laps are never merged (sameLap).
func merge(e, later protocol.ProgressReport) protocol.ProgressReport {
	if protocol.ProgressRank(later.Status) > protocol.ProgressRank(e.Status) {
		e.Status, e.At = later.Status, later.At
	}
	if later.HintsUsed != nil {
		e.HintsUsed = later.HintsUsed
	}
	if later.Resets != nil {
		e.Resets = later.Resets
	}
	if later.LapMs > 0 {
		e.LapMs, e.LapID = later.LapMs, later.LapID
	}
	e.Generation = later.Generation
	return e
}

// sameLap reports whether e and later may be merged: they are not the
// finished laps of two different lap ids, each of which the gateway
// counts.
func sameLap(e, later protocol.ProgressReport) bool {
	return e.LapID == "" || later.LapID == "" || e.LapID == later.LapID
}

// Snapshot is the outbox's persistent state: the reset generations
// applied and the reports not yet accepted, as state.json keeps them.
type Snapshot struct {
	Generations protocol.ProgressGenerations
	Entries     []protocol.ProgressReport
}

// item is a queued report; rev is unique among the queued items and
// changes whenever the report does, so the sender removes only what it
// delivered.
type item struct {
	protocol.ProgressReport
	rev uint64
}

// Retry bounds of the sender's backoff (Outbox.MinBackoff, MaxBackoff)
// and the timeout of one POST.
const (
	DefaultMinBackoff = time.Second
	DefaultMaxBackoff = 30 * time.Second
	SendTimeout       = 15 * time.Second
)

// Outbox holds the reports the gateway has not accepted yet, at most one
// per step and finished lap, in the order they were first recorded, and
// the reset generations they are stamped with. Record never blocks; Run is
// the only sender. Lock order: callers may hold their own locks (the
// session core's) while calling it; it calls Send and Persist holding
// none of its own.
type Outbox struct {
	// Send posts one report; a gwclient.StatusError below 500 rejects it
	// for good, anything else is retried.
	Send func(context.Context, protocol.ProgressReport) error
	// Persist writes state.json after a delivery changed the entries
	// (callers that change them otherwise persist themselves).
	Persist func()
	Log     *slog.Logger
	// MinBackoff and MaxBackoff bound the retry delay (defaults 1 s and
	// 30 s).
	MinBackoff, MaxBackoff time.Duration

	mu          sync.Mutex
	entries     []item
	generations protocol.ProgressGenerations
	rev         uint64
	wake        chan struct{}
	removed     chan struct{}
	// running is set by Run unless Close came first (closed); Close waits
	// for done, closed when Run returned, only when Run started.
	running, closed bool
	quit            chan struct{}
	done            chan struct{}
}

// New returns an outbox holding s (state.json); Run sends its entries.
func New(s Snapshot, send func(context.Context, protocol.ProgressReport) error, persist func(), log *slog.Logger) *Outbox {
	if log == nil {
		log = slog.Default()
	}
	o := &Outbox{Send: send, Persist: persist, Log: log, generations: maps.Clone(s.Generations),
		wake: make(chan struct{}, 1), removed: make(chan struct{}, 1),
		quit: make(chan struct{}), done: make(chan struct{})}
	if o.generations == nil {
		o.generations = protocol.ProgressGenerations{}
	}
	for _, e := range s.Entries {
		o.recordLocked(e)
	}
	return o
}

func signal(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// Record queues e stamped with its lesson's generation, folded into the
// step's last queued report when there is one of the same lap.
func (o *Outbox) Record(e protocol.ProgressReport) {
	o.mu.Lock()
	e.Generation = o.generations.ForLesson(e.LessonID)
	o.recordLocked(e)
	o.mu.Unlock()
	signal(o.wake)
}

func (o *Outbox) recordLocked(e protocol.ProgressReport) {
	o.rev++
	for i := len(o.entries) - 1; i >= 0; i-- {
		if q := o.entries[i]; q.LessonID == e.LessonID && q.StepID == e.StepID {
			if sameLap(q.ProgressReport, e) {
				o.entries[i] = item{merge(q.ProgressReport, e), o.rev}
				return
			}
			break
		}
	}
	o.entries = append(o.entries, item{e, o.rev})
}

// Newer reports whether a drop of scope at generation would apply:
// generation zero (a local drop), or newer than the scope's.
func (o *Outbox) Newer(scope string, generation int64) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return generation == 0 || generation > o.generations.ForLesson(scope)
}

// Drop applies a gateway reset of scope (a lesson, "" every lesson):
// the generation rises, unless it is not newer (then nothing changes),
// and the scope's entries older than it go. Generation zero drops the
// scope's entries without a generation. It returns whether it applied
// and, if so, the generations it replaced.
func (o *Outbox) Drop(scope string, generation int64) (protocol.ProgressGenerations, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if generation > 0 && generation <= o.generations.ForLesson(scope) {
		return nil, false
	}
	prev := maps.Clone(o.generations)
	in := func(lesson string) bool { return scope == "" || lesson == scope }
	if generation > 0 {
		o.generations[scope] = generation
	}
	o.entries = slices.DeleteFunc(o.entries, func(it item) bool {
		return in(it.LessonID) && (generation == 0 || it.Generation < generation)
	})
	return prev, true
}

// Sync adopts the gateway's generations exactly, lower ones included (a
// store replaced by an older one), and removes the entries below them.
// Newer generations must have been dropped first (the session core
// forgets their outcomes); the core runs both in one op, so no drop
// comes between the gateway's answer and its adoption.
func (o *Outbox) Sync(generations protocol.ProgressGenerations) {
	o.mu.Lock()
	defer o.mu.Unlock()
	next := maps.Clone(generations)
	if next == nil {
		next = protocol.ProgressGenerations{}
	}
	o.generations = next
	o.entries = slices.DeleteFunc(o.entries, func(it item) bool { return it.Generation < next.ForLesson(it.LessonID) })
}

// Snapshot returns the persistent state.
func (o *Outbox) Snapshot() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	s := Snapshot{Generations: maps.Clone(o.generations), Entries: make([]protocol.ProgressReport, len(o.entries))}
	for i, it := range o.entries {
		s.Entries[i] = it.ProgressReport
	}
	return s
}

// Len returns the number of queued entries.
func (o *Outbox) Len() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.entries)
}

func (o *Outbox) head() (item, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.entries) == 0 {
		return item{}, false
	}
	return o.entries[0], true
}

// delivered removes it (accepted or rejected) when it is unchanged
// since it was sent.
func (o *Outbox) delivered(it item) {
	o.mu.Lock()
	i := slices.IndexFunc(o.entries, func(x item) bool { return x.rev == it.rev })
	done := i >= 0
	if done {
		o.entries = slices.Delete(o.entries, i, i+1)
	}
	o.mu.Unlock()
	if done {
		signal(o.removed)
		if o.Persist != nil {
			o.Persist()
		}
	}
}

// Run sends the head entry until the gateway accepts or rejects it, then
// the next, until ctx ends or Close stops it: each POST is bounded by
// SendTimeout, failures are retried with a backoff from MinBackoff
// doubling to MaxBackoff, without a deadline. It runs once per outbox;
// after Close it returns at once.
func (o *Outbox) Run(ctx context.Context) {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.running = true
	o.mu.Unlock()
	defer close(o.done)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-o.quit:
			cancel()
		case <-ctx.Done():
		}
	}()
	minDelay, maxDelay := o.MinBackoff, o.MaxBackoff
	if minDelay <= 0 {
		minDelay = DefaultMinBackoff
	}
	if maxDelay <= 0 {
		maxDelay = DefaultMaxBackoff
	}
	minDelay = min(minDelay, maxDelay)
	delay := minDelay
	for ctx.Err() == nil {
		it, ok := o.head()
		if !ok {
			select {
			case <-ctx.Done():
			case <-o.wake:
			}
			continue
		}
		sctx, stop := context.WithTimeout(ctx, SendTimeout)
		err := o.Send(sctx, it.ProgressReport)
		stop()
		if ctx.Err() != nil {
			return
		}
		if err == nil || gwclient.Rejected(err) {
			if err != nil {
				o.Log.Warn("progress report rejected", "lesson", it.LessonID, "step", it.StepID, "err", err)
			}
			o.delivered(it)
			delay = minDelay
			continue
		}
		o.Log.Warn("progress report failed; retrying", "lesson", it.LessonID, "step", it.StepID, "err", err, "in", delay)
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
		}
		delay = min(delay*2, maxDelay)
	}
}

// Flush sends each queued entry once, in order, from the caller's
// goroutine, as Run does with a gateway that answers at once; an entry
// that fails stays queued. It is for tests of the components that record
// (in sessiond, Run is the only sender).
func (o *Outbox) Flush(ctx context.Context) {
	for range o.Len() {
		it, ok := o.head()
		if !ok {
			return
		}
		err := o.Send(ctx, it.ProgressReport)
		if err != nil && !gwclient.Rejected(err) {
			return
		}
		o.delivered(it)
	}
}

// Close waits until the outbox is empty or timeout passed, then stops
// Run and waits for it when it started; an outbox without Send (no
// gateway) has no Run and returns at once. What is left stays in
// state.json for the next start. Close is called once.
func (o *Outbox) Close(timeout time.Duration) {
	if o.Send == nil {
		return
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
wait:
	for o.Len() > 0 {
		select {
		case <-o.removed:
		case <-t.C:
			break wait
		}
	}
	o.mu.Lock()
	o.closed = true
	running := o.running
	o.mu.Unlock()
	close(o.quit)
	if running {
		<-o.done
	}
}
