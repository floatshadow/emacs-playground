package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"

	"github.com/floatshadow/emacs-playground/internal/gateway/auth"
	"github.com/floatshadow/emacs-playground/internal/gateway/sessions"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Sessions is what the proxy needs from the session manager.
type Sessions interface {
	Get(ctx context.Context, id string) (*sessions.Session, error)
	Touch(ctx context.Context, id string)
	Opened(id string)
	Closed(id string)
}

// DefaultMaxWebSocketsPerUser is the per-user WebSocket limit
// (backend.md §2.5).
const DefaultMaxWebSocketsPerUser = 6

// Options configure New.
type Options struct {
	Sessions Sessions
	// AuthorMode lets rpc eval through (features.authorMode).
	AuthorMode bool
	// MaxWebSocketsPerUser, default 6.
	MaxWebSocketsPerUser int
	// Transport to sessiond; default a dedicated http.Transport.
	Transport http.RoundTripper
	Logger    *slog.Logger
}

// Proxy serves /s/{id}/{rest...}.
type Proxy struct {
	o Options

	mu sync.Mutex
	// proxies caches a ReverseProxy per usable session; entries of
	// sessions found unusable or gone are dropped (serve, Prune).
	proxies map[string]*cached
	userWS  map[string]int
}

type cached struct {
	target, secret string
	rp             *httputil.ReverseProxy
}

// New returns the proxy.
func New(o Options) *Proxy {
	if o.MaxWebSocketsPerUser <= 0 {
		o.MaxWebSocketsPerUser = DefaultMaxWebSocketsPerUser
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Transport == nil {
		o.Transport = &http.Transport{
			Proxy:                 nil,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			MaxIdleConns:          256,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: time.Second,
		}
	}
	return &Proxy{o: o, proxies: map[string]*cached{}, userWS: map[string]int{}}
}

// userKey carries the user id from serve to the proxy's Rewrite.
type userKey struct{}

// ServeHTTP proxies a request whose pattern binds {id} (and optionally
// {rest...}).
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := p.serve(w, r); err != nil {
		httpx.WriteError(w, r, err)
	}
}

func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) error {
	user, ok := auth.UserFrom(r.Context())
	if !ok {
		return httpx.Unauthenticated("sign in to continue")
	}
	id := r.PathValue("id")
	sess, err := p.o.Sessions.Get(r.Context(), id)
	if errors.Is(err, sessions.ErrNotFound) {
		return httpx.NotFound("no such session")
	}
	if err != nil {
		return err
	}
	if !sess.OwnedBy(user.ID) {
		return httpx.Forbidden("this session belongs to someone else")
	}
	if !sess.Usable() {
		p.forget(id, nil)
		return httpx.SessionUnavailable("the session is " + string(sess.State)).
			WithDetails(map[string]string{"state": string(sess.State)})
	}
	prefix := "/s/" + id
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	if rest == "" {
		rest = "/"
	}
	if op, isRPC := strings.CutPrefix(rest, protocol.PathRPC); isRPC {
		switch protocol.RPCOp(op) {
		case protocol.OpEval:
			if !p.o.AuthorMode {
				return httpx.Forbidden("rpc eval is only available in author mode")
			}
		case protocol.OpDropStep:
			return httpx.Forbidden("drop-step is internal")
		}
	}

	if httpx.IsWebSocketUpgrade(r) {
		if !p.acquireWS(user.ID) {
			return httpx.SessionLimit("too many open connections; close other tabs of the playground")
		}
		p.o.Sessions.Opened(id)
		defer func() {
			p.releaseWS(user.ID)
			p.o.Sessions.Closed(id)
		}()
	}
	p.o.Sessions.Touch(r.Context(), id)

	u := *r.URL
	u.Path = rest
	if r.URL.RawPath != "" {
		u.RawPath = strings.TrimPrefix(r.URL.RawPath, prefix)
		if u.RawPath == "" {
			u.RawPath = "/"
		}
	}
	out := r.WithContext(context.WithValue(r.Context(), userKey{}, user.ID))
	out.URL = &u
	p.proxyFor(sess).ServeHTTP(w, out)
	return nil
}

func (p *Proxy) acquireWS(userID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.userWS[userID] >= p.o.MaxWebSocketsPerUser {
		return false
	}
	p.userWS[userID]++
	return true
}

func (p *Proxy) releaseWS(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.userWS[userID] <= 1 {
		delete(p.userWS, userID)
		return
	}
	p.userWS[userID]--
}

// proxyFor returns the cached ReverseProxy for the session's endpoint.
func (p *Proxy) proxyFor(sess *sessions.Session) *httputil.ReverseProxy {
	target, secret := sess.Endpoint.String(), sess.ProxySecret
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.proxies[sess.ID]; c != nil && c.target == target && c.secret == secret {
		return c.rp
	}
	endpoint := *sess.Endpoint
	rp := &httputil.ReverseProxy{
		Transport: p.o.Transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&endpoint)
			pr.SetXForwarded()
			h := pr.Out.Header
			h.Del("Cookie")
			h.Del("Authorization")
			h.Del(protocol.HeaderUser)
			h.Del(protocol.HeaderProxySecret)
			if secret != "" {
				h.Set(protocol.HeaderProxySecret, secret)
			}
			if userID, ok := pr.In.Context().Value(userKey{}).(string); ok {
				h.Set(protocol.HeaderUser, userID)
			}
			if id := httpx.RequestIDFrom(pr.In.Context()); id != "" {
				h.Set(protocol.HeaderRequestID, id)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(err, context.Canceled) {
				return
			}
			p.o.Logger.Warn("proxy: session unreachable", "session_id", sess.ID, "err", err)
			httpx.WriteError(w, r, httpx.SessionUnavailable("the session did not answer").Wrap(err))
		},
	}
	p.proxies[sess.ID] = &cached{target: target, secret: secret, rp: rp}
	return rp
}

// forget drops the cached proxy of session id; with c non-nil only when
// it is still c.
func (p *Proxy) forget(id string, c *cached) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cur := p.proxies[id]; cur != nil && (c == nil || cur == c) {
		delete(p.proxies, id)
	}
}

// Prune drops the cached proxies of sessions that are gone or no longer
// usable (stopped, failed, removed), so the cache holds only live
// sessions. WebSockets already open keep their ReverseProxy.
func (p *Proxy) Prune(ctx context.Context) {
	p.mu.Lock()
	snapshot := make(map[string]*cached, len(p.proxies))
	for id, c := range p.proxies {
		snapshot[id] = c
	}
	p.mu.Unlock()
	for id, c := range snapshot {
		sess, err := p.o.Sessions.Get(ctx, id)
		if err != nil && !errors.Is(err, sessions.ErrNotFound) {
			continue
		}
		if err != nil || !sess.Usable() {
			p.forget(id, c)
		}
	}
}

// Run prunes the cache every interval until ctx ends.
func (p *Proxy) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Prune(ctx)
		}
	}
}

// WebSockets returns the open WebSocket count per user (metrics, tests).
func (p *Proxy) WebSockets(userID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.userWS[userID]
}
