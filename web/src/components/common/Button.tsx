import { forwardRef, type ButtonHTMLAttributes } from 'react'
import styles from './Button.module.css'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'default' | 'primary' | 'quiet'
  size?: 'normal' | 'small'
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'default', size = 'normal', className, type = 'button', ...rest },
  ref,
) {
  const cls = [styles.btn, styles[variant], size === 'small' ? styles.small : '', className ?? '']
    .filter(Boolean)
    .join(' ')
  return <button ref={ref} type={type} className={cls} {...rest} />
})
