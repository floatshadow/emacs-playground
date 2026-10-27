/** Test fixtures: the state object from api.md §5 and the Go protocol tests. */
import type { State } from '../protocol.gen'

export function apiState(over: Partial<State> = {}): State {
  return {
    t: 'state',
    seq: 419,
    ts: 1761472000130,
    buf: 'main.rs',
    file: '/work/hello/src/main.rs',
    mode: 'rust-ts-mode',
    modeName: 'Rust',
    minors: ['eglot--managed-mode', 'corfu-mode', 'flymake-mode'],
    modified: true,
    readonly: false,
    narrowed: false,
    overwrite: false,
    line: 9,
    col: 20,
    point: 182,
    markActive: false,
    windows: [
      { id: 3, buf: 'main.rs', mode: 'rust-ts-mode', selected: true, dedicated: false, side: null },
      {
        id: 7,
        buf: '*compilation*',
        mode: 'compilation-mode',
        selected: false,
        dedicated: false,
        side: 'bottom',
      },
    ],
    layout: ['vsplit', 3, 7],
    frame: 'terminal',
    frames: 1,
    tabs: { count: 1, current: 'main' },
    minibuffer: null,
    isearch: false,
    recursive: 0,
    macro: false,
    prefixArg: null,
    readOnlyReason: null,
    diagAtPoint: { severity: 'error', message: 'mismatched types', line: 9, col: 20 },
    waiting: null,
    ...over,
  }
}

/** One window showing main.rs, nothing unusual. */
export function calmState(over: Partial<State> = {}): State {
  return apiState({
    modified: false,
    windows: [
      { id: 3, buf: 'main.rs', mode: 'rust-ts-mode', selected: true, dedicated: false, side: null },
    ],
    layout: 3,
    diagAtPoint: null,
    waiting: null,
    ...over,
  })
}
