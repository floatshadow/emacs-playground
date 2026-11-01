package session

import (
	"context"

	"github.com/floatshadow/emacs-playground/internal/sessiond/snapshot"
)

// pendingEditor is a restore made while the agent was away: its files are
// back, and its editor state goes to the agent's next hello while seq is
// still the current attempt (a later activation put another state in
// place).
type pendingEditor struct {
	ref string
	seq uint64
}

// deferEditor follows a restore of ref (deferred by the ops that restore,
// so it runs after the activation that follows the restore): when the agent
// was away, the checkpoint's editor state waits for the next hello, for the
// attempt current now.
func (c *Core) deferEditor(ref string, rep snapshot.Report) {
	if rep.EditorDeferred {
		c.pending = &pendingEditor{ref: ref, seq: c.attemptSeq()}
	} else {
		c.pending = nil
	}
}

// takePending returns the pending editor state if its attempt is still
// current, and forgets it either way.
func (c *Core) takePending() *pendingEditor {
	p := c.pending
	c.pending = nil
	if p == nil || p.seq != c.attemptSeq() {
		return nil
	}
	return p
}

// Reconnected queues the reconnect op for an agent that said hello (not
// for a daemon restarted after a crash: AfterRestart covers it). Being an
// op, it runs after the op in progress, so a restore that was still
// running when the agent came back, or whose step had not activated yet,
// has registered its editor state by then.
func (c *Core) Reconnected() {
	c.Submit(&Op{Kind: OpReconnect, Deadline: EditorDeadline, Preempt: true})
}

// reconnect is the reconnect op: the pending editor state, if any.
func (c *Core) reconnect(ctx context.Context) {
	if p := c.takePending(); p != nil {
		c.restoreEditor(ctx, p.ref)
	}
}

// restoreEditor restores the editor state of checkpoint ref; it reports
// false when that failed or the checkpoint is gone.
func (c *Core) restoreEditor(ctx context.Context, ref string) bool {
	if err := c.d.Snapshots.RestoreEditor(ctx, ref); err != nil {
		c.log.Warn("restoring the editor state of a restore made while the agent was away", "ref", ref, "err", err)
		return false
	}
	return true
}

// AfterRestart is the supervisor's hook once a restarted daemon said hello:
// it reopens the editor state (reopenEditor). Every restart sessiond asks
// for (modules, a load, rpc restart-emacs) runs inside an op, so the hook
// then runs in the worker and reopens inline: waiting for the queue there
// would deadlock. After a crash it runs on the supervisor's goroutine and
// waits for the recover op, so the reopen never interleaves with an op.
func (c *Core) AfterRestart(ctx context.Context, crashed bool) {
	if !crashed {
		c.reopenEditor(ctx)
		return
	}
	if _, err := c.do(ctx, &Op{Kind: OpRecover, Deadline: EditorDeadline, Preempt: true}); err != nil {
		c.log.Warn("reopening the editor state after a crash", "err", err)
	}
}

// reopenEditor puts the editor state back in a fresh daemon: a restore's
// state still waiting for the agent, else snapshot.json.
func (c *Core) reopenEditor(ctx context.Context) {
	if p := c.takePending(); p != nil && c.restoreEditor(ctx, p.ref) {
		return
	}
	if err := c.d.Snapshots.Open(ctx, c.d.Snapshots.LastEditorState()); err != nil {
		c.log.Warn("reopening the buffers after a restart", "err", err)
	}
}
