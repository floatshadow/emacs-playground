import { useTranslation } from 'react-i18next'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { Chip } from '../common/Chip'
import { Button } from '../common/Button'
import { cancelDemo } from '../lesson/demoPlayer'
import styles from './Screen.module.css'

/** The line above the screen: what is running, at what size, and transient states. */
export function ScreenHeader() {
  const { t } = useTranslation()
  const hello = useSessionStore((s) => s.hello)
  const mode = useSessionStore((s) => s.mode)
  const modules = useSessionStore((s) => s.modules)
  const termSize = useUiStore((s) => s.termSize)
  const guiStarting = useUiStore((s) => s.guiStarting)
  const demo = useUiStore((s) => s.demo)
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
      <span>{label}</span>
      {modules.length > 0 && (
        <Chip title={t('screen.modulesTitle')}>
          {t('screen.modules', { list: modules.filter((m) => m !== 'agent').join(' ') })}
        </Chip>
      )}
      {guiStarting && mode !== 'gui' && <Chip tone="busy">{t('frame.starting')}</Chip>}
      <span className={styles.spacer} />
      {demo && (
        <span className={styles.playing} role="status">
          {t('demo.playing', { goal: demo.goal })}
          <Button size="small" onClick={() => cancelDemo()}>
            {t('demo.stop')}
          </Button>
        </span>
      )}
    </div>
  )
}
