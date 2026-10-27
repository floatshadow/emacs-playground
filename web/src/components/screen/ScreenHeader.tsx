import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { Chip } from '../common/Chip'
import styles from './Screen.module.css'

/**
 * The line above the screen: what is running, at what size, and transient
 * states. One line of fixed height whatever it says (long text ends in an
 * ellipsis): if it grew, the screen below would shrink and the terminal
 * would resize. The demo caption lives under the screen instead.
 */
export function ScreenHeader() {
  const { t } = useTranslation()
  const hello = useSessionStore((s) => s.hello)
  const mode = useSessionStore((s) => s.mode)
  const modules = useSessionStore((s) => s.modules)
  const termSize = useUiStore((s) => s.termSize)
  const guiStarting = useUiStore((s) => s.guiStarting)
  const version = hello?.emacs?.version
  const label =
    mode === 'gui'
      ? t('screen.headerGui', { version: version ?? '…' })
      : t('screen.headerTerminal', {
          version: version ?? '…',
          size: termSize ? `${termSize.cols}×${termSize.rows}` : '',
        })
  return (
    <div className={styles.head}>
      <span className={styles.headLabel} title={label}>
        {label}
      </span>
      {modules.length > 0 && (
        <Chip title={t('screen.modulesTitle')}>
          {t('screen.modules', { list: modules.filter((m) => m !== 'agent').join(' ') })}
        </Chip>
      )}
      {guiStarting && mode !== 'gui' && (
        <span className={styles.headChip} role="status">
          <Chip tone="busy">{t('frame.starting')}</Chip>
        </span>
      )}
    </div>
  )
}
