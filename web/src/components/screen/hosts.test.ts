import { beforeEach, describe, expect, it } from 'vitest'
import { useSessionStore } from '../../store/session'
import { keysHeld } from './hosts'

beforeEach(() => useSessionStore.getState().reset())

describe('the terminal key guard', () => {
  it('holds keys while sessiond runs a lesson op or the session speaks another protocol', () => {
    expect(keysHeld()).toBe(false)
    const lesson = { rev: 1, step: null, phase: 'idle' as const, attempt: 0, outcomes: {} }
    const busy = { op: 'restore', lessonId: 'a/b', stepId: 'save', startedAt: 1 }
    useSessionStore.setState({ lesson: { ...lesson, busy } })
    expect(keysHeld()).toBe(true)
    useSessionStore.setState({ lesson })
    expect(keysHeld()).toBe(false)
    useSessionStore.setState({ incompatible: true })
    expect(keysHeld()).toBe(true)
  })
})
