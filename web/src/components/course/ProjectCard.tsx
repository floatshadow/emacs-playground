import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import type { Progress, ProjectRef } from '../../protocol.gen'
import { formatLap, minutes } from '../../lib/time'
import styles from './Course.module.css'

function projectDir(id: string): string {
  return id.replace(/^projects\//, '')
}

export function ProjectCard({
  project,
  progress,
}: {
  project: ProjectRef
  progress: Progress | undefined
}) {
  const { t } = useTranslation()
  const lp = progress?.lessons?.[project.id]
  const doneTasks = Object.values(lp?.steps ?? {}).filter((s) => s.status === 'done').length
  const heading = (
    <>
      {project.n} {project.title}
    </>
  )
  return (
    <div className={styles.project}>
      <h3>
        {project.status === 'ready' ? (
          <Link to={`/project/${projectDir(project.id)}`}>{heading}</Link>
        ) : (
          heading
        )}
        {project.status === 'planned' && (
          <span className={styles.tag}>{t('course.plannedTag')}</span>
        )}
      </h3>
      <p>
        {t('project.cardTasks', { count: project.tasks.length })} · {minutes(project.minutes)}
        {project.vscode ? ` · ${t('course.inVsCode', { what: project.vscode })}` : ''}
      </p>
      {project.tasks.length > 0 && (
        <ol className={styles.taskList}>
          {project.tasks.slice(0, 8).map((task) => (
            <li key={task.id}>{task.title}</li>
          ))}
        </ol>
      )}
      {doneTasks > 0 && (
        <p className={styles.projectProgress}>
          {t('project.doneTasks', { done: doneTasks, total: project.tasks.length })}
          {lp?.laps &&
            lp.laps.count > 0 &&
            ` · ${t('project.bestLap', { time: formatLap(lp.laps.bestMs) })}`}
        </p>
      )}
      <div className={styles.langs}>{project.languages.join(' · ')}</div>
    </div>
  )
}
