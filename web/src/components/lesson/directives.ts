/**
 * The lesson Markdown dialect (CURRICULUM.md Part B.2): a remark plugin that
 * runs after remark-directive and maps directives to custom hast elements,
 * which Markdown.tsx renders as components.
 *
 *   `C-x C-f`            → pg-kbd   (backticked text that parses as keys)
 *   :kbd[C-x C-f]        → pg-kbd
 *   :try[C-x C-f]        → pg-try   (keycap with a "try it" button)
 *   :jargon[buffer]      → pg-jargon (glossary tooltip)
 *   :::vscode … :::      → pg-vscode
 *   :::aside{title="…"}  → pg-aside
 *   :::gui-only … :::    → pg-gui-only (shown only in the GUI frame)
 *   ::steps              → pg-steps (where the step list renders)
 *
 * Unknown text and leaf directives are turned back into their source text,
 * so prose like "Keys:none" survives; unknown containers render their
 * content.
 */
import { visit } from 'unist-util-visit'
import { isKeySequence } from '../../advice/keys'

interface MdNode {
  type: string
  name?: string
  value?: string
  attributes?: Record<string, string | null | undefined> | null
  children?: MdNode[]
  data?: {
    hName?: string
    hProperties?: Record<string, unknown>
    hChildren?: unknown[]
    directiveLabel?: boolean
  }
}

export const PG_ELEMENTS = [
  'pg-kbd',
  'pg-try',
  'pg-jargon',
  'pg-vscode',
  'pg-aside',
  'pg-gui-only',
  'pg-steps',
] as const

/** Plain text of a node's descendants. */
export function textOf(n: MdNode): string {
  if (typeof n.value === 'string') return n.value
  return (n.children ?? []).map(textOf).join('')
}

function setElement(n: MdNode, hName: string, props: Record<string, unknown> = {}) {
  n.data = { ...(n.data ?? {}), hName, hProperties: props }
}

/** Source text for an unknown text or leaf directive. */
function restore(n: MdNode): MdNode {
  const label = (n.children ?? []).length ? `[${textOf(n)}]` : ''
  const prefix = n.type === 'leafDirective' ? '::' : ':'
  return { type: 'text', value: `${prefix}${n.name ?? ''}${label}` }
}

export interface DirectiveOptions {
  /** When false only the keycap rule for inline code applies (preview Markdown). */
  directives?: boolean
}

export function remarkPlayground(options: DirectiveOptions = {}) {
  const directives = options.directives !== false
  return (tree: MdNode) => {
    visit(tree as never, (node: MdNode, index: number | undefined, parent: MdNode | undefined) => {
      if (node.type === 'inlineCode') {
        const v = node.value ?? ''
        if (isKeySequence(v)) {
          node.data = {
            hName: 'pg-kbd',
            hProperties: { keys: v },
            hChildren: [{ type: 'text', value: v }],
          }
        }
        return
      }
      if (!directives) return
      if (node.type === 'textDirective') {
        const text = textOf(node).trim()
        switch (node.name) {
          case 'kbd':
            setElement(node, 'pg-kbd', { keys: text })
            return
          case 'try':
            setElement(node, 'pg-try', { keys: text })
            return
          case 'jargon':
            setElement(node, 'pg-jargon', { term: node.attributes?.['term'] ?? text })
            return
        }
        if (parent?.children && index !== undefined) parent.children[index] = restore(node)
        return
      }
      if (node.type === 'leafDirective') {
        if (node.name === 'steps') {
          setElement(node, 'pg-steps')
          node.children = []
          return
        }
        if (parent?.children && index !== undefined) {
          parent.children[index] = { type: 'paragraph', children: [restore(node)] }
        }
        return
      }
      if (node.type === 'containerDirective') {
        // remark-directive puts a [label] in a first paragraph marked directiveLabel.
        let label: string | undefined
        const first = node.children?.[0]
        if (first?.data?.directiveLabel) {
          label = textOf(first)
          node.children = node.children?.slice(1) ?? []
        }
        switch (node.name) {
          case 'vscode':
            setElement(node, 'pg-vscode', label ? { title: label } : {})
            return
          case 'aside':
            setElement(node, 'pg-aside', { title: node.attributes?.['title'] ?? label ?? '' })
            return
          case 'gui-only':
            setElement(node, 'pg-gui-only')
            return
          default:
            setElement(node, 'div', { className: ['directive-' + (node.name ?? 'unknown')] })
        }
      }
    })
  }
}

/** True when the lesson places the step list itself with ::steps. */
export function hasStepsMarker(markdown: string): boolean {
  return /^::steps\s*$/m.test(markdown)
}
