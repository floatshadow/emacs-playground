// Package curriculum serves the built curriculum bundle (backend.md §2.8,
// api.md §2.2 and §2.6) from <curriculum.dir> in the layout of
// protocol/curriculum.go:
//
//	GET /api/v1/curriculum                         curriculum[.<locale>].json
//	GET /api/v1/curriculum/lessons/{lessonId...}   lessons/<id>/bundle[.<locale>].json
//	GET /api/v1/curriculum/assets/{lessonId}/{path} lessons/<id>/assets/<path>
//	GET /internal/curriculum/scenarios/{lessonId...} lessons/<id>/scenario.json
//	GET /internal/curriculum/fixtures/{lessonId...}  fixtures/<id>.tar.zst, 204 without fixtures
//
// Files are served with strong ETags (SHA-256 of the content, cached by
// size and modification time) so If-None-Match answers 304; sessiond caches
// fixtures by ETag. Files are opened through os.Root so lesson ids and
// asset paths cannot escape the bundle directory.
//
// At start the bundle's format must equal protocol.CurriculumFormat or the
// gateway refuses to start; a missing bundle only logs a warning and the
// curriculum endpoints answer 503 until it appears (Reload, SIGHUP, or the
// automatic reload in author mode when curriculum.json changes).
//
// The locale is ?locale=, else the best Accept-Language match among the
// built locales, else curriculum.defaultLocale.
package curriculum
