// Package protocol is the single source of truth for every type that crosses
// a process boundary in Emacs Playground:
//
//   - WebSocket events on /s/{id}/events (api.md §4) and the Emacs state
//     object (api.md §5): events.go, state.go.
//   - PTY and VNC control frames and close codes (api.md §3.1 to §3.3):
//     session.go.
//   - Session RPC requests and results (api.md §3.4): rpc.go.
//   - The agent socket protocol between sessiond and playground-agent.el
//     (api.md §7): agent.go.
//   - The scenario object, conditions, stuck rules and the demo and key
//     syntaxes (api.md §6, CURRICULUM.md Part B.3): scenario.go, keys.go,
//     demo.go, layout.go.
//   - The curriculum index, lesson bundles and advice tables served by the
//     gateway (api.md §2.2, CURRICULUM.md Part B.4): curriculum.go.
//   - The gateway REST API and the sessiond-to-gateway internal API
//     (api.md §2): api.go.
//   - Problem details and stable error codes (api.md §1): errors.go.
//   - The deployment configuration file and sessiond's environment
//     (deployment.md §3): config.go.
//
// Rules for this package:
//
//   - Standard library only. Schema generators (TypeScript, JSON Schema) live
//     in other packages and reflect over these types.
//   - JSON field names are the wire names in the docs: camelCase for API and
//     event fields, snake_case only where the authoring format uses it
//     (scenario conditions such as not_contains, stuck rule fields such as
//     minibuffer_seconds).
//   - Optional fields carry omitempty; nullable fields are pointers without
//     omitempty so that they serialise as null.
//   - Timestamps are milliseconds since the Unix epoch (int64) in fields named
//     ts, createdAt, updatedAt, completedAt, lastActiveAt.
//   - Enumerations are typed strings with exported constants. Consumers must
//     tolerate unknown values (forward compatibility); producers must only
//     send the listed ones.
//   - Component code must not add wire types elsewhere. Change this package
//     (through integration) and regenerate web/src/protocol.gen.ts with
//     `playctl protocol typescript`.
package protocol
