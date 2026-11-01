package progress

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/gwclient"
)

// gateway is a fake gateway's progress endpoint: it answers with fail
// (nil: accept) and applies accepted reports as progress.Service.Apply
// does (stale generations discarded, statuses only rise, totals
// absolute, a lap counted once per id).
type gateway struct {
	mu       sync.Mutex
	fail     func(protocol.ProgressReport) error
	gens     protocol.ProgressGenerations
	received []protocol.ProgressReport
	status   map[string]protocol.ProgressStatus
	hints    map[string]int
	laps     map[string]int
	lastLap  map[string]string
}

func (g *gateway) send(_ context.Context, r protocol.ProgressReport) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fail != nil {
		if err := g.fail(r); err != nil {
			return err
		}
	}
	g.received = append(g.received, r)
	if r.Generation < g.gens.ForLesson(r.LessonID) {
		return nil
	}
	if g.status == nil {
		g.status, g.hints, g.laps, g.lastLap = map[string]protocol.ProgressStatus{}, map[string]int{}, map[string]int{}, map[string]string{}
	}
	k := r.LessonID + "/" + r.StepID
	if protocol.ProgressRank(r.Status) > protocol.ProgressRank(g.status[k]) {
		g.status[k] = r.Status
	}
	if r.HintsUsed != nil {
		g.hints[k] = *r.HintsUsed
	}
	if r.LapMs > 0 && g.lastLap[r.LessonID] != r.LapID {
		g.laps[r.LessonID]++
		g.lastLap[r.LessonID] = r.LapID
	}
	return nil
}

func (g *gateway) steps() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, r := range g.received {
		out = append(out, r.StepID+":"+string(r.Status))
	}
	return out
}

func unavailable() error { return &gwclient.StatusError{Status: 503} }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newOutbox(g *gateway, s Snapshot) (*Outbox, *int) {
	persisted := new(int)
	o := New(s, g.send, func() { *persisted++ }, quiet())
	return o, persisted
}

func ptr(n int) *int { return &n }

func TestOutboxRetriesUntilDelivered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		g := &gateway{fail: func(protocol.ProgressReport) error {
			if calls++; calls < 10 {
				return unavailable()
			}
			return nil
		}}
		o, persisted := newOutbox(g, Snapshot{})
		go o.Run(context.Background())
		start := time.Now()
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 100})
		o.Close(time.Hour)
		// 1+2+4+8+16+30+30+30+30 s of backoff before the tenth attempt.
		if calls != 10 || o.Len() != 0 || *persisted != 1 || time.Since(start) != 151*time.Second {
			t.Fatalf("calls %d, left %d, persisted %d, after %s", calls, o.Len(), *persisted, time.Since(start))
		}
	})
}

func TestOutboxRejectedDropsTheEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &gateway{fail: func(r protocol.ProgressReport) error {
			if r.StepID == "bad" {
				return &gwclient.StatusError{Status: 400, Problem: httpx.BadRequest("no such step")}
			}
			return nil
		}}
		o, _ := newOutbox(g, Snapshot{})
		go o.Run(context.Background())
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "bad", Status: protocol.ProgressDone})
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "good", Status: protocol.ProgressDone})
		o.Close(time.Minute)
		if got := g.steps(); !reflect.DeepEqual(got, []string{"good:done"}) || o.Len() != 0 {
			t.Fatalf("delivered %v, left %d", got, o.Len())
		}
	})
}

// A gateway down during three completions gets each step once, in the
// order the steps were first recorded, with the final status.
func TestOutboxCoalescesWhileTheGatewayIsDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		down := true
		g := &gateway{fail: func(protocol.ProgressReport) error {
			if down {
				return unavailable()
			}
			return nil
		}}
		o, _ := newOutbox(g, Snapshot{Generations: protocol.ProgressGenerations{"a/b": 7}})
		go o.Run(context.Background())
		for i, step := range []string{"one", "two", "three"} {
			o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: step, Status: protocol.ProgressInProgress, At: int64(10 * i)})
			o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: step, Status: protocol.ProgressDone, At: int64(10*i + 5), HintsUsed: ptr(i)})
			time.Sleep(time.Second)
		}
		// A lower status later keeps the done and its time; totals are the
		// latest.
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressInProgress, At: 99, Resets: ptr(2)})
		synctest.Wait()
		g.mu.Lock()
		down = false
		g.mu.Unlock()
		o.Close(time.Minute)
		g.mu.Lock()
		defer g.mu.Unlock()
		var got []protocol.ProgressReport
		for _, r := range g.received {
			got = append(got, r)
		}
		want := []protocol.ProgressReport{
			{Generation: 7, LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 5, HintsUsed: ptr(0), Resets: ptr(2)},
			{Generation: 7, LessonID: "a/b", StepID: "two", Status: protocol.ProgressDone, At: 15, HintsUsed: ptr(1)},
			{Generation: 7, LessonID: "a/b", StepID: "three", Status: protocol.ProgressDone, At: 25, HintsUsed: ptr(2)},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("delivered %+v", got)
		}
	})
}

// An entry that changes while its POST is in flight stays queued and is
// sent again; every report recorded after a delivery is sent.
func TestOutboxInFlight(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var o *Outbox
		first := true
		g := &gateway{fail: func(protocol.ProgressReport) error {
			if first {
				first = false
				o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 2})
			}
			return nil
		}}
		o, _ = newOutbox(g, Snapshot{})
		go o.Run(context.Background())
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressInProgress, At: 1})
		synctest.Wait()
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressInProgress, At: 3})
		synctest.Wait()
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressInProgress, At: 5, HintsUsed: ptr(1)})
		synctest.Wait()
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone, At: 6})
		o.Close(time.Minute)
		want := []string{"one:in-progress", "one:done", "one:in-progress", "one:in-progress", "one:done"}
		if got := g.steps(); !reflect.DeepEqual(got, want) {
			t.Fatalf("delivered %v, want %v", got, want)
		}
	})
}

func TestOutboxDropByGeneration(t *testing.T) {
	for _, scope := range []string{"a/b", ""} {
		t.Run("scope="+scope, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				down := true
				g := &gateway{fail: func(protocol.ProgressReport) error {
					if down {
						return unavailable()
					}
					return nil
				}}
				o, _ := newOutbox(g, Snapshot{})
				go o.Run(context.Background())
				o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone})
				o.Record(protocol.ProgressReport{LessonID: "c/d", StepID: "one", Status: protocol.ProgressDone})
				synctest.Wait()
				if prev, ok := o.Drop(scope, 4); !ok || len(prev) != 0 {
					t.Fatalf("drop: %v, replaced %v", ok, prev)
				}
				if _, ok := o.Drop(scope, 4); ok {
					t.Fatal("a drop applied twice")
				}
				if _, ok := o.Drop(scope, 3); ok {
					t.Fatal("an older drop applied")
				}
				o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "two", Status: protocol.ProgressDone})
				g.mu.Lock()
				down = false
				g.mu.Unlock()
				o.Close(time.Minute)
				got := map[string]int64{}
				for _, r := range g.received {
					got[r.LessonID+"/"+r.StepID] = r.Generation
				}
				want := map[string]int64{"a/b/two": 4}
				if scope == "a/b" {
					want["c/d/one"] = 0
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("delivered %v, want %v", got, want)
				}
				if gens := o.Snapshot().Generations; gens[scope] != 4 || !o.Newer("a/b", 5) || o.Newer("a/b", 4) {
					t.Fatalf("generations %v", gens)
				}
			})
		})
	}
}

// Sync adopts the gateway's generations exactly, also lower ones (a store
// replaced), and purges entries below them.
func TestOutboxSync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &gateway{}
		o, _ := newOutbox(g, Snapshot{Generations: protocol.ProgressGenerations{"a/b": 18, "c/d": 3}})
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone})
		o.Record(protocol.ProgressReport{LessonID: "c/d", StepID: "one", Status: protocol.ProgressDone})
		o.Sync(protocol.ProgressGenerations{"c/d": 5})
		if gens := o.Snapshot().Generations; !reflect.DeepEqual(gens, protocol.ProgressGenerations{"c/d": 5}) {
			t.Fatalf("generations %v", gens)
		}
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "two", Status: protocol.ProgressDone})
		go o.Run(context.Background())
		o.Close(time.Minute)
		var gens []int64
		for _, r := range g.received {
			gens = append(gens, r.Generation)
		}
		if got := g.steps(); !reflect.DeepEqual(got, []string{"one:done", "two:done"}) || !reflect.DeepEqual(gens, []int64{18, 0}) {
			t.Fatalf("delivered %v with generations %v", got, gens)
		}
	})
}

// Two laps of a lesson finished while the gateway is down are two
// entries, each counted; later reports of the step fold into the last.
func TestOutboxKeepsEachLap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		down := true
		g := &gateway{fail: func(protocol.ProgressReport) error {
			if down {
				return unavailable()
			}
			return nil
		}}
		o, _ := newOutbox(g, Snapshot{})
		go o.Run(context.Background())
		for _, start := range []int64{10, 20} {
			o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "last", Status: protocol.ProgressInProgress, At: start})
			o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "last", Status: protocol.ProgressDone, At: start + 5, LapMs: 5, LapID: protocol.LapID("a/b", start)})
		}
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "last", Status: protocol.ProgressInProgress, At: 30, HintsUsed: ptr(1)})
		synctest.Wait()
		// A restart keeps both.
		o.Close(time.Second)
		again, _ := newOutbox(g, o.Snapshot())
		if again.Len() != 2 {
			t.Fatalf("entries after a restart %+v", again.Snapshot().Entries)
		}
		g.mu.Lock()
		down = false
		g.mu.Unlock()
		go again.Run(context.Background())
		again.Close(time.Minute)
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.laps["a/b"] != 2 || g.hints["a/b/last"] != 1 || again.Len() != 0 {
			t.Fatalf("laps %d, hints %d, left %d", g.laps["a/b"], g.hints["a/b/last"], again.Len())
		}
	})
}

// sessiond stopped after the gateway accepted an entry but before
// state.json lost it: the next start sends it again, once, and the
// gateway's state does not change (the lap counts once by its id).
func TestOutboxRestartResendsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &gateway{}
		o, _ := newOutbox(g, Snapshot{})
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "last", Status: protocol.ProgressDone, At: 50, LapMs: 3000, LapID: protocol.LapID("a/b", 47)})
		saved := o.Snapshot()
		go o.Run(context.Background())
		o.Close(time.Minute)
		g.mu.Lock()
		before := map[string]any{"status": g.status["a/b/last"], "laps": g.laps["a/b"]}
		g.mu.Unlock()

		again, _ := newOutbox(g, saved)
		go again.Run(context.Background())
		again.Close(time.Minute)
		g.mu.Lock()
		defer g.mu.Unlock()
		after := map[string]any{"status": g.status["a/b/last"], "laps": g.laps["a/b"]}
		if len(g.received) != 2 || g.received[1] != g.received[0] || !reflect.DeepEqual(before, after) || g.laps["a/b"] != 1 {
			t.Fatalf("received %+v; state %v then %v", g.received, before, after)
		}
	})
}

func TestOutboxCloseStopsRetrying(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := &gateway{fail: func(protocol.ProgressReport) error { return unavailable() }}
		o, _ := newOutbox(g, Snapshot{})
		go o.Run(context.Background())
		o.Record(protocol.ProgressReport{LessonID: "a/b", StepID: "one", Status: protocol.ProgressDone})
		start := time.Now()
		o.Close(5 * time.Second)
		if time.Since(start) != 5*time.Second || o.Len() != 1 {
			t.Fatalf("closed after %s with %d left", time.Since(start), o.Len())
		}
		// Without a Send, Close returns at once; with one but no Run, it
		// returns after the timeout, and a Run after Close returns at once.
		idle := New(Snapshot{Entries: []protocol.ProgressReport{{LessonID: "a/b", StepID: "one"}}}, nil, nil, quiet())
		idle.Close(time.Hour)
		start = time.Now()
		stopped, _ := newOutbox(g, Snapshot{Entries: []protocol.ProgressReport{{LessonID: "a/b", StepID: "one"}}})
		stopped.Close(time.Second)
		stopped.Run(context.Background())
		if time.Since(start) != time.Second || stopped.Len() != 1 {
			t.Fatalf("closed after %s with %d left", time.Since(start), stopped.Len())
		}
	})
}
