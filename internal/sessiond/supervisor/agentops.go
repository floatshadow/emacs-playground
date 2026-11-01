package supervisor

import (
	"context"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
)

// AgentOps are the agent ops the supervisor calls (api.md §7).
type AgentOps interface {
	// LeaveDisplay deletes the GUI frames and closes Emacs's X display
	// connections before Xvnc stops.
	LeaveDisplay(ctx context.Context) (protocol.AgentLeaveDisplayResult, error)
	// ResumeLoop restarts the command loop's wait for keys on the
	// terminal frame.
	ResumeLoop(ctx context.Context) error
	// ResetDebug undoes what the debug signal (SIGUSR2) leaves set.
	ResetDebug(ctx context.Context) error
}

// agentOps implements AgentOps with Agent.Call.
type agentOps struct{ a Agent }

const opTimeout = 2 * time.Second

func (o agentOps) LeaveDisplay(ctx context.Context) (protocol.AgentLeaveDisplayResult, error) {
	var res protocol.AgentLeaveDisplayResult
	err := o.a.Call(ctx, protocol.AgentOpLeaveDisplay, nil, opTimeout, &res)
	return res, err
}

func (o agentOps) ResumeLoop(ctx context.Context) error {
	return o.a.Call(ctx, protocol.AgentOpResumeLoop, nil, opTimeout, nil)
}

func (o agentOps) ResetDebug(ctx context.Context) error {
	return o.a.Call(ctx, protocol.AgentOpResetDebug, nil, opTimeout, nil)
}
