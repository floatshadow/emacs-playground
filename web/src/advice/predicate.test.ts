import { describe, expect, it } from 'vitest'
import type { Predicate } from '../protocol.gen'
import {
  compileRe2,
  expandTemplate,
  matchPredicate,
  resolve,
  stateDoc,
  translateRe2,
  validatePredicate,
} from './predicate'
import { apiState } from './fixtures'

describe('matchPredicate (port of TestPredicate)', () => {
  const doc = stateDoc(apiState())
  const cases: [string, boolean][] = [
    ['{"modified": true}', true],
    ['{"modified": false}', false],
    ['{"minibuffer": null}', true],
    ['{"minibuffer.kind": "prompt"}', false],
    ['{"otherWindows.mode": {"in": ["help-mode", "compilation-mode"]}}', true],
    ['{"otherWindows.mode": ["help-mode"]}', false],
    ['{"windows.length": {"gte": 2}}', true],
    ['{"windows.length": {"gt": 2}}', false],
    ['{"minors": {"contains": "flymake-mode"}}', true],
    ['{"mode": {"not": "dired-mode"}}', true],
    ['{"mode": {"regex": "^rust"}}', true],
    ['{"diagAtPoint": {"exists": true}}', true],
    ['{"nosuch": {"exists": false}}', true],
    ['{"selectedWindow.buf": "main.rs"}', true],
    ['{"line": 9, "col": 20}', true],
  ]
  for (const [p, want] of cases) {
    it(p, () => {
      const pred = JSON.parse(p) as Predicate
      expect(validatePredicate(pred)).toBeNull()
      expect(matchPredicate(pred, doc)).toBe(want)
    })
  }

  it('expands templates against the element that satisfied the predicate', () => {
    const p = { 'otherWindows.mode': { in: ['compilation-mode'] } }
    expect(
      expandTemplate('A second window shows {otherWindows.buf} ({buf}, line {line}).', doc, p),
    ).toBe('A second window shows *compilation* (main.rs, line 9).')
  })

  it('rejects unknown operators', () => {
    expect(validatePredicate({ x: { bogus: 1 } })).not.toBeNull()
    expect(validatePredicate({ x: { in: 1 } })).not.toBeNull()
    expect(validatePredicate({ x: { regex: '(' } })).not.toBeNull()
  })

  it('negates operator objects and handles missing paths', () => {
    expect(matchPredicate({ prefixArg: { not: null } }, doc)).toBe(false)
    expect(
      matchPredicate({ prefixArg: { not: null } }, stateDoc(apiState({ prefixArg: '(4)' }))),
    ).toBe(true)
    expect(matchPredicate({ 'minibuffer.prompt': null }, doc)).toBe(true)
    expect(matchPredicate({ 'otherWindows.mode': { not: { in: ['help-mode'] } } }, doc)).toBe(true)
  })

  it('resolves array indices, lengths and mapped paths', () => {
    expect(resolve(doc, 'windows.1.buf')).toEqual([['*compilation*'], true])
    expect(resolve(doc, 'windows.length')).toEqual([[2], true])
    expect(resolve(doc, 'buf.length')).toEqual([[7], true])
    expect(resolve(doc, 'windows.mode')).toEqual([['rust-ts-mode', 'compilation-mode'], true])
    expect(resolve(doc, 'windows.9')).toEqual([[], false])
  })
})

describe('RE2 patterns (B11)', () => {
  it('turns a leading flag group into RegExp flags', () => {
    expect(translateRe2('(?i)buffer')).toEqual({ source: 'buffer', flags: 'i' })
    expect(translateRe2('(?is)a.b')).toEqual({ source: 'a.b', flags: 'is' })
    expect(compileRe2('(?i)buffer').test('Switch to Buffer: ')).toBe(true)
  })

  it('spells out scoped case folding and dot-all', () => {
    const { source, flags } = translateRe2('x(?i:ab[c-e])y')
    expect(flags).toBe('')
    expect(source).toBe('x(?:[aA][bB][c-eC-E])y')
    const re = compileRe2('x(?i:ab[c-e])y')
    expect(re.test('xABDy')).toBe(true)
    expect(re.test('XABDy')).toBe(false)
    expect(compileRe2('a(?s:.)b').test('a\nb')).toBe(true)
    expect(compileRe2('a.b').test('a\nb')).toBe(false)
  })

  it('keeps escapes, classes and named groups', () => {
    expect(translateRe2('(?i)\\w+\\.rs$').source).toBe('\\w+\\.rs$')
    expect(translateRe2('(?P<n>\\d+)').source).toBe('(?<n>\\d+)')
    expect(compileRe2('[]a]').test(']')).toBe(true)
    expect(translateRe2('\\p{L}+').flags).toBe('u')
  })

  it('rejects what JavaScript cannot express', () => {
    expect(() => translateRe2('a(?i)b')).toThrow(/must start/)
    expect(() => translateRe2('(?U)a+')).toThrow(/not supported/)
    expect(() => translateRe2('(?m:^a)')).toThrow(/not supported/)
    expect(() => translateRe2('(?-i:a)')).toThrow(/not supported/)
    expect(validatePredicate({ x: { regex: 'a(?i)b' } })).toMatch(/must start/)
    expect(validatePredicate({ x: { regex: '(?i)M-x|file' } })).toBeNull()
  })

  // The prompt rows of curriculum/tables/exits.yaml, with the number row the
  // fix plan adds (R7). Before the translation every (?i) row failed to
  // compile in the browser: the file, buffer and replace rows never matched
  // and the generic row matched every prompt.
  const rows: Record<string, Predicate> = {
    mx: { 'minibuffer.kind': 'prompt', 'minibuffer.prompt': { regex: 'M-x' } },
    file: {
      'minibuffer.kind': 'prompt',
      'minibuffer.prompt': { regex: '(?i)(find|open|write|visit|dired).*(file|directory)|file.*:' },
    },
    buffer: { 'minibuffer.kind': 'prompt', 'minibuffer.prompt': { regex: '(?i)buffer' } },
    yesNo: {
      'minibuffer.kind': 'prompt',
      'minibuffer.prompt': { regex: '\\((y or n|yes or no)' },
    },
    replace: { 'minibuffer.kind': 'prompt', 'minibuffer.prompt': { regex: '(?i)replac' } },
    number: {
      'minibuffer.kind': 'prompt',
      'minibuffer.prompt': { regex: '(?i)line|column|number|count' },
    },
    generic: {
      'minibuffer.kind': 'prompt',
      'minibuffer.prompt': {
        not: {
          regex:
            '(?i)M-x|file|directory|buffer|replac|line|column|number|count|\\((y or n|yes or no)',
        },
      },
    },
  }
  const matching = (prompt: string) => {
    const d = stateDoc(apiState({ minibuffer: { prompt, contents: '', depth: 1, kind: 'prompt' } }))
    return Object.entries(rows)
      .filter(([, p]) => matchPredicate(p, d))
      .map(([id]) => id)
  }
  const cases: [string, string[]][] = [
    ['M-x ', ['mx']],
    ['Find file: ', ['file']],
    ['Switch to buffer (default *scratch*): ', ['buffer']],
    ['Goto line: ', ['number']],
    ['Save position in the tutorial? (y or n) ', ['yesNo']],
    ['Really exit? (yes or no) ', ['yesNo']],
    ['Query replace: ', ['replace']],
    ['Eval: ', ['generic']],
  ]
  for (const [prompt, want] of cases) {
    it(`prompt ${JSON.stringify(prompt)} matches ${want.join(', ')}`, () => {
      for (const p of Object.values(rows)) expect(validatePredicate(p)).toBeNull()
      expect(matching(prompt)).toEqual(want)
    })
  }
})
