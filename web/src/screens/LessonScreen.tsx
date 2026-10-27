import { useEffect } from 'react'
import { Link, Navigate, useLocation, useParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/http'
import { useCurriculum, useEnsureSession, useLesson, useMe } from '../api/queries'
import { AppShell } from '../components/layout/AppShell'
import { Rail, RailSection } from '../components/layout/Rail'
import { ScreenActions } from '../components/layout/ScreenActions'
import { TopBar, type Crumb } from '../components/layout/TopBar'
import { Workspace } from '../components/layout/Workspace'
import { LessonPane } from '../components/lesson/LessonPane'
import { useLessonMentions, useLessonSteps, useScenarioLoad } from '../components/lesson/useLesson'
import { AuthorPanel } from '../components/live/AuthorPanel'
import { CheatSheet } from '../components/live/CheatSheet'
import { Nudges } from '../components/live/Nudges'
import { RescueCard } from '../components/live/RescueCard'
import { WhereAmI } from '../components/live/WhereAmI'
import { PreviewPane } from '../components/preview/PreviewPane'
import { Stage } from '../components/screen/Stage'
import { findLesson, nextLesson } from '../components/course/progress'
import { useSessionStore } from '../store/session'
import { useUiStore } from '../store/ui'
import lessonStyles from '../components/lesson/Lesson.module.css'

/** /lesson/:lessonId — the lesson id contains a slash, so the route is a trailing wildcard. */
export default function LessonScreen() {
  const params = useParams()
  const location = useLocation()
  const raw = params['*'] ?? ''
  // Lesson ids never end in a slash: /lesson/<id>/ is /lesson/<id>.
  const lessonId = raw.replace(/\/+$/, '')
  if (lessonId !== raw)
    return <Navigate to={`/lesson/${lessonId}${location.search}${location.hash}`} replace />
  return <LessonView key={lessonId} lessonId={lessonId} />
}

function LessonView({ lessonId }: { lessonId: string }) {
  const { t } = useTranslation()
  useEnsureSession()
  const me = useMe()
  const lesson = useLesson(lessonId)
  const curriculum = useCurriculum()
  const scenario = lesson.data?.scenario
  const steps = useLessonSteps(lessonId, scenario)
  const load = useScenarioLoad(lessonId, scenario)
  useLessonMentions(lessonId, lesson.data?.markdown, scenario)
  const found = findLesson(curriculum.data, lessonId)
  const next = nextLesson(curriculum.data, lessonId)
  const modeName = useSessionStore((s) => s.state?.modeName)
  const previewHidden = useUiStore((s) => s.previewHidden)
  const title = lesson.data
    ? lesson.data.front?.title || lesson.data.title
    : (found?.lesson.title ?? lessonId)

  useEffect(() => {
    document.title = `${found?.lesson.n ? found.lesson.n + ' ' : ''}${title} — Emacs Playground`
  }, [found?.lesson.n, title])

  const crumbs: Crumb[] = [{ label: t('topbar.course'), to: '/' }]
  if (found)
    crumbs.push({ label: `${found.part.n}. ${found.part.title}`, to: `/#part-${found.part.n}` })
  crumbs.push({ label: `${found?.lesson.n ?? ''} ${title}`.trim() })

  let left
  if (lesson.isError) {
    const notFound = lesson.error instanceof ApiError && lesson.error.status === 404
    // A 404 for a lesson the course map lists as planned: not written yet.
    const planned = notFound && found?.lesson.status === 'planned'
    left = (
      <aside className={lessonStyles.lesson}>
        <h1>
          {planned ? t('lesson.notFound') : notFound ? t('lesson.noSuchLesson') : t('lesson.error')}
        </h1>
        <p>
          {planned
            ? t('lesson.notFoundBody')
            : notFound
              ? t('lesson.noSuchLessonBody')
              : lesson.error instanceof Error
                ? lesson.error.message
                : ''}
        </p>
        <p>
          <Link to="/">{t('lesson.backToMap')}</Link>
        </p>
      </aside>
    )
  } else if (!lesson.data) {
    left = (
      <aside className={lessonStyles.lesson} aria-busy="true">
        <p className={lessonStyles.loading}>{t('common.loading')}</p>
      </aside>
    )
  } else {
    left = <LessonPane bundle={lesson.data} steps={steps} load={load} next={next} />
  }

  const hasPreview = !!scenario?.preview?.tabs?.length || !!scenario?.preview?.follow
  const live = (
    <>
      <RailSection title={t('rail.where')}>
        <WhereAmI />
      </RailSection>
      {/* Rescue before "Did you mean": cards never push it below the fold. */}
      <RailSection title={t('rail.rescue')}>
        <RescueCard />
      </RailSection>
      <RailSection title={t('rail.nudges')}>
        <Nudges />
      </RailSection>
      <RailSection title={t('cheat.title')} small={modeName}>
        <CheatSheet heading={false} />
      </RailSection>
      {me.data?.features?.authorMode && (
        <RailSection title={t('author.title')}>
          <AuthorPanel lessonId={lessonId} scenario={scenario} />
        </RailSection>
      )}
    </>
  )
  return (
    <AppShell
      topbar={
        <TopBar crumbs={crumbs}>
          <ScreenActions preview={hasPreview} />
        </TopBar>
      }
    >
      <Workspace
        left={left}
        center={<Stage />}
        right={
          <Rail
            top={
              hasPreview && !previewHidden ? (
                <PreviewPane owner={lessonId} spec={scenario?.preview} />
              ) : undefined
            }
          >
            {live}
          </Rail>
        }
      />
    </AppShell>
  )
}
