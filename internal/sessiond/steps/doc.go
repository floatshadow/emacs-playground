// Package steps is the step engine (backend.md §3.8, architecture.md §3.2):
// it holds the active protocol.Scenario and step, counts commands, evaluates
// conditions (event, elisp batched through the agent, file through os.Root,
// shell with timeouts, layout with protocol.MatchLayout, state with
// protocol.Predicate, combinators all/any/not with three-valued logic)
// debounced (an evaluation runs DefaultDebounce after the last trigger and
// at most DefaultDebounceMax after the first), emits check and step events,
// reports progress, takes the next step's checkpoint when a step completes,
// and evaluates stuck rules on a 2 s ticker, emitting nudges.
//
// Three-valued logic: a condition that cannot be evaluated (elisp timeout or
// error, shell timeout, no state yet) is unknown. all is false if any child
// is false, else unknown if any is unknown; any is true if any child is
// true, else unknown if any is unknown; not keeps unknown. A step completes
// only when every top-level condition is true.
//
// Shell conditions are cached until the next file event or step change, so
// the 2 s ticker does not re-run builds; file conditions are cheap and read
// fresh each time (capped at 1 MB).
package steps
