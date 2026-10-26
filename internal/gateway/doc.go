// Package gateway wires the gateway service (backend.md §2): the HTTP mux
// with method patterns, the middleware chain (request id, logging, recover,
// auth, Origin check), static web assets with SPA fallback, /api/v1,
// /s/{id}/ proxying, /internal for sessiond, /healthz and /metrics.
//
// TODO(gateway, backend.md §2.1-§2.2): server.go with New(cfg, deps) and Run.
package gateway
