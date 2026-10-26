import { useEffect } from 'react'
import { Link, useLocation } from 'react-router'
import { useTranslation } from 'react-i18next'
import { useCurriculum, useProgress } from '../api/queries'
import { buttonClass } from '../components/common/buttonClass'
import { CourseMap } from '../components/course/CourseMap'
import { currentLesson } from '../components/course/progress'
import { AppShell } from '../components/layout/AppShell'
import { TopBar } from '../components/layout/TopBar'
import { useSessionStore } from '../store/session'
import styles from './Screens.module.css'

/** / — the course map with progress (lazy-loaded; frontend.md §15). */
export default function CourseMapScreen() {
  const { t } = useTranslation()
  const curriculum = useCurriculum()
  const progress = useProgress()
  const active = useSessionStore((s) => s.activeStep)
  const location = useLocation()
  const current = curriculum.data
    ? currentLesson(curriculum.data, progress.data, active?.lessonId ?? null)
    : null

  useEffect(() => {
    document.title = `${t('course.crumb')} — Emacs Playground`
  }, [t])

  // Scroll to #part-N or #projects after the map renders.
  useEffect(() => {
    if (!curriculum.data || !location.hash) return
    document.getElementById(location.hash.slice(1))?.scrollIntoView()
  }, [curriculum.data, location.hash])

  return (
    <AppShell
      scroll
      topbar={
        <TopBar crumbs={[{ label: t('course.crumb') }]}>
          <Link className={buttonClass()} to="/session">
            {t('topbar.session')}
          </Link>
          {current && (
            <Link className={buttonClass('primary')} to={`/lesson/${current.id}`}>
              {progress.data?.lessons?.[current.id] ? t('course.continue') : t('course.start')}:{' '}
              {current.n} {current.title}
            </Link>
          )}
        </TopBar>
      }
    >
      {curriculum.isError ? (
        <main className={styles.message}>
          <h1>{t('course.loadError')}</h1>
          <p>{curriculum.error instanceof Error ? curriculum.error.message : ''}</p>
        </main>
      ) : !curriculum.data ? (
        <main className={styles.message} aria-busy="true">
          <p>{t('common.loading')}</p>
        </main>
      ) : (
        <CourseMap index={curriculum.data} progress={progress.data} current={current} />
      )}
    </AppShell>
  )
}
