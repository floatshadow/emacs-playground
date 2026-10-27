/**
 * Extended keys for the terminal frame (frontend.md §5, emacs.md §5).
 *
 * xterm.js encodes keys the legacy way unless the application pushes kitty
 * keyboard flags, and tmux never forwards Emacs's push to the outer
 * terminal. In the legacy encoding many chords Emacs binds do not exist:
 * C-; (iedit), C-= (expreg), C-? (undo-redo), C-> (multiple cursors),
 * C-1 (digit-argument), C-S-a, C-RET, and Ctrl+Alt with a symbol xterm.js
 * would send without Ctrl (C-M-_, C-M-/, C-M-\). For exactly those, the terminal pane
 * sends a CSI u sequence itself (`ESC [ code ; modifiers u`); tmux
 * (extended-keys always, csi-u) parses it and passes the key to Emacs, and
 * sessiond's input tap decodes it for the ticker. Everything legacy can
 * express keeps its legacy bytes, so nothing else changes.
 */

/** The subset of KeyboardEvent the encoder looks at. */
export interface KeyLike {
  type: string
  key: string
  ctrlKey: boolean
  altKey: boolean
  shiftKey: boolean
  metaKey: boolean
  /** AltGr on European layouts reports ctrl+alt in Chrome on Windows. */
  altGraph?: boolean
}

/**
 * Ctrl chords the legacy encoding already carries (C-a … C-z and friends).
 * With Alt as well, xterm.js sends ESC and the plain character, dropping
 * Ctrl (Ctrl+Alt+_ would be M-_, not C-M-_ undo-redo), so only Ctrl+Alt+SPC
 * stays legacy.
 */
const LEGACY_CTRL = new Set([' ', '@', '[', '\\', ']', '^', '_', '/'])

/**
 * Returns the CSI u sequence for a key legacy encoding cannot express, or
 * null to let xterm.js encode it.
 */
export function extendedKeySequence(e: KeyLike): string | null {
  if (e.type !== 'keydown' || e.metaKey || e.altGraph) return null
  const mods = 1 + (e.shiftKey ? 1 : 0) + (e.altKey ? 2 : 0) + (e.ctrlKey ? 4 : 0)
  if (e.key === 'Enter' && (e.ctrlKey || e.shiftKey)) return `\x1b[13;${mods}u`
  if (e.key === 'Backspace' && e.ctrlKey) return `\x1b[127;${mods}u`
  if (!e.ctrlKey) return null
  // Printable keys: e.key is the character the layout produced.
  if ([...e.key].length !== 1) return null
  const ch = e.key
  if (/^[a-z]$/.test(ch) && !e.shiftKey) return null // C-a … C-z, C-M-a …
  if (/^[A-Z]$/.test(ch)) {
    // Ctrl+Shift+letter: the base (lowercase) key with the shift modifier.
    return `\x1b[${ch.toLowerCase().charCodeAt(0)};${mods}u`
  }
  if (LEGACY_CTRL.has(ch) && (!e.altKey || ch === ' ')) return null
  // A shifted symbol (?, >, %) already carries the shift: report the
  // character itself without it, so C-? reaches Emacs as C-?.
  const isSymbol = !/^[a-z0-9]$/i.test(ch)
  const m = isSymbol && e.shiftKey ? mods - 1 : mods
  return `\x1b[${ch.codePointAt(0)};${m}u`
}
