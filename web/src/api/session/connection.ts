/**
 * The connection layer above the router (frontend.md §2, §4): the session is
 * ensured once (POST /api/v1/sessions) when a screen that needs Emacs
 * mounts, and the events stream lives here, so navigating between lessons
 * keeps the WebSockets open. The terminal and GUI panes keep their own
 * sockets in persistent hosts (components/screen/hosts.ts).
 */
import { APIPrefix, type Event, type SessionInfo } from '../../protocol.gen'
import { useSessionStore } from '../../store/session'
import { toast } from '../../store/ui'
import i18n from '../../i18n'
import { api } from '../http'
import { EventsClient } from './events'
import { rpc, sessionBase, wsUrl } from './rpc'

let ensurePromise: Promise<SessionInfo> | null = null
let events: EventsClient | null = null
const listeners = new Set<(ev: Event) => void>()

/** Subscribes to every event after it went through applyEvent. */
export function onSessionEvent(fn: (ev: Event) => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

function sideEffects(ev: Event) {
  switch (ev.t) {
    case 'error':
      toast(ev.message, 'error')
      break
    case 'emacs':
      if (ev.event === 'daemon-restarted') {
        toast(
          ev.reason && /module/i.test(ev.reason)
            ? i18n.t('emacs.modulesLoaded')
            : i18n.t('emacs.restarted'),
        )
      }
      break
    case 'gap':
      // Events were lost: ask for a fresh evaluation (it also re-emits state).
      void rpc('check', {}, { silent: true }).catch(() => undefined)
      break
  }
}

export function startEvents(sessionId: string): EventsClient {
  if (events) return events
  const store = useSessionStore
  events = new EventsClient({
    url: (after) => wsUrl(`${sessionBase(sessionId)}/events${after > 0 ? `?after=${after}` : ''}`),
    getLastSeq: () => store.getState().lastSeq,
    onEvent: (ev) => {
      store.getState().applyEvent(ev)
      sideEffects(ev)
      for (const fn of listeners) fn(ev)
    },
    onState: (s) => store.getState().setConnection('events', s),
  })
  events.start()
  return events
}

/** Creates or finds the learner's session (idempotent per user) and starts the events stream. */
export function ensureSession(): Promise<SessionInfo> {
  if (!ensurePromise) {
    ensurePromise = api
      .post<SessionInfo>(`${APIPrefix}/sessions`, {})
      .then((info) => {
        useSessionStore.getState().setSessionId(info.id)
        startEvents(info.id)
        return info
      })
      .catch((e: unknown) => {
        ensurePromise = null
        throw e
      })
  }
  return ensurePromise
}

/** Stops everything (logout, tests). */
export function disconnectSession(): void {
  events?.stop()
  events = null
  ensurePromise = null
}
