/** A same-origin path to return to after signing in (never /login itself), else "/". */
export function safeNext(from: unknown): string {
  return typeof from === 'string' &&
    from.startsWith('/') &&
    !from.startsWith('//') &&
    !from.startsWith('/\\') &&
    !/^\/login(?:[/?#]|$)/.test(from)
    ? from
    : '/'
}
