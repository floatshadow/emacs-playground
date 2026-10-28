import { useEffect } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../api/http'
import { useCurriculum, useEnsureSession, useLesson } from '../api/queries'
import { AppShell } from '../components/layout/AppShell'
import { Rail, RailSection } from '../components/layout/Rail'
import { ScreenActions } from '../components/layout/ScreenActions'
import { TopBar, type Crumb } from '../components/layout/TopBar'
import { Workspace } from '../components/layout/Workspace'
import { LessonNotices } from '../components/lesson/LessonNotices'
import { useLessonMentions, useLessonSteps, useScenarioLoad } from '../components/lesson/useLesson'
import { CheatSheet } from '../components/live/CheatSheet'
import { Nudges } from '../components/live/Nudges'
import { RescueCard } from '../components/live/RescueCard'
import { WhereAmI } from '../components/live/WhereAmI'
import { PreviewPane } from '../components/preview/PreviewPane'
import { Checkpoints } from '../components/project/Checkpoints'
import { FileTree } from '../components/project/FileTree'
import { TaskPanel } from '../components/project/TaskPanel'
import { Stage } from '../components/screen/Stage'
import { LESSON_PANE_ID } from '../components/screen/paneIds'
import { useSessionStore } from '../store/session'
import { useUiStore } from '../store/ui'
import projectStyles from '../components/project/Project.module.css'

/** /project/:projectId/:taskId? — file tree, tasks, checkpoints, the screen and the preview. */
export default function ProjectScreen() {
  const { projectId = '', taskId } = useParams()
  return <ProjectView key={projectId} projectId={projectId} taskId={taskId} />
}

function ProjectView({ projectId, taskId }: { projectId: string; taskId: string | undefined }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  useEnsureSession()
  const lessonId = `projects/${projectId}`
  const bundle = useLesson(lessonId)
  const curriculum = useCurriculum()
  const scenario = bundle.data?.scenario
  const steps = useLessonSteps(lessonId, scenario)
  const load = useScenarioLoad(lessonId, scenario, taskId)
  useLessonMentions(lessonId, bundle.data?.markdown, scenario)
  const active = useSessionStore((s) => s.activeStep)
  const previewHidden = useUiStore((s) => s.previewHidden)
  const modeName = useSessionStore((s) => s.state?.modeName)
  const project = curriculum.data?.projects.find((p) => p.id === lessonId)
  const title = project ? `${project.n} ${project.title}` : (bundle.data?.title ?? projectId)

  // Keep the URL on the active task as the project advances.
  useEffect(() => {
    if (active?.lessonId === lessonId && taskId && active.stepId !== taskId && !load.loading) {
      void navigate(`/project/${projectId}/${active.stepId}`, { replace: true })
    }
  }, [active, lessonId, taskId, projectId, navigate, load.loading])

  useEffect(() => {
    document.title = `${title} — Emacs Playground`
  }, [title])

  const activeView = steps.activeIndex >= 0 ? steps.views[steps.activeIndex] : undefined
  const crumbs: Crumb[] = [{ label: t('topbar.projects'), to: '/#projects' }, { label: title }]
  if (activeView) {
    crumbs.push({
      label: t('project.crumbTask', {
        n: steps.activeIndex + 1,
        total: steps.views.length,
        goal: activeView.goal,
      }),
    })
  }

  let left
  if (bundle.isError) {
    const notFound = bundle.error instanceof ApiError && bundle.error.status === 404
    // A 404 for a project the course map lists as planned: not written yet.
    const planned = notFound && project?.status === 'planned'
    left = (
      <aside className={projectStyles.side}>
        <h1>
          {planned
            ? t('project.notFound')
            : notFound
              ? t('project.noSuchProject')
              : t('lesson.error')}
        </h1>
        <p>
          {planned
            ? t('project.notFoundBody')
            : notFound
              ? t('project.noSuchProjectBody')
              : bundle.error instanceof Error
                ? bundle.error.message
                : ''}
        </p>
        <Link to="/">{t('lesson.backToMap')}</Link>
      </aside>
    )
  } else if (!bundle.data) {
    left = (
      <aside className={projectStyles.side} aria-busy="true">
        <p>{t('common.loading')}</p>
      </aside>
    )
  } else {
    const roots = scenario?.project?.roots?.length
      ? scenario.project.roots
      : [scenario?.setup?.root || projectId]
    const generated = Object.values(scenario?.preview?.follow ?? {})
    const taskName = (stepId: string) => {
      const i = steps.views.findIndex((v) => v.step.id === stepId)
      const v = steps.views[i]
      return v ? t('checkpoints.taskNamed', { n: i + 1, goal: v.goal }) : undefined
    }
    left = (
      <aside
        className={projectStyles.side}
        id={LESSON_PANE_ID}
        tabIndex={-1}
        aria-label={t('project.sideLabel')}
      >
        <LessonNotices scenario={scenario} />
        <section>
          <h2>
            {t('project.files')} <small>{t('project.filesNote')}</small>
          </h2>
          {roots.map((root) => (
            <FileTree key={root} root={root} owner={lessonId} generated={generated} />
          ))}
        </section>
        <section>
          <TaskPanel bundle={bundle.data} steps={steps} load={load} />
        </section>
        <section>
          <h2>
            {t('project.checkpoints')} <small>{t('project.checkpointsNote')}</small>
          </h2>
          <Checkpoints lessonId={lessonId} taskName={taskName} />
        </section>
      </aside>
    )
  }

  return (
    <AppShell
      topbar={
        <TopBar crumbs={crumbs}>
          <ScreenActions preview />
        </TopBar>
      }
    >
      <Workspace
        left={left}
        center={<Stage />}
        right={
          <Rail
            top={
              previewHidden ? undefined : <PreviewPane owner={lessonId} spec={scenario?.preview} />
            }
          >
            <RailSection title={t('rail.where')}>
              <WhereAmI />
            </RailSection>
            <RailSection title={t('rail.rescue')}>
              <RescueCard />
            </RailSection>
            <RailSection title={t('rail.nudges')}>
              <Nudges />
            </RailSection>
            {/* Projects open Magit, Eshell, compilation and more: their keys. */}
            <RailSection title={t('cheat.title')} small={modeName}>
              <CheatSheet heading={false} />
            </RailSection>
          </Rail>
        }
      />
    </AppShell>
  )
}
