// Package gwclient is sessiond's client for the gateway's internal API
// (api.md §2.6): scenarios and fixture tarballs with ETags, progress and
// stuck reports, and the heartbeat. Every request carries
// "Authorization: Bearer <PG_SESSION_TOKEN>". Fetches retry three times
// (architecture.md §6); progress reports are posted once each, by the
// progress outbox, which retries.
package gwclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// LessonIDPattern is what sessiond accepts as a lesson id (it goes into
// URLs and paths literally).
var LessonIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)?$`)

// ValidLessonID reports whether id is safe to use in URLs and file paths.
func ValidLessonID(id string) bool {
	return LessonIDPattern.MatchString(id) && !strings.Contains(id, "..")
}

// Client talks to the gateway.
type Client struct {
	Base      string
	Token     string
	SessionID string
	HTTP      *http.Client
	Log       *slog.Logger
	// Attempts and Backoff control retries of idempotent requests.
	Attempts int
	Backoff  []time.Duration
}

// New returns a client with sensible timeouts.
func New(base, token, sessionID string, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		Base: strings.TrimRight(base, "/"), Token: token, SessionID: sessionID, Log: log,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
		Attempts: 3,
		Backoff:  []time.Duration{300 * time.Millisecond, time.Second, 3 * time.Second},
	}
}

// StatusError is a non-2xx answer from the gateway.
type StatusError struct {
	Status  int
	Problem *httpx.Error
}

func (e *StatusError) Error() string {
	if e.Problem != nil {
		return fmt.Sprintf("gateway: %d %s", e.Status, e.Problem.Error())
	}
	return fmt.Sprintf("gateway: %d", e.Status)
}

// Rejected reports a 4xx from the gateway: the request is wrong and
// sending it again cannot help.
func Rejected(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status >= 400 && se.Status < 500
}

// NotFound reports a 404 from the gateway.
func NotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Status == http.StatusNotFound
}

func (c *Client) request(ctx context.Context, method, path string, body any, hdr http.Header) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set(protocol.InternalAuthorization, "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	return c.HTTP.Do(req)
}

// do runs a request with retries on network errors and 5xx answers. The
// caller closes the returned body.
func (c *Client) do(ctx context.Context, method, path string, body any, hdr http.Header) (*http.Response, error) {
	return c.try(ctx, c.Attempts, method, path, body, hdr)
}

// try is do with this many attempts.
func (c *Client) try(ctx context.Context, attempts int, method, path string, body any, hdr http.Header) (*http.Response, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			d := c.Backoff[min(i-1, len(c.Backoff)-1)]
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
		}
		resp, err := c.request(ctx, method, path, body, hdr)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		if resp.StatusCode >= 500 {
			lastErr = &StatusError{Status: resp.StatusCode, Problem: httpx.DecodeProblem(resp)}
			resp.Body.Close()
			continue
		}
		if resp.StatusCode >= 400 {
			defer resp.Body.Close()
			return nil, &StatusError{Status: resp.StatusCode, Problem: httpx.DecodeProblem(resp)}
		}
		return resp, nil
	}
	return nil, lastErr
}

// ifNoneMatch returns the conditional-request header for a cached etag.
func ifNoneMatch(etag string) http.Header {
	hdr := http.Header{}
	if etag != "" {
		hdr.Set("If-None-Match", etag)
	}
	return hdr
}

// Scenario fetches GET /internal/curriculum/scenarios/{lessonId}. etag is
// sent as If-None-Match; notModified is true on 304 (raw is then nil).
func (c *Client) Scenario(ctx context.Context, lessonID, etag string) (raw []byte, newETag string, notModified bool, err error) {
	if !ValidLessonID(lessonID) {
		return nil, "", false, fmt.Errorf("invalid lesson id %q", lessonID)
	}
	resp, err := c.do(ctx, http.MethodGet, protocol.InternalScenarioPath+lessonID, nil, ifNoneMatch(etag))
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, true, nil
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, "", false, err
	}
	return raw, resp.Header.Get("ETag"), false, nil
}

// FixtureResult is the outcome of a fixture fetch.
type FixtureResult struct {
	// Body is the zstd tarball (nil on 304 and 204). The caller closes it.
	Body io.ReadCloser
	ETag string
	// NotModified: the cached copy for the sent ETag is current.
	NotModified bool
	// None: the lesson has no fixtures (204).
	None bool
}

// Fixtures fetches GET /internal/curriculum/fixtures/{lessonId}.
func (c *Client) Fixtures(ctx context.Context, lessonID, etag string) (FixtureResult, error) {
	if !ValidLessonID(lessonID) {
		return FixtureResult{}, fmt.Errorf("invalid lesson id %q", lessonID)
	}
	resp, err := c.do(ctx, http.MethodGet, protocol.InternalFixturesPath+lessonID, nil, ifNoneMatch(etag))
	if err != nil {
		return FixtureResult{}, err
	}
	switch resp.StatusCode {
	case http.StatusNotModified:
		resp.Body.Close()
		return FixtureResult{NotModified: true, ETag: etag}, nil
	case http.StatusNoContent:
		resp.Body.Close()
		return FixtureResult{None: true}, nil
	}
	return FixtureResult{Body: resp.Body, ETag: resp.Header.Get("ETag")}, nil
}

func (c *Client) post(ctx context.Context, attempts int, what string, v any) error {
	resp, err := c.try(ctx, attempts, http.MethodPost, protocol.InternalSessionsPath+c.SessionID+"/"+what, v, nil)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	return nil
}

// ProgressGenerations reads resets that the session may have missed while stopped.
func (c *Client) ProgressGenerations(ctx context.Context) (protocol.ProgressGenerations, error) {
	resp, err := c.do(ctx, http.MethodGet, protocol.InternalSessionsPath+c.SessionID+"/progress-generations", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var generations protocol.ProgressGenerations
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&generations)
	return generations, err
}

// Progress posts one progress report, once: the progress outbox retries.
func (c *Client) Progress(ctx context.Context, r protocol.ProgressReport) error {
	return c.post(ctx, 1, "progress", r)
}

// Stuck posts a stuck report.
func (c *Client) Stuck(ctx context.Context, r protocol.InternalStuckReport) error {
	return c.post(ctx, c.Attempts, "stuck", r)
}

// Heartbeat posts the WebSocket count.
func (c *Client) Heartbeat(ctx context.Context, hb protocol.Heartbeat) error {
	return c.post(ctx, c.Attempts, "heartbeat", hb)
}
