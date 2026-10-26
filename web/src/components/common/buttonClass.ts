import styles from './Button.module.css'

export type ButtonVariant = 'default' | 'primary' | 'quiet'
export type ButtonSize = 'normal' | 'small'

/** A link styled as a button (react-router Link or plain anchor). */
export function buttonClass(variant: ButtonVariant = 'default', size: ButtonSize = 'normal') {
  return [styles.btn, styles[variant], size === 'small' ? styles.small : '']
    .filter(Boolean)
    .join(' ')
}
