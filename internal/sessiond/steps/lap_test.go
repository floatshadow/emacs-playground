package steps

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

const projectJSON = `{
  "id": "projects/p1-hello-cargo", "title": "Hello, cargo", "minutes": 75, "modules": ["base", "agent"],
  "steps": [
    {"id": "scaffold", "goal": "Scaffold", "demo": "a",
     "conditions": [{"event": {"command": "eshell"}}],
     "stuck": [{"when": {"step_seconds": 1}, "say": "Taking a while."}]},
    {"id": "commit", "goal": "Commit", "demo": "b",
     "conditions": [{"event": {"command": "magit-commit-create"}}]}
  ]
}`

func TestLap(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, projectJSON)
	ctx := context.Background()

	f.e.ActivateLap(sc, 0, LapStart)
	lap := f.e.Lap()
	if lap == nil || lap.LessonID != sc.ID || lap.StartedAt != f.clock.now().UnixMilli() {
		t.Fatalf("lap %+v", lap)
	}
	if ev := f.sink.events[0].(*protocol.StepEvent); !ev.Lap {
		t.Fatalf("loaded event without lap: %+v", ev)
	}
	if f.e.Hint("scaffold", 1) || f.e.StartDemo("scaffold") || f.e.BeginSkip("scaffold") {
		t.Fatal("hint, demo and skip must be refused during a lap")
	}
	// Stuck rules are silent.
	f.clock.add(5 * time.Second)
	f.sink.clear()
	f.e.Check(ctx)
	if strings.Contains(f.sink.types(), "nudge") {
		t.Fatalf("nudge during a lap: %s", f.sink.types())
	}
	// Reset this step keeps the lap.
	if !f.e.ResetTo(sc.ID, "scaffold") || f.e.Lap() == nil {
		t.Fatal("reset ended the lap")
	}
	f.e.OnKey(&protocol.KeyEvent{Keys: "M-x", Cmd: "eshell"})
	f.e.Check(ctx)
	f.clock.add(90 * time.Second)
	f.e.OnKey(&protocol.KeyEvent{Keys: "c c", Cmd: "magit-commit-create"})
	f.sink.clear()
	f.e.Check(ctx)
	var last *protocol.StepEvent
	for _, ev := range f.sink.events {
		if se, ok := ev.(*protocol.StepEvent); ok && se.Status == protocol.StepComplete {
			last = se
		}
	}
	if last == nil || !last.LessonComplete || !last.Lap || last.LapMs != 95000 {
		t.Fatalf("lap completion %+v", last)
	}
	var rep *protocol.ProgressReport
	for i := range f.sink.reports {
		if f.sink.reports[i].LapMs != 0 {
			rep = &f.sink.reports[i]
		}
	}
	if rep == nil || rep.StepID != "commit" || rep.Status != protocol.ProgressDone || rep.LapMs != 95000 {
		t.Fatalf("lap report %+v", f.sink.reports)
	}
	if f.e.Lap() != nil || f.e.Saved().Lap != nil {
		t.Fatal("the lap should be over")
	}
}

func TestLapEndsAndResumes(t *testing.T) {
	f := newFixture(t)
	sc := scenarioFromJSON(t, projectJSON)
	f.e.ActivateLap(sc, 0, LapStart)
	saved := f.e.Saved()
	if saved.Lap == nil {
		t.Fatal("lap not persisted")
	}
	// A restarted sessiond resumes the lap.
	g := newFixture(t)
	g.e.Restore(saved)
	g.e.ActivateLap(sc, 1, LapKeep)
	if l := g.e.Lap(); l == nil || l.StartedAt != saved.Lap.StartedAt {
		t.Fatalf("resumed lap %+v", l)
	}
	// An ordinary load ends it.
	g.e.Activate(sc, 1)
	if g.e.Lap() != nil {
		t.Fatal("Activate kept the lap")
	}
	// So does a progress reset.
	f.e.Drop(sc.ID)
	if f.e.Lap() != nil {
		t.Fatal("Drop kept the lap")
	}
	// LapKeep without a lap starts none.
	f.e.ActivateLap(sc, 0, LapKeep)
	if f.e.Lap() != nil {
		t.Fatal("LapKeep started a lap")
	}
}

// slowShell blocks each script until released.
type slowShell struct {
	mu      sync.Mutex
	release chan struct{}
	runs    int
	scripts []string
	res     ShellResult
}

func (s *slowShell) Run(ctx context.Context, dir, script string, timeout time.Duration) ShellResult {
	s.mu.Lock()
	s.runs++
	s.scripts = append(s.scripts, script)
	ch := s.release
	s.mu.Unlock()
	if ch != nil {
		<-ch
	}
	return s.res
}

func TestSlowShellRunsInBackground(t *testing.T) {
	sh := &slowShell{release: make(chan struct{}), res: ShellResult{OK: true, Output: "test result: ok"}}
	triggered := make(chan struct{}, 4)
	ec := &evalContext{ctx: context.Background(), workspace: t.TempDir(), shell: sh, shellCache: &shellCache{},
		patterns: &patternCache{}, shellWait: 20 * time.Millisecond, trigger: func() { triggered <- struct{}{} }}
	c := cond(t, `{"shell":{"run":"cargo test","timeout":120}}`)
	if r := ec.eval(&c); r.t != protocol.Unknown || r.detail != "running" {
		t.Fatalf("first eval %+v", r)
	}
	// A second evaluation does not start a second run.
	if r := ec.eval(&c); r.t != protocol.Unknown {
		t.Fatalf("second eval %+v", r)
	}
	close(sh.release)
	select {
	case <-triggered:
	case <-time.After(2 * time.Second):
		t.Fatal("no trigger after the run finished")
	}
	if r := ec.eval(&c); r.t != protocol.True {
		t.Fatalf("after the run %+v", r)
	}
	if sh.runs != 1 {
		t.Fatalf("runs %d", sh.runs)
	}
	// A result that arrives after the cache was cleared is not kept.
	sh.release = make(chan struct{})
	ec.shellCache.clear()
	ec.eval(&c)
	ec.shellCache.clear()
	close(sh.release)
	<-triggered
	if _, _, ok := ec.shellCache.get(ec.workspace+"\x00cargo test", time.Now()); ok {
		t.Fatal("stale result cached")
	}
}

func TestRemoteConditions(t *testing.T) {
	sh := &fakeShell{res: map[string]ShellResult{}}
	now := time.Unix(1000, 0)
	ec := &evalContext{ctx: context.Background(), workspace: t.TempDir(), shell: sh, shellCache: &shellCache{},
		patterns: &patternCache{}, now: func() time.Time { return now }}

	shellScript := SSHCommand("cd 'app' && sh -c 'grep -q ok log'")
	sh.res[shellScript] = ShellResult{OK: true}
	c := cond(t, `{"shell":{"run":"grep -q ok log","cwd":"app","host":"box"}}`)
	if r := ec.eval(&c); r.t != protocol.True {
		t.Fatalf("remote shell %+v (ran %v)", r, sh.runs)
	}
	// Cached for RemoteTTL, then run again.
	ec.eval(&c)
	if sh.runs[shellScript] != 1 {
		t.Fatalf("runs %d", sh.runs[shellScript])
	}
	now = now.Add(RemoteTTL + time.Second)
	ec.eval(&c)
	if sh.runs[shellScript] != 2 {
		t.Fatalf("runs after ttl %d", sh.runs[shellScript])
	}
	sh.res[shellScript] = ShellResult{ExitCode: 255}
	now = now.Add(RemoteTTL + time.Second)
	if r := ec.eval(&c); r.t != protocol.Unknown {
		t.Fatalf("ssh failure %+v", r)
	}

	read := SSHCommand("test -e 'site/index.html' || exit 3; test -f 'site/index.html' || exit 4; head -c 1048576 -- 'site/index.html'")
	sh.res[read] = ShellResult{OK: true, Output: "<h1>Docs</h1>"}
	exists := SSHCommand("test -e 'site/index.html' || exit 3; ")
	sh.res[exists] = ShellResult{ExitCode: 3}
	for _, tc := range []struct {
		cond string
		want protocol.Truth
	}{
		{`{"file":{"host":"box","path":"site/index.html","contains":"Docs"}}`, protocol.True},
		{`{"file":{"host":"box","path":"site/index.html","regex":"<h1>\\w+</h1>"}}`, protocol.True},
		{`{"file":{"host":"box","path":"site/index.html","not_contains":"Docs"}}`, protocol.False},
		{`{"file":{"host":"box","path":"site/index.html","exists":true}}`, protocol.False},
		{`{"file":{"host":"box","path":"site/index.html","exists":false}}`, protocol.True},
		{`{"file":{"host":"elsewhere","path":"x","exists":true}}`, protocol.Unknown},
		{`{"shell":{"host":"elsewhere","run":"true"}}`, protocol.Unknown},
	} {
		c := cond(t, tc.cond)
		if r := ec.eval(&c); r.t != tc.want {
			t.Errorf("%s: %v (%s)", tc.cond, r.t, r.detail)
		}
	}
}

func TestBufferCondition(t *testing.T) {
	yes := true
	form := BufferForm(&protocol.BufferCond{Name: `*compilation*`, Mode: "compilation-mode", Contains: `finished "ok"`, Process: protocol.ProcessNone, Modified: &yes})
	for _, want := range []string{`(get-buffer "*compilation*")`, `(derived-mode-p (intern "compilation-mode"))`,
		`(search-forward "finished \"ok\"" nil t)`, `(not (process-live-p`, `(buffer-modified-p)`} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %s:\n%s", want, form)
		}
	}
	if strings.Count(form, "(") != strings.Count(form, ")") {
		t.Fatalf("unbalanced form %s", form)
	}
	if got := ElispString(`a\b"c`); got != `"a\\b\"c"` {
		t.Fatalf("ElispString %s", got)
	}

	// Batched with the elisp forms and evaluated from their answers.
	f := newFixture(t)
	sc := scenarioFromJSON(t, `{"id":"a/b","title":"t","minutes":1,"modules":["base","agent"],"steps":[
	  {"id":"run","goal":"g","demo":"x","conditions":[{"buffer":{"name":"*eat*","process":"running"}},{"elisp":"(t)"}]}]}`)
	f.elisp.set(BufferForm(&protocol.BufferCond{Name: "*eat*", Process: protocol.ProcessRunning}), protocol.True)
	f.elisp.set("(t)", protocol.True)
	f.e.Activate(sc, 0)
	res, _ := f.e.Check(context.Background())
	if !res.Complete || len(f.elisp.calls[len(f.elisp.calls)-1]) != 2 {
		t.Fatalf("buffer condition %+v, calls %v", res, f.elisp.calls)
	}
}
