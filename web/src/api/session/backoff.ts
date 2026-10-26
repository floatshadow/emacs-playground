/**
 * Reconnect backoff for the session WebSockets (frontend.md §4,
 * architecture.md §3.8): exponential from 250 ms to 8 s with jitter.
 */
export const BACKOFF_MIN_MS = 250
export const BACKOFF_MAX_MS = 8000

/** Delay before reconnect attempt n (0-based): half fixed, half random. */
export function backoffDelay(attempt: number, random: () => number = Math.random): number {
  const ceiling = Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** Math.max(0, attempt))
  return Math.round(ceiling / 2 + random() * (ceiling / 2))
}
