package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// FakeHandler answers one call: it returns the reply value (marshalled to
// JSON) or an error (reply ok=false).
type FakeHandler func(op protocol.AgentOp, args json.RawMessage) (any, error)

// Fake is a minimal agent: it connects to the socket, sends hello, pings at
// PingInterval, and answers calls with Handle (default: DefaultFakeHandler).
// Send emits arbitrary agent messages (key, state, frame, …).
type Fake struct {
	Path         string
	Hello        protocol.AgentHelloMsg
	PingInterval time.Duration
	Handle       FakeHandler
	// Initial messages are sent right after hello (for example a frame
	// opened message).
	Initial []any
	// Silent stops pings (simulates a hung Emacs).
	silent bool

	mu    sync.Mutex
	c     net.Conn
	calls []protocol.AgentCallMsg
}

// DefaultFakeHandler answers every op with a plausible empty result.
func DefaultFakeHandler(op protocol.AgentOp, args json.RawMessage) (any, error) {
	switch op {
	case protocol.AgentOpCheck:
		var a protocol.AgentCheckArgs
		_ = json.Unmarshal(args, &a)
		res := protocol.AgentCheckResult{Results: make([]protocol.Truth, len(a.Forms))}
		for i := range res.Results {
			res.Results[i] = protocol.Unknown
		}
		return res, nil
	case protocol.AgentOpRescue:
		return protocol.AgentRescueResult{Did: []string{}}, nil
	case protocol.AgentOpSnapshot:
		return protocol.WindowSpec{Files: []protocol.FileSpec{}, Layout: "single", TakenAt: time.Now().UnixMilli()}, nil
	case protocol.AgentOpEval:
		return protocol.AgentEvalResult{Value: nil}, nil
	case protocol.AgentOpDescribe:
		return protocol.DescribeResult{}, nil
	}
	return map[string]any{}, nil
}

// Run connects and serves until ctx ends or the connection drops.
func (f *Fake) Run(ctx context.Context) error {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", f.Path)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.c = c
	f.mu.Unlock()
	defer c.Close()
	go func() {
		<-ctx.Done()
		_ = c.Close()
	}()
	h := f.Hello
	h.T = protocol.AgentHello
	if h.Protocol == 0 {
		h.Protocol = protocol.Version
	}
	if h.Agent == "" {
		h.Agent = protocol.AgentVersion
	}
	if h.Emacs == "" {
		h.Emacs = "31.1"
	}
	if err := f.Send(h); err != nil {
		return err
	}
	for _, m := range f.Initial {
		if err := f.Send(m); err != nil {
			return err
		}
	}
	interval := f.PingInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				f.mu.Lock()
				silent := f.silent
				f.mu.Unlock()
				if !silent {
					if err := f.Send(protocol.AgentPingMsg{T: protocol.AgentPing}); err != nil {
						return
					}
				}
			}
		}
	}()
	handle := f.Handle
	if handle == nil {
		handle = DefaultFakeHandler
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 64<<10), protocol.MaxAgentLine+1)
	for sc.Scan() {
		var head struct {
			T protocol.AgentMsgType `json:"t"`
		}
		if json.Unmarshal(sc.Bytes(), &head) != nil || head.T != protocol.AgentCall {
			continue
		}
		var m protocol.AgentCallMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		f.mu.Lock()
		f.calls = append(f.calls, m)
		f.mu.Unlock()
		go func(m protocol.AgentCallMsg) {
			v, err := handle(m.Op, m.Args)
			reply := protocol.AgentReplyMsg{T: protocol.AgentReply, ID: m.ID, OK: err == nil}
			if err != nil {
				reply.Error = err.Error()
			} else {
				b, _ := json.Marshal(v)
				reply.Value = b
			}
			_ = f.Send(reply)
		}(m)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("agent fake: connection closed")
}

// Send writes one message.
func (f *Fake) Send(v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.c == nil {
		return ErrNotConnected
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = f.c.Write(append(b, '\n'))
	return err
}

// SetSilent stops (true) or resumes (false) pings.
func (f *Fake) SetSilent(v bool) {
	f.mu.Lock()
	f.silent = v
	f.mu.Unlock()
}

// Calls returns the calls received so far.
func (f *Fake) Calls() []protocol.AgentCallMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.AgentCallMsg(nil), f.calls...)
}
