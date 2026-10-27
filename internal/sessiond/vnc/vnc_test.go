package vnc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/inputtap"
)

// fakeXvnc speaks just enough RFB 3.8 for the bridge: it sends the version
// and records what the client sends.
func fakeXvnc(t *testing.T) (string, *bytes.Buffer, *sync.Mutex) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	got := &bytes.Buffer{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte("RFB 003.008\n"))
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					mu.Lock()
					got.Write(buf[:n])
					mu.Unlock()
					if err != nil {
						return
					}
				}
			}(c)
		}
	}()
	return ln.Addr().String(), got, &mu
}

func keyEvent(down bool, ks uint32) []byte {
	b := make([]byte, 8)
	b[0] = 4
	if down {
		b[1] = 1
	}
	binary.BigEndian.PutUint32(b[4:], ks)
	return b
}

func TestBridge(t *testing.T) {
	addr, got, mu := fakeXvnc(t)
	var active atomic.Bool
	var pmu sync.Mutex
	var prefixes []string
	tap := inputtap.NewTap(func(ev *protocol.PrefixEvent) {
		pmu.Lock()
		prefixes = append(prefixes, ev.Keys)
		pmu.Unlock()
	})
	h := &Handler{Addr: addr, Active: active.Load, Tap: tap}
	srv := httptest.NewServer(h)
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Not in GUI mode: 4002.
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != protocol.CloseGUINotActive {
		t.Fatalf("inactive: %v", err)
	}

	active.Store(true)
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"binary"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Sec-WebSocket-Protocol") != "binary" {
		t.Fatalf("subprotocol %q", resp.Header.Get("Sec-WebSocket-Protocol"))
	}
	typ, data, err := c.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != "RFB 003.008\n" {
		t.Fatalf("server version: %v %q", err, data)
	}
	stream := []byte("RFB 003.008\n\x01\x01")
	stream = append(stream, keyEvent(true, 0xffe3)...) // Control_L
	stream = append(stream, keyEvent(true, 'x')...)
	stream = append(stream, keyEvent(false, 'x')...)
	stream = append(stream, keyEvent(false, 0xffe3)...)
	// Split across frames.
	if err := c.Write(ctx, websocket.MessageBinary, stream[:15]); err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, stream[15:]); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := got.Len()
		mu.Unlock()
		if n == len(stream) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	if !bytes.Equal(got.Bytes(), stream) {
		t.Fatalf("server received %q", got.Bytes())
	}
	mu.Unlock()
	pmu.Lock()
	if strings.Join(prefixes, ",") != "C-x" {
		t.Fatalf("tapped prefixes %v", prefixes)
	}
	pmu.Unlock()

	// Switching back to the terminal closes open bridges.
	t0 := time.Now()
	go h.CloseAll()
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != protocol.CloseGUINotActive {
		t.Fatalf("CloseAll: %v", err)
	}
	t.Logf("close took %v", time.Since(t0))
}

func TestBridgeNoServer(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	h := &Handler{Addr: addr, Active: func() bool { return true }}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != protocol.CloseEmacsUnavailable {
		t.Fatalf("no server: %v", err)
	}
}

// killableXvnc sends the RFB version on each connection and returns a
// function that drops every accepted connection (Xvnc being stopped).
func killableXvnc(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			_, _ = c.Write([]byte("RFB 003.008\n"))
			go func() { _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	return ln.Addr().String(), func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
		conns = nil
	}
}

func TestBridgeCloseCodes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		closeAll bool // the learner switched to the terminal first
		want     int
	}{
		{"mode switch then display stops", true, protocol.CloseGUINotActive},
		{"display exits in gui mode", false, protocol.CloseEmacsUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, kill := killableXvnc(t)
			var active atomic.Bool
			active.Store(true)
			h := &Handler{Addr: addr, Active: active.Load}
			srv := httptest.NewServer(h)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.CloseNow()
			if _, data, err := c.Read(ctx); err != nil || string(data) != "RFB 003.008\n" {
				t.Fatalf("server version: %v %q", err, data)
			}
			if tc.closeAll {
				// What the supervisor does on SetMode(terminal): record
				// the mode, close the bridges, then stop Xvnc at once.
				active.Store(false)
				h.CloseAll()
			}
			kill()
			_, _, err = c.Read(ctx)
			if got := websocket.CloseStatus(err); int(got) != tc.want {
				t.Fatalf("close status %d (%v), want %d", got, err, tc.want)
			}
		})
	}
}

func TestBridgeClientClose(t *testing.T) {
	addr, _ := killableXvnc(t)
	h := &Handler{Addr: addr, Active: func() bool { return true }}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Read(ctx); err != nil {
		t.Fatal(err)
	}
	// Reading concurrently lets the library answer the close handshake.
	go func() { _, _, _ = c.Read(ctx) }()
	if err := c.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("client close: %v", err)
	}
	// The handler unregisters the bridge.
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.mu.Lock()
		n := len(h.conns)
		h.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d bridges still registered", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
