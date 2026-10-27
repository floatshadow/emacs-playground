package supervisor

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Policy is a child's restart policy.
type Policy struct {
	// Backoff lists the delays before successive restarts; the last value
	// repeats. Empty means restart immediately.
	Backoff []time.Duration
	// MaxRestarts within Window makes the child give up (0: never).
	MaxRestarts int
	Window      time.Duration
	// ResetAfter: a run at least this long resets the backoff sequence.
	ResetAfter time.Duration
}

// DaemonPolicy is backend.md §3.3: backoff 1, 2, 4, 8 s, at most 5 restarts
// in 10 minutes.
var DaemonPolicy = Policy{
	Backoff:     []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second},
	MaxRestarts: 5, Window: 10 * time.Minute, ResetAfter: time.Minute,
}

// WhileActivePolicy re-runs GUI children while GUI mode is active, with a
// short backoff so a failing command cannot spin.
var WhileActivePolicy = Policy{
	Backoff:    []time.Duration{200 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second},
	ResetAfter: 10 * time.Second,
}

// Child supervises one process.
type Child struct {
	Name   string
	New    func() *exec.Cmd
	Policy Policy
	Log    *slog.Logger
	// OnStart is called after each successful start.
	OnStart func(pid int)
	// OnExit is called after each exit; restarting tells whether the
	// policy will start it again; gaveUp is set when the policy gave up.
	OnExit func(err error, restarting, gaveUp bool)

	mu       sync.Mutex
	wanted   bool
	cmd      *exec.Cmd
	pid      int
	started  time.Time
	restarts []time.Time
	gaveUp   bool
	stop     chan struct{}
	done     chan struct{}
}

// Start begins supervising (no-op when already running). It resets the
// give-up state.
func (c *Child) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wanted {
		return
	}
	c.wanted = true
	c.gaveUp = false
	c.restarts = nil
	c.stop = make(chan struct{})
	c.done = make(chan struct{})
	go c.loop(c.stop, c.done)
}

// Running reports whether the process is currently running.
func (c *Child) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cmd != nil
}

// Wanted reports whether the child is supervised (started and not given up
// or stopped).
func (c *Child) Wanted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wanted
}

// GaveUp reports whether the restart policy gave up.
func (c *Child) GaveUp() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gaveUp
}

// PID returns the running process id, or 0.
func (c *Child) PID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pid
}

// StartedAt returns when the current process started.
func (c *Child) StartedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started
}

// SignalProcess sends sig to the child process only, not its group (whose
// other members are Emacs subprocesses such as language servers and
// shells, for which SIGUSR2 would be fatal).
func (c *Child) SignalProcess(sig syscall.Signal) error {
	c.mu.Lock()
	pid := c.pid
	c.mu.Unlock()
	if pid == 0 {
		return os.ErrProcessDone
	}
	return syscall.Kill(pid, sig)
}

// Stop ends supervision: SIGTERM to the process group, SIGKILL after grace.
// It waits for the supervision loop to finish.
func (c *Child) Stop(grace time.Duration) {
	c.mu.Lock()
	if !c.wanted {
		done := c.done
		c.mu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	c.wanted = false
	close(c.stop)
	pid := c.pid
	done := c.done
	c.mu.Unlock()
	if pid != 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		select {
		case <-done:
			return
		case <-time.After(grace):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}
	<-done
}

func (c *Child) log() *slog.Logger {
	if c.Log == nil {
		return slog.Default()
	}
	return c.Log
}

func (c *Child) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	attempt := 0
	for {
		cmd := c.New()
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.Setpgid = true
		err := cmd.Start()
		start := time.Now()
		if err == nil {
			c.mu.Lock()
			c.cmd, c.pid, c.started = cmd, cmd.Process.Pid, start
			if !c.wanted {
				// Stop ran between Start and here and saw no pid.
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			}
			c.mu.Unlock()
			c.log().Info("child started", "child", c.Name, "pid", cmd.Process.Pid)
			if c.OnStart != nil {
				c.OnStart(cmd.Process.Pid)
			}
			err = cmd.Wait()
			// Reap the rest of the group (daemon subprocesses).
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		c.mu.Lock()
		c.cmd, c.pid = nil, 0
		wanted := c.wanted
		c.mu.Unlock()
		if !wanted {
			if c.OnExit != nil {
				c.OnExit(err, false, false)
			}
			return
		}
		if c.Policy.ResetAfter > 0 && time.Since(start) >= c.Policy.ResetAfter {
			attempt = 0
		}
		now := time.Now()
		c.mu.Lock()
		c.restarts = append(c.restarts, now)
		if c.Policy.Window > 0 {
			kept := c.restarts[:0]
			for _, t := range c.restarts {
				if now.Sub(t) <= c.Policy.Window {
					kept = append(kept, t)
				}
			}
			c.restarts = kept
		}
		giveUp := c.Policy.MaxRestarts > 0 && len(c.restarts) > c.Policy.MaxRestarts
		if giveUp {
			c.gaveUp = true
			c.wanted = false
		}
		c.mu.Unlock()
		c.log().Warn("child exited", "child", c.Name, "err", errString(err), "restarting", !giveUp)
		if c.OnExit != nil {
			c.OnExit(err, !giveUp, giveUp)
		}
		if giveUp {
			return
		}
		var delay time.Duration
		if n := len(c.Policy.Backoff); n > 0 {
			i := attempt
			if i >= n {
				i = n - 1
			}
			delay = c.Policy.Backoff[i]
		}
		attempt++
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}
	}
}

func errString(err error) string {
	if err == nil {
		return "exit status 0"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.String()
	}
	return err.Error()
}
