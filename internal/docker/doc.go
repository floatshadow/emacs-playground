// Package docker is a thin wrapper over the official Engine client
// (github.com/moby/moby/client) used by gateway/sessions in server mode:
// create, start, stop, remove, inspect and list labelled containers, and
// create, list and remove workspace volumes.
//
// Client is the interface the sessions package depends on, so the docker
// backend is tested with a fake; Engine implements it against a real
// daemon (normally through the docker-socket-proxy). Spec conversion to the
// Engine API types (CreateOptions) is a pure function, tested on its own.
//
// Harden applies the session container profile of deployment.md §9.2:
// uid 1000, read-only root filesystem with tmpfs /tmp, /run and
// /home/learner/.cache, cap_drop ALL, no-new-privileges, pids, CPU and
// memory limits with memory-swap equal to memory, a nofile ulimit (no nproc
// ulimit: it counts per uid across the host, and every session is uid
// 1000), no published ports, an init process and the configured runtime.
//
// The gateway only ever acts on containers and volumes carrying its labels
// (LabelRole and LabelSession).
package docker
