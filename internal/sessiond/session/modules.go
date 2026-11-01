package session

import (
	"context"
	"slices"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/scenario"
)

// validModules checks an rpc modules set: valid names, agent included.
func validModules(mods []string) error {
	if err := scenario.ValidateModules(mods); err != nil {
		return httpx.BadRequest(err.Error())
	}
	if !slices.Contains(mods, "agent") {
		return httpx.BadRequest("the agent module cannot be disabled in the playground")
	}
	return nil
}

// setModules is rpc modules: when the set changes it writes modules.el,
// announces the restart, posts {restarting: true}, restarts the daemon and
// announces the restart as over.
func (c *Core) setModules(ctx context.Context, op *Op) (bool, error) {
	mods := op.Req.([]string)
	if slices.Equal(c.d.Modules.Current(), mods) {
		return false, nil
	}
	if ctx.Err() != nil {
		return false, interrupted(ctx)
	}
	if err := c.d.Modules.Write(mods); err != nil {
		return false, httpx.Internal("could not write modules.el").Wrap(err)
	}
	c.emit(&protocol.ModulesEvent{Enabled: mods, Restarting: true})
	op.post(true)
	c.restart("module set changed")
	c.emit(&protocol.ModulesEvent{Enabled: mods, Restarting: false})
	return true, nil
}
