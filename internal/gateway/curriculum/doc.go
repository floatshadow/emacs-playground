// Package curriculum serves the built curriculum bundle (backend.md §2.8,
// api.md §2.2 and §2.6): loads <curriculum.dir>/curriculum.json, checks
// protocol.CurriculumFormat, serves the index, lesson bundles
// (protocol.LessonBundle), assets, scenarios and zstd fixture tarballs with
// ETags. Loading and validation of the source format are in
// internal/curriculum.
//
// TODO(gateway, backend.md §2.8).
package curriculum
