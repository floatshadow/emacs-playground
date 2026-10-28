/**
 * Facts about the special modes the later parts open (terminals, Magit,
 * Org, ediff, dape, calc, …) that the advice engine needs whatever the
 * curriculum tables say (frontend.md §6): which commands pass keys to a
 * terminal, and which windows are tools or pop-ups rather than a split
 * the learner made.
 */
import type { Window } from '../protocol.gen'

/**
 * Commands that send the key to a program running in a terminal buffer
 * (vterm, eat in semi-char or char mode, term in char mode): the key does
 * whatever the shell makes of it, so a habit card about its Emacs meaning
 * would be wrong.
 */
export const TERMINAL_INPUT_COMMANDS: ReadonlySet<string> = new Set([
  'vterm--self-insert',
  'vterm-send-key',
  'eat-self-input',
  'eat-input-char',
  'term-send-raw',
  'term-send-raw-meta',
])

/** Major modes of terminal emulators. */
export const TERMINAL_MODES: ReadonlySet<string> = new Set(['vterm-mode', 'eat-mode', 'term-mode'])

/**
 * Major modes whose windows a tool opens on its own (Magit's diff beside a
 * commit message, ediff's control window, dape's info and REPL windows,
 * calc's trail, the agenda): they are not windows the learner split.
 */
const TOOL_MODE = /^(magit-|ediff-|dape-|gud-|calc-|calc$|org-agenda-|edebug-|ielm-|debugger-)/

/** Buffer names a tool pops up that are not "*name*" buffers. */
const TOOL_BUFFER = /^(COMMIT_EDITMSG|git-rebase-todo|MERGE_MSG|TAG_EDITMSG)$/

/**
 * True for a window that a tool or pop-up shows (a side window, a "*name*"
 * buffer, a tool mode): the "three windows" pattern does not count it.
 */
export function isToolWindow(w: Pick<Window, 'buf' | 'mode' | 'side'>): boolean {
  if (w.side) return true
  if (w.buf.startsWith('*') || w.buf.startsWith(' ')) return true
  if (TOOL_BUFFER.test(w.buf)) return true
  return TOOL_MODE.test(w.mode)
}

/**
 * True when a key description is one printable character (a, S-a, SPC):
 * what a terminal input command counts as typing. Chords such as C-a sent
 * to a shell are commands in their own right.
 */
export function isPrintableKey(keys: string): boolean {
  const k = keys.trim()
  if (k === 'SPC') return true
  const bare = k.replace(/^S-/, '')
  return [...bare].length === 1
}
