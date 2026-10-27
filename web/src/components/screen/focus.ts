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

/** Keys that only modify others: pressing one alone is not input. */
const MODIFIER_KEYS = new Set([
  'Shift',
  'Control',
  'Alt',
  'AltGraph',
  'Meta',
  'OS',
  'Super',
  'Hyper',
  'Fn',
  'FnLock',
  'CapsLock',
  'NumLock',
  'ScrollLock',
  'Symbol',
  'SymbolLock',
])

/**
 * True for a keydown the learner typed (not a lone modifier). The terminal
 * reports learner input from its key handler and paste path, never from
 * xterm.js's onData: that also carries what the terminal answers on its own
 * (focus-in and focus-out reports with tmux's focus-events, mouse reports,
 * OSC 52 replies), which must not stop a demo.
 */
export function isLearnerKeydown(e: Pick<KeyboardEvent, 'type' | 'key'>): boolean {
  return e.type === 'keydown' && e.key !== '' && !MODIFIER_KEYS.has(e.key)
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
