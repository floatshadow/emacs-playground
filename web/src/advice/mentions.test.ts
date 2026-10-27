import { describe, expect, it } from 'vitest'
import type { Scenario } from '../protocol.gen'
import {
  commandsOf,
  isMentioned,
  lessonMentions,
  markdownKeys,
  mentionedNow,
  proseKeys,
} from './mentions'

describe('mentioned keys (contract d)', () => {
  it('reads backticked keys, :kbd and :try from lesson Markdown, normalised', () => {
    const md = [
      '`C-f` `C-b` `C-n` `C-p` are the arrows; `M-<` goes to the top.',
      'Paths stay code: `src/main.rs`, and so do words: `ma`.',
      ':try[C-l] scrolls; :kbd[M-C-f] jumps over a block.',
    ].join('\n')
    expect(markdownKeys(md).sort()).toEqual(
      ['C-M-f', 'C-b', 'C-f', 'C-l', 'C-n', 'C-p', 'M-<'].sort(),
    )
  })

  it('reads plain keys from step prose, but not a lone q', () => {
    expect(proseKeys('Press C-x, then C-b. Then q closes it.').sort()).toEqual(['C-b', 'C-x'])
    expect(proseKeys('`C-x 2` splits; C-x 3 too.').sort()).toEqual(['C-x 2', 'C-x 3'])
  })

  it('collects the lesson and each step, and merges them for the active step', () => {
    const sc = {
      id: 'L',
      title: 'L',
      minutes: 1,
      modules: [],
      setup: {},
      steps: [
        {
          id: 'a',
          goal: 'Search',
          detail: 'Press `C-s`.',
          conditions: [{ text: 'x', hint: 'C-g cancels.', elisp: 't' }],
        },
        { id: 'b', goal: 'Save', conditions: [{ text: 'y', hint: 'C-x C-s', elisp: 't' }] },
      ],
    } as unknown as Scenario
    const m = lessonMentions('L', 'Use `M-x`.', sc)
    expect(m.lesson).toEqual(['M-x'])
    expect(m.steps['a']?.sort()).toEqual(['C-g', 'C-s'])
    expect(mentionedNow(m, { lessonId: 'L', stepId: 'b' })).toEqual(['M-x', 'C-x C-s'])
    expect(mentionedNow(m, { lessonId: 'other', stepId: 'a' })).toEqual(['M-x'])
    expect(mentionedNow(null, null)).toEqual([])
  })

  it('a key is mentioned when a mentioned sequence starts with it', () => {
    expect(isMentioned('C-s', ['C-s'])).toBe(true)
    expect(isMentioned('C-s', ['C-s C-s'])).toBe(true)
    expect(isMentioned('C-s', ['C-x C-s'])).toBe(false)
    expect(isMentioned('C-x 2', ['C-x 2'])).toBe(true)
    expect(isMentioned('M-C-f', ['C-M-f'])).toBe(true)
  })

  it('a key that starts a later command of a mentioned sequence is mentioned (3.5 build)', () => {
    // "clear the offered command (`C-a C-k`)": two commands in one span.
    const m = proseKeys(
      '`C-x p c`, clear the offered command (`C-a C-k`), type `sh build.sh`, `RET`.',
    )
    expect(m).toContain('C-a C-k')
    expect(isMentioned('C-a', m)).toBe(true)
    expect(isMentioned('C-k', m)).toBe(true)
    // Keys after a prefix stay part of its command.
    expect(isMentioned('c', m)).toBe(false)
    expect(isMentioned('C-x p c', m)).toBe(true)
    expect(isMentioned('C-x 3', ['C-x 2 C-x 3'])).toBe(true)
    expect(isMentioned('C-n', ['M-g M-n'])).toBe(false)
    expect(isMentioned('C-f', ['C-x 4 C-f'])).toBe(false)
    expect(isMentioned('C-k', ['C-x C-k n'])).toBe(false)
  })

  it('splits a sequence into commands at keys that are not prefixes', () => {
    expect(commandsOf('C-a C-k')).toEqual(['C-a', 'C-k'])
    expect(commandsOf('C-x p c')).toEqual(['C-x p c'])
    expect(commandsOf('C-x 2 C-x 3')).toEqual(['C-x 2', 'C-x 3'])
    expect(commandsOf('C-s C-s')).toEqual(['C-s', 'C-s'])
    expect(commandsOf('C-h k C-k')).toEqual(['C-h k', 'C-k'])
    expect(commandsOf('C-x')).toEqual(['C-x'])
  })
})
