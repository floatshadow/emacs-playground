import { Fragment, memo } from 'react'
import { keyify } from '../../advice/keys'
import styles from './Kbd.module.css'

/**
 * A keycap. `keys` is a key sequence in Emacs notation. By default the whole
 * sequence is one cap (lesson prose, as in the demo); `split` renders one cap
 * per key (ticker, cheat sheet, hints).
 */
export function Kbd({
  keys,
  split = false,
  size,
}: {
  keys: string
  split?: boolean
  size?: 'sm' | 'lg'
}) {
  const cls = size ? `${styles.kbd} ${styles[size]}` : styles.kbd
  if (!split) return <kbd className={cls}>{keys}</kbd>
  const parts = keys.split(' ').filter(Boolean)
  return (
    <span className={styles.seq}>
      {parts.map((k, i) => (
        <kbd key={i} className={cls}>
          {k}
        </kbd>
      ))}
    </span>
  )
}

/** Prose with key names rendered as keycaps (the demo's keyify). */
export const KeyText = memo(function KeyText({ text, size }: { text: string; size?: 'sm' }) {
  const segs = keyify(text)
  return (
    <>
      {segs.map((s, i) =>
        'text' in s ? (
          <Fragment key={i}>{s.text}</Fragment>
        ) : (
          <Kbd key={i} keys={s.keys.join(' ')} split {...(size ? { size } : {})} />
        ),
      )}
    </>
  )
})
