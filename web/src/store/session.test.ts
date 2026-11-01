import { afterEach, describe, expect, it } from 'vitest'
import type { Event, LessonChange, LessonProgress, LessonSnapshot, Scenario } from '../protocol.gen'
import { record, type Recorder } from '../test/recorder'
import { lessonComplete, selectStepView, selectWhereAmI, stepStatuses } from './selectors'
import {
  activeStep,
  initialSessionData,
  reduce,
  useSessionStore,
  type SessionData,
} from './session'

let rec: Recorder | null = null
afterEach(() => rec?.dispose())

const run = (events: Event[], start: SessionData = initialSessionData()) =>
  events.reduce((s, ev, i) => reduce(s, ev, 1000 + i), start)

/** A snapshot of lesson L/1 with its first step active. */
const snap = (over: Partial<LessonSnapshot> = {}): LessonSnapshot => ({
  rev: 1,
  lessonId: 'L/1',
  step: { id: 'open', index: 0, count: 2 },
  phase: 'active',
  attempt: 1,
  outcomes: {},
  ...over,
})

const hello = (over: Partial<Extract<Event, { t: 'hello' }>> = {}): Event => ({
  t: 'hello',
  seq: 10,
  protocol: 2,
  sessionId: 'local',
  mode: 'terminal',
  emacs: { version: '31.1', uptimeMs: 5 },
  frames: { terminal: true, gui: false },
  modules: ['base', 'agent'],
  replayed: false,
  lesson: snap(),
  ...over,
})

const lesson = (
  change: LessonChange,
  over: Partial<LessonSnapshot>,
  ev: Partial<Extract<Event, { t: 'lesson' }>> = {},
): Event => ({ t: 'lesson', change, lesson: snap(over), ...ev })

const check = (attempt: number, ok = true): Event => ({
  t: 'check',
  lessonId: 'L/1',
  stepId: 'open',
  attempt,
  conditions: [
    { id: 'open/1', ok },
    { id: 'open/2', ok: false },
  ],
  complete: false,
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
  it('hello sets the session, mode, modules and lesson snapshot', () => {
    const s = run([hello()])
    expect(s.sessionId).toBe('local')
    expect(s.modules).toEqual(['base', 'agent'])
    expect(activeStep(s)).toEqual({ lessonId: 'L/1', stepId: 'open', index: 0, count: 2 })
    expect(activeStep(s)).toBe(activeStep(s))
    expect(s.incompatible).toBe(false)
    expect(s.lastSeq).toBe(10)
    expect(run([hello({ protocol: 1 })]).incompatible).toBe(true)
  })

  it('a lesson event applies only with a higher rev', () => {
    let s = run([hello(), lesson('loaded', { rev: 3, step: { id: 'save', index: 1, count: 2 } })])
    expect(activeStep(s)?.stepId).toBe('save')
    // A replayed or reordered event is old news.
    const same = reduce(s, lesson('loaded', { rev: 2 }), 2000)
    expect(same).toBe(s)
    s = run([lesson('complete', { rev: 4, step: null, phase: 'complete' })], s)
    expect(activeStep(s)).toBeNull()
    expect(s.lesson?.phase).toBe('complete')
  })

  it('hello always applies, also after a restart whose seq range overlaps the old one', () => {
    let s = run([hello({ seq: 40, lesson: snap({ rev: 9, attempt: 7 }) }), check(7)])
    expect(s.checks['open']).toBeDefined()
    // sessiond restarted: its hello has a lower rev and seq, and is the truth.
    s = run(
      [hello({ seq: 3, lesson: snap({ rev: 1, attempt: 1, outcomes: { open: 'done' } }) })],
      s,
    )
    expect(s.lesson?.rev).toBe(1)
    expect(s.lesson?.outcomes).toEqual({ open: 'done' })
    expect(s.checks).toEqual({})
    // Replayed events of the new process below the hello's rev change nothing.
    expect(reduce(s, lesson('reset', { rev: 1 }), 3000)).toBe(s)
    s = run([lesson('reset', { rev: 2, attempt: 2 })], s)
    expect(s.lesson?.attempt).toBe(2)
  })

  it('a check of another attempt is stale; a new attempt clears the checks', () => {
    let s = run([hello(), check(0), check(1)])
    expect(s.checks['open']?.attempt).toBe(1)
    s = run([lesson('phase', { rev: 2, phase: 'watching' }), check(1, false)], s)
    expect(s.checks['open']?.conditions[0]?.ok).toBe(false)
    s = run([lesson('reset', { rev: 3, attempt: 2 })], s)
    expect(s.checks).toEqual({})
    expect(run([check(1)], s).checks).toEqual({})
  })

  it('lapDone comes from lapMs; a new lap or another lesson clears it', () => {
    const lap = { id: 'P@1', lessonId: 'L/1', startedAt: 1 }
    let s = run([hello({ lesson: snap({ lap }) })])
    s = run([lesson('complete', { rev: 2, step: null, phase: 'complete' }, { lapMs: 61000 })], s)
    expect(s.lapDone).toEqual({ lessonId: 'L/1', ms: 61000 })
    s = run([lesson('loaded', { rev: 3, lap: { ...lap, id: 'P@2' } })], s)
    expect(s.lapDone).toBeNull()
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
    // The restart's reason names a module change (the overlay's text).
    s = run([{ t: 'emacs', event: 'restarting', reason: 'module set changed' }], s)
    expect(s.emacs.reason).toBe('module set changed')
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

  it('a stale prefix after the command that used its keys is ignored', () => {
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

  it('the ticker, the prefix and the habit cards belong to the lesson', () => {
    let s = run([
      hello({ lesson: snap({ lessonId: '' }) }),
      lesson('loaded', { rev: 2 }),
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
    s = run([lesson('loaded', { rev: 3, step: { id: 'save', index: 1, count: 2 } })], s)
    expect(s.ticker.length).toBe(2)
    // Another lesson starts empty.
    s = run([lesson('loaded', { rev: 4, lessonId: 'L/2' })], s)
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
    const s = run([hello(), check(1)])
    const views = selectStepView({
      scenario: SCENARIO,
      lessonId: 'L/1',
      lesson: s.lesson,
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

  it('a condition without a hint gives an empty hint text (the view says try Watch it)', () => {
    const views = selectStepView({
      scenario: {
        ...SCENARIO,
        steps: [{ id: 'open', goal: 'Open', conditions: [{ text: 'a', elisp: 't' }] }],
      },
      lessonId: 'L/1',
      lesson: snap(),
      progress: undefined,
      checks: {},
      hintLevels: { 'L/1#open': 1 },
      locale: 'en',
    })
    expect(views[0]?.hint).toEqual({ forText: 'a', text: '' })
  })

  it('no hint at level 0; unknown conditions are flagged', () => {
    const views = selectStepView({
      scenario: SCENARIO,
      lessonId: 'L/1',
      lesson: snap(),
      progress: undefined,
      checks: {
        open: {
          t: 'check',
          lessonId: 'L/1',
          stepId: 'open',
          attempt: 1,
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

  const at = (step: string | null, over: Partial<LessonSnapshot> = {}) =>
    snap({
      step: step ? { id: step, index: ['open', 'save', 'close'].indexOf(step), count: 3 } : null,
      ...over,
    })

  it('the snapshot of this lesson decides: its step is active, later steps show no tick', () => {
    const progress = saved({ open: 'done', save: 'done', close: 'done' })
    expect(stepStatuses(THREE, at('save'), progress)).toEqual(['todo', 'active', 'todo'])
    expect(
      stepStatuses(
        THREE,
        at('close', { outcomes: { open: 'skipped', save: 'demonstrated' } }),
        progress,
      ),
    ).toEqual(['skipped', 'demonstrated', 'active'])
    const done = at(null, {
      phase: 'complete',
      outcomes: { open: 'done', save: 'skipped', close: 'done' },
    })
    expect(stepStatuses(THREE, done, undefined)).toEqual(['done', 'skipped', 'done'])
    expect(lessonComplete(THREE, done, undefined)).toBe(true)
  })

  it('another lesson in the snapshot: saved progress decides', () => {
    const other = at('save', { lessonId: 'L/2' })
    const progress = saved({ open: 'done', save: 'skipped', close: 'in-progress' })
    expect(stepStatuses(THREE, other, progress)).toEqual(['done', 'skipped', 'todo'])
    expect(lessonComplete(THREE, other, progress)).toBe(false)
    expect(
      lessonComplete(THREE, null, saved({ open: 'done', save: 'skipped', close: 'done' })),
    ).toBe(true)
  })

  it('a watched active step offers Do it yourself, also after a reload', () => {
    const views = selectStepView({
      scenario: THREE,
      lessonId: 'L/1',
      lesson: at('save', { phase: 'watching', outcomes: { save: 'demonstrated' } }),
      progress: undefined,
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
})
