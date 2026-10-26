import styles from './SegmentedControl.module.css'

export interface Segment<T extends string> {
  value: T
  label: string
  disabled?: boolean
  title?: string
}

/** The demo's .seg: a small group of toggle buttons with one pressed. */
export function SegmentedControl<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: T
  options: Segment<T>[]
  onChange(v: T): void
}) {
  return (
    <div className={styles.seg} role="group" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          className={o.value === value ? styles.on : undefined}
          aria-pressed={o.value === value}
          disabled={o.disabled}
          title={o.title}
          onClick={() => o.value !== value && onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}
