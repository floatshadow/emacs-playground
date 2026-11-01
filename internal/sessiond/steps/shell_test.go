package steps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/preview"
)

// slowShell blocks each script until released.
type slowShell struct {
	mu      sync.Mutex
	started chan struct{}
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
	if s.started != nil {
		s.started <- struct{}{}
	}
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

type shellFunc func(context.Context, string, string, time.Duration) ShellResult

func (f shellFunc) Run(ctx context.Context, dir, script string, timeout time.Duration) ShellResult {
	return f(ctx, dir, script, timeout)
}

func TestShellWritesToWatchedTree(t *testing.T) {
	for _, wait := range []time.Duration{time.Second, time.Millisecond} {
		t.Run(wait.String(), func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input")
			if err := os.WriteFile(input, []byte("ok"), 0o644); err != nil {
				t.Fatal(err)
			}
			cache := &shellCache{}
			changed, triggered := make(chan struct{}, 8), make(chan struct{}, 8)
			watcher, err := preview.NewWatcher(dir, func(*protocol.FileEvent) {
				cache.clear()
				changed <- struct{}{}
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer watcher.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go watcher.Run(ctx)
			sh := shellFunc(func(ctx context.Context, dir, script string, timeout time.Duration) ShellResult {
				data, err := os.ReadFile(input)
				if err != nil {
					return ShellResult{Err: err}
				}
				if err := os.WriteFile(filepath.Join(dir, "results"), data, 0o644); err != nil {
					return ShellResult{Err: err}
				}
				select {
				case <-changed:
				case <-time.After(3 * time.Second):
					return ShellResult{Err: context.DeadlineExceeded}
				}
				return ShellResult{OK: string(data) == "ok"}
			})
			ec := &evalContext{ctx: ctx, workspace: dir, shell: sh, shellCache: cache,
				shellWait: wait, trigger: func() { triggered <- struct{}{} }}
			c := cond(t, `{"shell":{"run":"check"}}`)
			eval := func() result {
				r := ec.eval(&c)
				select {
				case <-triggered:
				case <-time.After(4 * time.Second):
					t.Fatal("shell did not finish")
				}
				if r.t == protocol.Unknown {
					r = ec.eval(&c)
				}
				return r
			}
			if r := eval(); r.t != protocol.True {
				t.Fatalf("own writes: %+v", r)
			}
			if _, _, ok := cache.get(dir+"\x00check", time.Now()); ok {
				t.Fatal("invalidated result cached")
			}
			if err := os.WriteFile(input, []byte("bad"), 0o644); err != nil {
				t.Fatal(err)
			}
			select {
			case <-changed:
			case <-time.After(3 * time.Second):
				t.Fatal("input change not observed")
			}
			if r := eval(); r.t != protocol.False {
				t.Fatalf("changed input: %+v", r)
			}
		})
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
	f.begin(sc, 0)
	res, _ := f.e.Check(context.Background())
	if !res.Complete || len(f.elisp.calls[len(f.elisp.calls)-1]) != 2 {
		t.Fatalf("buffer condition %+v, calls %v", res, f.elisp.calls)
	}
}
