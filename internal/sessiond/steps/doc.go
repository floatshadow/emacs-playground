// Package steps is the step engine (backend.md §3.8, architecture.md §3.2):
// holds the active protocol.Scenario and step, counts commands, evaluates
// conditions (event, elisp batched through the agent, file through os.Root,
// shell with timeouts, layout with protocol.MatchLayout, state with
// protocol.Predicate) debounced to 150 ms, emits check and step events,
// evaluates stuck rules on a 2 s ticker and emits nudges.
//
// TODO(sessiond, backend.md §3.8).
package steps
