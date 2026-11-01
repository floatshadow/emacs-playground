import { describe, expect, it } from 'vitest'
import type { Predicate } from '../protocol.gen'
import { matchPredicate, resolve, stateDoc, validatePredicate } from './predicate'
import { apiState } from './fixtures'

describe('resolve', () => {
  const doc = stateDoc(apiState())
  it('resolves array indices, lengths and mapped paths', () => {
    expect(resolve(doc, 'windows.1.buf')).toEqual([['*compilation*'], true])
    expect(resolve(doc, 'windows.length')).toEqual([[2], true])
    expect(resolve(doc, 'buf.length')).toEqual([[7], true])
    expect(resolve(doc, 'windows.mode')).toEqual([['rust-ts-mode', 'compilation-mode'], true])
    expect(resolve(doc, 'windows.9')).toEqual([[], false])
  })
})

describe('the prompt rows of exits.yaml', () => {
  // The prompt rows of curriculum/tables/exits.yaml: every (?i) row must
  // compile in the browser, or the file, buffer and replace rows would never
  // match and the generic row would match every prompt.
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
