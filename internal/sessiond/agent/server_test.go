package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

type recorder struct {
	mu      sync.Mutex
	hellos  int
	events  []protocol.Event
	frames  []protocol.AgentFrameMsg
	discons int
}

func (r *recorder) OnHello(*protocol.AgentHelloMsg) { r.mu.Lock(); r.hellos++; r.mu.Unlock() }
func (r *recorder) OnEvent(ev protocol.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}
func (r *recorder) OnFrame(f *protocol.AgentFrameMsg) {
	r.mu.Lock()
	r.frames = append(r.frames, *f)
	r.mu.Unlock()
}
func (r *recorder) OnDisconnect(error) { r.mu.Lock(); r.discons++; r.mu.Unlock() }

func sockPath(t *testing.T) string {
	t.Helper()
	// Unix socket paths are limited to ~108 bytes; t.TempDir can be long.
	dir, err := os.MkdirTemp("", "pgag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "agent.sock")
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHelloEventsPingAndCalls(t *testing.T) {
	rec := &recorder{}
	path := sockPath(t)
	s, err := Listen(path, rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", st.Mode())
	}
	if err := s.Call(context.Background(), protocol.AgentOpRescue, nil, 0, nil); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("call without agent: %v", err)
	}

	var inflight, maxInflight atomic.Int32
	fake := &Fake{Path: path, PingInterval: 20 * time.Millisecond, Hello: protocol.AgentHelloMsg{PID: 42, Modules: []string{"base", "agent"}}}
	fake.Handle = func(op protocol.AgentOp, args json.RawMessage) (any, error) {
		n := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			m := maxInflight.Load()
			if n <= m || maxInflight.CompareAndSwap(m, n) {
				break
			}
		}
		switch op {
		case protocol.AgentOpCheck:
			var a protocol.AgentCheckArgs
			_ = json.Unmarshal(args, &a)
			res := protocol.AgentCheckResult{}
			for _, f := range a.Forms {
				switch f {
				case "t":
					res.Results = append(res.Results, protocol.True)
				case "nil":
					res.Results = append(res.Results, protocol.False)
				default:
					res.Results = append(res.Results, protocol.Unknown)
				}
			}
			time.Sleep(10 * time.Millisecond)
			return res, nil
		case protocol.AgentOpEval:
			var a protocol.AgentEvalArgs
			_ = json.Unmarshal(args, &a)
			if a.Form == "(sleep-for 5)" {
				time.Sleep(time.Second)
			}
			if a.Form == "(error)" {
				return nil, errors.New("wrong-type-argument")
			}
			return protocol.AgentEvalResult{Value: "*scratch*"}, nil
		}
		return DefaultFakeHandler(op, args)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go fake.Run(ctx)

	h, err := s.WaitHello(ctx, 0)
	if err != nil || h.PID != 42 || h.Emacs != "31.1" {
		t.Fatalf("hello %+v %v", h, err)
	}
	if !s.Connected() || s.HelloCount() != 1 {
		t.Fatal("not connected")
	}

	// Events and frames reach the handler in order.
	_ = fake.Send(protocol.KeyEvent{Envelope: protocol.Envelope{T: protocol.EventKey}, Keys: "C-x C-f", Cmd: "find-file"})
	_ = fake.Send(map[string]any{"t": "state", "buf": "main.rs", "layout": []any{"vsplit", 3, 7}})
	_ = fake.Send(protocol.AgentFrameMsg{T: protocol.AgentFrame, Event: protocol.FrameClosed, Kind: protocol.FrameTerminal})
	_ = fake.Send(map[string]any{"t": "bogus"})
	waitFor(t, "events", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.events) == 2 && len(rec.frames) == 1
	})
	rec.mu.Lock()
	if k := rec.events[0].(*protocol.KeyEvent); k.Cmd != "find-file" {
		t.Fatalf("key %+v", k)
	}
	if st := rec.events[1].(*protocol.State); st.Buf != "main.rs" || st.Layout.String() != "(vsplit 3 7)" {
		t.Fatalf("state %+v", st)
	}
	rec.mu.Unlock()

	// Pings keep LastPing fresh.
	before := s.LastPing()
	waitFor(t, "ping", func() bool { return s.LastPing().After(before) })

	// Correlated, decoded reply.
	var res protocol.AgentCheckResult
	if err := s.Call(ctx, protocol.AgentOpCheck, protocol.AgentCheckArgs{Forms: []string{"t", "nil", "(boom)"}}, 0, &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 3 || res.Results[0] != protocol.True || res.Results[1] != protocol.False || res.Results[2] != protocol.Unknown {
		t.Fatalf("check %+v", res)
	}
	// Agent-side error.
	err = s.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: "(error)"}, 0, nil)
	var ce *CallError
	if !errors.As(err, &ce) || ce.Msg != "wrong-type-argument" {
		t.Fatalf("call error %v", err)
	}
	// Timeout: the reply arrives after timeout+grace.
	start := time.Now()
	err = s.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: "(sleep-for 5)"}, 100*time.Millisecond, nil)
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 900*time.Millisecond {
		t.Fatalf("timeout %v after %v", err, time.Since(start))
	}
	// The late reply is discarded and the next call gets its own answer.
	time.Sleep(600 * time.Millisecond)
	var ev protocol.AgentEvalResult
	if err := s.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: "(buffer-name)"}, 0, &ev); err != nil || ev.Value != "*scratch*" {
		t.Fatalf("after timeout: %v %v", ev, err)
	}

	// Concurrent callers are serialised: the agent never has two calls.
	maxInflight.Store(0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var r protocol.AgentCheckResult
			if err := s.Call(ctx, protocol.AgentOpCheck, protocol.AgentCheckArgs{Forms: []string{"t"}}, 0, &r); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maxInflight.Load() != 1 {
		t.Fatalf("calls overlapped: %d in flight", maxInflight.Load())
	}
	// Calls carry ids and timeouts.
	calls := fake.Calls()
	if calls[0].TimeoutMs != 2000 || calls[0].ID == 0 {
		t.Fatalf("call %+v", calls[0])
	}
	// A cancelled context returns promptly.
	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	if err := s.Call(cctx, protocol.AgentOpRescue, nil, 0, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestReplaceConnectionAndLimits(t *testing.T) {
	rec := &recorder{}
	path := sockPath(t)
	s, err := Listen(path, rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := &Fake{Path: path}
	go first.Run(ctx)
	if _, err := s.WaitHello(ctx, 0); err != nil {
		t.Fatal(err)
	}
	second := &Fake{Path: path, Hello: protocol.AgentHelloMsg{PID: 2}}
	go second.Run(ctx)
	h, err := s.WaitHello(ctx, 1)
	if err != nil || h.PID != 2 {
		t.Fatalf("second hello %+v %v", h, err)
	}
	// The replaced connection does not count as a disconnect.
	time.Sleep(50 * time.Millisecond)
	rec.mu.Lock()
	if rec.discons != 0 || rec.hellos != 2 {
		t.Fatalf("discons=%d hellos=%d", rec.discons, rec.hellos)
	}
	rec.mu.Unlock()

	// A line over 1 MB drops the connection.
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte(`{"t":"hello","protocol":1,"agent":"0.1.0","emacs":"31.1","pid":3}` + "\n"))
	if _, err := s.WaitHello(ctx, 2); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = c.Write([]byte(`{"t":"echo","text":"` + strings.Repeat("x", protocol.MaxAgentLine+10) + "\"}\n"))
	}()
	waitFor(t, "disconnect", func() bool { return !s.Connected() })
	rec.mu.Lock()
	if rec.discons != 1 {
		t.Fatalf("discons %d", rec.discons)
	}
	rec.mu.Unlock()
	// Disconnect fails calls fast.
	if err := s.Call(ctx, protocol.AgentOpRescue, nil, 0, nil); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("after disconnect: %v", err)
	}
	s.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("socket file not removed")
	}
	if err := s.Call(ctx, protocol.AgentOpRescue, nil, 0, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("after close: %v", err)
	}
}

func TestCallFailsWhenAgentDiesMidCall(t *testing.T) {
	path := sockPath(t)
	s, err := Listen(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	fake := &Fake{Path: path, Handle: func(op protocol.AgentOp, _ json.RawMessage) (any, error) {
		cancel() // the agent goes away while the call is in flight
		time.Sleep(time.Second)
		return nil, nil
	}}
	go fake.Run(ctx)
	if _, err := s.WaitHello(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = s.Call(context.Background(), protocol.AgentOpSnapshot, nil, 5*time.Second, nil)
	if !errors.Is(err, ErrNotConnected) || time.Since(start) > 2*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
}
