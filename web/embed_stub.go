//go:build !embedweb

// Package web embeds the built web bundle into the gateway binary
// (frontend.md §15). Build the bundle with `pnpm -C web build`, then the
// gateway with `go build -tags embedweb ./cmd/gateway`. Without the tag
// (plain `go build ./...`, development behind the Vite dev server) Dist
// reports no bundle and the gateway serves a short notice instead of the
// app.
package web

import "io/fs"

// Dist returns nil, false: this binary was built without -tags embedweb.
func Dist() (fs.FS, bool) { return nil, false }
