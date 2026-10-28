package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/coder/websocket"

	"github.com/floatshadow/emacs-playground/internal/curriculum"
	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// curriculumPlay implements `playctl curriculum play <lesson>... | all`
// (CURRICULUM.md Part B.5 step 3): for each lesson it loads the scenario in
// a running session, replays every step's demo through /rpc/keys with the
// browser's pacing, polls /rpc/check and requires each step to complete.
//
// The session URL is sessiond itself (http://127.0.0.1:7000 under make dev)
// or a gateway session path (http://localhost:8080/s/local); POSTs carry an
// Origin header equal to the URL's origin so the gateway's check passes.
// sessiond fetches scenarios and fixtures from its gateway, so the gateway
// must serve a bundle built from the same source.
//
// Replays are not a learner's progress: with a gateway session path each
// lesson's progress is reset after it was played (-reset-progress, default
// on for such URLs, off for sessiond itself, which has no progress API).
//
// -watch plays every step the way the browser's "Watch it" does (rpc demo
// start, the demo, rpc demo end) and requires a step demonstrated event
// with the step still active; then it resets the step (rpc restore {}) and
// plays the same keys as the learner, requiring the step to complete.
// -repeat N plays every lesson N times in a row, to expose flaky demos.
//
// A selector may name a starting step: "P1@test", "P1@5" (the fifth task),
// "2.3@save"; the lesson then loads at that step ("Go to this step":
// its checkpoint, else a fast-forward) and plays from there. -lap plays
// projects as the "again, without hints" lap (rpc scenario {lap: true}):
// hints must be refused and the last step must report the lap's time.
//
// Frames follow the browser: gui lessons and steps with mode gui play in
// the GUI frame, the other lessons in the terminal frame (rpc mode).
func curriculumPlay(args []string) int {
	fs := flag.NewFlagSet("curriculum play", flag.ExitOnError)
	session := fs.String("session", envOr("PG_PLAY_SESSION", "http://127.0.0.1:7000"), "session URL: sessiond or <gateway>/s/<id>")
	dir := fs.String("dir", "curriculum", "curriculum source directory (for the demo strings)")
	timeout := fs.Duration("timeout", 30*time.Second, "time a step may take to complete after its demo")
	speed := fs.Float64("speed", 1, "pacing multiplier for chord and character delays (2 = twice as fast)")
	keepGoing := fs.Bool("k", true, "continue with the next step after a failure")
	watch := fs.Bool("watch", false, "play each step as a Watch it demonstration (expect demonstrated), then as the learner (expect complete)")
	resetProgress := fs.Bool("reset-progress", false, "reset each played lesson's progress afterwards (default: on for a gateway session URL …/s/<id>)")
	repeat := fs.Int("repeat", 1, "play every lesson this many times in a row")
	lap := fs.Bool("lap", false, "play each lesson as the \"again, without hints\" lap (projects)")
	sel, err := parseInterleaved(fs, args)
	if err != nil {
		return 2
	}
	if len(sel) == 0 {
		fmt.Fprintln(os.Stderr, "usage: playctl curriculum play <lesson id | part dir | 2.3 | P1 | all>[@step]... [-session URL] [-watch | -lap] [-repeat N] [-reset-progress=false]")
		return 2
	}
	if *lap && *watch {
		fmt.Fprintln(os.Stderr, "-lap and -watch exclude each other: a lap has no demonstrations")
		return 2
	}
	resetSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "reset-progress" {
			resetSet = true
		}
	})
	if !resetSet {
		*resetProgress = gatewaySessionURL(*session)
	}
	if *repeat < 1 {
		*repeat = 1
	}
	src, diags := curriculum.Load(*dir)
	if diags.HasErrors() {
		fmt.Fprintln(os.Stderr, diags.Err())
		return 1
	}
	lessons, err := selectLessons(src, sel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	c, err := newSessionClient(*session)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if _, err := c.status(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "session %s is not reachable: %v\n", *session, err)
		return 1
	}
	p := &player{c: c, timeout: *timeout, speed: *speed, keepGoing: *keepGoing, log: os.Stderr, lap: *lap}
	if *watch || *lap {
		ev, err := c.subscribe(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "-watch and -lap need the session's events: %v\n", err)
			return 1
		}
		defer ev.close()
		p.events = ev
	}
	var results []stepResult
	for _, sel := range lessons {
		l := sel.l
		for n := 0; n < *repeat && ctx.Err() == nil; n++ {
			results = append(results, p.playLesson(ctx, l, sel.from)...)
		}
		if *resetProgress {
			// Also after an interrupt: the replay is not the learner's.
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			if err := c.resetProgress(rctx, l.ID); err != nil {
				fmt.Fprintf(os.Stderr, "resetting the progress of %s: %v\n", l.ID, err)
			}
			cancel()
		}
		if ctx.Err() != nil {
			break
		}
	}
	return printResults(os.Stdout, results)
}

// gatewaySessionURL reports whether u is a gateway session path
// (<gateway>/s/<id>) rather than sessiond itself.
func gatewaySessionURL(u string) bool {
	pu, err := url.Parse(strings.TrimRight(u, "/"))
	if err != nil {
		return false
	}
	dir, id := path.Split(pu.Path)
	return id != "" && strings.HasSuffix(dir, "/s/")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// selection is a lesson to play and the index of the step to start at.
type selection struct {
	l    *curriculum.Lesson
	from int
}

// selectLessons resolves selectors: "all", a lesson id, a part directory or
// number ("02-buffers-and-files", "part:2"), or a display number ("2.3",
// "P1"), each optionally followed by "@<step id or 1-based number>".
func selectLessons(src *curriculum.Source, sel []string) ([]selection, error) {
	var out []selection
	seen := map[string]bool{}
	for _, s := range sel {
		s, step, hasStep := strings.Cut(s, "@")
		var ids []string
		if s == "all" {
			ids = append(ids, src.Order...)
		} else {
			for _, p := range src.Index.Parts {
				if s == p.ID || s == fmt.Sprintf("part:%d", p.N) {
					for _, ref := range p.Lessons {
						ids = append(ids, ref.ID)
					}
				}
				for _, ref := range p.Lessons {
					if s == ref.ID || s == ref.N {
						ids = append(ids, ref.ID)
					}
				}
			}
			for _, pr := range src.Index.Projects {
				if s == pr.ID || s == pr.N {
					ids = append(ids, pr.ID)
				}
			}
		}
		matched := false
		for _, id := range ids {
			l := src.Lessons[id]
			if l == nil {
				continue
			}
			matched = true
			from := 0
			if hasStep {
				from = stepIndex(l, step)
				if from < 0 {
					return nil, fmt.Errorf("%s has no step %q", id, step)
				}
			}
			key := fmt.Sprintf("%s@%d", id, from)
			if !seen[key] {
				seen[key] = true
				out = append(out, selection{l: l, from: from})
			}
		}
		if !matched {
			return nil, fmt.Errorf("no ready lesson matches %q", s)
		}
	}
	return out, nil
}

// stepIndex resolves a step id or a 1-based step (task) number.
func stepIndex(l *curriculum.Lesson, step string) int {
	if i := l.Scenario.StepIndex(step); i >= 0 {
		return i
	}
	var n int
	if _, err := fmt.Sscanf(step, "%d", &n); err == nil && fmt.Sprint(n) == step && n >= 1 && n <= len(l.Scenario.Steps) {
		return n - 1
	}
	return -1
}

// sessionClient calls a session's RPC endpoints.
type sessionClient struct {
	base   string
	origin string
	hc     *http.Client
}

func newSessionClient(base string) (*sessionClient, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("session URL %q is not an absolute http(s) URL", base)
	}
	return &sessionClient{
		base:   u.String(),
		origin: u.Scheme + "://" + u.Host,
		hc:     &http.Client{Timeout: 2 * time.Minute},
	}, nil
}

// post sends v as JSON to u with the session's Origin. A non-2xx response
// becomes an *httpx.Error; the caller closes the body of a 2xx one.
func (c *sessionClient) post(ctx context.Context, u string, v any) (*http.Response, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", c.origin)
	resp, err := c.hc.Do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, httpx.DecodeProblem(resp)
	}
	return resp, nil
}

func (c *sessionClient) rpc(ctx context.Context, op protocol.RPCOp, req, res any) error {
	resp, err := c.post(ctx, c.base+protocol.PathRPC+string(op), req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env protocol.RawRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&env); err != nil {
		return fmt.Errorf("rpc %s: %w", op, err)
	}
	if res != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, res)
	}
	return nil
}

// resetProgress clears one lesson's progress through the gateway's API
// (POST <origin>/api/v1/progress/reset {lessonId}).
func (c *sessionClient) resetProgress(ctx context.Context, lessonID string) error {
	resp, err := c.post(ctx, c.origin+protocol.APIPrefix+"/progress/reset", protocol.ProgressResetRequest{LessonID: lessonID})
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// subscribe opens the session's /events stream (for -watch).
func (c *sessionClient) subscribe(ctx context.Context) (*eventLog, error) {
	u := "ws" + strings.TrimPrefix(c.base, "http") + protocol.PathEvents
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dctx, u, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {c.origin}}})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(4 << 20)
	rctx, stop := context.WithCancel(context.Background())
	l := &eventLog{conn: conn, stop: stop, changed: make(chan struct{}, 1)}
	go l.read(rctx)
	return l, nil
}

// eventLog records the step events of an /events stream.
type eventLog struct {
	conn    *websocket.Conn
	stop    context.CancelFunc
	mu      sync.Mutex
	steps   []*protocol.StepEvent
	lastSeq int64
	err     error
	changed chan struct{}
}

func (l *eventLog) read(ctx context.Context) {
	for {
		_, data, err := l.conn.Read(ctx)
		l.mu.Lock()
		if err != nil {
			l.err = err
			l.mu.Unlock()
			l.notify()
			return
		}
		if ev, derr := protocol.DecodeEvent(data); derr == nil {
			if s, ok := ev.(*protocol.StepEvent); ok {
				l.steps = append(l.steps, s)
			}
			if seq := ev.Env().Seq; seq > l.lastSeq {
				l.lastSeq = seq
			}
		}
		l.mu.Unlock()
		l.notify()
	}
}

func (l *eventLog) notify() {
	select {
	case l.changed <- struct{}{}:
	default:
	}
}

// mark returns a position: wait looks at step events after it.
func (l *eventLog) mark() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.steps)
}

// wait returns the first step event after mark for lessonID and stepID
// whose status is one of statuses.
func (l *eventLog) wait(ctx context.Context, mark int, lessonID, stepID string, d time.Duration, statuses ...protocol.StepStatus) (*protocol.StepEvent, error) {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	for {
		l.mu.Lock()
		for _, s := range l.steps[min(mark, len(l.steps)):] {
			if s.LessonID == lessonID && s.StepID == stepID && slices.Contains(statuses, s.Status) {
				l.mu.Unlock()
				return s, nil
			}
		}
		err := l.err
		l.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("events stream: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("no step %v event for %s within %v", statuses, stepID, d)
		case <-l.changed:
		}
	}
}

func (l *eventLog) close() {
	l.stop()
	_ = l.conn.Close(websocket.StatusNormalClosure, "")
}

func (c *sessionClient) status(ctx context.Context) (protocol.StatusResult, error) {
	var st protocol.StatusResult
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+protocol.PathStatus, nil)
	if err != nil {
		return st, err
	}
	resp, err := c.hc.Do(r)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, httpx.DecodeProblem(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&st)
	return st, err
}

type stepResult struct {
	Lesson, Step string
	OK           bool
	Took         time.Duration
	Detail       string
}

type player struct {
	c         *sessionClient
	timeout   time.Duration
	speed     float64
	keepGoing bool
	log       io.Writer
	// events is set with -watch (each step is first played as a "Watch
	// it" demonstration) and -lap.
	events *eventLog
	// lap plays the lesson as the "again, without hints" lap.
	lap bool
}

func (p *player) playLesson(ctx context.Context, l *curriculum.Lesson, from int) []stepResult {
	steps := l.Scenario.Steps
	var out []stepResult
	fail := func(from int, msg string) []stepResult {
		for _, s := range steps[from:] {
			out = append(out, stepResult{Lesson: l.ID, Step: s.ID, Detail: msg})
		}
		return out
	}
	fmt.Fprintf(p.log, "▶ %s (%d steps)\n", l.ID, len(steps)-from)
	if want, ok := lessonFrame(&l.Scenario); ok {
		if err := p.ensureFrame(ctx, want); err != nil {
			return fail(from, "frame: "+err.Error())
		}
	}
	var lapMark int
	switch {
	case p.lap:
		lapMark = p.events.mark()
		if err := p.loadLap(ctx, l.ID, steps[0].ID); err != nil {
			return fail(0, "lap: "+err.Error())
		}
	case from > 0:
		// "Go to this step": its checkpoint, else a fast-forward.
		if err := p.load(ctx, l.ID, steps[from].ID, false); err != nil {
			return fail(from, "load: "+err.Error())
		}
	default:
		if err := p.load(ctx, l.ID, steps[0].ID, true); err != nil {
			return fail(0, "load: "+err.Error())
		}
	}
	for i := from; i < len(steps); i++ {
		st := &steps[i]
		start := time.Now()
		if i > from {
			// The engine advances by itself after a completion; jump when it
			// did not (after a failed step).
			if err := p.waitStep(ctx, l.ID, st.ID, 5*time.Second); err != nil {
				if err := p.load(ctx, l.ID, st.ID, false); err != nil {
					out = append(out, stepResult{Lesson: l.ID, Step: st.ID, Detail: "load: " + err.Error()})
					continue
				}
			}
		}
		if st.Mode == protocol.LessonGUI {
			// As the browser does when the step becomes active.
			if err := p.ensureFrame(ctx, protocol.FrameGUI); err != nil {
				out = append(out, stepResult{Lesson: l.ID, Step: st.ID, Detail: "frame: " + err.Error()})
				continue
			}
		}
		time.Sleep(300 * time.Millisecond)
		if p.events != nil && !p.lap {
			if detail := p.watch(ctx, l.ID, st); detail != "" {
				r := stepResult{Lesson: l.ID, Step: st.ID, Took: time.Since(start), Detail: "watch: " + detail}
				out = append(out, r)
				fmt.Fprintf(p.log, "  ✗ %s %s %s\n", st.ID, r.Took.Round(100*time.Millisecond), r.Detail)
				if !p.keepGoing {
					return fail(i+1, "skipped")
				}
				continue
			}
			time.Sleep(300 * time.Millisecond)
		}
		if err := p.playDemo(ctx, st.Demo); err != nil {
			out = append(out, stepResult{Lesson: l.ID, Step: st.ID, Took: time.Since(start), Detail: "demo: " + err.Error()})
			if !p.keepGoing {
				return fail(i+1, "skipped")
			}
			continue
		}
		ok, detail := p.awaitComplete(ctx, l.ID, st.ID, i == len(steps)-1)
		r := stepResult{Lesson: l.ID, Step: st.ID, OK: ok, Took: time.Since(start), Detail: detail}
		out = append(out, r)
		mark := "✓"
		if !ok {
			mark = "✗"
		}
		fmt.Fprintf(p.log, "  %s %s %s %s\n", mark, st.ID, r.Took.Round(100*time.Millisecond), detail)
		if !ok && !p.keepGoing {
			return fail(i+1, "skipped")
		}
		if ctx.Err() != nil {
			return out
		}
	}
	if p.lap {
		last := steps[len(steps)-1].ID
		r := stepResult{Lesson: l.ID, Step: "(lap)", OK: true}
		ev, err := p.events.wait(ctx, lapMark, l.ID, last, 10*time.Second, protocol.StepComplete)
		switch {
		case err != nil:
			r.OK, r.Detail = false, err.Error()
		case !ev.Lap || ev.LapMs <= 0:
			r.OK, r.Detail = false, fmt.Sprintf("the last step did not report the lap (lap %v, lapMs %d)", ev.Lap, ev.LapMs)
		default:
			r.Took = time.Duration(ev.LapMs) * time.Millisecond
		}
		out = append(out, r)
	}
	return out
}

// lessonFrame is the frame a lesson plays in: the GUI frame for gui
// lessons (mode gui or tagged gui), as the browser switches on load; the
// terminal frame for the others, so a replay does not depend on the frame
// an earlier lesson left. Lessons with mode any keep the current frame.
func lessonFrame(sc *protocol.Scenario) (protocol.FrameKind, bool) {
	switch {
	case sc.Mode == protocol.LessonGUI || slices.Contains(sc.Tags, protocol.TagGUI):
		return protocol.FrameGUI, true
	case sc.Mode == protocol.LessonAny:
		return "", false
	default:
		return protocol.FrameTerminal, true
	}
}

// ensureFrame switches the session to frame want (rpc mode) unless it
// shows it already, and waits until the switch has settled: the new frame
// is up, the old one gone and the agent connected.
func (p *player) ensureFrame(ctx context.Context, want protocol.FrameKind) error {
	st, err := p.c.status(ctx)
	if err != nil {
		return err
	}
	if st.Mode == want {
		return nil
	}
	fmt.Fprintf(p.log, "  ⇄ %s frame\n", want)
	var res protocol.ModeResult
	if err := p.c.rpc(ctx, protocol.OpMode, protocol.ModeRequest{Mode: want}, &res); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		st, err := p.c.status(ctx)
		if err == nil && st.Mode == want && st.Emacs.Agent &&
			(want != protocol.FrameGUI || st.Frames.GUI) && (want != protocol.FrameTerminal || !st.Frames.GUI) {
			// Emacs finishes deleting or creating frames after the
			// supervisor reports them.
			p.sleep(ctx, 2*time.Second)
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the %s frame did not settle within 30s", want)
		}
		p.sleep(ctx, 250*time.Millisecond)
	}
}

// loadLap starts the lesson's lap and checks that hints are refused.
func (p *player) loadLap(ctx context.Context, lessonID, first string) error {
	var res protocol.ScenarioResult
	if err := p.c.rpc(ctx, protocol.OpScenario, protocol.ScenarioRequest{LessonID: lessonID, Lap: true}, &res); err != nil {
		return err
	}
	if res.Lap == nil {
		return errors.New("rpc scenario did not start a lap")
	}
	wait := 20 * time.Second
	if res.Restarting {
		wait = 120 * time.Second
	}
	if err := p.waitStep(ctx, lessonID, first, wait); err != nil {
		return err
	}
	var he *httpx.Error
	if err := p.c.rpc(ctx, protocol.OpHint, protocol.HintRequest{StepID: first, Level: 1}, nil); !errors.As(err, &he) || he.Code != protocol.ErrConflict {
		return fmt.Errorf("a hint during the lap answered %v, want 409", err)
	}
	return nil
}

// load sends rpc scenario and waits until the step is active.
func (p *player) load(ctx context.Context, lessonID, stepID string, reset bool) error {
	var res protocol.ScenarioResult
	if err := p.c.rpc(ctx, protocol.OpScenario, protocol.ScenarioRequest{LessonID: lessonID, StepID: stepID, Reset: reset}, &res); err != nil {
		return err
	}
	wait := 20 * time.Second
	if res.Restarting {
		wait = 120 * time.Second
	}
	return p.waitStep(ctx, lessonID, stepID, wait)
}

func (p *player) waitStep(ctx context.Context, lessonID, stepID string, d time.Duration) error {
	deadline := time.Now().Add(d)
	var last string
	for {
		st, err := p.c.status(ctx)
		if err == nil && st.Emacs.Agent && st.Step != nil && st.Step.LessonID == lessonID && st.Step.StepID == stepID {
			return nil
		}
		if err != nil {
			last = err.Error()
		} else if st.Step != nil {
			last = "active step is " + st.Step.LessonID + " " + st.Step.StepID
		} else {
			last = "no active step"
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("step %s did not become active: %s", stepID, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (p *player) sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// watch plays step st as the browser's "Watch it" does: rpc demo start,
// the demo, rpc demo end. The step must report demonstrated (not complete)
// and stay active; then rpc restore {} ("Do it yourself") must reset it.
// It returns "" on success, else what went wrong.
func (p *player) watch(ctx context.Context, lessonID string, st *protocol.Step) string {
	mark := p.events.mark()
	if err := p.c.rpc(ctx, protocol.OpDemo, protocol.DemoRequest{StepID: st.ID, Phase: protocol.DemoStart}, nil); err != nil {
		return "demo start: " + err.Error()
	}
	perr := p.playDemo(ctx, st.Demo)
	// A conflict on end means the step is no longer active: the events
	// below say why.
	var he *httpx.Error
	if err := p.c.rpc(ctx, protocol.OpDemo, protocol.DemoRequest{StepID: st.ID, Phase: protocol.DemoEnd}, nil); err != nil && perr == nil &&
		!(errors.As(err, &he) && he.Code == protocol.ErrConflict) {
		perr = fmt.Errorf("demo end: %w", err)
	}
	if perr != nil {
		return "demo: " + perr.Error()
	}
	ev, err := p.events.wait(ctx, mark, lessonID, st.ID, p.timeout, protocol.StepDemonstrated, protocol.StepComplete)
	if err != nil {
		var res protocol.CheckResult
		if cerr := p.c.rpc(ctx, protocol.OpCheck, protocol.Empty{}, &res); cerr == nil {
			return err.Error() + "; unmet: " + unmet(res)
		}
		return err.Error()
	}
	if ev.Status != protocol.StepDemonstrated {
		return "the demonstration completed the step as done"
	}
	if s, err := p.c.status(ctx); err != nil || s.Step == nil || s.Step.StepID != st.ID {
		return "the step is no longer active after its demonstration"
	}
	mark = p.events.mark()
	if err := p.c.rpc(ctx, protocol.OpRestore, protocol.RestoreRequest{}, nil); err != nil {
		return "restore: " + err.Error()
	}
	if _, err := p.events.wait(ctx, mark, lessonID, st.ID, 15*time.Second, protocol.StepReset); err != nil {
		return "after Do it yourself: " + err.Error()
	}
	return ""
}

// unmet lists the conditions of a check result that do not hold.
func unmet(res protocol.CheckResult) string {
	var out []string
	for _, c := range res.Conditions {
		if !c.OK {
			s := c.ID
			if c.Unknown {
				s += " (unknown)"
			}
			if c.Detail != "" {
				s += ": " + strings.SplitN(c.Detail, "\n", 2)[0]
			}
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}

// playDemo replays a demo string through rpc keys with playctl's pacing
// (protocol.PlayDemo: 170 ms per chord, 70 ms per character, newlines as
// RET and tabs as TAB, pauses kept as written).
func (p *player) playDemo(ctx context.Context, demo string) error {
	return protocol.PlayDemo(ctx, demo, p.speed, func(ctx context.Context, req protocol.KeysRequest) error {
		return p.c.rpc(ctx, protocol.OpKeys, req, nil)
	})
}

// awaitComplete polls rpc check until the step is complete, the engine has
// moved past it, or the timeout expires.
func (p *player) awaitComplete(ctx context.Context, lessonID, stepID string, last bool) (bool, string) {
	deadline := time.Now().Add(p.timeout)
	var lastCheck protocol.CheckResult
	for {
		var res protocol.CheckResult
		err := p.c.rpc(ctx, protocol.OpCheck, protocol.Empty{}, &res)
		var he *httpx.Error
		switch {
		case err == nil && res.LessonID == lessonID && res.StepID == stepID && res.Complete:
			return true, ""
		case err == nil && (res.LessonID != lessonID || res.StepID != stepID):
			// The engine advanced to the next step: this one completed.
			return true, ""
		case err == nil:
			lastCheck = res
		case errors.As(err, &he) && he.Code == protocol.ErrConflict:
			// No active step: the lesson completed.
			if last {
				return true, ""
			}
		case ctx.Err() != nil:
			return false, "interrupted"
		}
		if time.Now().After(deadline) {
			if err != nil {
				return false, "timeout: " + err.Error()
			}
			return false, "timeout; unmet: " + unmet(lastCheck)
		}
		p.sleep(ctx, 300*time.Millisecond)
	}
}

func printResults(w io.Writer, results []stepResult) int {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "LESSON\tSTEP\tRESULT\tTIME\tDETAIL")
	failed := 0
	for _, r := range results {
		res := "ok"
		if !r.OK {
			res = "FAIL"
			failed++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Lesson, r.Step, res, r.Took.Round(100*time.Millisecond), r.Detail)
	}
	_ = tw.Flush()
	if failed > 0 {
		// The failures again, with their last check, where a long table
		// cannot hide them.
		fmt.Fprintln(w, "\nFailed steps:")
		for _, r := range results {
			if !r.OK {
				fmt.Fprintf(w, "  %s %s: %s\n", r.Lesson, r.Step, r.Detail)
			}
		}
	}
	fmt.Fprintf(w, "%d steps, %d passed, %d failed\n", len(results), len(results)-failed, failed)
	if failed > 0 || len(results) == 0 {
		return 1
	}
	return 0
}
