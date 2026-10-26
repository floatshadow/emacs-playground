import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { request } from '../../api/http'
import { Markdown } from '../lesson/Markdown'
import styles from './Preview.module.css'

/** Fetches a workspace file as text, refetching when its version changes. */
function useFileText(url: string, version: number) {
  return useQuery({
    queryKey: ['preview-text', url, version],
    queryFn: ({ signal }) =>
      request<string>(`${url}${url.includes('?') ? '&' : '?'}v=${version}`, { text: true, signal }),
    staleTime: Infinity,
    placeholderData: (prev) => prev,
  })
}

/**
 * Markdown preview: the browser renders the raw file with the lesson
 * pipeline minus directives (frontend.md §9, architecture.md §3.6). Relative
 * links and images resolve against the file's directory in the workspace.
 */
export function MarkdownView({ url, version }: { url: string; version: number }) {
  const { t } = useTranslation()
  const q = useFileText(url, version)
  const dir = url.replace(/[^/]*$/, '')
  if (q.isError) return <p className={styles.error}>{t('preview.loadError')}</p>
  if (q.data === undefined) return <p className={styles.dim}>{t('common.loading')}</p>
  return (
    <div className={`${styles.page} ${styles.md}`}>
      <Markdown markdown={q.data} directives={false} baseUrl={dir} />
    </div>
  )
}

/** Plain text files (source code opened from the tree). */
export function TextView({ url, version }: { url: string; version: number }) {
  const { t } = useTranslation()
  const q = useFileText(url, version)
  if (q.isError) return <p className={styles.error}>{t('preview.loadError')}</p>
  if (q.data === undefined) return <p className={styles.dim}>{t('common.loading')}</p>
  return <pre className={styles.text}>{q.data}</pre>
}
