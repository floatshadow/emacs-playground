/** Throws when cond is falsy; narrows the type. */
export function invariant(cond: unknown, message: string): asserts cond {
  if (!cond) throw new Error('invariant: ' + message)
}
