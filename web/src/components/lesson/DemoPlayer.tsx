import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../store/ui'
import { Button } from '../common/Button'
import { cancelDemo, playDemo } from './demoPlayer'

/** "Watch it": plays the step's demo into the live Emacs; "Stop" while playing. */
export function DemoPlayer({ stepId, goal, demo }: { stepId: string; goal: string; demo: string }) {
  const { t } = useTranslation()
  const playing = useUiStore((s) => s.demo?.stepId === stepId)
  return playing ? (
    <Button size="small" onClick={() => cancelDemo()}>
      {t('demo.stop')}
    </Button>
  ) : (
    <Button
      size="small"
      onClick={() => void playDemo(stepId, goal, demo)}
      title={t('demo.watchTitle')}
    >
      {t('demo.watch')}
    </Button>
  )
}
