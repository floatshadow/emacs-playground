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
	"strings"
	"text/tabwriter"
	"time"

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
func curriculumPlay(args []string) int {
	fs := flag.NewFlagSet("curriculum play", flag.ExitOnError)
	session := fs.String("session", envOr("PG_PLAY_SESSION", "http://127.0.0.1:7000"), "session URL: sessiond or <gateway>/s/<id>")
	dir := fs.String("dir", "curriculum", "curriculum source directory (for the demo strings)")
	timeout := fs.Duration("timeout", 30*time.Second, "time a step may take to complete after its demo")
	speed := fs.Float64("speed", 1, "pacing multiplier for chord and character delays (2 = twice as fast)")
	keepGoing := fs.Bool("k", true, "continue with the next step after a failure")
	sel, err := parseInterleaved(fs, args)
	if err != nil {
		return 2
	}
	if len(sel) == 0 {
		fmt.Fprintln(os.Stderr, "usage: playctl curriculum play <lesson id | part dir | 2.3 | all>... [-session URL]")
		return 2
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
	p := &player{c: c, timeout: *timeout, speed: *speed, keepGoing: *keepGoing, log: os.Stderr}
	var results []stepResult
	for _, l := range lessons {
		results = append(results, p.playLesson(ctx, l)...)
		if ctx.Err() != nil {
			break
		}
	}
	return printResults(os.Stdout, results)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// selectLessons resolves selectors: "all", a lesson id, a part directory or
// number ("02-buffers-and-files", "part:2"), or a display number ("2.3").
func selectLessons(src *curriculum.Source, sel []string) ([]*curriculum.Lesson, error) {
	var out []*curriculum.Lesson
	seen := map[string]bool{}
	matched := false
	add := func(id string) {
		l := src.Lessons[id]
		if l == nil {
			return
		}
		matched = true
		if !seen[id] {
			seen[id] = true
			out = append(out, l)
		}
	}
	for _, s := range sel {
		matched = false
		switch {
		case s == "all":
			for _, id := range src.Order {
				add(id)
			}
		default:
			for _, p := range src.Index.Parts {
				if s == p.ID || s == fmt.Sprintf("part:%d", p.N) {
					for _, ref := range p.Lessons {
						add(ref.ID)
					}
				}
				for _, ref := range p.Lessons {
					if s == ref.ID || s == ref.N {
						add(ref.ID)
					}
				}
			}
			for _, pr := range src.Index.Projects {
				if s == pr.ID || s == pr.N {
					add(pr.ID)
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("no ready lesson matches %q", s)
		}
	}
	return out, nil
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

func (c *sessionClient) rpc(ctx context.Context, op protocol.RPCOp, req, res any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+protocol.PathRPC+string(op), bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", c.origin)
	resp, err := c.hc.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return httpx.DecodeProblem(resp)
	}
	var env protocol.RawRPCResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&env); err != nil {
		return fmt.Errorf("rpc %s: %w", op, err)
	}
	if res != nil && len(env.Result) > 0 {
		return json.Unmarshal(env.Result, res)
	}
	return nil
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
}

func (p *player) playLesson(ctx context.Context, l *curriculum.Lesson) []stepResult {
	steps := l.Scenario.Steps
	var out []stepResult
	fail := func(from int, msg string) []stepResult {
		for _, s := range steps[from:] {
			out = append(out, stepResult{Lesson: l.ID, Step: s.ID, Detail: msg})
		}
		return out
	}
	fmt.Fprintf(p.log, "▶ %s (%d steps)\n", l.ID, len(steps))
	if err := p.load(ctx, l.ID, steps[0].ID, true); err != nil {
		return fail(0, "load: "+err.Error())
	}
	for i := range steps {
		st := &steps[i]
		start := time.Now()
		if i > 0 {
			// The engine advances by itself after a completion; jump when it
			// did not (after a failed step).
			if err := p.waitStep(ctx, l.ID, st.ID, 5*time.Second); err != nil {
				if err := p.load(ctx, l.ID, st.ID, false); err != nil {
					out = append(out, stepResult{Lesson: l.ID, Step: st.ID, Detail: "load: " + err.Error()})
					continue
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
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
	return out
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
	var unmet []string
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
			unmet = unmet[:0]
			for _, c := range res.Conditions {
				if !c.OK {
					s := c.ID
					if c.Unknown {
						s += " (unknown)"
					}
					if c.Detail != "" {
						s += ": " + strings.SplitN(c.Detail, "\n", 2)[0]
					}
					unmet = append(unmet, s)
				}
			}
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
			return false, "timeout; unmet: " + strings.Join(unmet, ", ")
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
	fmt.Fprintf(w, "%d steps, %d passed, %d failed\n", len(results), len(results)-failed, failed)
	if failed > 0 || len(results) == 0 {
		return 1
	}
	return 0
}
