// Package session is the session core (backend.md §3.12): the single owner
// of lesson orchestration. Load, resume, restore, module changes and drops
// are ops run one at a time by one worker, each to completion with its own
// deadline and detached from the HTTP request that asked for it; a request
// that goes away changes nothing. Ops check their context between phases;
// a deadline or a preempting drop ends them there, before anything
// activates. A load that restarts Emacs for its modules answers
// {restarting: true} early and finishes in the background; later requests
// queue behind it and get final results.
package session
