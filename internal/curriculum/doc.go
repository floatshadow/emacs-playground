// Package curriculum loads, validates and builds the curriculum source tree
// (CURRICULUM.md Part B) for playctl and tests: index.yaml, each lesson's
// lesson.md front matter and scenario.yaml, fixtures, assets and the advice
// tables.
//
//	src, diags := curriculum.Load("curriculum")   // parse and validate
//	if diags.HasErrors() { ... }
//	err := curriculum.Build(src, "out", curriculum.BuildOptions{})
//
// YAML is decoded by converting it to JSON with github.com/goccy/go-yaml's
// YAMLToJSON and unmarshalling into the internal/protocol types with unknown
// fields rejected, so the protocol package needs only JSON (un)marshalers.
//
// Validation (CURRICULUM.md Part B.5): schema (strict decoding), exactly one
// kind per condition (protocol.Condition.Kinds), key names
// (protocol.ParseKeys) in :kbd and :try directives, habits and cheats, demo
// strings (protocol.ParseDemo), layout patterns (protocol.ParseLayoutPattern),
// predicates (protocol.ValidatePredicate) whose paths start at a known state
// field, fixture paths (regular files and directories only), asset and setup
// paths, and index references (every ready lesson has a directory, every
// lesson directory is listed).
//
// The built bundle layout is the contract in protocol/curriculum.go
// (curriculum.json, lessons/<id>/bundle.json and scenario.json,
// lessons/<id>/assets/, fixtures/<id>.tar.zst). Fixture tarballs are
// reproducible: sorted entries, zero mtimes, uid and gid 0, one zstd
// encoder goroutine, so their sha256 is a stable ETag.
package curriculum
