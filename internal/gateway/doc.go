// Package gateway wires the gateway service (backend.md §2): the
// net/http.ServeMux with method patterns, the middleware chain (request id,
// logging, recover, Origin check for state-changing requests and WebSocket
// upgrades, auth per route group), the static web bundle with SPA fallback
// and security headers, /fonts, /api/v1 (api.md §2), /s/{id}/ proxying,
// /internal for sessiond (bearer token per session), /healthz and the
// optional Prometheus listener.
//
//	app, err := gateway.Build(ctx, cfg, gateway.Options{Web: dist})
//	err = app.Run(ctx)  // serves until ctx ends, then drains for 10 s
//
// Build opens the store (JSON in local mode, SQLite in server mode), the
// cookie secret, the invites file, the curriculum bundle and the session
// backend (static or docker), and rebuilds the session registry.
package gateway
