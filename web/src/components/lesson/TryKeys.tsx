import { useTranslation } from 'react-i18next'
import { rpc } from '../../api/session/rpc'
import { toast } from '../../store/ui'
import { focusTerminal } from '../screen/focus'
import { Kbd } from '../common/Kbd'
import styles from './Lesson.module.css'

/** :try[C-x C-f]: a keycap with a "try it" button that sends the keys to Emacs. */
export function TryKeys({ keys }: { keys: string }) {
  const { t } = useTranslation()
  const send = async () => {
    try {
      await rpc('keys', { keys })
      toast(t('try.sent', { keys }))
    } catch {
      // toasted by rpc
    }
    focusTerminal()
  }
  return (
    <button
      type="button"
      className={styles.try}
      onClick={() => void send()}
      title={t('try.title', { keys })}
    >
      <Kbd keys={keys} />
      {t('try.label')}
    </button>
  )
}
