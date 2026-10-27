/**
 * The fake session's Emacs: a TypeScript port of design/demo/mock-emacs.js
 * (about sixty commands, the minibuffer, windows, and the "oops" states:
 * narrowing, overwrite, macros, closed frame, suspend). It is NOT Emacs.
 *
 * Differences from the demo: buffers visit files in a workspace "disk"
 * (paths relative to /work) so saves produce file events and the preview
 * server has something to serve; the engine reports protocol events (key,
 * prefix, echo, minibuffer, emacs, file) through `emit`, and `state()`
 * builds the State object the agent would send.
 */
import type { Event, Layout, State, Window } from '../src/protocol.gen.ts'
import { DEFAULT_GLOSS } from '../src/advice/defaults.ts'
import { normalizeKeysLoose } from '../src/advice/keys.ts'
import { COMPILE_FAIL, COMPILE_OK } from './workspace.ts'

export const BINDINGS: Record<string, string> = {
  'C-x C-f': 'find-file',
  'C-x C-s': 'save-buffer',
  'C-x s': 'save-some-buffers',
  'C-x C-b': 'list-buffers',
  'C-x b': 'switch-to-buffer',
  'C-x k': 'kill-buffer',
  'C-x 0': 'delete-window',
  'C-x 1': 'delete-other-windows',
  'C-x 2': 'split-window-below',
  'C-x 3': 'split-window-right',
  'C-x o': 'other-window',
  'C-x C-c': 'save-buffers-kill-terminal',
  'C-x C-q': 'read-only-mode',
  'C-x n n': 'narrow-to-region',
  'C-x n d': 'narrow-to-defun',
  'C-x n w': 'widen',
  'C-x (': 'kmacro-start-macro',
  'C-x )': 'kmacro-end-macro',
  '<f3>': 'kmacro-start-macro',
  '<f4>': 'kmacro-end-or-call-macro',
  'C-x u': 'undo',
  'C-/': 'undo',
  'C-_': 'undo',
  'C-x C-e': 'eval-last-sexp',
  'C-x h': 'mark-whole-buffer',
  'C-x d': 'dired',
  'C-g': 'keyboard-quit',
  'ESC ESC ESC': 'keyboard-escape-quit',
  'C-s': 'isearch-forward',
  'C-r': 'isearch-backward',
  'M-x': 'execute-extended-command',
  'M-%': 'query-replace',
  'C-h k': 'describe-key',
  'C-h f': 'describe-function',
  'C-h m': 'describe-mode',
  'C-h t': 'help-with-tutorial',
  'C-h .': 'display-local-help',
  'C-z': 'suspend-frame',
  'C-x C-z': 'suspend-frame',
  'C-a': 'move-beginning-of-line',
  'C-e': 'move-end-of-line',
  'C-f': 'forward-char',
  'C-b': 'backward-char',
  'C-n': 'next-line',
  'C-p': 'previous-line',
  '<left>': 'left-char',
  '<right>': 'right-char',
  '<up>': 'previous-line',
  '<down>': 'next-line',
  '<home>': 'move-beginning-of-line',
  '<end>': 'move-end-of-line',
  'M-f': 'forward-word',
  'M-b': 'backward-word',
  'M-<': 'beginning-of-buffer',
  'M->': 'end-of-buffer',
  'C-v': 'scroll-up-command',
  'M-v': 'scroll-down-command',
  'C-l': 'recenter-top-bottom',
  'C-k': 'kill-line',
  'C-y': 'yank',
  'C-w': 'kill-region',
  'M-w': 'kill-ring-save',
  'C-SPC': 'set-mark-command',
  'C-d': 'delete-char',
  DEL: 'delete-backward-char',
  'M-d': 'kill-word',
  RET: 'newline',
  TAB: 'indent-for-tab-command',
  '<insert>': 'overwrite-mode',
  'C-u': 'universal-argument',
  'M-g M-n': 'next-error',
  'M-g g': 'goto-line',
  'M-.': 'xref-find-definitions',
  'M-,': 'xref-go-back',
  'C-c C-c': 'compile',
  'M-;': 'comment-dwim',
  'C-x C-;': 'comment-line',
}

const LOCAL: Record<string, Record<string, string>> = {
  'Buffer-menu-mode': {
    q: 'quit-window',
    RET: 'Buffer-menu-this-window',
    n: 'next-line',
    p: 'previous-line',
  },
  'help-mode': { q: 'quit-window', n: 'next-line', p: 'previous-line' },
  'dired-mode': {
    q: 'quit-window',
    RET: 'dired-find-file',
    n: 'next-line',
    p: 'previous-line',
    '^': 'dired-up-directory',
  },
  'compilation-mode': {
    q: 'quit-window',
    RET: 'compile-goto-error',
    n: 'next-error-no-select',
    g: 'recompile',
  },
  'messages-buffer-mode': { q: 'quit-window' },
}

const WHICHKEY: Record<string, [string, string][]> = {
  'C-x': [
    ['C-f', 'find-file'],
    ['C-s', 'save-buffer'],
    ['C-b', 'list-buffers'],
    ['b', 'switch-to-buffer'],
    ['k', 'kill-buffer'],
    ['1', 'delete-other-windows'],
    ['2', 'split-window-below'],
    ['3', 'split-window-right'],
    ['0', 'delete-window'],
    ['o', 'other-window'],
    ['C-c', 'save-buffers-kill-terminal'],
    ['C-q', 'read-only-mode'],
    ['n', '+narrow'],
    ['u', 'undo'],
    ['h', 'mark-whole-buffer'],
    ['d', 'dired'],
    ['(', 'kmacro-start-macro'],
    [')', 'kmacro-end-macro'],
  ],
  'C-x n': [
    ['n', 'narrow-to-region'],
    ['d', 'narrow-to-defun'],
    ['w', 'widen'],
  ],
  'C-h': [
    ['k', 'describe-key'],
    ['f', 'describe-function'],
    ['m', 'describe-mode'],
    ['t', 'help-with-tutorial'],
    ['.', 'display-local-help'],
  ],
  'M-g': [
    ['M-n', 'next-error'],
    ['g', 'goto-line'],
  ],
  'C-c': [['C-c', 'compile']],
  ESC: [
    ['ESC ESC', 'keyboard-escape-quit'],
    ['x', 'execute-extended-command (same as M-x)'],
  ],
  'ESC ESC': [['ESC', 'keyboard-escape-quit']],
}

const GLOSS: Record<string, string> = DEFAULT_GLOSS

const PRINTABLE = /^[\x20-\x7e]$/u
const isPrintable = (k: string) =>
  k === 'SPC' || ([...k].length === 1 && (PRINTABLE.test(k) || k.charCodeAt(0) > 0x7f))
const keyChar = (k: string) => (k === 'SPC' ? ' ' : k)

interface Diag {
  line: number
  col: number
  len: number
  msg: string
}

export interface Buffer {
  name: string
  mode: string
  modeName: string
  minors: string
  /** Workspace-relative path for file buffers. */
  path: string | null
  text: string
  orig: string
  point: number
  mark: number | null
  /** Transient-mark-mode: the region is active (highlighted). M-< sets the mark without it. */
  active: boolean
  modified: boolean
  readonly: boolean
  special: boolean
  narrow: [number, number] | null
  overwrite: boolean
  undo: { text: string; point: number }[]
  scroll: number
  diags: Diag[]
}

interface Win {
  id: number
  buf: string
  side?: boolean
}

interface Mini {
  kind: string
  prompt: string
  input: string
  def?: string
  start?: number
  origin?: number
  match?: [number, number] | null
  fail?: boolean
  from?: string
  depth: number
}

export interface EngineHooks {
  emit(ev: Event): void
  /** A buffer was saved: the server updates the disk and emits file events. */
  saved?(path: string, text: string): void
  /** The frame was closed (C-x C-c) or suspended (C-z). */
  now?(): number
}

export function guessMode(name: string): { mode: string; modeName: string; minors: string } {
  if (name.endsWith('.rs'))
    return { mode: 'rust-ts-mode', modeName: 'Rust', minors: 'Eglot Flymake' }
  if (name.endsWith('.md')) return { mode: 'markdown-mode', modeName: 'Markdown', minors: '' }
  if (name.endsWith('.toml')) return { mode: 'conf-toml-mode', modeName: 'Conf[TOML]', minors: '' }
  if (name.endsWith('.py')) return { mode: 'python-ts-mode', modeName: 'Python', minors: 'Eglot' }
  if (name.endsWith('.typ')) return { mode: 'typst-ts-mode', modeName: 'Typst', minors: 'Eglot' }
  if (name.endsWith('.ml')) return { mode: 'tuareg-mode', modeName: 'Tuareg', minors: 'Eglot' }
  if (name.endsWith('.html')) return { mode: 'mhtml-mode', modeName: 'HTML+', minors: '' }
  return { mode: 'fundamental-mode', modeName: 'Fundamental', minors: '' }
}

const MINOR_SYMBOLS: Record<string, string> = {
  Eglot: 'eglot--managed-mode',
  Flymake: 'flymake-mode',
}

let winIds = 3

export class MockEmacs {
  buffers: Record<string, Buffer> = {}
  windows: Win[] = []
  sel = 0
  mini: Mini | null = null
  echo = ''
  prefix: string[] = []
  prefixAt = 0
  argPending = false
  macro = false
  closed = false
  suspended = false
  recursive = 0
  kill = ''
  counts: Record<string, number> = {}
  total = 0
  lastCmd: string | null = null
  prevBuf: string | null = null
  describe: string[] | null = null
  disk: Map<string, string>
  hooks: EngineHooks
  private timers: ReturnType<typeof setTimeout>[] = []

  constructor(disk: Map<string, string>, hooks: EngineHooks) {
    this.disk = disk
    this.hooks = hooks
    this.reset([])
  }

  now(): number {
    return this.hooks.now?.() ?? Date.now()
  }

  /** Fresh buffers: *scratch*, *Messages*, and the given files opened. */
  reset(open: string[]) {
    this.buffers = {}
    this.make('*scratch*', {
      mode: 'lisp-interaction-mode',
      modeName: 'Lisp Interaction',
      text: ';; This buffer is for text that is not saved, and for Lisp evaluation.\n;; To create a file, visit it with C-x C-f and enter text in its buffer.\n\n',
    })
    this.make('*Messages*', {
      mode: 'messages-buffer-mode',
      modeName: 'Messages',
      special: true,
      readonly: true,
      text: 'Loading /home/learner/.config/emacs/init.el (source)...done\nFor information about GNU Emacs and the GNU system, type C-h C-a.\n',
    })
    let first = '*scratch*'
    for (const p of open) {
      const b = this.visit(p)
      if (b && first === '*scratch*') first = b.name
    }
    this.windows = [{ id: winIds++, buf: first }]
    this.sel = 0
    this.mini = null
    this.prefix = []
    this.macro = false
    this.recursive = 0
    this.describe = null
    this.echo = ''
  }

  private make(
    name: string,
    f: {
      mode: string
      modeName: string
      text: string
      minors?: string
      special?: boolean
      readonly?: boolean
      path?: string | null
      diags?: Diag[]
    },
  ): Buffer {
    const b: Buffer = {
      name,
      mode: f.mode,
      modeName: f.modeName,
      minors: f.minors ?? '',
      path: f.path ?? null,
      text: f.text,
      orig: f.text,
      point: 0,
      mark: null,
      active: false,
      modified: false,
      readonly: !!f.readonly,
      special: !!f.special,
      narrow: null,
      overwrite: false,
      undo: [],
      scroll: 0,
      diags: (f.diags ?? []).map((d) => ({ ...d })),
    }
    this.buffers[name] = b
    return b
  }

  /** Opens a workspace file into a buffer (or returns the existing one). */
  visit(path: string): Buffer | null {
    const existing = Object.values(this.buffers).find((b) => b.path === path)
    if (existing) return existing
    const text = this.disk.get(path)
    if (text === undefined) return null
    const base = path.split('/').pop() ?? path
    let name = base
    if (this.buffers[name]) name = `${base}<${path.split('/').slice(-2, -1)[0] ?? ''}>`
    const gm = guessMode(base)
    const diags: Diag[] =
      path === 'polyglot/src/main.rs' && /greet\(name\)\)/.test(text)
        ? [{ line: 8, col: 25, len: 4, msg: 'error: expected `&str`, found `String` [E0308]' }]
        : []
    return this.make(name, { ...gm, path, text, diags })
  }

  dispose() {
    this.timers.forEach((t) => clearTimeout(t))
  }

  private later(ms: number, fn: () => void) {
    this.timers.push(setTimeout(fn, ms))
  }

  cur(): Buffer {
    const w = this.windows[this.sel] ?? this.windows[0]
    return this.buffers[w?.buf ?? '*scratch*'] ?? (this.buffers['*scratch*'] as Buffer)
  }

  private otherBufferName(): string {
    const curName = this.cur().name
    if (this.prevBuf && this.buffers[this.prevBuf] && this.prevBuf !== curName) return this.prevBuf
    return (
      Object.keys(this.buffers).find((n) => n !== curName && !this.buffers[n]?.special) ??
      '*scratch*'
    )
  }

  private showBuffer(name: string, winIndex?: number) {
    const w = this.windows[winIndex ?? this.sel]
    if (!w) return
    if (w.buf !== name) this.prevBuf = w.buf
    w.buf = name
  }

  private ensureBuffer(name: string, f?: Parameters<MockEmacs['make']>[1]): Buffer {
    return (
      this.buffers[name] ??
      this.make(name, f ?? { mode: 'fundamental-mode', modeName: 'Fundamental', text: '' })
    )
  }

  /* --- text helpers --- */
  lineOf(b: Buffer, p: number): [number, number] {
    const t = b.text.slice(0, p)
    const l = (t.match(/\n/g) ?? []).length
    return [l, p - (t.lastIndexOf('\n') + 1)]
  }
  private lineStart(b: Buffer, p: number) {
    return b.text.lastIndexOf('\n', p - 1) + 1
  }
  private lineEnd(b: Buffer, p: number) {
    const e = b.text.indexOf('\n', p)
    return e < 0 ? b.text.length : e
  }
  private clamp(b: Buffer, p: number) {
    const lo = b.narrow ? b.narrow[0] : 0
    const hi = b.narrow ? b.narrow[1] : b.text.length
    return Math.max(lo, Math.min(hi, p))
  }
  private snapshot(b: Buffer) {
    b.undo.push({ text: b.text, point: b.point })
    if (b.undo.length > 100) b.undo.shift()
  }
  private insert(b: Buffer, s: string): boolean {
    if (b.readonly) {
      this.echo = 'Buffer is read-only: #<buffer ' + b.name + '>'
      return false
    }
    this.snapshot(b)
    if (b.overwrite && s !== '\n' && b.point < b.text.length && b.text[b.point] !== '\n')
      b.text = b.text.slice(0, b.point) + s + b.text.slice(b.point + 1)
    else b.text = b.text.slice(0, b.point) + s + b.text.slice(b.point)
    if (b.narrow) b.narrow[1] += s.length
    b.point += s.length
    b.modified = b.text !== b.orig
    this.shiftDiags(b, b.point - s.length, s.length)
    return true
  }
  private del(b: Buffer, from: number, to: number): string {
    if (from >= to) return ''
    if (b.readonly) {
      this.echo = 'Buffer is read-only: #<buffer ' + b.name + '>'
      return ''
    }
    this.snapshot(b)
    const s = b.text.slice(from, to)
    b.text = b.text.slice(0, from) + b.text.slice(to)
    if (b.narrow) b.narrow[1] -= s.length
    b.point = from
    b.modified = b.text !== b.orig
    this.shiftDiags(b, from, -s.length)
    return s
  }
  private shiftDiags(b: Buffer, at: number, delta: number) {
    for (const d of b.diags) {
      const [l, c] = this.lineOf(b, at)
      if (d.line === l) {
        if (c <= d.col) d.col += delta
        else if (c < d.col + d.len) d.len += delta
      }
    }
    b.diags = b.diags.filter((d) => d.len > 0)
  }
  private moveLine(b: Buffer, dir: number) {
    const [l, c] = this.lineOf(b, b.point)
    const lines = b.text.split('\n')
    const nl = l + dir
    if (nl < 0 || nl >= lines.length) {
      this.echo = dir < 0 ? 'Beginning of buffer' : 'End of buffer'
      return
    }
    let p = 0
    for (let i = 0; i < nl; i++) p += (lines[i] as string).length + 1
    b.point = this.clamp(b, p + Math.min(c, (lines[nl] as string).length))
  }
  private wordMove(b: Buffer, dir: number) {
    if (dir > 0) {
      const re = /[A-Za-z0-9_]+/g
      re.lastIndex = b.point
      const m = re.exec(b.text)
      b.point = this.clamp(b, m ? m.index + m[0].length : b.text.length)
    } else {
      const t = b.text.slice(0, b.point)
      const ms = [...t.matchAll(/[A-Za-z0-9_]+/g)]
      b.point = this.clamp(b, ms.length ? (ms[ms.length - 1]?.index ?? 0) : 0)
    }
  }
  diagAtPoint(b: Buffer): Diag | undefined {
    const [l, c] = this.lineOf(b, b.point)
    return b.diags.find((d) => d.line === l && c >= d.col && c <= d.col + d.len)
  }

  /* --- events --- */
  private emitKey(keys: string[], cmd: string, mx = false) {
    const b = this.cur()
    const ev: Event = {
      t: 'key',
      keys: keys.join(' '),
      cmd,
      buf: b.name,
      mode: b.mode,
      frame: 'terminal',
    }
    if (mx) ev.mx = true
    this.hooks.emit(ev)
  }
  private count(name: string) {
    this.counts[name] = (this.counts[name] ?? 0) + 1
    this.total++
  }

  /* --- minibuffer --- */
  private openMini(m: Omit<Mini, 'input' | 'depth'> & { input?: string }) {
    this.mini = { input: '', depth: 1, ...m }
    this.echo = ''
    this.hooks.emit({
      t: 'minibuffer',
      open: true,
      prompt: m.prompt,
      depth: 1,
      kind: m.kind === 'isearch' ? 'isearch' : 'prompt',
    })
  }
  private closeMini() {
    const m = this.mini
    this.mini = null
    if (m)
      this.hooks.emit({
        t: 'minibuffer',
        open: false,
        prompt: m.prompt,
        depth: 0,
        kind: m.kind === 'isearch' ? 'isearch' : 'prompt',
      })
  }

  private dirOf(b: Buffer): string {
    if (!b.path) return 'hello/'
    const parts = b.path.split('/')
    parts.pop()
    return parts.join('/') + '/'
  }
  /** Workspace-relative path from what was typed: ~/x, /work/x or relative. */
  private resolvePath(input: string): string {
    let p = input.trim()
    // "//" restarts at the root and "/~/" at home, as file-name-shadow-mode shows.
    const i = Math.max(p.lastIndexOf('//'), p.lastIndexOf('/~/'))
    if (i >= 0) p = p.slice(i + 1)
    p = p
      .replace(/^~\//, '')
      .replace(/^\/work\//, '')
      .replace(/^\//, '')
    return p
  }

  private completeMini() {
    const m = this.mini
    if (!m) return
    if (m.kind === 'find-file') {
      const dirTyped = m.input.slice(0, m.input.lastIndexOf('/') + 1)
      const part = m.input.slice(dirTyped.length)
      const dir = this.resolvePath(dirTyped)
      const names = new Set<string>()
      for (const p of this.disk.keys()) {
        if (!p.startsWith(dir)) continue
        const rest = p.slice(dir.length)
        const seg = rest.split('/')[0] ?? ''
        names.add(rest.includes('/') ? seg + '/' : seg)
      }
      const cands = [...names].filter((n) => n.startsWith(part)).sort()
      if (cands.length === 1) m.input = dirTyped + cands[0]
      else if (cands.length > 1) {
        m.input = dirTyped + commonPrefix(cands)
        this.echo = '{' + cands.join(' | ') + '}'
      } else this.echo = '[No match]'
    } else if (m.kind === 'switch-buffer' || m.kind === 'kill-buffer') {
      const cands = Object.keys(this.buffers).filter((n) =>
        n.toLowerCase().includes(m.input.toLowerCase()),
      )
      if (cands.length === 1) m.input = cands[0] as string
      else if (cands.length) this.echo = '{' + cands.join(' | ') + '}'
      else this.echo = '[No match]'
    } else if (m.kind === 'M-x') {
      const cands = Object.keys(GLOSS).filter((n) => n.startsWith(m.input))
      if (cands.length === 1) m.input = cands[0] as string
      else if (cands.length) {
        m.input = commonPrefix(cands)
        this.echo = '{' + cands.slice(0, 8).join(' | ') + (cands.length > 8 ? ' | …' : '') + '}'
      } else this.echo = '[No match]'
    }
    this.count('minibuffer-complete')
    this.emitKey(['TAB'], 'minibuffer-complete')
  }

  private submitMini() {
    const m = this.mini
    if (!m) return
    const input = m.input
    this.count('exit-minibuffer')
    if (m.kind === 'find-file') {
      this.closeMini()
      const path = this.resolvePath(input)
      this.emitKey(['RET'], 'exit-minibuffer')
      if (path === '' || input.trim().endsWith('/')) {
        this.openDired(path)
        return
      }
      if (this.disk.has(path) || Object.values(this.buffers).some((b) => b.path === path)) {
        const b = this.visit(path)
        if (b) this.showBuffer(b.name)
        this.echo = ''
      } else if ([...this.disk.keys()].some((p) => p.startsWith(path + '/'))) {
        this.openDired(path + '/')
      } else {
        const base = path.split('/').pop() ?? path
        const b = this.ensureBuffer(base, { ...guessMode(base), text: '', path })
        this.showBuffer(b.name)
        this.echo = '(New file)'
      }
      return
    }
    this.emitKey(['RET'], 'exit-minibuffer')
    if (m.kind === 'switch-buffer') {
      this.closeMini()
      const name = input || m.def || '*scratch*'
      this.ensureBuffer(name)
      this.showBuffer(name)
      this.echo = ''
    } else if (m.kind === 'kill-buffer') {
      this.closeMini()
      const name = input || m.def || ''
      if (!this.buffers[name]) {
        this.echo = 'No such buffer: ' + name
        return
      }
      delete this.buffers[name]
      for (const w of this.windows)
        if (w.buf === name)
          w.buf = Object.keys(this.buffers).find((n) => !this.buffers[n]?.special) ?? '*scratch*'
      this.echo = ''
    } else if (m.kind === 'M-x') {
      if (!GLOSS[input] || input === 'self-insert-command') {
        this.echo = '[No match]'
        return
      }
      this.closeMini()
      this.runCommand(input, ['M-x'], null, true)
    } else if (m.kind === 'query-replace-from') {
      if (!input) {
        this.echo = '[Empty string]'
        return
      }
      this.closeMini()
      this.openMini({
        kind: 'query-replace-to',
        prompt: 'Query replace ' + input + ' with: ',
        from: input,
      })
    } else if (m.kind === 'query-replace-to') {
      this.closeMini()
      const b = this.cur()
      const from = m.from ?? ''
      const n = b.text.split(from).length - 1
      if (n) {
        this.snapshot(b)
        b.text = b.text.split(from).join(input)
        b.modified = b.text !== b.orig
      }
      this.echo = 'Replaced ' + n + ' occurrence' + (n === 1 ? '' : 's')
    } else if (m.kind === 'goto-line') {
      this.closeMini()
      const b = this.cur()
      const lines = b.text.split('\n')
      const n = Math.max(1, Math.min(lines.length, parseInt(input || '1', 10) || 1))
      let p = 0
      for (let i = 0; i < n - 1; i++) p += (lines[i] as string).length + 1
      b.point = this.clamp(b, p)
      this.echo = ''
    } else if (m.kind === 'compile') {
      this.closeMini()
      this.doCompile(input || 'cargo build')
    } else if (m.kind === 'describe-function') {
      this.closeMini()
      this.openHelp(
        input +
          ' is an interactive command.\n\n' +
          (GLOSS[input] ?? '(not in this fake)') +
          '\n\nIt is bound to ' +
          (Object.keys(BINDINGS)
            .filter((k) => BINDINGS[k] === input)
            .join(', ') || 'no key') +
          '.',
      )
    } else this.closeMini()
  }

  private abortMini() {
    const m = this.mini
    this.closeMini()
    if (m?.kind === 'isearch' && m.start !== undefined) this.cur().point = m.start
    this.echo = 'Quit'
  }

  private openDired(dir: string) {
    const d = dir.replace(/\/$/, '')
    const prefix = d ? d + '/' : ''
    const names = new Map<string, boolean>()
    for (const p of this.disk.keys()) {
      if (!p.startsWith(prefix)) continue
      const rest = p.slice(prefix.length)
      const seg = rest.split('/')[0] ?? ''
      if (seg) names.set(seg, rest.includes('/'))
    }
    const entries = [...names.entries()]
      .sort()
      .map(([n, isDir]) =>
        isDir
          ? '  drwxr-xr-x 1 learner learner  4096 Oct 26 17:55 ' + n
          : '  -rw-r--r-- 1 learner learner ' +
            String((this.disk.get(prefix + n) ?? '').length).padStart(5) +
            ' Oct 26 17:55 ' +
            n,
      )
    const text =
      '  /work/' +
      d +
      ':\n  total used in directory 24 available 1.2 TiB\n  drwxr-xr-x 1 learner learner  4096 Oct 26 17:55 .\n  drwxr-xr-x 1 learner learner  4096 Oct 26 17:55 ..\n' +
      entries.join('\n') +
      '\n'
    const name = (d.split('/').pop() || 'work') + '/'
    const b = this.ensureBuffer(name, {
      mode: 'dired-mode',
      modeName: 'Dired by name',
      special: true,
      readonly: true,
      text,
      path: null,
    })
    b.text = text
    b.orig = text
    b.point = text.indexOf('\n', text.indexOf(' ..')) + 1 + 2
    ;(b as Buffer & { diredDir?: string }).diredDir = prefix
    this.showBuffer(name)
    this.echo = ''
  }
  private openHelp(text: string) {
    const b = this.ensureBuffer('*Help*', {
      mode: 'help-mode',
      modeName: 'Help',
      special: true,
      readonly: true,
      text,
    })
    b.text = text
    b.point = 0
    this.showInOther('*Help*')
  }
  private showInOther(name: string) {
    if (this.windows.length === 1) this.windows.push({ id: winIds++, buf: name })
    else {
      const w = this.windows[(this.sel + 1) % this.windows.length]
      if (w) w.buf = name
    }
  }
  private bufferList(): string {
    const rows = Object.values(this.buffers).map(
      (b) =>
        ' ' +
        (b.readonly ? '%' : ' ') +
        (b.modified ? '*' : ' ') +
        ' ' +
        b.name.padEnd(16) +
        String(b.text.length).padStart(6) +
        '  ' +
        b.modeName.padEnd(18) +
        (b.path ? '/work/' + b.path : ''),
    )
    return ' CRM Buffer           Size  Mode              File\n' + rows.join('\n') + '\n'
  }
  private doCompile(cmdline: string, show = true) {
    const main = Object.values(this.buffers).find((b) => b.path === 'polyglot/src/main.rs')
    const src = main?.path
      ? (this.disk.get(main.path) ?? main.text)
      : (this.disk.get('polyglot/src/main.rs') ?? '')
    const ok = /println!\("\{\}", greet\(&name\)\)/.test(src)
    const b = this.ensureBuffer('*compilation*', {
      mode: 'compilation-mode',
      modeName: 'Compilation',
      special: true,
      readonly: true,
      text: '',
    })
    b.text = (ok ? COMPILE_OK : COMPILE_FAIL).replace('cargo build', cmdline)
    b.point = 0
    if (ok && main) main.diags = []
    if (!show) return
    this.showInOther('*compilation*')
    this.echo = ok ? 'Compilation finished' : 'Compilation exited abnormally with code 101'
  }
  private searchFrom(m: Mini) {
    const b = this.cur()
    if (!m.input) {
      b.point = m.start ?? b.point
      m.fail = false
      return
    }
    const i = b.text.indexOf(m.input, m.origin ?? 0)
    if (i >= 0) {
      b.point = i + m.input.length
      m.match = [i, i + m.input.length]
      m.fail = false
    } else {
      m.fail = true
      m.match = null
    }
  }

  private save(b: Buffer) {
    if (!b.path) {
      this.echo = 'Buffer ' + b.name + ' is not visiting a file'
      return
    }
    if (!b.modified && this.disk.has(b.path)) {
      this.echo = '(No changes need to be saved)'
      return
    }
    b.modified = false
    b.orig = b.text
    this.disk.set(b.path, b.text)
    this.echo = 'Wrote /work/' + b.path
    this.hooks.saved?.(b.path, b.text)
  }

  /* --- commands --- */
  private readonly CMD: Record<string, (arg?: string | null) => void> = {
    'find-file': () => {
      const b = this.cur()
      this.openMini({
        kind: 'find-file',
        prompt: 'Find file: ',
        input: '~/' + (b.path ? this.dirOf(b) : 'hello/'),
      })
    },
    dired: () =>
      this.openMini({
        kind: 'find-file',
        prompt: 'Dired (directory): ',
        input: '~/' + this.dirOf(this.cur()),
      }),
    'save-buffer': () => this.save(this.cur()),
    'save-some-buffers': () => {
      const dirty = Object.values(this.buffers).filter((b) => b.modified && b.path)
      dirty.forEach((b) => this.save(b))
      this.echo = dirty.length
        ? `Saved ${dirty.length} buffer${dirty.length === 1 ? '' : 's'}`
        : '(No files need saving)'
    },
    'list-buffers': () => {
      const b = this.ensureBuffer('*Buffer List*', {
        mode: 'Buffer-menu-mode',
        modeName: 'Buffer Menu',
        special: true,
        readonly: true,
        text: '',
      })
      b.text = this.bufferList()
      b.point = b.text.indexOf('\n') + 1
      this.showInOther('*Buffer List*')
      this.echo = 'Commands: d, s, x, u; f, o, 1, 2, m, v; ~, %; q to quit; ? for help.'
    },
    'switch-to-buffer': () => {
      const d = this.otherBufferName()
      this.openMini({
        kind: 'switch-buffer',
        prompt: 'Switch to buffer (default ' + d + '): ',
        def: d,
      })
    },
    'kill-buffer': () =>
      this.openMini({
        kind: 'kill-buffer',
        prompt: 'Kill buffer (default ' + this.cur().name + '): ',
        def: this.cur().name,
      }),
    'delete-window': () => {
      if (this.windows.length === 1) {
        this.echo = 'Attempt to delete sole ordinary window'
        return
      }
      this.windows.splice(this.sel, 1)
      this.sel = Math.max(0, this.sel - 1)
    },
    'delete-other-windows': () => {
      this.windows = [this.windows[this.sel] as Win]
      this.sel = 0
    },
    'split-window-below': () => {
      if (this.windows.length >= 4) {
        this.echo = 'Window too small for splitting'
        return
      }
      this.windows.splice(this.sel + 1, 0, { id: winIds++, buf: this.cur().name })
    },
    'split-window-right': () => {
      if (this.windows.length >= 4) {
        this.echo = 'Window too small for splitting'
        return
      }
      this.windows.splice(this.sel + 1, 0, { id: winIds++, buf: this.cur().name, side: true })
    },
    'other-window': () => {
      this.sel = (this.sel + 1) % this.windows.length
    },
    'save-buffers-kill-terminal': () => {
      this.closed = true
      this.hooks.emit({ t: 'emacs', event: 'frame-closed', frame: 'terminal', reason: 'C-x C-c' })
      this.later(2600, () => {
        this.closed = false
        this.echo =
          'Playground: your frame was closed by C-x C-c; it has been reopened. Buffers are intact.'
        this.hooks.emit({ t: 'emacs', event: 'frame-opened', frame: 'terminal' })
        this.changed()
      })
    },
    'read-only-mode': () => {
      const b = this.cur()
      b.readonly = !b.readonly
      this.echo = b.readonly
        ? 'Read-Only mode enabled in current buffer'
        : 'Read-Only mode disabled in current buffer'
    },
    'narrow-to-region': () => {
      const b = this.cur()
      if (b.mark == null) {
        this.echo = 'The mark is not set now, so there is no region'
        return
      }
      b.narrow = [Math.min(b.mark, b.point), Math.max(b.mark, b.point)]
      b.mark = null
    },
    'narrow-to-defun': () => {
      const b = this.cur()
      const t = b.text
      const before = t.slice(0, b.point)
      const s = Math.max(
        before.lastIndexOf('\nfn '),
        before.lastIndexOf('\npub fn '),
        before.lastIndexOf('\ndef '),
        before.lastIndexOf('\nlet '),
      )
      const start = s < 0 ? 0 : s + 1
      let e = t.indexOf('\n}\n', start)
      e = e < 0 ? t.length : e + 3
      b.narrow = [start, e]
      b.point = this.clamp(b, b.point)
    },
    widen: () => {
      this.cur().narrow = null
    },
    'kmacro-start-macro': () => {
      if (this.macro) {
        this.echo = 'Already defining keyboard macro'
        return
      }
      this.macro = true
      this.echo = 'Defining kbd macro...'
    },
    'kmacro-end-macro': () => {
      if (!this.macro) {
        this.echo = 'Not defining kbd macro'
        return
      }
      this.macro = false
      this.echo = 'Keyboard macro defined'
    },
    'kmacro-end-or-call-macro': () => {
      if (this.macro) {
        this.macro = false
        this.echo = 'Keyboard macro defined'
      } else this.echo = 'No kbd macro has been defined'
    },
    undo: () => {
      const b = this.cur()
      const u = b.undo.pop()
      if (!u) {
        this.echo = 'No further undo information'
        return
      }
      b.text = u.text
      b.point = u.point
      b.modified = b.text !== b.orig
      this.echo = 'Undo'
    },
    'eval-last-sexp': () => {
      this.echo = this.cur().mode === 'lisp-interaction-mode' ? '42' : 'End of file during parsing'
    },
    'mark-whole-buffer': () => {
      const b = this.cur()
      b.mark = b.text.length
      b.active = true
      b.point = 0
      this.echo = 'Mark set'
    },
    'keyboard-quit': () => {
      if (this.mini) {
        this.abortMini()
        return
      }
      if (this.macro) {
        this.macro = false
        this.echo = 'Quit (keyboard macro cancelled)'
        return
      }
      this.cur().mark = null
      this.argPending = false
      this.echo = 'Quit'
    },
    'keyboard-escape-quit': () => {
      if (this.mini) this.closeMini()
      this.recursive = 0
      if (this.windows.length > 1) {
        this.windows = [this.windows[this.sel] as Win]
        this.sel = 0
      }
      this.cur().mark = null
      this.echo = ''
    },
    'isearch-forward': () => {
      const b = this.cur()
      this.openMini({ kind: 'isearch', prompt: 'I-search: ', start: b.point, origin: b.point })
    },
    'isearch-backward': () => {
      const b = this.cur()
      this.openMini({ kind: 'isearch', prompt: 'I-search backward: ', start: b.point, origin: 0 })
    },
    'execute-extended-command': () => this.openMini({ kind: 'M-x', prompt: 'M-x ' }),
    'query-replace': () => this.openMini({ kind: 'query-replace-from', prompt: 'Query replace: ' }),
    'describe-key': () => {
      this.describe = []
      this.echo = 'Describe key (or click or menu item): '
    },
    'describe-function': () =>
      this.openMini({ kind: 'describe-function', prompt: 'Describe function: ' }),
    'describe-mode': () => {
      const c = this.cur()
      this.openHelp(
        c.name +
          ' is in ' +
          c.modeName +
          ' mode.\n\nThe major mode is ' +
          c.mode +
          '.\nMinor modes enabled: ' +
          (c.minors || 'none') +
          '.\n\nkey  binding\n---  -------\nC-c C-c  compile\nM-.      xref-find-definitions\n',
      )
    },
    'help-with-tutorial': () =>
      this.openHelp(
        'Emacs tutorial.  See end for copying conditions.\n\nEmacs commands generally involve the CONTROL key...\n\n(In the playground the lesson pane on the left replaces it.)',
      ),
    'display-local-help': () => {
      const d = this.diagAtPoint(this.cur())
      this.echo = d ? d.msg : 'No local help at point'
    },
    'suspend-frame': () => {
      this.suspended = true
      this.hooks.emit({ t: 'emacs', event: 'suspended', frame: 'terminal' })
      this.later(1800, () => {
        this.suspended = false
        this.echo =
          'Playground: Emacs was suspended with C-z and resumed automatically. On a real terminal you would type fg.'
        this.hooks.emit({ t: 'emacs', event: 'resumed', frame: 'terminal' })
        this.changed()
      })
    },
    'move-beginning-of-line': () => {
      const b = this.cur()
      b.point = this.clamp(b, this.lineStart(b, b.point))
    },
    'move-end-of-line': () => {
      const b = this.cur()
      b.point = this.clamp(b, this.lineEnd(b, b.point))
    },
    'forward-char': () => {
      const b = this.cur()
      if (b.point >= (b.narrow ? b.narrow[1] : b.text.length)) {
        this.echo = 'End of buffer'
        return
      }
      b.point++
    },
    'backward-char': () => {
      const b = this.cur()
      if (b.point <= (b.narrow ? b.narrow[0] : 0)) {
        this.echo = 'Beginning of buffer'
        return
      }
      b.point--
    },
    'right-char': () => this.CMD['forward-char']?.(),
    'left-char': () => this.CMD['backward-char']?.(),
    'next-line': () => this.moveLine(this.cur(), 1),
    'previous-line': () => this.moveLine(this.cur(), -1),
    'forward-word': () => this.wordMove(this.cur(), 1),
    'backward-word': () => this.wordMove(this.cur(), -1),
    'beginning-of-buffer': () => {
      const b = this.cur()
      b.mark = b.point
      b.active = false
      b.point = this.clamp(b, 0)
      this.echo = 'Mark set'
    },
    'end-of-buffer': () => {
      const b = this.cur()
      b.mark = b.point
      b.active = false
      b.point = this.clamp(b, b.text.length)
      this.echo = 'Mark set'
    },
    'scroll-up-command': () => {
      for (let i = 0; i < 10; i++) this.moveLine(this.cur(), 1)
    },
    'scroll-down-command': () => {
      for (let i = 0; i < 10; i++) this.moveLine(this.cur(), -1)
    },
    'recenter-top-bottom': () => {},
    'kill-line': () => {
      const b = this.cur()
      const e = this.lineEnd(b, b.point)
      this.kill =
        e === b.point
          ? this.del(b, b.point, Math.min(b.text.length, b.point + 1))
          : this.del(b, b.point, e)
    },
    yank: () => {
      if (!this.kill) {
        this.echo = 'Kill ring is empty'
        return
      }
      this.insert(this.cur(), this.kill)
    },
    'kill-region': () => {
      const b = this.cur()
      if (b.mark == null) {
        this.echo = 'The mark is not set now, so there is no region'
        return
      }
      this.kill = this.del(b, Math.min(b.mark, b.point), Math.max(b.mark, b.point))
      b.mark = null
    },
    'kill-ring-save': () => {
      const b = this.cur()
      if (b.mark == null) {
        this.echo = 'The mark is not set now, so there is no region'
        return
      }
      this.kill = b.text.slice(Math.min(b.mark, b.point), Math.max(b.mark, b.point))
      b.mark = null
      this.echo = 'Copied'
    },
    'set-mark-command': () => {
      const b = this.cur()
      b.mark = b.point
      b.active = true
      this.echo = 'Mark set'
    },
    'delete-char': () => {
      const b = this.cur()
      if (b.point >= b.text.length) {
        this.echo = 'End of buffer'
        return
      }
      this.del(b, b.point, b.point + 1)
    },
    'delete-backward-char': () => {
      const b = this.cur()
      if (b.point <= (b.narrow ? b.narrow[0] : 0)) {
        this.echo = 'Beginning of buffer'
        return
      }
      this.del(b, b.point - 1, b.point)
    },
    'kill-word': () => {
      const b = this.cur()
      const p = b.point
      this.wordMove(b, 1)
      const e = b.point
      b.point = p
      this.kill = this.del(b, p, e)
    },
    newline: () => {
      this.insert(this.cur(), '\n')
    },
    'indent-for-tab-command': () => {
      const b = this.cur()
      const ls = this.lineStart(b, b.point)
      const want = '    '
      if (!b.text.startsWith(want, ls)) {
        const p = b.point
        b.point = ls
        this.insert(b, want)
        b.point = p + want.length
      }
    },
    'overwrite-mode': () => {
      const b = this.cur()
      b.overwrite = !b.overwrite
      this.echo = b.overwrite
        ? 'Overwrite mode enabled in current buffer'
        : 'Overwrite mode disabled in current buffer'
    },
    'universal-argument': () => {
      this.argPending = true
      this.echo = 'C-u-'
    },
    'next-error': () => {
      const main = Object.values(this.buffers).find((b) => b.path === 'polyglot/src/main.rs')
      if (!main || !main.diags.length) {
        this.echo = 'No more errors'
        return
      }
      let wi = this.windows.findIndex((w) => w.buf === main.name)
      if (wi < 0) {
        this.showBuffer(main.name)
        wi = this.sel
      }
      this.sel = wi
      const d = main.diags[0] as Diag
      const lines = main.text.split('\n')
      let p = 0
      for (let i = 0; i < d.line; i++) p += (lines[i] as string).length + 1
      main.point = p + d.col
      this.echo = d.msg
    },
    'goto-line': () => this.openMini({ kind: 'goto-line', prompt: 'Goto line: ' }),
    'xref-find-definitions': () => {
      const b = this.cur()
      const [l] = this.lineOf(b, b.point)
      const line = b.text.split('\n')[l] ?? ''
      const lib =
        Object.values(this.buffers).find((x) => x.name === 'lib.rs') ??
        this.visit(this.dirOf(b) + 'lib.rs')
      if (/greet/.test(line) && lib) {
        this.prevBuf = b.name
        this.showBuffer(lib.name)
        lib.point = 7
        this.echo = ''
      } else
        this.echo =
          'No definitions found for: ' + ((line.match(/[A-Za-z_]+/) ?? ['identifier'])[0] as string)
    },
    'xref-go-back': () => {
      if (this.prevBuf && this.buffers[this.prevBuf]) this.showBuffer(this.prevBuf)
      else this.echo = 'At start of xref history.'
    },
    compile: () =>
      this.openMini({ kind: 'compile', prompt: 'Compile command: ', input: 'cargo build' }),
    recompile: () => this.doCompile('cargo build'),
    'comment-dwim': () => {
      const b = this.cur()
      const ls = this.lineStart(b, b.point)
      const p = b.point
      b.point = ls
      this.insert(b, '// ')
      b.point = p + 3
    },
    'comment-line': () => this.CMD['comment-dwim']?.(),
    'self-insert-command': (ch) => {
      this.insert(this.cur(), ch ?? '')
    },
    'quit-window': () => {
      if (this.windows.length > 1) {
        this.windows.splice(this.sel, 1)
        this.sel = Math.max(0, this.sel - 1)
      } else this.showBuffer(this.otherBufferName())
      this.echo = ''
    },
    'Buffer-menu-this-window': () => {
      const b = this.cur()
      const [l] = this.lineOf(b, b.point)
      const line = b.text.split('\n')[l] ?? ''
      const name = line.slice(4, 20).trim()
      if (this.buffers[name]) {
        this.showBuffer(name)
        this.echo = ''
      } else this.echo = 'No buffer on this line'
    },
    'dired-find-file': () => {
      const b = this.cur() as Buffer & { diredDir?: string }
      const [l] = this.lineOf(b, b.point)
      const line = b.text.split('\n')[l] ?? ''
      const name = line.trim().split(/\s+/).pop() ?? ''
      const full = (b.diredDir ?? '') + name
      if (line.trimStart().startsWith('d') && name !== '.' && name !== '..')
        this.openDired(full + '/')
      else {
        const v = this.visit(full)
        if (v) {
          this.showBuffer(v.name)
          this.echo = ''
        } else this.echo = 'No file on this line'
      }
    },
    'dired-up-directory': () => {
      const b = this.cur() as Buffer & { diredDir?: string }
      const parts = (b.diredDir ?? '').replace(/\/$/, '').split('/')
      parts.pop()
      this.openDired(parts.join('/') + (parts.length ? '/' : ''))
    },
    'compile-goto-error': () => this.CMD['next-error']?.(),
    'next-error-no-select': () => this.CMD['next-error']?.(),
  }

  /** Re-render hook: called after asynchronous changes (frame reopened, compile). */
  changed: () => void = () => {}

  private runCommand(name: string, seq: string[], arg: string | null, viaMx = false) {
    const fn = this.CMD[name]
    this.lastCmd = name
    this.count(name)
    this.echo = ''
    if (fn) fn(arg)
    else this.echo = '(' + name + ' is not simulated in this fake)'
    this.argPending = name === 'universal-argument'
    const b = this.cur()
    const d = this.diagAtPoint(b)
    if (d && !this.echo && !this.mini) this.echo = d.msg
    this.emitKey(viaMx ? ['M-x'] : seq, name, viaMx)
  }

  private miniKey(key: string): boolean {
    const m = this.mini as Mini
    if (key === 'C-g') {
      this.abortMini()
      this.count('keyboard-quit')
      this.emitKey(['C-g'], 'abort-minibuffers')
      return true
    }
    if (m.kind === 'isearch') {
      if (key === 'RET') {
        this.closeMini()
        this.echo = ''
        this.count('isearch-exit')
        this.emitKey(['RET'], 'isearch-exit')
        return true
      }
      if (key === 'C-s') {
        if (m.match) m.origin = m.match[0] + 1
        this.searchFrom(m)
        if (m.fail) m.origin = 0
        this.count('isearch-repeat-forward')
        this.emitKey(['C-s'], 'isearch-repeat-forward')
        return true
      }
      if (key === 'DEL') {
        m.input = m.input.slice(0, -1)
        this.searchFrom(m)
        return true
      }
      if (isPrintable(key)) {
        m.input += keyChar(key)
        this.searchFrom(m)
        this.emitKey([key], 'isearch-printing-char')
        return true
      }
      this.closeMini()
      this.echo = ''
      return false
    }
    if (key === 'RET') {
      this.submitMini()
      return true
    }
    if (key === 'DEL') {
      m.input = m.input.slice(0, -1)
      this.echo = ''
      return true
    }
    if (key === 'TAB') {
      this.completeMini()
      return true
    }
    if (isPrintable(key)) {
      m.input += keyChar(key)
      this.echo = ''
      this.emitKey([key], 'self-insert-command')
      return true
    }
    if (['C-a', 'C-e', 'C-f', 'C-b', '<left>', '<right>', '<home>', '<end>'].includes(key))
      return true
    if (key.startsWith('C-') || key.startsWith('M-') || key.startsWith('<') || key === 'ESC') {
      this.echo = 'Command attempted to use minibuffer while in minibuffer'
      this.emitKey([key], 'undefined')
      return true
    }
    return true
  }

  handleKey(raw: string) {
    let key = normalizeKeysLoose(raw)
    if (this.closed || this.suspended) return
    if (this.describe) {
      this.describe.push(key)
      const s = this.describe.join(' ')
      const c = this.cur()
      const local = c.special ? LOCAL[c.mode] : undefined
      const name = local?.[s] ?? BINDINGS[s]
      if (name) {
        this.describe = null
        this.count('describe-key')
        this.openHelp(
          s +
            ' runs the command ' +
            name +
            ', which is an interactive command.\n\n' +
            (GLOSS[name] ?? '') +
            '.\n\nIt is bound to ' +
            Object.keys(BINDINGS)
              .filter((k) => BINDINGS[k] === name)
              .join(', ') +
            '.',
        )
        this.echo = ''
        this.emitKey(['C-h', 'k', ...s.split(' ')], 'describe-key')
      } else if (this.isPrefix(s, local)) {
        this.echo = 'Describe key (or click or menu item): ' + s + '-'
      } else {
        this.describe = null
        this.echo = s + ' is undefined'
      }
      return
    }
    if (this.prefix.length === 1 && this.prefix[0] === 'ESC' && key !== 'ESC') {
      this.prefix = []
      key = normalizeKeysLoose('M-' + key)
    }
    if (this.prefix.length && key === 'C-g') {
      this.prefix = []
      this.hooks.emit({ t: 'prefix', keys: '', source: 'tap' })
      this.runCommand('keyboard-quit', ['C-g'], null)
      return
    }
    if (this.mini && this.prefix.length === 0 && this.miniKey(key)) return
    const seq = [...this.prefix, key]
    const s = seq.join(' ')
    const b = this.cur()
    const local = b.special ? LOCAL[b.mode] : undefined
    const name = local?.[s] ?? BINDINGS[s]
    if (name) {
      this.prefix = []
      this.runCommand(name, seq, null)
      return
    }
    if (this.isPrefix(s, local)) {
      this.prefix = seq
      this.prefixAt = this.now()
      this.echo = s + '-'
      this.hooks.emit({ t: 'prefix', keys: s, source: 'tap' })
      this.later(450, () => this.changed())
      return
    }
    if (this.prefix.length === 0 && isPrintable(key)) {
      this.runCommand('self-insert-command', [key], keyChar(key))
      return
    }
    this.prefix = []
    this.echo = s + ' is undefined'
    this.count('(unbound)')
    this.emitKey(seq, 'undefined')
  }

  private isPrefix(seq: string, local?: Record<string, string>): boolean {
    const s = seq + ' '
    const maps = local ? [local, BINDINGS] : [BINDINGS]
    return maps.some((m) => Object.keys(m).some((k) => k.startsWith(s)))
  }

  rescue(): string[] {
    const did: string[] = []
    if (this.describe) {
      this.describe = null
      did.push('cancelled the C-h k prompt')
    }
    if (this.mini) {
      this.abortMini()
      did.push('cancelled the prompt')
    }
    if (this.prefix.length) {
      this.prefix = []
      did.push('dropped the pending prefix')
    }
    if (this.macro) {
      this.macro = false
      did.push('stopped the keyboard macro')
    }
    if (this.recursive) {
      this.recursive = 0
      did.push('left the recursive edit')
    }
    if (this.windows.length > 1) {
      const keep =
        this.windows.find((w, i) => !this.buffers[w.buf]?.special && i === this.sel) ??
        this.windows.find((w) => !this.buffers[w.buf]?.special) ??
        (this.windows[this.sel] as Win)
      this.windows = [keep]
      this.sel = 0
      did.push('closed the extra windows')
    }
    const b = this.cur()
    if (b.special && b.mode !== 'lisp-interaction-mode') {
      this.showBuffer(this.otherBufferName())
      did.push('left ' + b.name)
    }
    const c = this.cur()
    if (c.narrow) {
      c.narrow = null
      did.push('widened the buffer')
    }
    if (c.overwrite) {
      c.overwrite = false
      did.push('turned off overwrite mode')
    }
    c.mark = null
    this.argPending = false
    this.echo = did.length
      ? 'Rescue: ' + did.join(', ') + '.'
      : 'Rescue: nothing to undo; you were already in a normal state.'
    return did
  }

  /** Shows a buffer in the selected window (rpc eval find-file from the file tree). */
  show(name: string) {
    this.showBuffer(name)
    this.echo = ''
  }

  /** Reverts file buffers to the disk (after a restore). */
  revertAll() {
    for (const b of Object.values(this.buffers)) {
      if (!b.path) continue
      const t = this.disk.get(b.path)
      if (t === undefined) {
        delete this.buffers[b.name]
        for (const w of this.windows) if (w.buf === b.name) w.buf = '*scratch*'
        continue
      }
      b.text = t
      b.orig = t
      b.modified = false
      b.point = Math.min(b.point, t.length)
      b.narrow = null
      b.undo = []
    }
  }

  /** Opens files and applies a simple layout pattern ("single" or "(vsplit a b)"). */
  open(files: string[], layout?: string) {
    const opened: Buffer[] = []
    for (const f of files) {
      const b = this.visit(f)
      if (b) opened.push(b)
    }
    const first = opened[0]
    if (first) {
      this.windows = [{ id: winIds++, buf: first.name }]
      this.sel = 0
    }
    const m = layout ? /^\((vsplit|hsplit)\s+(.+)\)$/.exec(layout.trim()) : null
    if (m) {
      const names = (m[2] ?? '').split(/\s+/)
      this.windows = names.map((n) => {
        const base = n.split('/').pop() ?? n
        const b =
          Object.values(this.buffers).find((x) => x.name === base) ??
          (n.includes('/') ? this.visit(n) : null)
        if (base === '*compilation*') this.doCompile('cargo build', false)
        const w: Win = { id: winIds++, buf: b?.name ?? (this.buffers[base] ? base : '*scratch*') }
        if (m[1] === 'hsplit') w.side = true
        return w
      })
      this.sel = 0
      this.echo = ''
    }
  }

  layout(): Layout {
    if (this.windows.length === 1) return this.windows[0]?.id ?? null
    const side = this.windows.some((w) => w.side)
    return [side ? 'hsplit' : 'vsplit', ...this.windows.map((w) => w.id)]
  }

  state(mode: 'terminal' | 'gui' = 'terminal'): State {
    const b = this.cur()
    const [l, c] = this.lineOf(b, b.point)
    const minors = b.minors
      .split(/\s+/)
      .filter(Boolean)
      .map((m) => MINOR_SYMBOLS[m] ?? m.toLowerCase() + '-mode')
    if (b.overwrite) minors.push('overwrite-mode')
    const windows: Window[] = this.windows.map((w, i) => ({
      id: w.id,
      buf: w.buf,
      mode: this.buffers[w.buf]?.mode ?? 'fundamental-mode',
      selected: i === this.sel,
      dedicated: false,
      side: null,
    }))
    const d = this.diagAtPoint(b)
    const m = this.mini
    return {
      t: 'state',
      buf: b.name,
      file: b.path ? '/work/' + b.path : null,
      mode: b.mode,
      modeName: b.modeName,
      minors,
      modified: b.modified,
      readonly: b.readonly,
      narrowed: !!b.narrow,
      overwrite: b.overwrite,
      line: l + 1,
      col: c,
      point: b.point + 1,
      markActive: b.mark !== null && b.active,
      windows,
      layout: this.layout(),
      frame: mode,
      frames: 2,
      tabs: { count: 1, current: 'main' },
      minibuffer: m
        ? {
            depth: m.kind === 'isearch' ? 0 : 1,
            prompt: m.prompt,
            contents: m.input,
            kind: m.kind === 'isearch' ? 'isearch' : 'prompt',
          }
        : this.describe
          ? {
              depth: 1,
              prompt: 'Describe key (or click or menu item): ',
              contents: this.describe.join(' '),
              kind: 'prompt',
            }
          : null,
      isearch: m?.kind === 'isearch',
      recursive: this.recursive,
      macro: this.macro,
      prefixArg: this.argPending ? '(4)' : null,
      readOnlyReason: null,
      diagAtPoint: d
        ? { severity: 'error', message: d.msg, line: d.line + 1, col: d.col, source: 'rustc' }
        : null,
      waiting: null,
    }
  }

  whichKey(): { prefix: string; entries: [string, string][] } | null {
    if (!this.prefix.length || this.now() - this.prefixAt < 400) return null
    const s = this.prefix.join(' ')
    const list = WHICHKEY[s]
    if (list) return { prefix: s, entries: list }
    const entries = Object.keys(BINDINGS)
      .filter((k) => k.startsWith(s + ' '))
      .map((k) => [k.slice(s.length + 1), BINDINGS[k] as string] as [string, string])
    return entries.length ? { prefix: s, entries } : null
  }

  /** What a key sequence runs here (rpc describe). */
  describeKeys(keys: string): { cmd: string; doc: string } {
    const s = normalizeKeysLoose(keys)
    const c = this.cur()
    const local = c.special ? LOCAL[c.mode] : undefined
    const cmd = local?.[s] ?? BINDINGS[s] ?? ''
    return { cmd, doc: cmd ? (GLOSS[cmd] ?? `${cmd} (fake session)`) : '' }
  }
}

function commonPrefix(xs: string[]): string {
  let p = xs[0] ?? ''
  for (const x of xs) while (!x.startsWith(p)) p = p.slice(0, -1)
  return p
}
