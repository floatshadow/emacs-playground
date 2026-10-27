import { afterEach, describe, expect, it } from 'vitest'
import type { Event, LessonProgress, Scenario } from '../protocol.gen'
import { record, type Recorder } from '../test/recorder'
import { resumeStep, selectStepView, selectWhereAmI, stepStatuses } from './selectors'
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

  it('mouse clicks and scrolling add no ticker entry and count as no command', () => {
    const key = (keys: string, cmd: string): Event => ({
      t: 'key',
      keys,
      cmd,
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: 'terminal',
    })
    const s = run([
      hello(),
      key('C-n', 'next-line'),
      key('<down-mouse-1>', 'mouse-drag-region'),
      key('<mouse-1>', 'mouse-set-point'),
      key('<wheel-down>', 'mwheel-scroll'),
    ])
    expect(s.ticker.map((e) => e.cmd)).toEqual(['next-line'])
    expect(s.totalCommands).toBe(1)
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
    // The GUI frame closing while the terminal is shown covers nothing.
    let s = run([{ t: 'emacs', event: 'frame-closed', frame: 'gui' }])
    expect(s.emacs.overlay).toBeNull()
    s = run([{ t: 'emacs', event: 'frame-closed', frame: 'terminal', reason: 'C-x C-c' }])
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
    expect(s.outcomes['L/1']).toEqual({ open: 'done' })
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
    // Redo the first step: it loses this visit's outcome until it ends again.
    s = run(
      [{ t: 'step', lessonId: 'L/1', stepId: 'open', status: 'loaded', index: 0, count: 2 }],
      s,
    )
    expect(s.outcomes['L/1']).toEqual({ save: 'done' })
    expect(s.lessonsComplete).toEqual([])
  })

  it('demonstrated, skipped and dropped steps (contracts a-c)', () => {
    let s = run([
      hello(),
      { t: 'step', lessonId: 'L/1', stepId: 'open', status: 'demonstrated', index: 0, count: 2 },
    ])
    // A demonstration does not advance: the step stays active, watched.
    expect(s.activeStep?.stepId).toBe('open')
    expect(s.outcomes['L/1']).toEqual({ open: 'demonstrated' })
    expect(s.watched).toEqual({ lessonId: 'L/1', stepId: 'open' })
    // Do it yourself: restore {} resets the step, which is no longer watched.
    s = run(
      [{ t: 'step', lessonId: 'L/1', stepId: 'open', status: 'reset', index: 0, count: 2 }],
      s,
    )
    expect(s.watched).toBeNull()
    expect(s.outcomes['L/1']).toEqual({})
    // Skip: the step is skipped and the next one loads.
    s = run(
      [
        { t: 'step', lessonId: 'L/1', stepId: 'open', status: 'skipped', index: 0, count: 2 },
        { t: 'step', lessonId: 'L/1', stepId: 'save', status: 'loaded', index: 1, count: 2 },
      ],
      s,
    )
    expect(s.outcomes['L/1']).toEqual({ open: 'skipped' })
    expect(s.activeStep?.stepId).toBe('save')
    // Start over: sessiond drops the active step, and the page forgets the
    // lesson's outcomes, so it resumes at the first step (X10).
    s = run(
      [{ t: 'step', lessonId: 'L/1', stepId: 'save', status: 'dropped', index: 1, count: 2 }],
      s,
    )
    expect(s.activeStep).toBeNull()
    expect(s.outcomes['L/1']).toBeUndefined()
  })

  it('a reset from the demo’s own restore keeps the step watched', () => {
    let s = run([hello()])
    s = { ...s, demoPlaying: true, watched: { lessonId: 'L/1', stepId: 'open' } }
    s = run(
      [{ t: 'step', lessonId: 'L/1', stepId: 'open', status: 'reset', index: 0, count: 2 }],
      s,
    )
    expect(s.watched).toEqual({ lessonId: 'L/1', stepId: 'open' })
  })

  it('a stale prefix after the command that used its keys is ignored (B4)', () => {
    const s = run([
      hello(),
      {
        t: 'key',
        ts: 2000,
        keys: 'ESC ESC ESC',
        cmd: 'keyboard-escape-quit',
        buf: 'main.rs',
        mode: 'rust-ts-mode',
        frame: 'terminal',
      },
      { t: 'prefix', ts: 1990, keys: 'ESC', source: 'tap' },
    ])
    expect(s.prefix).toBeNull()
    expect(s.ticker.map((e) => e.kind)).toEqual(['quit'])
  })

  it('the ticker, the prefix and the habit cards belong to the lesson (R9, decision 8)', () => {
    let s = run([
      hello(),
      { t: 'step', lessonId: 'L/1', stepId: 'open', status: 'loaded', index: 0, count: 2 },
      {
        t: 'key',
        keys: 'C-z',
        cmd: 'suspend-frame',
        buf: 'main.rs',
        mode: 'rust-ts-mode',
        frame: 'terminal',
      },
      { t: 'prefix', keys: 'C-x', source: 'tap' },
    ])
    expect(s.ticker.length).toBe(2)
    expect(s.cards.length).toBe(1)
    // Another step of the same lesson keeps them.
    s = run(
      [{ t: 'step', lessonId: 'L/1', stepId: 'save', status: 'loaded', index: 1, count: 2 }],
      s,
    )
    expect(s.ticker.length).toBe(2)
    // Another lesson starts empty.
    s = run([{ t: 'step', lessonId: 'L/2', stepId: 'a', status: 'loaded', index: 0, count: 1 }], s)
    expect(s.ticker).toEqual([])
    expect(s.prefix).toBeNull()
    expect(s.cards).toEqual([])
    // "Once each" still holds across lessons.
    expect(s.habitsSeen.has('C-z')).toBe(true)
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

  it('habit keys seen persist per session across reloads; Start over forgets them', () => {
    localStorage.clear()
    const st = useSessionStore
    st.getState().reset()
    st.getState().applyEvent(hello({ sessionId: 's1' }))
    st.getState().applyEvent({
      t: 'key',
      keys: 'C-s',
      cmd: 'isearch-forward',
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: 'terminal',
    })
    expect(JSON.parse(localStorage.getItem('pg.habitsSeen.s1') ?? '[]')).toEqual(['C-s'])
    // A reload: a fresh store learns the session id and reads the keys back.
    st.getState().reset()
    st.getState().applyEvent(hello({ sessionId: 's1' }))
    expect(st.getState().habitsSeen.has('C-s')).toBe(true)
    st.getState().forgetProgress()
    expect(st.getState().habitsSeen.size).toBe(0)
    expect(localStorage.getItem('pg.habitsSeen.s1')).toBeNull()
    st.getState().reset()
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
      outcomes: s.outcomes['L/1'] ?? {},
      progress: undefined,
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
      outcomes: {},
      progress: undefined,
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

  const THREE: Scenario = {
    ...SCENARIO,
    steps: [
      ...SCENARIO.steps,
      { id: 'close', goal: 'Close', conditions: [{ text: 'd', hint: 'hd', elisp: 't' }] },
    ],
  }
  const saved = (
    steps: Record<string, 'done' | 'demonstrated' | 'skipped' | 'in-progress'>,
  ): LessonProgress => ({
    status: 'in-progress',
    counts: { done: 0, demonstrated: 0, skipped: 0, total: 3 },
    steps: Object.fromEntries(
      Object.entries(steps).map(([k, status]) => [k, { status, hintsUsed: 0, resets: 0 }]),
    ),
  })

  it('the active step is active whatever progress says; later steps show no tick (X1)', () => {
    const progress = saved({ open: 'done', save: 'done', close: 'done' })
    expect(stepStatuses(THREE, 'save', {}, progress)).toEqual(['done', 'active', 'todo'])
  })

  it('earlier steps show this visit’s outcome, else a saved done, watched or skipped', () => {
    const progress = saved({ open: 'skipped', save: 'demonstrated' })
    expect(stepStatuses(THREE, 'close', {}, progress)).toEqual([
      'skipped',
      'demonstrated',
      'active',
    ])
    expect(stepStatuses(THREE, 'close', { save: 'done' }, progress)).toEqual([
      'skipped',
      'done',
      'active',
    ])
    // No active step in this lesson: outcomes and progress decide.
    expect(stepStatuses(THREE, null, {}, saved({ save: 'in-progress', close: 'done' }))).toEqual([
      'todo',
      'todo',
      'done',
    ])
    // The last step completed in this visit shows done although it stays "active".
    expect(stepStatuses(THREE, 'close', { close: 'done' }, undefined)).toEqual([
      'todo',
      'todo',
      'done',
    ])
  })

  it('a watched active step offers Do it yourself', () => {
    const views = selectStepView({
      scenario: THREE,
      lessonId: 'L/1',
      activeStepId: 'save',
      outcomes: { save: 'demonstrated' },
      progress: undefined,
      watched: true,
      checks: {},
      hintLevels: {},
      locale: 'en',
    })
    expect(views.map((v) => [v.status, v.watched])).toEqual([
      ['todo', false],
      ['active', true],
      ['todo', false],
    ])
  })

  it('resumes at the first step neither done nor skipped; a watched step is practised', () => {
    expect(resumeStep(THREE, {}, saved({ open: 'skipped', save: 'demonstrated' }))).toBe('save')
    expect(resumeStep(THREE, { save: 'done' }, saved({ open: 'done' }))).toBe('close')
    expect(resumeStep(THREE, {}, saved({ open: 'done', save: 'done', close: 'skipped' }))).toBe(
      undefined,
    )
  })
})
