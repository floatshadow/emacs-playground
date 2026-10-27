import { describe, expect, it } from 'vitest'
import type { DemoAction } from '../../lib/demo'
import { runActions } from './demoPlayer'

const keys = (k: string): DemoAction => ({ kind: 'keys', keys: k, delayMs: 0 })

describe('runActions', () => {
  it('sends nothing more once its step is no longer the active one', async () => {
    const sent: string[] = []
    let onStep = true
    const done = await runActions(
      [keys('C-x'), keys('o'), keys('RET')],
      new AbortController().signal,
      (a) => {
        if (a.kind === 'keys') sent.push(a.keys)
        // The second key completes the step and the engine moves on.
        if (sent.length === 2) onStep = false
        return Promise.resolve()
      },
      undefined,
      () => onStep,
    )
    expect(done).toBe(false)
    expect(sent).toEqual(['C-x', 'o'])
  })
})
