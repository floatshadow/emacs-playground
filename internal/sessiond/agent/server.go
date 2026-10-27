package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Timeouts (backend.md §3.5).
const (
	DefaultTimeout = 2 * time.Second
	MaxEvalTimeout = 10 * time.Second
	// grace is added to the agent-side timeout before sessiond gives up.
	grace        = 500 * time.Millisecond
	writeTimeout = 2 * time.Second
)

// Errors returned by Call.
var (
	ErrNotConnected = errors.New("agent: not connected")
	ErrTimeout      = errors.New("agent: call timed out")
	ErrClosed       = errors.New("agent: server closed")
)

// CallError is an error reported by the agent (reply with ok=false).
type CallError struct {
	Op  protocol.AgentOp
	Msg string
}

func (e *CallError) Error() string { return fmt.Sprintf("agent %s: %s", e.Op, e.Msg) }

// Handler receives what the agent sends. Methods are called from the
// connection's reader goroutine, in order; they must not block for long and
// must not call Server.Call synchronously.
type Handler interface {
	// OnHello is called for every hello (first connection and reconnects).
	OnHello(h *protocol.AgentHelloMsg)
	// OnEvent receives *protocol.KeyEvent, *protocol.State,
	// *protocol.EchoEvent and *protocol.MinibufferEvent, unstamped.
	OnEvent(ev protocol.Event)
	// OnFrame receives frame opened/closed messages.
	OnFrame(f *protocol.AgentFrameMsg)
	// OnDisconnect is called when the current connection ends.
	OnDisconnect(err error)
}

// Server is the agent socket server.
type Server struct {
	path    string
	handler Handler
	log     *slog.Logger

	ln     net.Listener
	mu     sync.Mutex
	cur    *conn
	hello  *protocol.AgentHelloMsg
	helloN uint64
	// helloCh is closed and replaced on every hello.
	helloCh chan struct{}

	calls    chan *call
	nextID   atomic.Int64
	lastPing atomic.Int64 // unix ms of the last ping (or hello)
	closed   chan struct{}
	once     sync.Once
	wg       sync.WaitGroup
}

type conn struct {
	c       net.Conn
	wmu     sync.Mutex
	replies chan *protocol.AgentReplyMsg
	done    chan struct{}
}

type call struct {
	ctx     context.Context
	op      protocol.AgentOp
	args    any
	timeout time.Duration
	result  chan callResult
}

type callResult struct {
	value json.RawMessage
	err   error
}

// Listen creates the socket at path (removing a stale one), owner-only, and
// starts accepting. handler may be nil.
func Listen(path string, handler Handler, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("agent socket: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("agent socket: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	s := &Server{
		path: path, handler: handler, log: log, ln: ln,
		helloCh: make(chan struct{}), calls: make(chan *call), closed: make(chan struct{}),
	}
	s.wg.Add(2)
	go s.acceptLoop()
	go s.callLoop()
	return s, nil
}

// SetHandler replaces the handler (wiring order in sessiond).
func (s *Server) SetHandler(h Handler) {
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
}

func (s *Server) getHandler() Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handler
}

// Path returns the socket path.
func (s *Server) Path() string { return s.path }

// Close stops accepting, drops the connection and fails pending calls.
func (s *Server) Close() error {
	s.once.Do(func() {
		close(s.closed)
		_ = s.ln.Close()
		s.mu.Lock()
		if s.cur != nil {
			_ = s.cur.c.Close()
		}
		s.mu.Unlock()
		_ = os.Remove(s.path)
	})
	s.wg.Wait()
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			s.log.Warn("agent accept", "err", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		nc := &conn{c: c, replies: make(chan *protocol.AgentReplyMsg, 4), done: make(chan struct{})}
		s.mu.Lock()
		old := s.cur
		s.cur = nc
		s.mu.Unlock()
		if old != nil {
			s.log.Info("agent connection replaced")
			_ = old.c.Close()
		}
		s.wg.Add(1)
		go s.readLoop(nc)
	}
}

// Connected reports whether an agent connection is open.
func (s *Server) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur != nil
}

// HelloCount returns how many hellos have been received.
func (s *Server) HelloCount() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.helloN
}

// WaitHello waits until more than after hellos have been received (pass
// HelloCount() taken before (re)starting Emacs).
func (s *Server) WaitHello(ctx context.Context, after uint64) (*protocol.AgentHelloMsg, error) {
	for {
		s.mu.Lock()
		n, h, ch := s.helloN, s.hello, s.helloCh
		s.mu.Unlock()
		if n > after {
			return h, nil
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.closed:
			return nil, ErrClosed
		}
	}
}

// LastPing returns when the agent last pinged (or said hello); zero when
// never.
func (s *Server) LastPing() time.Time {
	ms := s.lastPing.Load()
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func (s *Server) readLoop(nc *conn) {
	defer s.wg.Done()
	defer close(nc.done)
	sc := bufio.NewScanner(nc.c)
	sc.Buffer(make([]byte, 0, 64<<10), protocol.MaxAgentLine+1)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		s.dispatch(nc, line)
	}
	err := sc.Err()
	if errors.Is(err, bufio.ErrTooLong) {
		s.log.Warn("agent line longer than 1 MB; dropping the connection")
	}
	_ = nc.c.Close()
	s.mu.Lock()
	current := s.cur == nc
	if current {
		s.cur = nil
	}
	s.mu.Unlock()
	if current {
		if err == nil {
			err = io.EOF
		}
		s.log.Info("agent disconnected", "err", err)
		if h := s.getHandler(); h != nil {
			h.OnDisconnect(err)
		}
	}
}

func (s *Server) dispatch(nc *conn, line []byte) {
	msg, err := protocol.DecodeAgentMessage(line)
	if err != nil {
		s.log.Warn("agent message", "err", err)
		return
	}
	h := s.getHandler()
	switch m := msg.(type) {
	case *protocol.AgentHelloMsg:
		if m.Protocol != protocol.Version {
			s.log.Warn("agent protocol mismatch", "agent", m.Protocol, "sessiond", protocol.Version)
		}
		if major(m.Agent) != major(protocol.AgentVersion) {
			s.log.Warn("agent version mismatch", "agent", m.Agent, "expected", protocol.AgentVersion)
		}
		s.lastPing.Store(time.Now().UnixMilli())
		s.mu.Lock()
		s.hello = m
		s.mu.Unlock()
		s.log.Info("agent hello", "agent", m.Agent, "emacs", m.Emacs, "pid", m.PID, "modules", m.Modules)
		// The handler sees the hello before WaitHello callers wake up, so
		// state derived from it (readiness) is in place for them.
		if h != nil {
			h.OnHello(m)
		}
		s.mu.Lock()
		s.helloN++
		close(s.helloCh)
		s.helloCh = make(chan struct{})
		s.mu.Unlock()
	case *protocol.AgentPingMsg:
		s.lastPing.Store(time.Now().UnixMilli())
		_ = s.write(nc, protocol.AgentPongMsg{T: protocol.AgentPong})
	case *protocol.AgentReplyMsg:
		select {
		case nc.replies <- m:
		default:
			// Nobody waiting (late reply after a timeout): drop.
		}
	case *protocol.AgentFrameMsg:
		if h != nil {
			h.OnFrame(m)
		}
	case protocol.Event:
		if h != nil {
			h.OnEvent(m)
		}
	}
}

func major(v string) string {
	if i := strings.IndexByte(v, '.'); i >= 0 {
		return v[:i]
	}
	return v
}

func (s *Server) write(nc *conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	nc.wmu.Lock()
	defer nc.wmu.Unlock()
	_ = nc.c.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err = nc.c.Write(b)
	return err
}

// Call runs op in the agent and decodes the reply value into out (when out
// is non-nil). timeout 0 means DefaultTimeout; it is capped at
// MaxEvalTimeout.
func (s *Server) Call(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration, out any) error {
	raw, err := s.callRaw(ctx, op, args, timeout)
	if err != nil {
		return err
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("agent %s: decoding reply: %w", op, err)
		}
	}
	return nil
}

// callRaw is Call returning the undecoded value.
func (s *Server) callRaw(ctx context.Context, op protocol.AgentOp, args any, timeout time.Duration) (json.RawMessage, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxEvalTimeout {
		timeout = MaxEvalTimeout
	}
	c := &call{ctx: ctx, op: op, args: args, timeout: timeout, result: make(chan callResult, 1)}
	select {
	case s.calls <- c:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, ErrClosed
	}
	select {
	case r := <-c.result:
		return r.value, r.err
	case <-s.closed:
		return nil, ErrClosed
	}
}

// callLoop serialises calls.
func (s *Server) callLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.closed:
			return
		case c := <-s.calls:
			v, err := s.run(c)
			c.result <- callResult{v, err}
		}
	}
}

func (s *Server) run(c *call) (json.RawMessage, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	nc := s.cur
	s.mu.Unlock()
	if nc == nil {
		return nil, ErrNotConnected
	}
	// Drop stale replies from earlier timed-out calls.
	for len(nc.replies) > 0 {
		<-nc.replies
	}
	id := s.nextID.Add(1)
	msg, err := protocol.NewAgentCall(id, c.op, c.args, int(c.timeout/time.Millisecond))
	if err != nil {
		return nil, err
	}
	if err := s.write(nc, msg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConnected, err)
	}
	timer := time.NewTimer(c.timeout + grace)
	defer timer.Stop()
	for {
		select {
		case r := <-nc.replies:
			if r.ID != id {
				continue
			}
			if !r.OK {
				msg := r.Error
				if msg == "" {
					msg = "failed"
				}
				return nil, &CallError{Op: c.op, Msg: msg}
			}
			return r.Value, nil
		case <-nc.done:
			return nil, ErrNotConnected
		case <-timer.C:
			return nil, ErrTimeout
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		case <-s.closed:
			return nil, ErrClosed
		}
	}
}
