import { useTranslation } from 'react-i18next'
import type { CurriculumIndex, LessonRef, Progress } from '../../protocol.gen'
import { PartSection } from './PartSection'
import { ProjectCard } from './ProjectCard'
import styles from './Course.module.css'

/** The course map (frontend.md §10): parts with progress, then projects. Order is suggested, not enforced. */
export function CourseMap({
  index,
  progress,
  current,
}: {
  index: CurriculumIndex
  progress: Progress | undefined
  current: LessonRef | null
}) {
  const { t } = useTranslation()
  return (
    <main className={styles.map}>
      <header>
        <h1>{t('course.title')}</h1>
        <p>{t('course.intro')}</p>
        <div className={styles.legend}>
          <span>
            <span className={styles.tag}>terminal</span> {t('course.legend.terminal')}
          </span>
          <span>
            <span className={`${styles.tag} ${styles.gui}`}>gui</span> {t('course.legend.gui')}
          </span>
          <span>
            <span className={`${styles.tag} ${styles.remote}`}>remote</span>{' '}
            {t('course.legend.remote')}
          </span>
          <span>
            <span className={`${styles.tag} ${styles.proj}`}>project</span>{' '}
            {t('course.legend.project')}
          </span>
        </div>
      </header>
      {index.parts.map((p) => (
        <PartSection key={p.id} part={p} index={index} progress={progress} current={current} />
      ))}
      <h2 className={styles.projectsHeading}>{t('course.projects')}</h2>
      <p className={styles.projectsIntro}>{t('course.projectsIntro')}</p>
      <div className={styles.projects}>
        {index.projects.map((p) => (
          <ProjectCard key={p.id} project={p} progress={progress} />
        ))}
      </div>
    </main>
  )
}
