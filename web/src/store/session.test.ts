import { afterEach, describe, expect, it } from 'vitest'
import type { Event, Scenario } from '../protocol.gen'
import { record, type Recorder } from '../test/recorder'
import { doneSteps, selectStepView, selectWhereAmI } from './selectors'
import { initialSessionData, reduce, useSessionStore, type SessionData } from './session'

let rec: Recorder | null = null
afterEach(() => rec?.dispose())

const run = (events: Event[], start: SessionData = initialSessionData()) =>
  events.reduce((s, ev, i) => reduce(s, ev, 1000 + i), start)

const hello = (over: Partial<Extract<Event, { t: 'hello' }>> = {}): Event => ({
  t: 'hello',
  seq: 10,
  protocol: 1,
  sessionId: 'local',
  mode: 'terminal',
  emacs: { version: '31.1', uptimeMs: 5 },
  frames: { terminal: true, gui: false },
  step: { lessonId: 'L/1', stepId: 'open' },
  modules: ['base', 'agent'],
  replayed: false,
  ...over,
})

const SCENARIO: Scenario = {
  id: 'L/1',
  title: 'T',
  minutes: 1,
  modules: [],
  setup: {},
  steps: [
    {
      id: 'open',
      goal: 'Open',
      demo: 'C-x C-f',
      conditions: [
        { text: 'a', hint: 'ha', event: { command: 'find-file' } },
        { text: 'b', hint: 'hb', elisp: 't' },
      ],
    },
    {
      id: 'save',
      goal: 'Save',
      conditions: [{ text: 'c', hint: 'hc', event: { command: 'save-buffer' } }],
    },
  ],
}

describe('applyEvent reducer', () => {
  it('hello sets the session, mode, modules and active step', () => {
    const s = run([hello()])
    expect(s.sessionId).toBe('local')
    expect(s.modules).toEqual(['base', 'agent'])
    expect(s.activeStep).toEqual({ lessonId: 'L/1', stepId: 'open' })
    expect(s.lastSeq).toBe(10)
  })

  it('tracks lastSeq for replay, and a fresh hello resets it (sessiond restarted)', () => {
    let s = run([
      hello({ seq: 50 }),
      { t: 'echo', seq: 51, text: 'x' },
      { t: 'echo', seq: 52, text: 'y' },
    ])
    expect(s.lastSeq).toBe(52)
    s = run([hello({ seq: 52, replayed: true })], s)
    expect(s.lastSeq).toBe(52)
    s = run([hello({ seq: 3, replayed: false })], s)
    expect(s.lastSeq).toBe(3)
  })

  it('a recorded editing session produces state, ticker and counts', () => {
    rec = record(['hello/README.md'])
    rec.keys('C-x C-f').type('src/ma').keys('TAB RET')
    const s = rec.replay()
    expect(s.state?.buf).toBe('main.rs')
    expect(s.state?.file).toBe('/work/hello/src/main.rs')
    expect(s.prefix).toBeNull()
    expect(s.minibufferSince).toBeNull()
    expect(s.totalCommands).toBeGreaterThan(5)
    expect(selectWhereAmI(s)?.exits).toEqual([])
  })

  it('a pending prefix is kept until the command arrives', () => {
    rec = record()
    rec.keys('C-x')
    let s = rec.replay()
    expect(s.prefix).toBe('C-x')
    expect(s.ticker.at(-1)?.kind).toBe('prefix')
    rec.keys('2')
    s = rec.replay()
    expect(s.prefix).toBeNull()
    expect(s.ticker.at(-1)?.cmd).toBe('split-window-below')
  })

  it('records when the minibuffer opened and clears it when it closes', () => {
    rec = record()
    rec.keys('C-x C-f')
    let s = rec.replay()
    expect(s.minibufferSince).not.toBeNull()
    rec.keys('C-g')
    s = rec.replay()
    expect(s.minibufferSince).toBeNull()
  })

  it('emacs events drive the overlays', () => {
    let s = run([{ t: 'emacs', event: 'frame-closed', frame: 'terminal', reason: 'C-x C-c' }])
    expect(s.emacs.overlay).toBe('frame-closed')
    s = run([{ t: 'emacs', event: 'frame-opened', frame: 'terminal' }], s)
    expect(s.emacs.overlay).toBeNull()
    s = run([{ t: 'emacs', event: 'restarting' }], s)
    expect(s.emacs.overlay).toBe('restarting')
    s = run([{ t: 'emacs', event: 'daemon-restarted' }], s)
    expect(s.emacs.overlay).toBeNull()
    expect(s.emacs.restartedAt).toBeDefined()
    s = run([{ t: 'emacs', event: 'unresponsive' }], s)
    expect(s.emacs.busy).toBe(true)
    s = run([{ t: 'emacs', event: 'responsive' }], s)
    expect(s.emacs.busy).toBe(false)
    s = run(
      [
        { t: 'emacs', event: 'suspended' },
        { t: 'emacs', event: 'resumed' },
      ],
      s,
    )
    expect(s.emacs.overlay).toBeNull()
  })

  it('file events bump per-path versions and the tree version for creations', () => {
    const s = run([
      { t: 'file', path: 'a.pdf', event: 'modified' },
      { t: 'file', path: 'a.pdf', event: 'modified' },
      { t: 'file', path: 'b.txt', event: 'created' },
    ])
    expect(s.files['a.pdf']?.version).toBe(2)
    expect(s.treeVersion).toBe(1)
  })

  it('check and step events: complete, advance, reset, redo', () => {
    let s = run([
      hello(),
      {
        t: 'check',
        lessonId: 'L/1',
        stepId: 'open',
        conditions: [
          { id: 'open/1', ok: true },
          { id: 'open/2', ok: false },
        ],
        complete: false,
      },
    ])
    expect(s.checks['open']?.conditions[0]?.ok).toBe(true)
    s = run(
      [
        { t: 'step', lessonId: 'L/1', stepId: 'open', status: 'complete', index: 0, count: 2 },
        { t: 'step', lessonId: 'L/1', stepId: 'save', status: 'loaded', index: 1, count: 2 },
      ],
      s,
    )
    expect(s.completed['L/1']).toEqual(['open'])
    expect(s.activeStep).toEqual({ lessonId: 'L/1', stepId: 'save', index: 1, count: 2 })
    s = run(
      [
        {
          t: 'step',
          lessonId: 'L/1',
          stepId: 'save',
          status: 'complete',
          index: 1,
          count: 2,
          lessonComplete: true,
        },
      ],
      s,
    )
    expect(s.lessonsComplete).toEqual(['L/1'])
    // Redo the first step: it is reopened and no longer counted as done.
    s = run(
      [{ t: 'step', lessonId: 'L/1', stepId: 'open', status: 'loaded', index: 0, count: 2 }],
      s,
    )
    expect(s.reopened['L/1']).toEqual(['open'])
    expect(s.completed['L/1']).toEqual(['save'])
    expect(s.lessonsComplete).toEqual([])
  })

  it('nudge, checkpoint, mode, modules, gap and error events', () => {
    const s = run([
      { t: 'nudge', id: 'n1', kind: 'calm', title: 't', body: 'b' },
      { t: 'nudge', id: 'n1', kind: 'ask', title: 't2', body: 'b2' },
      { t: 'checkpoint', label: 'L/1/open', ref: 'refs/playground/L/1/open', createdAt: 1 },
      { t: 'mode', mode: 'gui' },
      { t: 'modules', enabled: ['base', 'agent', 'files'], restarting: true },
      { t: 'gap', from: 3, to: 9 },
      { t: 'error', code: 'internal', message: 'boom' },
    ])
    expect(s.serverNudges).toEqual([{ id: 'n1', kind: 'ask', title: 't2', body: 'b2' }])
    expect(s.checkpointsVersion).toBe(1)
    expect(s.mode).toBe('gui')
    expect(s.modules).toEqual(['base', 'agent', 'files'])
    expect(s.modulesRestarting).toBe(true)
    expect(s.ticker.at(-1)?.kind).toBe('gap')
    expect(s.lastError?.message).toBe('boom')
  })

  it('ignores unknown event types', () => {
    const s0 = initialSessionData()
    expect(reduce(s0, { t: 'nope' } as unknown as Event, 1)).toBe(s0)
  })

  it('the store wires applyEvent and dismiss', () => {
    useSessionStore.getState().reset()
    useSessionStore.getState().applyEvent(hello())
    expect(useSessionStore.getState().sessionId).toBe('local')
    useSessionStore.getState().dismiss('x')
    expect(useSessionStore.getState().dismissed.has('x')).toBe(true)
    useSessionStore.getState().reset()
  })
})

describe('step views', () => {
  it('marks done, active, met, next and the hint for the first unmet condition', () => {
    const s = run([
      hello(),
      {
        t: 'check',
        lessonId: 'L/1',
        stepId: 'open',
        conditions: [
          { id: 'open/1', ok: true },
          { id: 'open/2', ok: false },
        ],
        complete: false,
      },
    ])
    const views = selectStepView({
      scenario: SCENARIO,
      lessonId: 'L/1',
      activeStepId: 'open',
      done: doneSteps(undefined, 'L/1', s.completed, s.reopened),
      checks: s.checks,
      hintLevels: { 'L/1#open': 1 },
      locale: 'en',
    })
    expect(views.map((v) => v.status)).toEqual(['active', 'todo'])
    expect(views[0]?.conds.map((c) => c.status)).toEqual(['met', 'next'])
    expect(views[0]?.firstUnmet).toBe(1)
    expect(views[0]?.hint).toEqual({ forText: 'b', text: 'hb' })
  })

  it('no hint at level 0; unknown conditions are flagged', () => {
    const views = selectStepView({
      scenario: SCENARIO,
      lessonId: 'L/1',
      activeStepId: 'open',
      done: new Set(),
      checks: {
        open: {
          t: 'check',
          lessonId: 'L/1',
          stepId: 'open',
          conditions: [{ id: 'open/1', ok: false, unknown: true }],
          complete: false,
        },
      },
      hintLevels: {},
      locale: 'en',
    })
    expect(views[0]?.hint).toBeNull()
    expect(views[0]?.conds[0]?.status).toBe('next')
    expect(views[0]?.conds[1]?.status).toBe('unmet')
  })

  it('progress marks earlier steps done unless reopened this session', () => {
    const progress = {
      lessons: {
        'L/1': {
          status: 'done' as const,
          steps: {
            open: { status: 'done' as const, hintsUsed: 0, resets: 0 },
            save: { status: 'done' as const, hintsUsed: 0, resets: 0 },
          },
        },
      },
      updatedAt: 1,
    }
    expect([...doneSteps(progress, 'L/1', {}, {})]).toEqual(['open', 'save'])
    expect([...doneSteps(progress, 'L/1', {}, { 'L/1': ['save'] })]).toEqual(['open'])
  })
})
