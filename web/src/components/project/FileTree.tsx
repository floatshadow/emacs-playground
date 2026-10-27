import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { PreviewListing } from '../../protocol.gen'
import { request, workspacePath } from '../../api/http'
import { rpc } from '../../api/session/rpc'
import { sessionBase } from '../../api/session/rpc'
import { useSessionStore } from '../../store/session'
import { useUiStore } from '../../store/ui'
import { focusTerminal } from '../screen/focus'
import { isPreviewable, workspaceRelative } from '../preview/kinds'
import styles from './Project.module.css'

const HIDDEN = new Set(['.git', 'target', '_build', '.venv', 'node_modules', '.playground'])

/** Visits a workspace file in Emacs (rpc open; eval is author-mode only). */
async function openInEmacs(path: string): Promise<void> {
  try {
    await rpc('open', { path })
  } catch {
    // toasted
  }
  focusTerminal()
}

function useListing(dir: string) {
  const sessionId = useSessionStore((s) => s.sessionId)
  const v = useSessionStore((s) => s.treeVersion)
  return useQuery({
    queryKey: ['listing', sessionId, dir, v],
    queryFn: ({ signal }) =>
      request<PreviewListing>(`${sessionBase(sessionId ?? '')}/preview/${workspacePath(dir)}/`, {
        signal,
      }),
    enabled: !!sessionId,
    placeholderData: (prev) => prev,
    staleTime: 30_000,
  })
}

function Dir({
  path,
  name,
  owner,
  generated,
  depth,
}: {
  path: string
  name: string
  owner: string
  generated: ReadonlySet<string>
  depth: number
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(depth < 2)
  const q = useListing(path)
  const current = useSessionStore((s) => workspaceRelative(s.state?.file))
  const modified = useSessionStore((s) => !!s.state?.modified)
  const addTab = useUiStore((s) => s.addPreviewTab)
  const entries = (q.data?.entries ?? [])
    .filter((e) => !HIDDEN.has(e.name) && !e.name.startsWith('.'))
    .sort((a, b) => (a.dir === b.dir ? a.name.localeCompare(b.name) : a.dir ? -1 : 1))
  return (
    <li>
      <button
        type="button"
        className={styles.dir}
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        {name}/
      </button>
      {open && (
        <ul role="group">
          {q.isError && <li className={styles.treeNote}>{t('tree.error')}</li>}
          {entries.map((e) => {
            const p = `${path}/${e.name}`
            if (e.dir) {
              return (
                <Dir
                  key={p}
                  path={p}
                  name={e.name}
                  owner={owner}
                  generated={generated}
                  depth={depth + 1}
                />
              )
            }
            const on = current === p
            const cls = [
              styles.f,
              on ? styles.on : '',
              on && modified ? styles.mod : '',
              generated.has(p) ? styles.gen : '',
            ]
              .filter(Boolean)
              .join(' ')
            return (
              <li key={p} className={styles.fileRow}>
                <button
                  type="button"
                  className={cls}
                  onClick={() => void openInEmacs(p)}
                  title={t('tree.open', { path: p })}
                  aria-current={on ? 'true' : undefined}
                >
                  {e.name}
                  {on && modified && <span className="sr-only"> {t('tree.modified')}</span>}
                </button>
                {isPreviewable(p) && (
                  <button
                    type="button"
                    className={styles.previewBtn}
                    onClick={() => addTab(owner, p)}
                    aria-label={t('tree.preview', { name: e.name })}
                    title={t('tree.preview', { name: e.name })}
                  >
                    ◧
                  </button>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </li>
  )
}

/**
 * The file tree (frontend.md §10) from GET /preview/<root>/ listings. A click
 * opens the file in Emacs; previewable artefacts also get a preview button.
 */
export function FileTree({
  root,
  owner,
  generated,
}: {
  root: string
  owner: string
  generated?: string[]
}) {
  const { t } = useTranslation()
  const sessionId = useSessionStore((s) => s.sessionId)
  if (!sessionId) return <p className={styles.treeNote}>{t('tree.waiting')}</p>
  return (
    <ul className={styles.tree} aria-label={t('tree.label')}>
      <Dir path={root} name={root} owner={owner} generated={new Set(generated ?? [])} depth={0} />
    </ul>
  )
}
