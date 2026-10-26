// Package curriculum loads and validates the curriculum source tree
// (CURRICULUM.md Part B) for playctl and the gateway: index.yaml, each
// lesson's lesson.md front matter and scenario.yaml, fixtures and tables.
//
// YAML is decoded by converting it to JSON with
// github.com/goccy/go-yaml's YAMLToJSON and unmarshalling into the
// internal/protocol types with unknown fields rejected, so the protocol
// package needs only JSON (un)marshalers.
//
// Validation (CURRICULUM.md Part B.5): schema, exactly one kind per
// condition (protocol.Condition.Kinds), key names (protocol.ParseKeys),
// demo strings (protocol.ParseDemo), layout patterns
// (protocol.ParseLayoutPattern), predicates (protocol.ValidatePredicate),
// fixture paths, index references.
//
// TODO(playctl, backend.md §4, CURRICULUM.md Part B.5).
package curriculum
