package session

// drop applies a gateway reset (rpc drop-step): saved progress, the lap and
// the active step of the lesson (with "" of every lesson) go when the
// generation is newer than the one applied, with a step {status: dropped}
// event, and so do the queued reports of that scope. Submit runs it next
// and, when the generation is newer, cancels a running op on the same
// lesson, which then activates nothing.
func (c *Core) drop(lessonID string, generation int64) {
	if c.d.DropProgress != nil {
		c.d.DropProgress(lessonID, generation)
		return
	}
	c.d.Engine.DropGeneration(lessonID, generation)
}
