import { Kbd } from '../common/Kbd'

/** Keycaps for one ticker entry: one cap per key, large for the newest entry. */
export function Keycap({ keys, last }: { keys: string[]; last: boolean }) {
  return <Kbd keys={keys.join(' ')} split {...(last ? { size: 'lg' as const } : {})} />
}
