import { memo, useContext, useId, type ReactNode } from 'react'
import ReactMarkdown, { defaultUrlTransform, type Components } from 'react-markdown'
import remarkDirective from 'remark-directive'
import remarkGfm from 'remark-gfm'
import { useTranslation } from 'react-i18next'
import { APIPrefix } from '../../protocol.gen'
import { lessonPath } from '../../api/http'
import { useSessionStore } from '../../store/session'
import { Kbd } from '../common/Kbd'
import { remarkPlayground } from './directives'
import { StepsSlotContext } from './stepsSlot'
import { TryKeys } from './TryKeys'
import styles from './Lesson.module.css'

function Jargon({ term, children }: { term: string; children?: ReactNode }) {
  const glossary = useSessionStore((s) => s.tables.glossary)
  const id = useId()
  const key = term.toLowerCase()
  const def =
    glossary[key] ??
    Object.entries(glossary).find(([k]) => k.toLowerCase() === key)?.[1] ??
    glossary[key.replace(/s$/, '')]
  if (!def) return <>{children}</>
  return (
    <span className={styles.jargonWrap}>
      <button type="button" className={styles.jargon} aria-describedby={id}>
        {children}
      </button>
      <span role="tooltip" id={id} className={styles.tooltip}>
        {def}
      </span>
    </span>
  )
}

function VsCode({ title, children }: { title?: string; children?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <aside className={styles.vscode} aria-label={t('lesson.vscode')}>
      <b>{title || t('lesson.vscode')}</b>
      {children}
    </aside>
  )
}

function Aside({ title, children }: { title?: string; children?: ReactNode }) {
  return (
    <aside className={styles.aside}>
      {title && <b>{title}</b>}
      {children}
    </aside>
  )
}

function GuiOnly({ children }: { children?: ReactNode }) {
  const mode = useSessionStore((s) => s.mode)
  return mode === 'gui' ? <div className={styles.guiOnly}>{children}</div> : null
}

function StepsMarker() {
  return <>{useContext(StepsSlotContext)}</>
}

type PgProps = { keys?: string; term?: string; title?: string; children?: ReactNode }

const pgComponents = {
  'pg-kbd': ({ keys, children }: PgProps) => <Kbd keys={keys ?? String(children ?? '')} />,
  'pg-try': ({ keys }: PgProps) => <TryKeys keys={keys ?? ''} />,
  'pg-jargon': ({ term, children }: PgProps) => <Jargon term={term ?? ''}>{children}</Jargon>,
  'pg-vscode': ({ title, children }: PgProps) => (
    <VsCode {...(title ? { title } : {})}>{children}</VsCode>
  ),
  'pg-aside': ({ title, children }: PgProps) => (
    <Aside {...(title ? { title } : {})}>{children}</Aside>
  ),
  'pg-gui-only': ({ children }: PgProps) => <GuiOnly>{children}</GuiOnly>,
  'pg-steps': () => <StepsMarker />,
}

const baseComponents: Components = {
  a: ({ href, children, node: _node, ...rest }) => {
    const external = !!href && /^[a-z]+:/i.test(href)
    return (
      <a
        href={href}
        {...rest}
        {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}
      >
        {children}
      </a>
    )
  },
}

/** Lesson images are relative paths into assets/ (CURRICULUM.md Part B.2). */
function assetUrl(lessonId: string, url: string): string {
  if (/^([a-z]+:|\/|#)/i.test(url)) return url
  const rel = url.replace(/^\.\//, '').replace(/^assets\//, '')
  return `${APIPrefix}/curriculum/assets/${lessonPath(lessonId)}/${rel}`
}

export interface MarkdownProps {
  markdown: string
  /** Enables lesson directives (off for the preview pane). */
  directives?: boolean
  /** Resolves relative image paths against this lesson's assets. */
  lessonId?: string
  /** Renders inline content without a wrapping paragraph (step details). */
  inline?: boolean
  /** Base URL for relative links and images in preview Markdown. */
  baseUrl?: string
  className?: string
}

/**
 * Markdown through react-markdown with remark-gfm, remark-directive and the
 * playground plugin. Memoised on its props so the lesson is parsed once per
 * lesson, not on every store update (frontend.md §16).
 */
export const Markdown = memo(function Markdown({
  markdown,
  directives = true,
  lessonId,
  inline,
  baseUrl,
  className,
}: MarkdownProps) {
  const remarkPlugins = directives
    ? [remarkGfm, remarkDirective, [remarkPlayground, { directives: true }] as const]
    : [remarkGfm, [remarkPlayground, { directives: false }] as const]
  const components = {
    ...baseComponents,
    ...pgComponents,
    ...(inline ? { p: ({ children }: { children?: ReactNode }) => <>{children}</> } : {}),
  } as Components
  const md = (
    <ReactMarkdown
      remarkPlugins={remarkPlugins as never}
      components={components}
      urlTransform={(url, key) => {
        const safe = defaultUrlTransform(url)
        if (key === 'src' && lessonId) return assetUrl(lessonId, safe)
        if (baseUrl && (key === 'src' || key === 'href') && !/^([a-z]+:|\/|#)/i.test(safe)) {
          return baseUrl + safe
        }
        return safe
      }}
    >
      {markdown}
    </ReactMarkdown>
  )
  if (inline) return <span className={className}>{md}</span>
  return <div className={className}>{md}</div>
})
