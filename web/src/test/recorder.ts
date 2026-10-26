/**
 * Records event streams from the fake session's mock Emacs (a port of the
 * demo engine), so reducer and advice tests run against realistic streams:
 * key, prefix, minibuffer and echo events, then a state after every key,
 * as the agent coalesces them.
 */
import type { Event } from '../protocol.gen'
import { MockEmacs } from '../../dev/mock-emacs.ts'
import { HELLO, POLY } from '../../dev/workspace.ts'
import { initialSessionData, reduce, type SessionData } from '../store/session'

export interface Recorder {
  eng: MockEmacs
  events: Event[]
  now(): number
  /** Presses keys (space-separated Emacs names), 200 ms apart. */
  keys(seq: string): Recorder
  /** Types text literally, 70 ms per character. */
  type(text: string): Recorder
  /** Advances the clock. */
  wait(ms: number): Recorder
  /** Replays everything recorded so far into a fresh store. */
  replay(start?: SessionData): SessionData
  dispose(): void
}

export function record(
  open: string[] = ['hello/src/main.rs'],
  files = { ...HELLO, ...POLY },
): Recorder {
  const events: Event[] = []
  let seq = 0
  let t = 1_761_472_000_000
  const push = (ev: Event) => {
    ev.seq = ++seq
    ev.ts = t
    events.push(ev)
  }
  const eng = new MockEmacs(new Map(Object.entries(files)), { emit: push, now: () => t })
  eng.reset(open)
  let lastState = ''
  const pushState = () => {
    const st = eng.state()
    const s = JSON.stringify(st)
    if (s === lastState) return
    lastState = s
    push(st)
  }
  pushState()
  const r: Recorder = {
    eng,
    events,
    now: () => t,
    keys(s) {
      for (const k of s.trim().split(/\s+/)) {
        t += 200
        eng.handleKey(k)
        pushState()
      }
      return r
    },
    type(text) {
      for (const ch of text) {
        t += 70
        eng.handleKey(ch === ' ' ? 'SPC' : ch === '\n' ? 'RET' : ch)
        pushState()
      }
      return r
    },
    wait(ms) {
      t += ms
      return r
    },
    replay(start = initialSessionData()) {
      return events.reduce((s, ev) => reduce(s, ev, ev.ts ?? t), start)
    },
    dispose() {
      eng.dispose()
    },
  }
  return r
}
