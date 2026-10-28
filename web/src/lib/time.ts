/** Small time helpers for status lines ("12 s ago", "3 min"). */

export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason ?? new DOMException('aborted', 'AbortError'))
      return
    }
    const onAbort = () => {
      clearTimeout(t)
      reject(signal?.reason ?? new DOMException('aborted', 'AbortError'))
    }
    const t = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

/** Compact relative time: "just now", "12 s ago", "3 min ago", "2 h ago". */
export function ago(ts: number, now: number = Date.now()): string {
  const s = Math.max(0, Math.round((now - ts) / 1000))
  if (s < 3) return 'just now'
  if (s < 60) return `${s} s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h} h ago`
  return `${Math.round(h / 24)} d ago`
}

/** Duration in words: "45 min", "1.5 h", "2 h". */
export function minutes(n: number): string {
  if (n < 60) return `${n} min`
  const h = n / 60
  return `${Number.isInteger(h) ? h : h.toFixed(1)} h`
}

/** Uptime from milliseconds: "40 s", "3 min", "2 h 5 min". */
export function uptime(ms: number): string {
  const m = Math.floor(ms / 60000)
  if (m < 1) return `${Math.max(0, Math.floor(ms / 1000))} s`
  if (m < 60) return `${m} min`
  return `${Math.floor(m / 60)} h ${m % 60} min`
}

/** Clock time for checkpoint lists: "today 18:01" or "Oct 25 17:48". */
export function clock(ts: number, now: number = Date.now()): string {
  const d = new Date(ts)
  const hm = d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  if (d.toDateString() === new Date(now).toDateString()) return `today ${hm}`
  return `${d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} ${hm}`
}

/** A stopwatch time: "4:05", "1:02:03". */
export function formatLap(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const ss = String(s).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}
