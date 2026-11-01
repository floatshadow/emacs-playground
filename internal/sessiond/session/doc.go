// Package session is the session core (backend.md §3.8, §3.12): the single
// owner of the lesson state (scenario, active step, attempt, phase, demo
// latch, saved progress) and of lesson orchestration. Load, resume,
// restore, module changes, drops, demo start, skip and the advance after a
// completion are ops run one at a time by one worker, each to completion
// with its own deadline and detached from the HTTP request that asked for
// it; a request that goes away changes nothing. Ops check their context
// between phases; a deadline or a preempting drop ends them there, before
// anything activates. A load that restarts Emacs for its modules answers
// {restarting: true} early and finishes in the background; later requests
// queue behind it and get final results.
//
// Recovery goes through the same worker: the progress sync, daemon
// restarts, the editor state an agent gets when it says hello after a
// restore made while it was away (reconnect), the reopen after a crash
// (recover) and the autosave of snapshot.json are ops too, so none of
// them interleaves with a restore or a capture.
//
// Every activation is a new Attempt. The evaluator (package steps) reports
// observations of the attempt it was given; Observe ignores those of any
// other attempt and applies the rest according to the Phase: done and
// advance (Active), demonstrated once (Watching), nothing (Skipping,
// Rehearsing, Advancing, Idle). Demo start and skip run only for the
// attempt they were admitted for. Every outcome is recorded in the
// progress outbox (package progress) under the core's lock, as drops,
// syncs and the startup reconciliation change it, so state.json is written
// from one consistent reading of both (Persistent).
package session
