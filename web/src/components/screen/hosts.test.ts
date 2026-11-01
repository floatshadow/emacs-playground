import { beforeEach, describe, expect, it } from 'vitest'
import { useSessionStore } from '../../store/session'
import { keysHeld } from './hosts'

beforeEach(() => useSessionStore.getState().reset())

describe('the terminal key guard', () => {
  it('holds keys while sessiond runs a Go to this step the page did not start', () => {
    expect(keysHeld()).toBe(false)
    const store = useSessionStore.getState()
    store.setServerBusy({ op: 'load', lessonId: 'a/b', stepId: 'save', startedAt: 1 })
    expect(keysHeld()).toBe(true)
    // Other lesson ops show no veil and hold no keys.
    store.setServerBusy({ op: 'restore', lessonId: 'a/b', stepId: 'save', startedAt: 1 })
    expect(keysHeld()).toBe(false)
    store.setServerBusy(null)
    expect(keysHeld()).toBe(false)
  })
})
