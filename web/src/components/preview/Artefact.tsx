import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { baseName } from './kinds'
import styles from './Preview.module.css'

type ArtefactState = 'present' | 'missing'

/** HEAD /preview/<path>: whether the file exists (rechecked when its version changes). */
async function probeArtefact(url: string, version: number): Promise<ArtefactState> {
  const res = await fetch(`${url}${url.includes('?') ? '&' : '?'}v=${version}`, {
    method: 'HEAD',
    credentials: 'same-origin',
  })
  if (res.ok) return 'present'
  if (res.status === 404) return 'missing'
  throw new Error(`HTTP ${res.status}`)
}

/**
 * Shows the view only once the file exists (frontend.md §9): an artefact
 * that is not built yet (a PDF before the first compile, an export not run)
 * says so and what makes it, instead of a broken view. The file event that
 * creates it bumps the version, which checks again.
 */
export function ArtefactGate({
  url,
  version,
  path,
  source,
  children,
}: {
  url: string
  version: number
  path: string
  /** The source the artefact follows (preview.follow), if any. */
  source?: string | undefined
  children: ReactNode
}) {
  const { t } = useTranslation()
  const q = useQuery({
    queryKey: ['artefact', url, version],
    queryFn: () => probeArtefact(url, version),
    staleTime: Infinity,
    retry: 1,
    // A new version of the same file keeps the view mounted while it is
    // checked (the PDF keeps its scale and scroll).
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[1] === url ? prev : undefined),
  })
  // Errors other than 404 fall through to the view, which reports its own.
  if (q.data === 'missing') {
    return (
      <div className={styles.missing} role="status">
        <p>{t('preview.missing', { name: baseName(path) })}</p>
        <p className={styles.dim}>
          {source
            ? t('preview.missingFollow', { source: baseName(source) })
            : t('preview.missingBody')}
        </p>
      </div>
    )
  }
  if (q.isPending) return <p className={styles.dim}>{t('common.loading')}</p>
  return <>{children}</>
}
