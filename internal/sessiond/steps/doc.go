// Package steps is the condition evaluator (backend.md §3.8,
// architecture.md §3.2): for the attempt the session core began (Begin) it
// counts commands, evaluates the step's conditions (event, elisp batched
// through the agent, file through os.Root, shell with timeouts, layout with
// protocol.MatchLayout, state with protocol.Predicate, combinators
// all/any/not with three-valued logic) debounced (an evaluation runs
// DefaultDebounce after the last trigger and at most DefaultDebounceMax
// after the first) and on a 2 s ticker, and hands each result with the
// attempt's key-stream statistics to the core as an Observation. What a
// completion means (done, demonstrated, skipped, nothing) is the core's
// business, as are progress, laps, checkpoints and stuck rules.
//
// Three-valued logic: a condition that cannot be evaluated (elisp timeout or
// error, shell timeout, no state yet) is unknown. all is false if any child
// is false, else unknown if any is unknown; any is true if any child is
// true, else unknown if any is unknown; not keeps unknown. A step completes
// only when every top-level condition is true.
//
// Shell conditions are cached until the next file event or attempt, so
// the 2 s ticker does not re-run builds; file conditions are cheap and read
// fresh each time (capped at 1 MB).
package steps
