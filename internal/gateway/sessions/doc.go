// Package sessions owns the session registry and lifecycle (backend.md
// §2.3-§2.4, architecture.md §5.1).
//
// Service implements Manager (Ensure, Get, Stop, Touch, Reap) over a Backend
// and the store:
//
//   - StaticBackend (local mode): one pre-existing session at
//     local.sessionUrl, shared by every user of the install; Create, Start,
//     Stop and Remove are no-ops and Inspect probes sessiond's /healthz, so
//     the request path is the same as on a server.
//   - DockerBackend (server mode, experimental in slice 1): one container
//     per learner through internal/docker, named pg-session-<id>, labelled
//     playground.session, playground.user, playground.version and
//     playground.role, on the sessions network, with the workspace volume
//     pg-ws-<user> at /work and the hardening of deployment.md §9.2.
//
// Ensure is idempotent and serialised per user with a keyed mutex: a running
// healthy session is returned; a stopped or failed one is re-created on the
// same volume with a fresh token; otherwise a new session is created,
// started and polled on /healthz for up to 30 s. Failures mark the session
// failed and remove the container, keeping the volume.
//
// Presence: the proxy reports WebSocket opens and closes (Opened, Closed);
// sessiond's heartbeat reports its own count. Reap runs every minute:
// sessions without WebSockets turn idle, and after sessions.idleStopMinutes
// are stopped (container removed, volume kept); volumes of sessions stopped
// longer than sessions.volumeRetentionDays are removed with an audit entry.
// Rebuild lists labelled containers at start so a gateway restart never
// orphans running sessions.
//
// Session tokens are 256-bit random values given to the container as
// PG_SESSION_TOKEN; only their SHA-256 is stored. Authenticate maps a bearer
// token back to its session for /internal.
//
// TODO(gateway, deployment.md §6): the per-learner remote box (needs the
// learner's public key from the workspace) and TODO(gateway,
// deployment.md §9.2): the registries egress proxy and HTTP(S)_PROXY, with
// NO_PROXY=gateway (the gateway refuses /internal requests that carry
// X-Forwarded-For in server mode).
package sessions
