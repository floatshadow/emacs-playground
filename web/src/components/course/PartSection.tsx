import { useTranslation } from 'react-i18next'
import type { CurriculumIndex, LessonRef, Part, Progress } from '../../protocol.gen'
import { minutes } from '../../lib/time'
import { KeyText } from '../common/Kbd'
import { LessonRow } from './LessonRow'
import { lessonState, partDoneFraction } from './progress'
import styles from './Course.module.css'

export function PartSection({
  part,
  index,
  progress,
  current,
}: {
  part: Part
  index: CurriculumIndex
  progress: Progress | undefined
  current: LessonRef | null
}) {
  const { t } = useTranslation()
  const isProjects =
    part.lessons.length === 0 || part.lessons.every((l) => l.id.startsWith('projects/'))
  const done = partDoneFraction(part, progress)
  const count = isProjects ? index.projects.length : part.lessons.length
  return (
    <section className={styles.part} aria-labelledby={`part-${part.n}`}>
      <div>
        <h2 id={`part-${part.n}`}>
          {part.n}. {part.title}
        </h2>
        <div className={styles.meta}>
          {minutes(part.minutes)} ·{' '}
          {t(isProjects ? 'course.projectsCount' : 'course.lessonsCount', { count })}
          {part.status === 'planned' && (
            <span className={styles.tag}>{t('course.plannedTag')}</span>
          )}
        </div>
        <div className={styles.outcome}>
          <KeyText text={part.outcome} size="sm" />
        </div>
        <div
          className={styles.progress}
          role="progressbar"
          aria-label={t('course.partProgress', { n: part.n })}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(done * 100)}
        >
          <i style={{ width: `${Math.round(done * 100)}%` }} />
        </div>
        {part.suggestedAfter !== undefined && part.n > 0 && (
          <div className={styles.suggested}>
            {t('course.suggestedAfter', { n: part.suggestedAfter })}
          </div>
        )}
      </div>
      <div>
        {isProjects ? (
          <p className={styles.projectsNote}>{t('course.projectsBelow')}</p>
        ) : (
          <ul className={styles.lessons}>
            {part.lessons.map((l) => (
              <LessonRow
                key={l.id}
                part={part}
                lesson={l}
                state={lessonState(part, l, index, progress, current)}
              />
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}
