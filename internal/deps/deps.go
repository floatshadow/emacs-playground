// Package deps pins every third-party Go module from docs/third-party.md §5
// with blank imports, so that go.mod and go.sum stay complete (and go mod
// tidy keeps them) before the components that use them are written. Nothing
// imports this package. Remove an entry only when a real import replaces it
// and the module is still needed, or when the module is dropped from
// third-party.md.
package deps

import (
	_ "github.com/coder/websocket"                              // WebSockets for pty, events, vnc
	_ "github.com/coreos/go-oidc/v3/oidc"                       // OIDC login
	_ "github.com/creack/pty"                                   // pseudo-terminals for tmux attach
	_ "github.com/fsnotify/fsnotify"                            // workspace watching
	_ "github.com/goccy/go-yaml"                                // scenario and table parsing
	_ "github.com/invopop/jsonschema"                           // JSON Schema from protocol and config structs
	_ "github.com/klauspost/compress/zstd"                      // zstd fixture tarballs
	_ "github.com/moby/moby/api/types/container"                // Engine API types
	_ "github.com/moby/moby/api/types/network"                  // Engine API types
	_ "github.com/moby/moby/api/types/volume"                   // Engine API types
	_ "github.com/moby/moby/client"                             // Docker Engine API client
	_ "github.com/prometheus/client_golang/prometheus"          // optional metrics
	_ "github.com/prometheus/client_golang/prometheus/promhttp" // optional metrics handler
	_ "github.com/testcontainers/testcontainers-go"             // service integration tests
	_ "golang.org/x/oauth2"                                     // OIDC login
	_ "modernc.org/sqlite"                                      // gateway store without cgo
)
