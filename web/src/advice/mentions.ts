/**
 * Nudge suppression (fix plan contract d, frontend.md §6): a "Did you
 * mean" card must not fire for a key the lesson just told the learner to
 * press. The mentioned keys of a lesson are the key sequences in its
 * Markdown (backticked spans that parse as keys, :kbd[…] and :try[…]) and,
 * per step, in the goal, the detail and every condition hint, normalised.
 */
import type { Scenario } from '../protocol.gen'
import { isKeySequence, KEY_RE, normalizeKeysLoose } from './keys'

/** The mentioned keys of one lesson: lesson-wide and per step. */
export interface Mentions {
  lessonId: string
  /** From the lesson's Markdown. */
  lesson: string[]
  /** From each step's goal, detail and condition hints, by step id. */
  steps: Record<string, string[]>
}

const BACKTICK = /`([^`\n]+)`/g
const DIRECTIVE = /:(?:kbd|try)\[([^\]\n]+)\]/g

function add(out: Set<string>, seq: string) {
  const n = normalizeKeysLoose(seq.trim())
  if (n) out.add(n)
}

/** Key sequences in lesson Markdown: backticked keys, :kbd[…] and :try[…]. */
export function markdownKeys(markdown: string): string[] {
  const out = new Set<string>()
  for (const m of markdown.matchAll(BACKTICK)) {
    const v = m[1] ?? ''
    if (isKeySequence(v)) add(out, v)
  }
  for (const m of markdown.matchAll(DIRECTIVE)) add(out, m[1] ?? '')
  return [...out]
}

/**
 * Key sequences in step text (goal, detail, hints), which are short prose
 * with keys written plainly ("Press C-x, then C-b.") or backticked.
 */
export function proseKeys(text: string): string[] {
  const out = new Set(markdownKeys(text))
  const plain = text.replace(BACKTICK, ' $1 ')
  for (const m of plain.matchAll(new RegExp(KEY_RE.source, 'g'))) {
    const v = m[1] ?? ''
    // A lone q in prose is usually the word, not the key.
    if (v && v !== 'q') add(out, v)
  }
  return [...out]
}

const str = (v: unknown): string =>
  typeof v === 'string'
    ? v
    : v && typeof v === 'object'
      ? Object.values(v as Record<string, unknown>)
          .filter((x): x is string => typeof x === 'string')
          .join('\n')
      : ''

/** The mentioned keys of a lesson from its Markdown and scenario. */
export function lessonMentions(
  lessonId: string,
  markdown: string,
  scenario: Scenario | undefined,
): Mentions {
  const steps: Record<string, string[]> = {}
  for (const st of scenario?.steps ?? []) {
    const texts = [str(st.goal), str(st.detail), ...st.conditions.map((c) => str(c.hint))]
    steps[st.id] = proseKeys(texts.join('\n'))
  }
  return { lessonId, lesson: markdownKeys(markdown), steps }
}

/** The keys mentioned where the learner is: the lesson plus the active step. */
export function mentionedNow(
  m: Mentions | null,
  active: { lessonId: string; stepId: string } | null,
): string[] {
  if (!m) return []
  if (!active || active.lessonId !== m.lessonId) return m.lesson
  return [...m.lesson, ...(m.steps[active.stepId] ?? [])]
}

/** True when some mentioned sequence starts with key (C-s in "C-s C-s", not in "C-x C-s"). */
export function isMentioned(key: string, mentioned: readonly string[]): boolean {
  const k = normalizeKeysLoose(key)
  return mentioned.some((m) => m === k || m.startsWith(k + ' '))
}
