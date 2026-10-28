package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// Limits (backend.md §3.8, §5).
const (
	MaxFileRead    = 1 << 20
	MaxShellOutput = 4 << 10
	ElispTimeout   = 2 * time.Second
)

// Checker evaluates elisp forms in Emacs (the agent's check op).
type Checker interface {
	Check(ctx context.Context, forms []string, timeout time.Duration) ([]protocol.Truth, error)
}

// ShellResult is the outcome of one shell condition.
type ShellResult struct {
	OK       bool
	TimedOut bool
	// ExitCode is the exit status (0 when OK; -1 when it did not exit).
	ExitCode int
	// Output is stdout truncated to MaxShellOutput (or the cap given to
	// RunCapped).
	Output string
	Err    error
}

// CappedShellRunner is a ShellRunner that can keep more output (remote
// file conditions read the file through ssh).
type CappedShellRunner interface {
	RunCapped(ctx context.Context, dir, script string, timeout time.Duration, maxOutput int) ShellResult
}

// ShellSyncWait is how long an evaluation waits for a shell condition
// before reporting it unknown ("running") and finishing in the background.
const ShellSyncWait = 1500 * time.Millisecond

// RemoteTTL is how long the result of a condition on the box is kept: the
// box sends no file events.
const RemoteTTL = 3 * time.Second

// ShellRunner runs `sh -c script` in dir.
type ShellRunner interface {
	Run(ctx context.Context, dir, script string, timeout time.Duration) ShellResult
}

// ExecShell is the real ShellRunner: sh -c in its own process group, killed
// as a group on timeout.
type ExecShell struct {
	// Sh is the shell binary, default /bin/sh.
	Sh string
	// Env is extra environment for the command.
	Env []string
}

// Run implements ShellRunner.
func (s ExecShell) Run(ctx context.Context, dir, script string, timeout time.Duration) ShellResult {
	return s.RunCapped(ctx, dir, script, timeout, MaxShellOutput)
}

// RunCapped implements CappedShellRunner.
func (s ExecShell) RunCapped(ctx context.Context, dir, script string, timeout time.Duration, maxOutput int) ShellResult {
	sh := s.Sh
	if sh == "" {
		sh = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sh, "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &capWriter{b: &out, max: maxOutput}
	cmd.Stderr = io.Discard
	err := cmd.Run()
	res := ShellResult{Output: out.String(), ExitCode: -1}
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
	case err == nil:
		res.OK = true
		res.ExitCode = 0
	default:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.Err = err
		}
	}
	return res
}

type capWriter struct {
	b   *bytes.Buffer
	max int
}

func (c *capWriter) Write(p []byte) (int, error) {
	if room := c.max - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

// evalContext carries what one evaluation pass needs; it is built under the
// engine lock and used without it.
type evalContext struct {
	ctx          context.Context
	counts       map[string]int
	lessonCounts map[string]int
	state        *protocol.State
	stateDoc     map[string]any
	elisp        map[string]protocol.Truth
	elispErr     string
	root         *os.Root
	workspace    string
	setupRoot    string
	shell        ShellRunner
	shellCache   *shellCache
	patterns     *patternCache
	// trigger schedules another evaluation (a background shell finished).
	trigger func()
	// shellWait overrides ShellSyncWait (tests).
	shellWait time.Duration
	now       func() time.Time
}

// result is a three-valued truth with an optional detail for author mode.
type result struct {
	t      protocol.Truth
	detail string
}

func and(rs []result) result {
	out := result{t: protocol.True}
	for _, r := range rs {
		switch r.t {
		case protocol.False:
			return result{t: protocol.False, detail: r.detail}
		case protocol.Unknown:
			out = result{t: protocol.Unknown, detail: r.detail}
		}
	}
	return out
}

func or(rs []result) result {
	out := result{t: protocol.False}
	for _, r := range rs {
		switch r.t {
		case protocol.True:
			return result{t: protocol.True, detail: r.detail}
		case protocol.Unknown:
			out = result{t: protocol.Unknown, detail: r.detail}
		}
	}
	return out
}

func not(r result) result {
	switch r.t {
	case protocol.True:
		r.t = protocol.False
	case protocol.False:
		r.t = protocol.True
	}
	return r
}

func truth(b bool) protocol.Truth {
	if b {
		return protocol.True
	}
	return protocol.False
}

// collectElisp appends the elisp forms of c (depth first) to forms.
func collectElisp(c *protocol.Condition, forms []string, seen map[string]bool) []string {
	switch c.Kind() {
	case protocol.CondElisp:
		if !seen[c.Elisp] {
			seen[c.Elisp] = true
			forms = append(forms, c.Elisp)
		}
	case protocol.CondBuffer:
		if f := BufferForm(c.Buffer); !seen[f] {
			seen[f] = true
			forms = append(forms, f)
		}
	case protocol.CondAll:
		for i := range c.All {
			forms = collectElisp(&c.All[i], forms, seen)
		}
	case protocol.CondAny:
		for i := range c.Any {
			forms = collectElisp(&c.Any[i], forms, seen)
		}
	case protocol.CondNot:
		forms = collectElisp(c.Not, forms, seen)
	}
	return forms
}

func (ec *evalContext) eval(c *protocol.Condition) result {
	switch c.Kind() {
	case protocol.CondEvent:
		counts := ec.counts
		if c.Event.Since == protocol.SinceLesson {
			counts = ec.lessonCounts
		}
		n := counts[c.Event.Command]
		return result{t: truth(n >= c.Event.MinOrDefault()), detail: fmt.Sprintf("%s ran %d time(s)", c.Event.Command, n)}
	case protocol.CondElisp:
		t, ok := ec.elisp[c.Elisp]
		if !ok {
			return result{t: protocol.Unknown, detail: ec.elispErr}
		}
		return result{t: t, detail: ec.elispErr}
	case protocol.CondBuffer:
		t, ok := ec.elisp[BufferForm(c.Buffer)]
		if !ok {
			return result{t: protocol.Unknown, detail: ec.elispErr}
		}
		d := "buffer " + c.Buffer.Name
		if t == protocol.False {
			d += " does not match"
		}
		return result{t: t, detail: d}
	case protocol.CondFile:
		if c.File.Host != "" {
			return ec.evalRemoteFile(c.File)
		}
		return ec.evalFile(c.File)
	case protocol.CondShell:
		return ec.evalShell(c.Shell)
	case protocol.CondLayout:
		if ec.state == nil {
			return result{t: protocol.Unknown, detail: "no state from Emacs yet"}
		}
		p, err := ec.patterns.get(c.Layout)
		if err != nil {
			return result{t: protocol.Unknown, detail: err.Error()}
		}
		ok := protocol.MatchLayout(p, ec.state.Layout, ec.state.BufferOf)
		return result{t: truth(ok), detail: "layout " + describeLayout(ec.state)}
	case protocol.CondState:
		if ec.stateDoc == nil {
			return result{t: protocol.Unknown, detail: "no state from Emacs yet"}
		}
		return result{t: truth(c.State.Match(ec.stateDoc))}
	case protocol.CondAll:
		rs := make([]result, len(c.All))
		for i := range c.All {
			rs[i] = ec.eval(&c.All[i])
		}
		return and(rs)
	case protocol.CondAny:
		rs := make([]result, len(c.Any))
		for i := range c.Any {
			rs[i] = ec.eval(&c.Any[i])
		}
		return or(rs)
	case protocol.CondNot:
		return not(ec.eval(c.Not))
	}
	return result{t: protocol.Unknown, detail: "condition has no kind"}
}

// describeLayout renders the state's layout with buffer names, e.g.
// "(vsplit main.rs *compilation*)".
func describeLayout(st *protocol.State) string {
	var walk func(l protocol.Layout) string
	walk = func(l protocol.Layout) string {
		if l.IsZero() {
			return "nil"
		}
		if l.IsLeaf() {
			if b := st.BufferOf(l.Window); b != "" {
				return b
			}
			return fmt.Sprint(l.Window)
		}
		parts := []string{string(l.Split)}
		for _, c := range l.Children {
			parts = append(parts, walk(c))
		}
		return "(" + strings.Join(parts, " ") + ")"
	}
	return walk(st.Layout)
}

// cleanRel validates a workspace-relative path.
func cleanRel(p string) (string, error) {
	p = strings.TrimPrefix(p, "/work/")
	p = strings.TrimPrefix(p, "./")
	c := path.Clean(p)
	if !fs.ValidPath(c) || c == "." {
		return "", fmt.Errorf("path %q is not inside the workspace", p)
	}
	return c, nil
}

func (ec *evalContext) evalFile(f *protocol.FileCond) result {
	rel, err := cleanRel(f.Path)
	if err != nil {
		return result{t: protocol.Unknown, detail: err.Error()}
	}
	if ec.root == nil {
		return result{t: protocol.Unknown, detail: "workspace not available"}
	}
	st, statErr := ec.root.Stat(rel)
	exists := statErr == nil && !st.IsDir()
	if f.Exists != nil && f.Contains == "" && f.NotContains == "" && f.Regex == "" && f.JSONPath == "" {
		if *f.Exists {
			return result{t: truth(statErr == nil), detail: rel + existsWord(statErr == nil)}
		}
		return result{t: truth(statErr != nil && errors.Is(statErr, fs.ErrNotExist)), detail: rel + existsWord(statErr == nil)}
	}
	if f.Contains == "" && f.NotContains == "" && f.Regex == "" && f.JSONPath == "" {
		return result{t: truth(statErr == nil), detail: rel + existsWord(statErr == nil)}
	}
	if !exists {
		return result{t: protocol.False, detail: rel + " does not exist"}
	}
	data, err := ec.readCapped(rel)
	if err != nil {
		return result{t: protocol.Unknown, detail: err.Error()}
	}
	return ec.matchContent(f, data, rel)
}

// matchContent applies a file condition's content predicate to data.
func (ec *evalContext) matchContent(f *protocol.FileCond, data []byte, rel string) result {
	switch {
	case f.Contains != "":
		return result{t: truth(bytes.Contains(data, []byte(f.Contains)))}
	case f.NotContains != "":
		return result{t: truth(!bytes.Contains(data, []byte(f.NotContains)))}
	case f.Regex != "":
		re, err := ec.patterns.regex(f.Regex)
		if err != nil {
			return result{t: protocol.Unknown, detail: err.Error()}
		}
		return result{t: truth(re.Match(data))}
	default:
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			return result{t: protocol.False, detail: rel + " is not valid JSON"}
		}
		vals, found := protocol.Resolve(doc, f.JSONPath)
		if !found || len(vals) == 0 {
			return result{t: protocol.False, detail: f.JSONPath + " not found"}
		}
		return result{t: truth(jsonEqual(vals[0], f.Equals)), detail: fmt.Sprintf("%s = %v", f.JSONPath, vals[0])}
	}
}

func existsWord(ok bool) string {
	if ok {
		return " exists"
	}
	return " does not exist"
}

func (ec *evalContext) readCapped(rel string) ([]byte, error) {
	fh, err := ec.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return io.ReadAll(io.LimitReader(fh, MaxFileRead))
}

// jsonEqual compares two decoded JSON values after normalising both through
// encoding/json (so 1 and 1.0 are equal).
func jsonEqual(a, b any) bool {
	na, err1 := normalize(a)
	nb, err2 := normalize(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return reflect.DeepEqual(na, nb)
}

func normalize(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(b, &out)
	return out, err
}

func (ec *evalContext) evalShell(s *protocol.ShellCond) result {
	timeout := time.Duration(s.TimeoutOrDefault()) * time.Second
	if s.Host != "" {
		if s.Host != protocol.BoxHost {
			return result{t: protocol.Unknown, detail: "unknown host " + s.Host}
		}
		remote := "sh -c " + ShQuote(s.Run)
		if s.Cwd != "" {
			remote = "cd " + ShQuote(s.Cwd) + " && " + remote
		}
		script := SSHCommand(remote)
		return ec.runShell("box\x00"+s.Cwd+"\x00"+s.Run, ec.workspace, script, timeout, RemoteTTL, MaxShellOutput, func(res ShellResult) result {
			if res.ExitCode == 255 {
				return result{t: protocol.Unknown, detail: "ssh box failed"}
			}
			return result{t: truth(res.OK), detail: res.Output}
		})
	}
	dir := ec.workspace
	sub := s.Cwd
	if sub == "" {
		sub = ec.setupRoot
	}
	if sub != "" {
		rel, err := cleanRel(sub)
		if err != nil {
			return result{t: protocol.Unknown, detail: err.Error()}
		}
		if st, err := ec.root.Stat(rel); err != nil || !st.IsDir() {
			return result{t: protocol.False, detail: "directory " + rel + " does not exist"}
		}
		dir = path.Join(ec.workspace, rel)
	}
	return ec.runShell(dir+"\x00"+s.Run, dir, s.Run, timeout, 0, MaxShellOutput, func(res ShellResult) result {
		return result{t: truth(res.OK), detail: res.Output}
	})
}

// evalRemoteFile reads a file on the box over ssh and applies the same
// predicates as evalFile.
func (ec *evalContext) evalRemoteFile(f *protocol.FileCond) result {
	if f.Host != protocol.BoxHost {
		return result{t: protocol.Unknown, detail: "unknown host " + f.Host}
	}
	p := f.Path
	onlyExists := f.Contains == "" && f.NotContains == "" && f.Regex == "" && f.JSONPath == ""
	// Exit 3: the file is missing; 255: ssh failed.
	remote := "test -e " + ShQuote(p) + " || exit 3; "
	if !onlyExists {
		remote += "test -f " + ShQuote(p) + " || exit 4; head -c " + itoa(MaxFileRead) + " -- " + ShQuote(p)
	}
	key, _ := json.Marshal(f)
	return ec.runShell("box-file\x00"+string(key), ec.workspace, SSHCommand(remote), 10*time.Second, RemoteTTL, MaxFileRead, func(res ShellResult) result {
		switch {
		case res.TimedOut:
			return result{t: protocol.Unknown, detail: "ssh box timed out"}
		case res.ExitCode == 255 || res.Err != nil:
			return result{t: protocol.Unknown, detail: "ssh box failed"}
		}
		exists := res.ExitCode != 3
		if onlyExists {
			want := f.Exists == nil || *f.Exists
			return result{t: truth(exists == want), detail: "box:" + p + existsWord(exists)}
		}
		if !res.OK {
			return result{t: protocol.False, detail: "box:" + p + " does not exist"}
		}
		return ec.matchContent(f, []byte(res.Output), "box:"+p)
	})
}

func itoa(n int) string { return fmt.Sprint(n) }

// ShQuote quotes s for a POSIX shell.
func ShQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// SSHCommand is the local script that runs remote (a shell command line)
// on the box. It never prompts (the alias sets BatchMode).
func SSHCommand(remote string) string {
	return "exec ssh -o ConnectTimeout=5 " + protocol.BoxHost + " " + ShQuote(remote)
}

// runShell runs script once per key until the cache is cleared (or ttl
// passes, when set) and turns its result into a condition result with
// conv. A run that takes longer than the sync wait finishes in the
// background and triggers another evaluation; meanwhile it is unknown.
func (ec *evalContext) runShell(key, dir, script string, timeout, ttl time.Duration, maxOutput int, conv func(ShellResult) result) result {
	now := time.Now
	if ec.now != nil {
		now = ec.now
	}
	if r, ok := ec.shellCache.get(key, now()); ok {
		return r
	}
	if ec.shell == nil {
		return result{t: protocol.Unknown, detail: "shell conditions unavailable"}
	}
	fl, started, epoch := ec.shellCache.start(key)
	if !started {
		// A run from an earlier evaluation: do not hold this one up.
		select {
		case <-fl.done:
			return fl.r
		default:
			return result{t: protocol.Unknown, detail: "running"}
		}
	}
	bctx := context.WithoutCancel(ec.ctx)
	go func() {
		var res ShellResult
		if cr, ok := ec.shell.(CappedShellRunner); ok && maxOutput != MaxShellOutput {
			res = cr.RunCapped(bctx, dir, script, timeout, maxOutput)
		} else {
			res = ec.shell.Run(bctx, dir, script, timeout)
		}
		var r result
		switch {
		case res.TimedOut:
			r = result{t: protocol.Unknown, detail: fmt.Sprintf("timed out after %ds", int(timeout/time.Second))}
		case res.Err != nil:
			r = result{t: protocol.Unknown, detail: res.Err.Error()}
		default:
			r = conv(res)
		}
		var exp time.Time
		if ttl > 0 {
			exp = now().Add(ttl)
		}
		ec.shellCache.finish(key, epoch, fl, r, exp)
		if ec.trigger != nil {
			ec.trigger()
		}
	}()
	wait := ShellSyncWait
	if ec.shellWait > 0 {
		wait = ec.shellWait
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-fl.done:
		return fl.r
	case <-t.C:
		return result{t: protocol.Unknown, detail: "running"}
	case <-ec.ctx.Done():
		return result{t: protocol.Unknown, detail: "cancelled"}
	}
}

// shellCache keeps shell results until invalidated by a file event or a
// step change (remote ones also expire), and runs each script once at a
// time.
type shellCache struct {
	mu       sync.Mutex
	m        map[string]cached
	inflight map[string]*flight
	epoch    uint64
}

type cached struct {
	r   result
	exp time.Time
}

type flight struct {
	done chan struct{}
	r    result
}

func (c *shellCache) get(k string, now time.Time) (result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || (!e.exp.IsZero() && now.After(e.exp)) {
		return result{}, false
	}
	return e.r, true
}

// start returns the run in flight for k, or a new one (started true) the
// caller must finish.
func (c *shellCache) start(k string) (*flight, bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if f, ok := c.inflight[k]; ok {
		return f, false, c.epoch
	}
	if c.inflight == nil {
		c.inflight = map[string]*flight{}
	}
	f := &flight{done: make(chan struct{})}
	c.inflight[k] = f
	return f, true, c.epoch
}

// finish records a run's result, unless the cache was cleared since it
// started (the result may be stale).
func (c *shellCache) finish(k string, epoch uint64, f *flight, r result, exp time.Time) {
	c.mu.Lock()
	if c.inflight[k] == f {
		delete(c.inflight, k)
	}
	if epoch == c.epoch {
		if c.m == nil {
			c.m = map[string]cached{}
		}
		c.m[k] = cached{r: r, exp: exp}
	}
	f.r = r
	c.mu.Unlock()
	close(f.done)
}

func (c *shellCache) clear() {
	c.mu.Lock()
	c.m = nil
	c.epoch++
	c.mu.Unlock()
}

// patternCache caches parsed layout patterns and regular expressions.
type patternCache struct {
	mu      sync.Mutex
	layouts map[string]layoutEntry
	regexes map[string]regexEntry
}

type layoutEntry struct {
	p   protocol.LayoutPattern
	err error
}

type regexEntry struct {
	re  *regexp.Regexp
	err error
}

func (c *patternCache) get(s string) (protocol.LayoutPattern, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.layouts[s]; ok {
		return e.p, e.err
	}
	p, err := protocol.ParseLayoutPattern(s)
	if c.layouts == nil {
		c.layouts = map[string]layoutEntry{}
	}
	c.layouts[s] = layoutEntry{p, err}
	return p, err
}

func (c *patternCache) regex(s string) (*regexp.Regexp, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.regexes[s]; ok {
		return e.re, e.err
	}
	re, err := regexp.Compile(s)
	if c.regexes == nil {
		c.regexes = map[string]regexEntry{}
	}
	c.regexes[s] = regexEntry{re, err}
	return re, err
}
