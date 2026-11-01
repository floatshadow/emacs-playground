package session

import (
	"context"
	"errors"
	"strings"

	"github.com/floatshadow/emacs-playground/internal/httpx"
	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
)

// restore is rpc restore (backend.md §3.9): the checkpoint's files and
// editor state come back, then the step it was taken for restarts as reset
// (counted). Nothing activates when the restore failed or ctx ended.
func (c *Core) restore(ctx context.Context, op *Op) (any, error) {
	ref, err := c.resolveRestore(ctx, op.Req.(protocol.RestoreRequest))
	if err != nil {
		return nil, err
	}
	if lesson, step, ok := splitStepRef(ref); ok {
		c.setTarget(op, lesson, step)
	}
	n, meta, err := c.d.Snapshots.Restore(ctx, ref)
	if err != nil {
		if errors.Is(err, checkpoint.ErrMetadata) {
			return nil, httpx.Conflict("checkpoint metadata is missing or unreadable").Wrap(err)
		}
		if isNotExist(err) {
			return nil, httpx.NotFound("no such checkpoint")
		}
		return nil, httpx.Internal("restore incomplete").Wrap(err)
	}
	if ctx.Err() != nil {
		return nil, interrupted(ctx)
	}
	if meta.Step != nil {
		c.resetTo(meta.Step.LessonID, meta.Step.StepID, true)
	} else if lesson, step, ok := splitStepRef(ref); ok {
		c.resetTo(lesson, step, true)
	}
	return protocol.RestoreResult{RestoredFiles: n}, nil
}

// resolveRestore turns a restore request into a ref: the ref, the newest
// checkpoint with the label, or the active step's checkpoint.
func (c *Core) resolveRestore(ctx context.Context, req protocol.RestoreRequest) (string, error) {
	switch {
	case req.Ref != "" && req.Label != "":
		return "", httpx.BadRequest("send either label or ref, not both")
	case req.Ref != "":
		if !checkpoint.ValidRef(req.Ref) {
			return "", httpx.BadRequest("ref must be a refs/playground/ checkpoint")
		}
		return req.Ref, nil
	case req.Label != "":
		if ref := checkpoint.ManualRef(req.Label); c.d.Snapshots.Exists(ctx, ref) {
			if meta, err := c.d.Snapshots.ReadMeta(ref); err != nil || meta.Label == req.Label {
				return ref, nil
			}
		}
		items, err := c.d.Snapshots.List(ctx)
		if err != nil {
			return "", httpx.Internal("could not list checkpoints").Wrap(err)
		}
		for _, it := range items {
			if it.Label == req.Label {
				return it.Ref, nil
			}
		}
		return "", httpx.NotFound("no checkpoint labelled " + req.Label)
	default:
		act := c.Active()
		if act == nil {
			return "", httpx.Conflict("no step is active; name a checkpoint")
		}
		return checkpoint.StepRef(act.LessonID, act.StepID), nil
	}
}

// splitStepRef parses refs/playground/<part>/<lesson>/<step>.
func splitStepRef(ref string) (lesson, step string, ok bool) {
	rest := strings.TrimPrefix(ref, checkpoint.RefPrefix)
	if rest == ref || strings.HasPrefix(ref, checkpoint.ManualPrefix) {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '/')
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}
