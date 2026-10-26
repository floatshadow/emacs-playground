import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import type { LessonRef, Part } from '../../protocol.gen'
import type { LessonState } from './progress'
import styles from './Course.module.css'

const TAG_CLASS: Record<string, string | undefined> = {
  gui: styles.gui,
  remote: styles.remote,
  project: styles.proj,
}

export function LessonRow({
  part,
  lesson,
  state,
}: {
  part: Part
  lesson: LessonRef
  state: LessonState
}) {
  const { t } = useTranslation()
  const title = (
    <>
      <span className={styles.n}>{lesson.n}</span>
      {lesson.title}
      {(lesson.tags ?? []).map((tag) => (
        <span key={tag} className={`${styles.tag} ${TAG_CLASS[tag] ?? ''}`}>
          {tag}
        </span>
      ))}
      {lesson.vscode && lesson.vscode !== 'none' && (
        <span className={styles.vs}>{t('course.inVsCode', { what: lesson.vscode })}</span>
      )}
    </>
  )
  // The part heading already says "suggested after part N"; repeating it on
  // every row is noise, so suggested rows read "not started" (dimmed) and
  // carry the suggestion in their tooltip only.
  const label = t(`course.state.${state === 'suggested' ? 'not-started' : state}`)
  const hint =
    state === 'suggested' ? t('course.state.suggested', { n: part.suggestedAfter }) : undefined
  return (
    <li className={state === 'current' ? styles.current : undefined}>
      {lesson.status === 'ready' ? (
        <Link
          to={`/lesson/${lesson.id}`}
          className={styles.lessonLink}
          aria-current={state === 'current' ? 'step' : undefined}
        >
          {title}
        </Link>
      ) : (
        <span className={styles.planned}>{title}</span>
      )}
      <span className={`${styles.state} ${styles[state] ?? ''}`} title={hint}>
        {label}
        {lesson.minutes > 0 && state !== 'done' && state !== 'planned' && (
          <span className={styles.minutes}> · {t('course.minutes', { n: lesson.minutes })}</span>
        )}
      </span>
    </li>
  )
}
