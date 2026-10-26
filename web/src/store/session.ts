/**
 * Live session state (frontend.md §3): one Zustand store fed by the single
 * reducer applyEvent for /events. `reduce` is pure (it receives the clock)
 * and is unit-tested with recorded event streams; the store only writes its
 * result.
 */
import { create } from 'zustand'
import type {
  CheckEvent,
  ErrorEvent,
  Event,
  FrameKind,
  HelloEvent,
  State,
  Tables,
} from '../protocol.gen'
import { expireCards, habitCardFor, type HabitCard, type Nudge } from '../advice/habits'
import {
  tickerGap,
  tickerNote,
  tickerOnKey,
  tickerOnPrefix,
  type TickerEntry,
} from '../advice/ticker'
import { DEFAULT_TABLES } from '../advice/defaults'

export type ConnState =
  | 'idle'
  | 'connecting'
  | 'open'
  | 'reconnecting'
  | 'closed'
  /** /pty only: another viewer took over (close 4000). */
  | 'replaced'
  /** /pty only: Emacs unavailable (close 4001). */
  | 'unavailable'

export type EmacsOverlay = 'frame-closed' | 'suspended' | 'restarting' | 'stopping' | null

export interface EmacsStatusView {
  overlay: EmacsOverlay
  /** Missed heartbeats: the "Emacs is busy" banner. */
  busy: boolean
  reason?: string
  /** When the daemon last came back after a restart (ms); drives a notice. */
  restartedAt?: number
  /** Frame that was closed, for the overlay text. */
  frame?: FrameKind
}

export interface ActiveStep {
  lessonId: string
  stepId: string
  index?: number
  count?: number
}

export interface FileMark {
  event: 'created' | 'modified' | 'deleted'
  ts: number
  /** Increments on every event for the path: a cache-busting version. */
  version: number
}

export interface SessionData {
  sessionId: string | null
  connection: { events: ConnState; pty: ConnState; vnc: ConnState }
  hello: HelloEvent | null
  state: State | null
  prefix: string | null
  ticker: TickerEntry[]
  /** Latest check by stepId (only for the active lesson). */
  checks: Record<string, CheckEvent>
  activeStep: ActiveStep | null
  /** Steps completed this session, by lesson id. */
  completed: Record<string, string[]>
  /** Steps (re)loaded this session, by lesson id: they are not done until completed again. */
  reopened: Record<string, string[]>
  /** Lesson ids whose last step completed this session. */
  lessonsComplete: string[]
  mode: FrameKind
  modules: string[]
  modulesRestarting: boolean
  serverNudges: Nudge[]
  cards: HabitCard[]
  dismissed: Set<string>
  habitsSeen: Set<string>
  totalCommands: number
  emacs: EmacsStatusView
  echo: { text: string; at: number } | null
  minibufferSince: number | null
  files: Record<string, FileMark>
  /** Increments on checkpoint events; checkpoint lists refetch on change. */
  checkpointsVersion: number
  /** Increments when a workspace file is created or deleted; the file tree refetches. */
  treeVersion: number
  lastError: ErrorEvent | null
  lastSeq: number
  /** Advice tables from the curriculum index (with built-in defaults). */
  tables: Tables
  /** rpc('describe') glosses for commands missing from tables.gloss. */
  described: Record<string, string>
  /**
   * Key events before this time (ms) raise no habit cards: the demo player
   * is typing, not the learner, and a habit shows once per session, so a
   * demo that uses C-s to search must not use up "C-s is not save".
   */
  quietHabitsUntil: number
}

export function initialSessionData(): SessionData {
  return {
    sessionId: null,
    connection: { events: 'idle', pty: 'idle', vnc: 'idle' },
    hello: null,
    state: null,
    prefix: null,
    ticker: [],
    checks: {},
    activeStep: null,
    completed: {},
    reopened: {},
    lessonsComplete: [],
    mode: 'terminal',
    modules: [],
    modulesRestarting: false,
    serverNudges: [],
    cards: [],
    dismissed: new Set(),
    habitsSeen: new Set(),
    totalCommands: 0,
    emacs: { overlay: null, busy: false },
    echo: null,
    minibufferSince: null,
    files: {},
    checkpointsVersion: 0,
    treeVersion: 0,
    lastError: null,
    lastSeq: 0,
    tables: DEFAULT_TABLES,
    described: {},
    quietHabitsUntil: 0,
  }
}

function addTo(
  m: Record<string, string[]>,
  lessonId: string,
  stepId: string,
): Record<string, string[]> {
  const cur = m[lessonId] ?? []
  if (cur.includes(stepId)) return m
  return { ...m, [lessonId]: [...cur, stepId] }
}

function removeFrom(
  m: Record<string, string[]>,
  lessonId: string,
  stepId: string,
): Record<string, string[]> {
  const cur = m[lessonId]
  if (!cur || !cur.includes(stepId)) return m
  return { ...m, [lessonId]: cur.filter((x) => x !== stepId) }
}

/**
 * The reducer for /events. Pure: the result depends only on the inputs.
 * Unknown event types are ignored (forward compatibility).
 */
export function reduce(s: SessionData, ev: Event, now: number): SessionData {
  const seq = typeof ev.seq === 'number' ? ev.seq : s.lastSeq
  const base: SessionData = seq !== s.lastSeq ? { ...s, lastSeq: Math.max(s.lastSeq, seq) } : s
  switch (ev.t) {
    case 'hello': {
      const next: SessionData = {
        ...base,
        // A fresh (non-replayed) hello may come from a restarted sessiond
        // whose seq starts again at 1.
        lastSeq: ev.replayed ? base.lastSeq : (ev.seq ?? 0),
        hello: ev,
        sessionId: ev.sessionId || base.sessionId,
        mode: ev.mode,
        modules: ev.modules ?? [],
        emacs: { ...base.emacs, overlay: null, busy: false },
        prefix: null,
      }
      if (ev.step) {
        next.activeStep =
          base.activeStep &&
          base.activeStep.lessonId === ev.step.lessonId &&
          base.activeStep.stepId === ev.step.stepId
            ? base.activeStep
            : { lessonId: ev.step.lessonId, stepId: ev.step.stepId }
      } else {
        next.activeStep = null
      }
      return next
    }
    case 'key': {
      const ticker = tickerOnKey(base.ticker, ev, now)
      const entry = ticker[ticker.length - 1]
      const totalCommands = base.totalCommands + 1
      let cards = expireCards(base.cards, totalCommands)
      let habitsSeen = base.habitsSeen
      if (entry && now >= base.quietHabitsUntil) {
        const card = habitCardFor(entry, base.tables.habits, habitsSeen, totalCommands)
        if (card) {
          habitsSeen = new Set(habitsSeen).add(card.key)
          cards = [...cards, card]
        }
      }
      return { ...base, ticker, prefix: null, totalCommands, cards, habitsSeen }
    }
    case 'prefix': {
      const keys = ev.keys.trim()
      return { ...base, ticker: tickerOnPrefix(base.ticker, ev, now), prefix: keys || null }
    }
    case 'state': {
      const opened = ev.minibuffer !== null && ev.minibuffer !== undefined
      let minibufferSince = base.minibufferSince
      if (opened && minibufferSince === null) minibufferSince = ev.ts ?? now
      if (!opened) minibufferSince = null
      return { ...base, state: ev, minibufferSince }
    }
    case 'echo':
      return { ...base, echo: { text: ev.text, at: ev.ts ?? now } }
    case 'minibuffer': {
      if (ev.open) {
        return base.minibufferSince === null ? { ...base, minibufferSince: ev.ts ?? now } : base
      }
      return { ...base, minibufferSince: null }
    }
    case 'emacs': {
      const e = base.emacs
      switch (ev.event) {
        case 'frame-closed': {
          const next: EmacsStatusView = { ...e, overlay: 'frame-closed' }
          if (ev.frame) next.frame = ev.frame
          if (ev.reason) next.reason = ev.reason
          return { ...base, emacs: next }
        }
        case 'frame-opened':
          return {
            ...base,
            emacs: { ...e, overlay: e.overlay === 'frame-closed' ? null : e.overlay },
          }
        case 'suspended':
          return { ...base, emacs: { ...e, overlay: 'suspended' } }
        case 'resumed':
          return { ...base, emacs: { ...e, overlay: e.overlay === 'suspended' ? null : e.overlay } }
        case 'restarting': {
          const next: EmacsStatusView = { ...e, overlay: 'restarting', busy: false }
          if (ev.reason) next.reason = ev.reason
          return { ...base, emacs: next, prefix: null }
        }
        case 'daemon-restarted':
          return {
            ...base,
            emacs: { ...e, overlay: null, busy: false, restartedAt: ev.ts ?? now },
            modulesRestarting: false,
          }
        case 'unresponsive':
          return { ...base, emacs: { ...e, busy: true } }
        case 'responsive':
          return { ...base, emacs: { ...e, busy: false } }
        case 'stopping':
          return { ...base, emacs: { ...e, overlay: 'stopping' } }
      }
      return base
    }
    case 'file': {
      const prev = base.files[ev.path]
      return {
        ...base,
        treeVersion: ev.event === 'modified' ? base.treeVersion : base.treeVersion + 1,
        files: {
          ...base.files,
          [ev.path]: { event: ev.event, ts: ev.ts ?? now, version: (prev?.version ?? 0) + 1 },
        },
      }
    }
    case 'check':
      return { ...base, checks: { ...base.checks, [ev.stepId]: ev } }
    case 'step': {
      const ref = { lessonId: ev.lessonId, stepId: ev.stepId, index: ev.index, count: ev.count }
      if (ev.status === 'loaded' || ev.status === 'reset') {
        const sameLesson = base.activeStep?.lessonId === ev.lessonId
        const checks = { ...(sameLesson || ev.status === 'reset' ? base.checks : {}) }
        delete checks[ev.stepId]
        return {
          ...base,
          activeStep: ref,
          checks,
          completed: removeFrom(base.completed, ev.lessonId, ev.stepId),
          reopened: addTo(base.reopened, ev.lessonId, ev.stepId),
          lessonsComplete: base.lessonsComplete.filter((x) => x !== ev.lessonId),
        }
      }
      // complete
      const completed = addTo(base.completed, ev.lessonId, ev.stepId)
      const reopened = removeFrom(base.reopened, ev.lessonId, ev.stepId)
      const lessonsComplete =
        ev.lessonComplete && !base.lessonsComplete.includes(ev.lessonId)
          ? [...base.lessonsComplete, ev.lessonId]
          : base.lessonsComplete
      const activeStep = ev.lessonComplete ? { ...ref } : base.activeStep
      return { ...base, completed, reopened, lessonsComplete, activeStep }
    }
    case 'nudge': {
      const n: Nudge = { id: ev.id, kind: ev.kind, title: ev.title, body: ev.body }
      return { ...base, serverNudges: [...base.serverNudges.filter((x) => x.id !== ev.id), n] }
    }
    case 'checkpoint':
      return { ...base, checkpointsVersion: base.checkpointsVersion + 1 }
    case 'mode':
      return { ...base, mode: ev.mode }
    case 'modules':
      return { ...base, modules: ev.enabled ?? base.modules, modulesRestarting: ev.restarting }
    case 'gap':
      return { ...base, ticker: tickerGap(base.ticker, now) }
    case 'error':
      return { ...base, lastError: ev }
  }
  return base
}

/** How long after a demo ends its trailing key events still raise no habit cards. */
export const DEMO_QUIET_TAIL_MS = 600

export interface SessionStore extends SessionData {
  applyEvent(ev: Event): void
  dismiss(id: string): void
  setSessionId(id: string | null): void
  setConnection(kind: 'events' | 'pty' | 'vnc', st: ConnState): void
  setTables(t: Tables): void
  setDescribed(cmd: string, gloss: string): void
  /**
   * Demo playback started (true) or ended (false). Habit cards stay quiet
   * while it plays and for tailMs (default DEMO_QUIET_TAIL_MS) after, since
   * the agent's key events trail the keys the player sent. When the
   * learner's own typing stopped the demo the tail is 0: their keys count.
   */
  setDemoPlaying(playing: boolean, tailMs?: number): void
  /** A synthetic ticker entry (Rescue, Reset). */
  note(entry: { keys: string[]; cmd: string; gloss?: string; kind: TickerEntry['kind'] }): void
  /** Back to the initial state (tests, logout). */
  reset(): void
}

export const useSessionStore = create<SessionStore>()((set) => ({
  ...initialSessionData(),
  applyEvent: (ev) => set((s) => reduce(s, ev, Date.now())),
  dismiss: (id) => set((s) => ({ dismissed: new Set(s.dismissed).add(id) })),
  setSessionId: (id) => set({ sessionId: id }),
  setConnection: (kind, st) =>
    set((s) => (s.connection[kind] === st ? s : { connection: { ...s.connection, [kind]: st } })),
  setTables: (tables) => set({ tables }),
  setDescribed: (cmd, gloss) => set((s) => ({ described: { ...s.described, [cmd]: gloss } })),
  setDemoPlaying: (playing, tailMs = DEMO_QUIET_TAIL_MS) =>
    set({ quietHabitsUntil: playing ? Number.POSITIVE_INFINITY : Date.now() + tailMs }),
  note: (entry) => set((s) => ({ ticker: tickerNote(s.ticker, entry, Date.now()) })),
  reset: () => set(initialSessionData()),
}))

/** Resolves once the store has a session id (rpc and the panes wait on it). */
export function whenSessionId(timeoutMs = 30000): Promise<string> {
  const cur = useSessionStore.getState().sessionId
  if (cur) return Promise.resolve(cur)
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => {
      unsub()
      reject(new Error('no session'))
    }, timeoutMs)
    const unsub = useSessionStore.subscribe((s) => {
      if (s.sessionId) {
        clearTimeout(t)
        unsub()
        resolve(s.sessionId)
      }
    })
  })
}
