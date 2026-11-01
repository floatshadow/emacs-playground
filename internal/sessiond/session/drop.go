package session

// drop applies a gateway reset (rpc drop-step, and SyncGenerations for a
// reset missed while the session was unavailable): the queued reports of
// the lesson (with "" of every lesson) are purged, and its saved progress,
// lap and active step go when the generation is newer than the one
// applied, with a step {status: dropped} event. Submit runs it next and,
// when the generation is newer, cancels a running op on the same lesson,
// which then activates nothing.
func (c *Core) drop(lessonID string, generation int64) {
	if c.d.PurgeReports != nil {
		c.d.PurgeReports(lessonID, generation)
	}
	c.dropGeneration(lessonID, generation)
}
