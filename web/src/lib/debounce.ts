/** Trailing-edge debounce with cancel and flush. */
export interface Debounced<A extends unknown[]> {
  (...args: A): void
  cancel(): void
  flush(): void
}

export function debounce<A extends unknown[]>(fn: (...args: A) => void, ms: number): Debounced<A> {
  let timer: ReturnType<typeof setTimeout> | null = null
  let pending: A | null = null
  const run = () => {
    timer = null
    const args = pending
    pending = null
    if (args) fn(...args)
  }
  const d = ((...args: A) => {
    pending = args
    if (timer) clearTimeout(timer)
    timer = setTimeout(run, ms)
  }) as Debounced<A>
  d.cancel = () => {
    if (timer) clearTimeout(timer)
    timer = null
    pending = null
  }
  d.flush = () => {
    if (timer) clearTimeout(timer)
    run()
  }
  return d
}
