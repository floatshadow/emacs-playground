// Package gwclient is sessiond's client for the gateway's internal API
// (api.md §2.6): scenarios and fixture tarballs with ETags, progress and
// stuck reports, and the heartbeat. Every request carries
// "Authorization: Bearer <PG_SESSION_TOKEN>". Fetches retry three times
// (architecture.md §6); progress reports go through an asynchronous queue
// so the session core never waits for the network.
package gwclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"sync"
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
	attempts := c.Attempts
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

func (c *Client) post(ctx context.Context, what string, v any) error {
	resp, err := c.do(ctx, http.MethodPost, protocol.InternalSessionsPath+c.SessionID+"/"+what, v, nil)
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

// Progress posts one progress report.
func (c *Client) Progress(ctx context.Context, r protocol.ProgressReport) error {
	return c.post(ctx, "progress", r)
}

// Stuck posts a stuck report.
func (c *Client) Stuck(ctx context.Context, r protocol.InternalStuckReport) error {
	return c.post(ctx, "stuck", r)
}

// Heartbeat posts the WebSocket count.
func (c *Client) Heartbeat(ctx context.Context, hb protocol.Heartbeat) error {
	return c.post(ctx, "heartbeat", hb)
}

// Reporter queues progress reports and retries each transient failure for
// up to an hour. Reconciliation recovers persisted progress after restart or overflow.
type Reporter struct {
	c                *Client
	queue            chan queuedReport
	wake             chan struct{}
	done             chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	reconcile        func() []protocol.ProgressReport
	mu               sync.Mutex
	closed           bool
	epoch            uint64
	lessons          map[string]uint64
	generations      protocol.ProgressGenerations
	activeGeneration int64
	activeLesson     string
	activeCancel     context.CancelFunc
}

type queuedReport struct {
	report      protocol.ProgressReport
	epoch       uint64
	lessonEpoch uint64
}

// NewReporter starts a reporter; reconcile returns saved absolute progress
// without lap increments. Close drains reports until its deadline.
func NewReporter(c *Client, reconcile func() []protocol.ProgressReport) *Reporter {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Reporter{c: c, queue: make(chan queuedReport, 256), wake: make(chan struct{}, 1),
		done: make(chan struct{}), ctx: ctx, cancel: cancel, reconcile: reconcile, lessons: map[string]uint64{}, generations: protocol.ProgressGenerations{}}
	go r.run()
	return r
}

func (r *Reporter) queued(p protocol.ProgressReport) queuedReport {
	return queuedReport{report: p, epoch: r.epoch, lessonEpoch: r.lessons[p.LessonID]}
}

// Report queues a report without holding up the session core. An overflow schedules
// reconciliation from the core's saved progress.
func (r *Reporter) Report(p protocol.ProgressReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	select {
	case r.queue <- r.queued(p):
	default:
		r.c.Log.Warn("progress queue full; reconciling saved steps", "lesson", p.LessonID, "step", p.StepID)
		r.Reconcile()
	}
}

// Reconcile schedules replay of saved progress without blocking its caller.
func (r *Reporter) Reconcile() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// SyncGenerations replaces local cutoffs with the authoritative gateway snapshot.
func (r *Reporter) SyncGenerations(generations protocol.ProgressGenerations) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generations = maps.Clone(generations)
	if r.generations == nil {
		r.generations = protocol.ProgressGenerations{}
	}
	if r.activeCancel != nil && r.activeGeneration < generations.ForLesson(r.activeLesson) {
		r.activeCancel()
	}
}

// Purge invalidates queued reports and cancels the matching in-flight retry.
// An empty lessonID invalidates every lesson.
func (r *Reporter) Purge(lessonID string, generation int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation > 0 {
		r.generations[lessonID] = max(r.generations[lessonID], generation)
	} else if lessonID == "" {
		r.epoch++
	} else {
		r.lessons[lessonID]++
	}
	if r.activeCancel != nil && (lessonID == "" || r.activeLesson == lessonID) && (generation == 0 || r.activeGeneration < generation) {
		r.activeCancel()
	}
}

func (r *Reporter) send(q queuedReport) {
	r.mu.Lock()
	if q.epoch != r.epoch || q.lessonEpoch != r.lessons[q.report.LessonID] || q.report.Generation < r.generations.ForLesson(q.report.LessonID) {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, time.Hour)
	r.activeLesson, r.activeCancel = q.report.LessonID, cancel
	r.activeGeneration = q.report.Generation
	r.mu.Unlock()
	defer func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.c.Log.Warn("progress report retry deadline reached; awaiting restart reconciliation", "lesson", q.report.LessonID, "step", q.report.StepID)
		}
		cancel()
		r.mu.Lock()
		r.activeCancel = nil
		r.mu.Unlock()
	}()
	delay := time.Second
	for ctx.Err() == nil {
		attempt, stop := context.WithTimeout(ctx, 15*time.Second)
		err := r.c.Progress(attempt, q.report)
		stop()
		if err == nil {
			return
		}
		var se *StatusError
		if errors.As(err, &se) && se.Status < 500 {
			r.c.Log.Warn("progress report rejected", "status", se.Status, "err", err)
			return
		}
		r.c.Log.Warn("progress report failed; retrying", "err", err, "in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (r *Reporter) run() {
	defer close(r.done)
	for {
		select {
		case <-r.ctx.Done():
			return
		case q, ok := <-r.queue:
			if !ok {
				return
			}
			r.send(q)
		case <-r.wake:
			if r.reconcile == nil {
				continue
			}
			// Capture generations with the snapshot so a later purge also
			// invalidates the remainder of this reconciliation.
			r.mu.Lock()
			reports := r.reconcile()
			pending := make([]queuedReport, len(reports))
			for i, p := range reports {
				pending[i] = r.queued(p)
			}
			r.mu.Unlock()
			for _, q := range pending {
				r.send(q)
			}
		}
	}
}

// Close stops accepting reports and drains them until timeout, then cancels
// retries. Persisted progress is reconciled on the next startup.
func (r *Reporter) Close(timeout time.Duration) {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-r.done:
	case <-timer.C:
	}
	r.cancel()
}
