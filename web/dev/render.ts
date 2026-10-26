/**
 * Renders the fake Emacs as a full terminal screen in ANSI (true colour),
 * a port of the demo's renderScreen: windows with syntax colours, mode
 * lines, the which-key popup, the echo area or minibuffer, the cursor.
 */
import type { MockEmacs, Buffer } from './mock-emacs.ts'

type Rgb = [number, number, number]
const hex = (h: string): Rgb => [
  parseInt(h.slice(1, 3), 16),
  parseInt(h.slice(3, 5), 16),
  parseInt(h.slice(5, 7), 16),
]

const C = {
  bg: hex('#0d0e1c'),
  fg: hex('#e8e8f0'),
  kw: hex('#b6a0ff'),
  str: hex('#79a8ff'),
  fn: hex('#feacd0'),
  ty: hex('#6ae4b9'),
  mac: hex('#f78fe7'),
  num: hex('#00bcff'),
  cm: hex('#989898'),
  hd: hex('#ff7f86'),
  math: hex('#6ae4b9'),
  err: hex('#ff5f59'),
  warn: hex('#d0bc00'),
  ok: hex('#44bc44'),
  empty: hex('#3a3b55'),
  mlOn: hex('#484d67'),
  mlOnFg: hex('#ffffff'),
  mlOff: hex('#292d48'),
  mlOffFg: hex('#9a9ab0'),
  region: hex('#555a66'),
  match: hex('#d0bc00'),
  prompt: hex('#00bcff'),
  wkBg: hex('#1a1b2f'),
  wkKey: hex('#b6a0ff'),
  wkCmd: hex('#c8c8d8'),
  cursorBg: hex('#ffffff'),
  black: hex('#000000'),
  rule: hex('#2e2f4a'),
}

const HL: Record<string, [RegExp, (keyof typeof C)[]]> = {
  'rust-ts-mode': [
    /(\/\/.*$)|("(?:[^"\\]|\\.)*")|(\b(?:fn|let|mut|pub|use|struct|impl|enum|match|if|else|return|for|in|while|loop|mod|crate|self|Self|as|where|trait|type|const|static|ref|move|dyn|unsafe|async|await)\b)|(\b[A-Z][A-Za-z0-9_]*\b)|(\b[a-z_][a-z0-9_]*!)|(\b[a-z_][a-z0-9_]*(?=\())|(\b\d+\b)/gm,
    ['cm', 'str', 'kw', 'ty', 'mac', 'fn', 'num'],
  ],
  'python-ts-mode': [
    /(#.*$)|("(?:[^"\\]|\\.)*")|(\b(?:def|return|if|else|elif|for|in|import|from|class|while|with|as|lambda|yield|not|and|or|None|True|False)\b)|(\b[a-z_][a-z0-9_]*(?=\())|(\b\d+\b)/gm,
    ['cm', 'str', 'kw', 'fn', 'num'],
  ],
  'tuareg-mode': [
    /(\(\*.*?\*\))|("(?:[^"\\]|\\.)*")|(\b(?:let|rec|in|if|then|else|fun|function|match|with|type|module|struct|sig|end|open|and|of|begin|done|do|while|for|to)\b)|(\b\d+\b)/gm,
    ['cm', 'str', 'kw', 'num'],
  ],
  'typst-ts-mode': [
    /(\/\/.*$)|("(?:[^"\\]|\\.)*")|(^=+ .*$)|(#[a-z]+)|(\$[^$]*\$)|(\b\d+(?:\.\d+)?(?:cm|pt|mm|em)?\b)/gm,
    ['cm', 'str', 'hd', 'kw', 'math', 'num'],
  ],
  'conf-toml-mode': [
    /(#.*$)|(^\[.*\]$)|("(?:[^"\\]|\\.)*")|(^[a-z_-]+(?=\s*=))/gm,
    ['cm', 'hd', 'str', 'kw'],
  ],
  'markdown-mode': [/(^#+ .*$)|(`[^`]+`)|(^\s*[-*] )/gm, ['hd', 'str', 'kw']],
  'lisp-interaction-mode': [
    /(;.*$)|("(?:[^"\\]|\\.)*")|(\b(?:defun|let|setq|lambda|if|when|unless)\b)/gm,
    ['cm', 'str', 'kw'],
  ],
  'compilation-mode': [
    /(^error(?:\[E\d+\])?:.*$)|(^warning:.*$)|(^\s*-->.*$|^src\/.*$)|(^help:.*$)|(^\s*Finished.*$|^Compilation finished.*$)|(^Compilation exited abnormally.*$)/gm,
    ['err', 'warn', 'fn', 'ok', 'ok', 'err'],
  ],
  'help-mode': [/(\b[a-z]+(?:-[a-z]+)+\b)|(^.*runs the command.*$)/gm, ['fn', 'hd']],
  'Buffer-menu-mode': [/(^ CRM.*$)|(^\s*%?\*?\s+\S+)/gm, ['hd', 'fn']],
  'dired-mode': [
    /(^\s*\/.*:$)|(\bd[rwx-]{9}\b.*$)|(\S+\.(?:rs|md|toml|py|typ|ml|html)$)/gm,
    ['hd', 'kw', 'fn'],
  ],
}

interface Cell {
  ch: string
  fg: Rgb
  bg: Rgb
  bold?: boolean
  curl?: boolean
}

function classesFor(line: string, mode: string): (keyof typeof C | '')[] {
  const cls: (keyof typeof C | '')[] = new Array(line.length).fill('')
  const rule = HL[mode]
  if (!rule) return cls
  const [re, names] = rule
  re.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(line))) {
    if (m[0].length === 0) {
      re.lastIndex++
      continue
    }
    const gi = m.slice(1).findIndex((g) => g !== undefined)
    for (let i = m.index; i < m.index + m[0].length; i++) cls[i] = names[gi] ?? ''
  }
  return cls
}

function sgr(c: Cell): string {
  let s = `\x1b[0;38;2;${c.fg.join(';')};48;2;${c.bg.join(';')}`
  if (c.bold) s += ';1'
  s += 'm'
  if (c.curl) s += `\x1b[4:3m\x1b[58;2;${C.err.join(';')}m`
  return s
}

function row(cols: number, bg: Rgb = C.bg, fg: Rgb = C.fg): Cell[] {
  return Array.from({ length: cols }, () => ({ ch: ' ', fg, bg }))
}

function put(r: Cell[], col: number, text: string, fg: Rgb, bg?: Rgb, bold?: boolean) {
  let i = col
  for (const ch of text) {
    if (i >= r.length) break
    const cell = r[i] as Cell
    cell.ch = ch
    cell.fg = fg
    if (bg) cell.bg = bg
    if (bold) cell.bold = true
    i++
  }
}

function windowRows(
  eng: MockEmacs,
  b: Buffer,
  selected: boolean,
  width: number,
  height: number,
  onWin: boolean,
): Cell[][] {
  const out: Cell[][] = []
  const body = Math.max(1, height - 1)
  const lo = b.narrow ? b.narrow[0] : 0
  const hi = b.narrow ? b.narrow[1] : b.text.length
  const lines = b.text.slice(lo, hi).split('\n')
  const [cl, cc] = eng.lineOf(b, b.point)
  const [nl0] = eng.lineOf(b, lo)
  const curLine = cl - nl0
  if (curLine < b.scroll) b.scroll = curLine
  if (curLine >= b.scroll + body) b.scroll = curLine - body + 1
  if (b.scroll < 0) b.scroll = 0
  const region =
    b.mark != null && b.active && selected
      ? [Math.min(b.mark, b.point), Math.max(b.mark, b.point)]
      : null
  const mini = eng.mini
  const match = mini?.kind === 'isearch' && mini.match && selected ? mini.match : null
  for (let i = 0; i < body; i++) {
    const r = row(width)
    const li = b.scroll + i
    const line = lines[li]
    if (line === undefined) {
      put(r, 1, '~', C.empty)
      out.push(r)
      continue
    }
    let off = lo
    for (let k = 0; k < li; k++) off += (lines[k] as string).length + 1
    const cls = classesFor(line, b.mode)
    const diags = b.diags.filter((d) => d.line === li + nl0)
    for (let x = 0; x < line.length && x + 1 < width; x++) {
      const cell = r[x + 1] as Cell
      cell.ch = line[x] as string
      const k = cls[x]
      if (k) cell.fg = C[k]
      if (k === 'hd') cell.bold = true
      const pos = off + x
      if (region && pos >= (region[0] as number) && pos < (region[1] as number)) cell.bg = C.region
      if (match && pos >= match[0] && pos < match[1]) {
        cell.bg = C.match
        cell.fg = C.black
      }
      if (diags.some((d) => x >= d.col && x < d.col + d.len)) cell.curl = true
    }
    const showCursor =
      selected && li === curLine && !(mini && mini.kind !== 'isearch') && !eng.describe
    if (showCursor && cc + 1 < width) {
      const cell = r[cc + 1] as Cell
      cell.bg = C.cursorBg
      cell.fg = C.bg
    }
    out.push(r)
  }
  const pct =
    lines.length <= body
      ? 'All'
      : b.scroll === 0
        ? 'Top'
        : b.scroll + body >= lines.length
          ? 'Bot'
          : Math.round((100 * b.scroll) / lines.length) + '%'
  const flag = b.readonly ? (b.modified ? '%*' : '%%') : b.modified ? '**' : '--'
  const extras =
    (b.narrow ? ' Narrow' : '') + (b.overwrite ? ' Ovwrt' : '') + (eng.macro ? ' Def' : '')
  const ml =
    ' -:' +
    flag +
    '-  ' +
    b.name.padEnd(16) +
    ' L' +
    (cl + 1) +
    ' C' +
    cc +
    '  ' +
    pct.padEnd(4) +
    '  (' +
    b.modeName +
    (b.minors ? ' ' + b.minors : '') +
    ')' +
    extras
  const bg = onWin ? C.mlOn : C.mlOff
  const fg = onWin ? C.mlOnFg : C.mlOffFg
  const mr = row(width, bg, fg)
  put(mr, 0, ml, fg, bg)
  out.push(mr)
  return out
}

/** Renders the whole screen as an ANSI string that repaints from the top-left. */
export function renderAnsi(eng: MockEmacs, cols: number, rows: number): string {
  cols = Math.max(20, Math.min(cols, 400))
  rows = Math.max(6, Math.min(rows, 200))
  const screen: Cell[][] = []
  const wk = eng.whichKey()
  const wkRows = wk ? Math.min(6, Math.ceil(wk.entries.length / 3) + 1) : 0
  const avail = rows - 1 - wkRows
  const n = eng.windows.length
  const side = eng.windows.some((w) => w.side)
  if (side) {
    const widths: number[] = []
    let left = cols
    for (let i = 0; i < n; i++) {
      const w = i === n - 1 ? left : Math.floor((cols - (n - 1)) / n)
      widths.push(w)
      left -= w + 1
    }
    const parts = eng.windows.map((w, i) =>
      windowRows(
        eng,
        eng.buffers[w.buf] as Buffer,
        i === eng.sel,
        widths[i] as number,
        avail,
        i === eng.sel,
      ),
    )
    for (let y = 0; y < avail; y++) {
      const r: Cell[] = []
      parts.forEach((p, i) => {
        r.push(...(p[y] ?? row(widths[i] as number)))
        if (i < n - 1) r.push({ ch: '│', fg: C.rule, bg: C.bg })
      })
      screen.push(r.slice(0, cols))
    }
  } else {
    let used = 0
    eng.windows.forEach((w, i) => {
      const h = i === n - 1 ? avail - used : Math.floor(avail / n)
      used += h
      screen.push(
        ...windowRows(eng, eng.buffers[w.buf] as Buffer, i === eng.sel, cols, h, i === eng.sel),
      )
    })
  }
  if (wk) {
    const t = row(cols, C.wkBg)
    put(t, 1, wk.prefix + '-', C.prompt, C.wkBg)
    screen.push(t)
    const colW = Math.floor((cols - 2) / 3)
    for (let y = 0; y < wkRows - 1; y++) {
      const r = row(cols, C.wkBg)
      for (let x = 0; x < 3; x++) {
        const e = wk.entries[y * 3 + x]
        if (!e) continue
        put(r, 1 + x * colW, e[0], C.wkKey, C.wkBg)
        put(
          r,
          1 + x * colW + e[0].length + 1,
          e[1].slice(0, colW - e[0].length - 2),
          C.wkCmd,
          C.wkBg,
        )
      }
      screen.push(r)
    }
  }
  const mr = row(cols)
  if (eng.mini) {
    put(mr, 0, eng.mini.prompt, C.prompt)
    put(mr, eng.mini.prompt.length, eng.mini.input, C.fg)
    const cx = eng.mini.prompt.length + eng.mini.input.length
    if (cx < cols) {
      const cell = mr[cx] as Cell
      cell.bg = C.cursorBg
      cell.fg = C.bg
    }
    if (eng.mini.fail) put(mr, cx + 2, '[Failing]', C.err)
  } else {
    put(mr, 0, eng.echo, C.fg)
  }
  screen.push(mr)
  let out = '\x1b[?25l\x1b[H'
  screen.slice(0, rows).forEach((r, y) => {
    let last = ''
    for (const c of r) {
      const s = sgr(c)
      if (s !== last) {
        out += s
        last = s
      }
      out += c.ch
    }
    out += '\x1b[0m'
    if (y < rows - 1) out += '\r\n'
  })
  return out
}
