// Package docker is a thin wrapper over the official Engine client
// (github.com/moby/moby/client) used by gateway/sessions: create, start,
// stop, remove and inspect labelled session and remote-box containers and
// workspace volumes, with the hardening options of deployment.md §9.2. It
// exposes an interface so the docker backend can be tested with a fake.
//
// TODO(gateway, backend.md §2.3, deployment.md §2 and §9.2).
package docker
