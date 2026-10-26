/**
 * Terminal input decoder for the fake session (a small cousin of sessiond's
 * input tap, backend.md §3.6): xterm.js legacy encoding to Emacs key names.
 * The fake never enables the kitty protocol, so legacy bytes are all it sees.
 */

const CSI_TILDE: Record<string, string> = {
  '1': '<home>',
  '2': '<insert>',
  '3': '<deletechar>',
  '4': '<end>',
  '5': '<prior>',
  '6': '<next>',
  '15': '<f5>',
  '17': '<f6>',
  '18': '<f7>',
  '19': '<f8>',
  '20': '<f9>',
  '21': '<f10>',
  '23': '<f11>',
  '24': '<f12>',
}
const CSI_FINAL: Record<string, string> = {
  A: '<up>',
  B: '<down>',
  C: '<right>',
  D: '<left>',
  H: '<home>',
  F: '<end>',
  P: '<f1>',
  Q: '<f2>',
  R: '<f3>',
  S: '<f4>',
}

function modPrefix(m: number): string {
  // xterm modifier parameter: 1 + (shift 1 | alt 2 | ctrl 4)
  const v = m - 1
  let p = ''
  if (v & 4) p += 'C-'
  if (v & 2) p += 'M-'
  if (v & 1) p += 'S-'
  return p
}

function ctrlKey(b: number): string | null {
  if (b === 0x00) return 'C-SPC'
  if (b === 0x09) return 'TAB'
  if (b === 0x0d || b === 0x0a) return b === 0x0d ? 'RET' : 'C-j'
  if (b === 0x1b) return 'ESC'
  if (b === 0x1c) return 'C-\\'
  if (b === 0x1d) return 'C-]'
  if (b === 0x1e) return 'C-^'
  if (b === 0x1f) return 'C-_'
  if (b === 0x7f) return 'DEL'
  if (b >= 0x01 && b <= 0x1a) return 'C-' + String.fromCharCode(b + 0x60)
  return null
}

function plainKey(ch: string): string {
  return ch === ' ' ? 'SPC' : ch
}

/** Decodes one chunk of terminal input into Emacs key names. */
export function decodeInput(data: Buffer | string): string[] {
  const s = typeof data === 'string' ? data : data.toString('utf8')
  const out: string[] = []
  let i = 0
  while (i < s.length) {
    const c = s.charCodeAt(i)
    if (c === 0x1b) {
      const next = s[i + 1]
      if (next === undefined) {
        out.push('ESC')
        i++
        continue
      }
      if (next === '[' || next === 'O') {
        // CSI or SS3: parameters then a final byte.
        let j = i + 2
        while (j < s.length && /[0-9;:<=>?]/.test(s[j] as string)) j++
        const params = s.slice(i + 2, j)
        const final = s[j]
        i = j + 1
        if (final === '~') {
          const [code, mod] = params.split(';')
          const name = CSI_TILDE[code ?? '']
          if (name) out.push((mod ? modPrefix(Number(mod)) : '') + name)
          continue
        }
        if (final && CSI_FINAL[final]) {
          const parts = params.split(';')
          const mod = parts.length > 1 ? Number(parts[1]) : 1
          out.push(modPrefix(mod) + (CSI_FINAL[final] as string))
          continue
        }
        if (final === 'Z') {
          out.push('<backtab>')
          continue
        }
        // Mouse and other reports are ignored.
        continue
      }
      // Meta: ESC followed by a key.
      const n = s.charCodeAt(i + 1)
      const ctrl = ctrlKey(n)
      if (n === 0x1b) {
        out.push('ESC')
        i++
        continue
      }
      out.push('M-' + (ctrl ?? plainKey(next)))
      i += 2
      continue
    }
    const ctrl = ctrlKey(c)
    if (ctrl) {
      out.push(ctrl)
      i++
      continue
    }
    const cp = s.codePointAt(i) as number
    const ch = String.fromCodePoint(cp)
    out.push(plainKey(ch))
    i += ch.length
  }
  return out
}

/** Splits rpc('keys') text into key names for literal typing. */
export function textToKeys(text: string): string[] {
  return [...text].map((ch) => (ch === '\n' ? 'RET' : ch === '\t' ? 'TAB' : plainKey(ch)))
}
