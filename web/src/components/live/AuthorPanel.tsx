import { useTranslation } from 'react-i18next'
import type { Scenario } from '../../protocol.gen'
import { evaluateExits } from '../../advice/exits'
import { useSessionStore } from '../../store/session'
import styles from './AuthorPanel.module.css'

/**
 * Author mode (CURRICULUM.md Part B.5 step 4): live condition results, the
 * exits rows that matched, and the raw state, so authors see what the
 * learner will see. Shown only when /me reports features.authorMode.
 */
export function AuthorPanel({
  lessonId,
  scenario,
}: {
  lessonId: string
  scenario: Scenario | undefined
}) {
  const { t } = useTranslation()
  const state = useSessionStore((s) => s.state)
  const exits = useSessionStore((s) => s.tables.exits)
  const active = useSessionStore((s) => s.activeStep)
  const check = useSessionStore((s) => (s.activeStep ? s.checks[s.activeStep.stepId] : undefined))
  const matched = state ? evaluateExits(state, exits).map((e) => e.id) : []
  return (
    <div className={styles.panel}>
      <p>
        <b>{t('author.step')}</b>{' '}
        {active
          ? `${active.lessonId === lessonId ? '' : active.lessonId + ' / '}${active.stepId}`
          : '—'}
        {scenario && active?.lessonId === lessonId && active.index !== undefined
          ? ` (${active.index + 1}/${active.count ?? scenario.steps.length})`
          : ''}
      </p>
      {check && (
        <ul>
          {check.conditions.map((c) => (
            <li key={c.id}>
              <code>{c.id}</code> {c.ok ? '✓' : c.unknown ? '?' : '✗'}
              {c.detail && <pre>{c.detail}</pre>}
            </li>
          ))}
        </ul>
      )}
      <p>
        <b>{t('author.exits')}</b> {matched.length ? matched.join(', ') : '—'}
      </p>
      <details>
        <summary>{t('author.state')}</summary>
        <pre>{JSON.stringify(state, null, 1)}</pre>
      </details>
    </div>
  )
}
