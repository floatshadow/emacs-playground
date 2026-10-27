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
import {
  habitCardFor,
  loadHabitsSeen,
  saveHabitsSeen,
  type HabitCard,
  type Nudge,
} from '../advice/habits'
import { mentionedNow, type Mentions } from '../advice/mentions'
import {
  tickerGap,
  tickerNote,
  isMouseOnly,
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

/**
 * How a step ended in this visit (fix plan contract a): completed by the
 * learner ("complete" → done), completed while a "Watch it" demonstration
 * was latched, or skipped.
 */
export type StepOutcome = 'done' | 'demonstrated' | 'skipped'

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
  /**
   * This visit's outcome per step, by lesson id. A step that loads again
   * (redo, go to) loses its outcome until it ends again.
   */
  outcomes: Record<string, Record<string, StepOutcome>>
  /** Lesson ids whose last step completed (or was skipped) this session. */
  lessonsComplete: string[]
  /**
   * The active step was watched: a demo started on it, or it was
   * demonstrated. It shows "Do it yourself" until the step is reset or
   * loaded again (contract b).
   */
  watched: { lessonId: string; stepId: string } | null
  /** True while the demo player plays (resets from its restore keep `watched`). */
  demoPlaying: boolean
  mode: FrameKind
  modules: string[]
  modulesRestarting: boolean
  serverNudges: Nudge[]
  cards: HabitCard[]
  dismissed: Set<string>
  habitsSeen: Set<string>
  /** Keys the current lesson mentions (habit cards and patterns stay quiet for them). */
  mentions: Mentions | null
  totalCommands: number
  /** Server time (ts) of the newest key event: older prefix events are stale (B4). */
  lastKeyTs: number
  emacs: EmacsStatusView
  echo: { text: string; at: number } | null
  /** Echo text still shown: set by echo events, cleared by the next key (exits.yaml session.echo). */
  echoLive: string | null
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
    outcomes: {},
    lessonsComplete: [],
    watched: null,
    demoPlaying: false,
    mode: 'terminal',
    modules: [],
    modulesRestarting: false,
    serverNudges: [],
    cards: [],
    dismissed: new Set(),
    habitsSeen: new Set(),
    mentions: null,
    totalCommands: 0,
    lastKeyTs: 0,
    emacs: { overlay: null, busy: false },
    echo: null,
    echoLive: null,
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

function setOutcome(
  m: SessionData['outcomes'],
  lessonId: string,
  stepId: string,
  outcome: StepOutcome | null,
): SessionData['outcomes'] {
  const cur = m[lessonId] ?? {}
  if (outcome === null) {
    if (!(stepId in cur)) return m
    const next = { ...cur }
    delete next[stepId]
    return { ...m, [lessonId]: next }
  }
  if (cur[stepId] === outcome) return m
  return { ...m, [lessonId]: { ...cur, [stepId]: outcome } }
}

const sameStep = (
  a: { lessonId: string; stepId: string } | null,
  lessonId: string,
  stepId: string,
) => !!a && a.lessonId === lessonId && a.stepId === stepId

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
      // Server time only: prefix events carry the same clock (the client's may differ).
      const lastKeyTs = ev.ts !== undefined ? Math.max(base.lastKeyTs, ev.ts) : base.lastKeyTs
      // Clicks and scrolling are not keys: no ticker entry, no habit card.
      if (!ev.mx && isMouseOnly(ev.keys)) return { ...base, echoLive: null, lastKeyTs }
      const ticker = tickerOnKey(base.ticker, ev, now)
      const entry = ticker[ticker.length - 1]
      const totalCommands = base.totalCommands + 1
      let cards = base.cards
      let habitsSeen = base.habitsSeen
      if (entry && now >= base.quietHabitsUntil) {
        const mentioned = mentionedNow(base.mentions, base.activeStep)
        const card = habitCardFor(entry, base.tables.habits, habitsSeen, totalCommands, mentioned)
        if (card) {
          habitsSeen = new Set(habitsSeen).add(card.key)
          cards = [...cards, card]
        }
      }
      return {
        ...base,
        ticker,
        prefix: null,
        totalCommands,
        cards,
        habitsSeen,
        echoLive: null,
        lastKeyTs,
      }
    }
    case 'prefix': {
      // A prefix older than the newest command is stale: Emacs already used
      // its keys (the tap reports a lone ESC from a timer, B4).
      if (ev.ts !== undefined && ev.ts < base.lastKeyTs) return base
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
      return { ...base, echo: { text: ev.text, at: ev.ts ?? now }, echoLive: ev.text }
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
          // The GUI frame closing while the terminal is shown (a switch back
          // to the terminal) is not something to cover the screen for.
          if (ev.frame === 'gui' && base.mode !== 'gui') return base
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
    case 'step':
      return reduceStep(base, ev)
    case 'nudge': {
      const n: Nudge = { id: ev.id, kind: ev.kind, title: ev.title, body: ev.body }
      return { ...base, serverNudges: [...base.serverNudges.filter((x) => x.id !== ev.id), n] }
    }
    case 'checkpoint':
      return { ...base, checkpointsVersion: base.checkpointsVersion + 1 }
    case 'mode':
      // Another frame reads keys from another keyboard: a prefix pending in
      // the one left behind is not what the learner is in.
      return { ...base, mode: ev.mode, prefix: ev.mode === base.mode ? base.prefix : null }
    case 'modules':
      return { ...base, modules: ev.enabled ?? base.modules, modulesRestarting: ev.restarting }
    case 'gap':
      return { ...base, ticker: tickerGap(base.ticker, now) }
    case 'error':
      return { ...base, lastError: ev }
  }
  return base
}

/** The step lifecycle (api.md §4.1). */
function reduceStep(base: SessionData, ev: Extract<Event, { t: 'step' }>): SessionData {
  const ref: ActiveStep = {
    lessonId: ev.lessonId,
    stepId: ev.stepId,
    index: ev.index,
    count: ev.count,
  }
  switch (ev.status) {
    case 'loaded':
    case 'reset': {
      const otherLesson = base.activeStep?.lessonId !== ev.lessonId
      const checks = { ...(otherLesson && ev.status === 'loaded' ? {} : base.checks) }
      delete checks[ev.stepId]
      // A reset from the demo's own restore (demo start) keeps "watched".
      const keepWatched =
        ev.status === 'reset' && base.demoPlaying && sameStep(base.watched, ev.lessonId, ev.stepId)
      const next: SessionData = {
        ...base,
        activeStep: ref,
        checks,
        outcomes: setOutcome(base.outcomes, ev.lessonId, ev.stepId, null),
        lessonsComplete: base.lessonsComplete.filter((x) => x !== ev.lessonId),
        watched: keepWatched ? base.watched : null,
      }
      if (ev.status === 'loaded' && otherLesson) {
        // The ticker, the pending prefix and the habit cards belong to the
        // lesson (decision 8; R6, R9, B9).
        next.ticker = []
        next.prefix = null
        next.cards = []
      }
      return next
    }
    case 'demonstrated':
      return {
        ...base,
        outcomes: setOutcome(base.outcomes, ev.lessonId, ev.stepId, 'demonstrated'),
        watched: { lessonId: ev.lessonId, stepId: ev.stepId },
      }
    case 'complete':
    case 'skipped': {
      const outcomes = setOutcome(
        base.outcomes,
        ev.lessonId,
        ev.stepId,
        ev.status === 'complete' ? 'done' : 'skipped',
      )
      const lessonsComplete =
        ev.lessonComplete && !base.lessonsComplete.includes(ev.lessonId)
          ? [...base.lessonsComplete, ev.lessonId]
          : base.lessonsComplete
      const activeStep = ev.lessonComplete ? { ...ref } : base.activeStep
      const watched = sameStep(base.watched, ev.lessonId, ev.stepId) ? null : base.watched
      return { ...base, outcomes, lessonsComplete, activeStep, watched }
    }
    case 'dropped': {
      const dropped = sameStep(base.activeStep, ev.lessonId, ev.stepId)
      return {
        ...base,
        activeStep: dropped ? null : base.activeStep,
        checks: dropped ? {} : base.checks,
        watched: sameStep(base.watched, ev.lessonId, ev.stepId) ? null : base.watched,
      }
    }
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
  /** The keys the lesson on screen mentions (nudge suppression, contract d). */
  setMentions(m: Mentions | null): void
  /**
   * Demo playback started (true) or ended (false). Habit cards stay quiet
   * while it plays and for tailMs (default DEMO_QUIET_TAIL_MS) after, since
   * the agent's key events trail the keys the player sent. When the
   * learner's own typing stopped the demo the tail is 0: their keys count.
   */
  setDemoPlaying(playing: boolean, tailMs?: number): void
  /** A demo started on this step: it shows "Do it yourself" until reset (contract b). */
  markWatched(lessonId: string, stepId: string): void
  /**
   * "Start over" (contract a): forgets what this page remembers about the
   * learner's progress: outcomes, completions, checks, habit keys seen
   * (also in localStorage), habit cards and dismissals. The active step
   * clears when sessiond's "dropped" event arrives.
   */
  forgetProgress(lessonId?: string): void
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
  setMentions: (mentions) => set({ mentions }),
  setDemoPlaying: (playing, tailMs = DEMO_QUIET_TAIL_MS) =>
    set({
      demoPlaying: playing,
      quietHabitsUntil: playing ? Number.POSITIVE_INFINITY : Date.now() + tailMs,
    }),
  markWatched: (lessonId, stepId) => set({ watched: { lessonId, stepId } }),
  forgetProgress: (lessonId) =>
    set((s) => {
      if (lessonId) {
        const outcomes = { ...s.outcomes }
        delete outcomes[lessonId]
        return {
          outcomes,
          lessonsComplete: s.lessonsComplete.filter((x) => x !== lessonId),
          checks: s.activeStep?.lessonId === lessonId ? {} : s.checks,
          watched: s.watched?.lessonId === lessonId ? null : s.watched,
        }
      }
      if (s.sessionId) saveHabitsSeen(s.sessionId, new Set())
      return {
        outcomes: {},
        lessonsComplete: [],
        checks: {},
        watched: null,
        habitsSeen: new Set<string>(),
        cards: [],
        dismissed: new Set<string>(),
      }
    }),
  note: (entry) => set((s) => ({ ticker: tickerNote(s.ticker, entry, Date.now()) })),
  reset: () => set(initialSessionData()),
}))

// "Once each" survives reloads (contract d): the habit keys already shown
// are kept per session in localStorage, read when the session id becomes
// known and written whenever a card fires.
useSessionStore.subscribe((s, prev) => {
  if (s.sessionId && s.sessionId !== prev.sessionId) {
    const stored = loadHabitsSeen(s.sessionId)
    if (stored.some((k) => !s.habitsSeen.has(k))) {
      useSessionStore.setState({ habitsSeen: new Set([...s.habitsSeen, ...stored]) })
    }
    return
  }
  if (s.sessionId && s.habitsSeen !== prev.habitsSeen && s.habitsSeen.size > 0) {
    saveHabitsSeen(s.sessionId, s.habitsSeen)
  }
})

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
