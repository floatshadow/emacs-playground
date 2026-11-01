/// <reference types="node" />
/**
 * The conformance corpus shared with Go (internal/protocol/testdata/conformance,
 * conformance_test.go): the ports of predicates, templates, demos and keys
 * must give Go's answers. layouts.json has no port.
 */
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import type { Predicate, State } from '../protocol.gen'
import { parseDemo } from '../lib/demo'
import { normalizeKeys } from './keys'
import { expandTemplate, matchPredicate, stateDoc, validatePredicate } from './predicate'
// Read at run time (tests run in web/): the web image builds without internal/.
const corpus = <T>(name: string): T =>
  JSON.parse(
    readFileSync(
      join(process.cwd(), '../internal/protocol/testdata/conformance', name + '.json'),
      'utf8',
    ),
  ) as T

interface Case {
  name: string
  error?: true
  state?: boolean
  doc?: unknown
}
const predicates = corpus<(Case & { predicate: Predicate; want: boolean })[]>('predicates')
const templates =
  corpus<(Case & { predicate: Predicate; template: string; want: string })[]>('templates')
const demos = corpus<(Case & { demo: string; want: unknown })[]>('demos')
const keys = corpus<(Case & { keys: string; want: string })[]>('keys')
const state = corpus<State>('state')
const docOf = (c: Case) => (c.state ? stateDoc(state) : c.doc)

describe('predicates.json', () => {
  it.each(predicates.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    const p = c.predicate
    const err = validatePredicate(p)
    if (c.error) return expect(err).not.toBeNull()
    expect(err).toBeNull()
    expect(matchPredicate(p, docOf(c))).toBe(c.want)
  })
})

describe('templates.json', () => {
  it.each(templates.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    expect(expandTemplate(c.template, docOf(c), c.predicate)).toBe(c.want)
  })
})

describe('demos.json', () => {
  it.each(demos.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    if (c.error) return expect(() => parseDemo(c.demo)).toThrow()
    expect(parseDemo(c.demo)).toEqual(c.want)
  })
})

describe('keys.json', () => {
  it.each(keys.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    if (c.error) return expect(() => normalizeKeys(c.keys)).toThrow()
    expect(normalizeKeys(c.keys)).toBe(c.want)
  })
})
