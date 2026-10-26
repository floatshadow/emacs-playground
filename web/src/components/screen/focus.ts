/**
 * Focus helpers that do not import xterm.js: the hosts register themselves,
 * and buttons elsewhere (Rescue, Try it) can return focus to Emacs.
 */
let focusFn: (() => void) | null = null
let inputFns = new Set<() => void>()

export function registerTerminalFocus(fn: () => void): void {
  focusFn = fn
}

/** Puts keyboard focus on whichever frame is visible (no-op before the screen mounts). */
export function focusTerminal(): void {
  focusFn?.()
}

/** Subscribes to learner keyboard input in the terminal (the demo player cancels on it). */
export function onTerminalInput(fn: () => void): () => void {
  inputFns.add(fn)
  return () => {
    inputFns.delete(fn)
  }
}

export function emitTerminalInput(): void {
  for (const fn of inputFns) fn()
}

/** Test helper. */
export function resetFocusRegistry(): void {
  focusFn = null
  inputFns = new Set()
}
