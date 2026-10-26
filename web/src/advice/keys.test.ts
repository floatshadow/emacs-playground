import { describe, expect, it } from 'vitest'
import { isKeySequence, keyify, normalizeKeys, parseKey, parseKeys, isPrintableKey } from './keys'

describe('parseKey and normalizeKeys (port of TestParseKeys)', () => {
  const ok: Record<string, string> = {
    'C-x C-f': 'C-x C-f',
    'M-C-x': 'C-M-x',
    'C-M-%': 'C-M-%',
    F3: '<f3>',
    '<f4>': '<f4>',
    'C-x 4 f': 'C-x 4 f',
    'ESC ESC ESC': 'ESC ESC ESC',
    'C--': 'C--',
    '-': '-',
    'M-<': 'M-<',
    'C-<left>': 'C-<left>',
    'S-<up>': 'S-<up>',
    's-a': 's-a',
    'C-;': 'C-;',
    'C-`': 'C-`',
    'C-\\': 'C-\\',
    'M-g M-g': 'M-g M-g',
    RET: 'RET',
    é: 'é',
  }
  for (const [input, want] of Object.entries(ok)) {
    it(`normalises ${input}`, () => expect(normalizeKeys(input)).toBe(want))
  }
  for (const bad of ['', 'Ctrl-x', 'C-C-x', 'X-a', 'foo', '<>', 'C-', '<f 3>']) {
    it(`rejects ${JSON.stringify(bad)}`, () => expect(() => parseKeys(bad)).toThrow())
  }
  it('knows printable keys', () => {
    expect(isPrintableKey(parseKey('a'))).toBe(true)
    expect(isPrintableKey(parseKey('C-a'))).toBe(false)
    expect(isPrintableKey(parseKey('SPC'))).toBe(true)
    expect(isPrintableKey(parseKey('<f3>'))).toBe(false)
  })
})

describe('keyify (the demo regex)', () => {
  const keysOf = (s: string) => keyify(s).flatMap((x) => ('keys' in x ? [x.keys.join(' ')] : []))
  it('finds sequences and single named keys', () => {
    expect(keysOf('RET accepts, C-g cancels. TAB completes names.')).toEqual(['RET', 'C-g', 'TAB'])
    expect(keysOf('Press q inside it, or C-x 1 here to keep only this window.')).toEqual([
      'q',
      'C-x 1',
    ])
    expect(keysOf('C-x 1 keeps only this one. C-x o moves between them.')).toEqual([
      'C-x 1',
      'C-x o',
    ])
    expect(keysOf('C-x n w shows everything again.')).toEqual(['C-x n w'])
    expect(keysOf('F4 stops recording.')).toEqual(['F4'])
    expect(keysOf('M-g M-n: hold Alt, press g, then Alt n.')).toEqual(['M-g M-n'])
  })
  it('does not swallow ordinary words', () => {
    expect(keysOf('C-s then type Hello. The cursor jumps')).toEqual(['C-s'])
    expect(keysOf('quit the queue')).toEqual([])
    expect(keysOf('A prompt is open: Find file:')).toEqual([])
  })
  it('keeps the text around keys intact', () => {
    const segs = keyify('Press C-x C-s now.')
    expect(segs).toEqual([{ text: 'Press ' }, { keys: ['C-x', 'C-s'] }, { text: ' now.' }])
  })
})

describe('isKeySequence (backticked text in lessons)', () => {
  it('accepts key sequences', () => {
    for (const s of [
      'C-x C-f',
      'TAB',
      'RET',
      'M-x',
      'C-g',
      'q',
      '<f3>',
      'F3',
      'ESC ESC ESC',
      'C-x r m / C-x r b',
      'M-%',
    ])
      expect(isKeySequence(s), s).toBe(true)
  })
  it('rejects words, paths and mode-line text', () => {
    for (const s of [
      'ma',
      'x',
      'src/main.rs',
      '-:**-',
      'L12 C4',
      '(Rust Eglot)',
      '**',
      'greet(&name)',
      '',
    ])
      expect(isKeySequence(s), s).toBe(false)
  })
})
