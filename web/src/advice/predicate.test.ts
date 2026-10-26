import { describe, expect, it } from 'vitest'
import type { Predicate } from '../protocol.gen'
import { expandTemplate, matchPredicate, resolve, stateDoc, validatePredicate } from './predicate'
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
