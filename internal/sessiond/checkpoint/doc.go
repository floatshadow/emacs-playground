// Package checkpoint records and restores git-based checkpoints under
// refs/playground/ with a temporary index, plus the protocol.WindowSpec
// saved next to each ref (backend.md §3.9, ADR-009).
//
// TODO(sessiond, backend.md §3.9).
package checkpoint
