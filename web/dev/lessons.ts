/**
 * Lesson bundles served by the fake session. Three lessons and project P6
 * are written out (2.3 is the design demo's lesson); every other ready
 * lesson in the index gets a small generic bundle so all links work.
 * The real lessons live in curriculum/ (CURRICULUM.md Part B).
 */
import type { LessonBundle, LessonRef, Scenario } from '../src/protocol.gen.ts'

function assign(s: Scenario): Scenario {
  for (const st of s.steps) {
    st.conditions.forEach((c, i) => {
      c.id ??= `${st.id}/${i + 1}`
    })
    st.stuck?.forEach((r, i) => {
      r.id ??= `${st.id}/stuck/${i + 1}`
    })
  }
  return s
}

const OPENING_FILES: LessonBundle = {
  id: '02-buffers-and-files/03-opening-files',
  title: 'Opening files',
  locale: 'en',
  front: {
    title: 'Opening files',
    summary:
      'Emacs asks for a path in the minibuffer, the one-line area at the bottom of the screen. Once you trust that line, most of Emacs stops being mysterious.',
    minutes: 12,
  },
  assets: [],
  markdown: `:::vscode
Ctrl+P and the Explorer both become \`C-x C-f\`. Files you have opened are :jargon[buffers], and \`C-x b\` switches between them the way Ctrl+Tab does. There is no tab bar by default.
:::

Press :try[C-x C-f]. Emacs shows *Find file:* with the current directory already typed. Type the first letters of a name and press \`TAB\` to complete it. \`RET\` opens the file.

If the file does not exist, Emacs opens an empty buffer and says *(New file)*. Nothing is written to disk until you save with \`C-x C-s\`.

When something looks wrong, \`C-g\` cancels. It is always safe. The panel on the right tells you which state you are in and how to leave it.

::steps

:::aside{title="Why a minibuffer?"}
Every command that needs an answer asks in the same place, with the same keys: \`TAB\` completes, \`M-p\` recalls history, \`C-g\` cancels. Learn it once.
:::
`,
  scenario: assign({
    id: '02-buffers-and-files/03-opening-files',
    title: 'Opening files',
    minutes: 12,
    mode: 'terminal',
    tags: [],
    modules: ['base', 'agent', 'editing', 'files'],
    vscode: 'Ctrl+P, Ctrl+O',
    setup: {
      workspace: 'fixtures/hello-rust',
      root: 'hello',
      open: ['hello/README.md'],
      layout: 'single',
    },
    steps: [
      {
        id: 'open',
        goal: 'Open src/main.rs',
        detail: 'Hold Control, press x, then Control f. Type `src/ma`, press `TAB`, then `RET`.',
        demo: 'C-x C-f ~400 "src/ma" ~300 TAB ~400 RET',
        conditions: [
          {
            text: 'the Find file prompt was used',
            event: { command: 'find-file', min: 1 },
            hint: 'C-x then C-f. The prompt appears in the bottom line. If you see C-x- in the bottom line, Emacs is waiting for the second key.',
          },
          {
            text: 'main.rs is the current buffer',
            elisp: '(equal (buffer-file-name) "/work/hello/src/main.rs")',
            hint: 'In the prompt, type src/ma and press TAB; it completes to main.rs. Then RET.',
          },
        ],
        stuck: [
          {
            when: { minibuffer_seconds: 30 },
            say: 'Still in the prompt? RET accepts, C-g cancels.',
          },
        ],
        checkpoint: true,
      },
      {
        id: 'edit',
        goal: 'Change the greeting',
        detail:
          'Turn *Hello* into *Hi*. `C-s` finds text as you type. There is no insert mode: typing inserts.',
        demo: 'M-< C-s "Hello" ~300 RET DEL DEL DEL DEL DEL "Hi"',
        conditions: [
          {
            text: 'the cursor is on the greeting line',
            state: { buf: 'main.rs', line: 4 },
            hint: 'C-s then type Hello. The cursor jumps to the match. RET leaves the search with the cursor there.',
          },
          {
            text: 'the text changed',
            elisp: '(not (string-match-p "Hello, {name}" (buffer-string)))',
            hint: 'Backspace (DEL) deletes to the left. Then just type. The mode line shows ** when the buffer has unsaved changes.',
          },
        ],
      },
      {
        id: 'save',
        goal: 'Save it',
        detail: '`C-x C-s` writes the buffer to disk. Watch the mode line: `**` means unsaved.',
        demo: 'C-x C-s',
        conditions: [
          {
            text: 'save-buffer ran',
            event: { command: 'save-buffer' },
            hint: 'C-x C-s. If the bottom line says (No changes need to be saved), the edit did not happen yet.',
          },
          {
            text: 'main.rs has no unsaved changes',
            file: { path: 'hello/src/main.rs', not_contains: 'Hello, {name}' },
            hint: 'The mode line still shows **. Press C-x C-s again, in the main.rs window.',
          },
        ],
      },
      {
        id: 'oops',
        goal: 'Make a mess, then clean it up',
        detail:
          'Press `C-x C-b`. A second window appears, on purpose. Then get back to one window.',
        demo: 'C-x C-b ~1500 C-x 1',
        conditions: [
          {
            text: 'C-x C-b opened the buffer list',
            event: { command: 'list-buffers' },
            hint: 'C-x C-b lists all buffers in a second window. This is the most common "what just happened?" moment, so you are doing it on purpose.',
          },
          {
            text: 'back to a single window',
            layout: 'single',
            hint: 'C-x 1 keeps only the window the cursor is in. Or C-x o to move into the list and q to close it.',
          },
        ],
      },
    ],
  }),
}

const GETTING_UNSTUCK: LessonBundle = {
  id: '00-orientation/02-getting-unstuck',
  title: 'C-g and the three ways to get lost',
  locale: 'en',
  front: {
    title: 'C-g and the three ways to get lost',
    summary:
      'Three traps catch every newcomer: a prompt, a second window, a search. Walk into each on purpose and walk out again.',
    minutes: 12,
  },
  assets: [],
  markdown: `\`C-g\` is the universal "never mind". It cancels a prompt, a half-typed key sequence, a search. Pressing it twice is harmless.

:::vscode
Esc does many of these jobs in VS Code. In Emacs, Esc is a prefix key (it means Meta), so the job goes to \`C-g\`.
:::

::steps
`,
  scenario: assign({
    id: '00-orientation/02-getting-unstuck',
    title: 'C-g and the three ways to get lost',
    minutes: 12,
    modules: ['base', 'agent'],
    setup: {
      workspace: 'fixtures/hello-rust',
      root: 'hello',
      open: ['hello/src/main.rs'],
      layout: 'single',
    },
    steps: [
      {
        id: 'prompt',
        goal: 'Open a prompt and cancel it',
        detail: 'Press `M-x` (Alt and x). A prompt appears at the bottom. Press `C-g` to cancel.',
        demo: 'M-x ~600 C-g',
        conditions: [
          {
            text: 'M-x opened a prompt',
            event: { command: 'execute-extended-command' },
            hint: 'Hold Alt (Meta) and press x.',
          },
          {
            text: 'the prompt is closed again',
            state: { minibuffer: null },
            hint: 'C-g cancels the prompt.',
          },
        ],
      },
      {
        id: 'window',
        goal: 'Open the buffer list and get rid of it',
        detail: '`C-x C-b` splits the screen. `C-x 1` keeps only your window.',
        demo: 'C-x C-b ~1200 C-x 1',
        conditions: [
          {
            text: 'the buffer list was opened',
            event: { command: 'list-buffers' },
            hint: 'C-x then C-b.',
          },
          {
            text: 'one window again',
            layout: 'single',
            hint: 'C-x 1 keeps only the window with the cursor.',
          },
        ],
      },
      {
        id: 'search',
        goal: 'Start a search and leave it',
        detail: '`C-s` starts searching as you type. `C-g` jumps back to where you started.',
        demo: 'C-s "greet" ~600 C-g',
        conditions: [
          {
            text: 'a search was started',
            event: { command: 'isearch-forward' },
            hint: 'C-s, then type a word.',
          },
          {
            text: 'the search is over',
            state: { isearch: false },
            hint: 'C-g cancels the search; RET stops at the match.',
          },
        ],
      },
    ],
  }),
}

const WINDOWS: LessonBundle = {
  id: '03-windows-and-layouts/01-windows-and-frames',
  title: 'Windows and frames',
  locale: 'en',
  front: {
    title: 'Windows and frames',
    summary:
      'A frame is what your desktop calls a window. Inside it, Emacs windows show buffers, and one buffer can be in two windows at once.',
    minutes: 10,
  },
  assets: [],
  markdown: `\`C-x 2\` splits the current :jargon[window] in two, one above the other. \`C-x 3\` splits side by side. \`C-x o\` moves the cursor to the other window and \`C-x 0\` closes the one you are in.

| Keys | Does |
|---|---|
| \`C-x 2\` | split: above and below |
| \`C-x 3\` | split: side by side |
| \`C-x o\` | other window |
| \`C-x 0\` | close this window |
| \`C-x 1\` | keep only this window |
`,
  scenario: assign({
    id: '03-windows-and-layouts/01-windows-and-frames',
    title: 'Windows and frames',
    minutes: 10,
    modules: ['base', 'agent', 'editing', 'files', 'windows'],
    setup: {
      workspace: 'fixtures/hello-rust',
      root: 'hello',
      open: ['hello/src/main.rs'],
      layout: 'single',
    },
    steps: [
      {
        id: 'split',
        goal: 'Show main.rs in two windows',
        detail: '`C-x 2` splits; both windows show the same buffer.',
        demo: 'C-x 2',
        conditions: [
          {
            text: 'main.rs above main.rs',
            layout: '(vsplit main.rs main.rs)',
            hint: 'C-x 2 splits the window above and below.',
          },
        ],
      },
      {
        id: 'move',
        goal: 'Move to the other window and edit there',
        detail: 'Press `C-x o`, then type something. Both windows change: it is one buffer.',
        demo: 'C-x o "// both windows see this" RET',
        conditions: [
          { text: 'other-window ran', event: { command: 'other-window' }, hint: 'C-x then o.' },
          {
            text: 'the buffer changed',
            state: { modified: true },
            hint: 'Type anything; the mode line shows **.',
          },
        ],
      },
      {
        id: 'single',
        goal: 'End with one window',
        detail: '`C-x 1` keeps only the window the cursor is in. The buffer and your edit stay.',
        demo: 'C-x 1',
        conditions: [{ text: 'one window', layout: 'single', hint: 'C-x 1.' }],
      },
    ],
  }),
}

const POLYGLOT: LessonBundle = {
  id: 'projects/p6-polyglot',
  title: 'Polyglot service',
  locale: 'en',
  front: {
    title: 'Polyglot service',
    summary: 'Rust, Python, OCaml and Typst in one repository.',
    minutes: 75,
  },
  assets: [],
  markdown: `A Rust binary, a Python scoring tool, an OCaml proof and a Typst report live in one repository. Each task leaves a checkpoint you can go back to.

The preview on the right follows \`docs/report.typ\`: save it and \`report.pdf\` rebuilds.
`,
  scenario: assign({
    id: 'projects/p6-polyglot',
    title: 'Polyglot service',
    minutes: 75,
    tags: ['project', 'remote'],
    modules: ['base', 'agent', 'editing', 'files', 'windows', 'lsp', 'lang-rust', 'lang-python'],
    project: { lapMinutes: 15 },
    preview: {
      tabs: ['polyglot/docs/report.pdf', 'polyglot/README.md', 'polyglot/site/index.html'],
      follow: { 'polyglot/docs/report.typ': 'polyglot/docs/report.pdf' },
    },
    setup: {
      workspace: 'fixtures/polyglot',
      root: 'polyglot',
      open: ['polyglot/src/main.rs'],
      layout: '(vsplit polyglot/src/main.rs *compilation*)',
    },
    steps: [
      {
        id: 'tabs',
        goal: 'Look around the project',
        detail: 'Open `README.md` from the file tree on the left, or with `C-x C-f`.',
        demo: 'C-x C-f "~/polyglot/README.md" RET',
        conditions: [
          {
            text: 'README.md is open',
            elisp: '(equal (buffer-file-name) "/work/polyglot/README.md")',
            hint: 'Click README.md in the file tree, or C-x C-f ~/polyglot/README.md RET.',
          },
        ],
        checkpoint: true,
      },
      {
        id: 'rust-build',
        goal: 'Fix the build, then rebuild',
        detail:
          '`M-g M-n` jumps to the first error. Make it `greet(&name)`, save with `C-x C-s`, rebuild with `C-c C-c`.',
        demo: 'C-x b "main.rs" RET M-g M-n "&" C-x C-s ~300 C-c C-c ~400 RET',
        conditions: [
          {
            text: 'next-error ran',
            event: { command: 'next-error' },
            hint: 'M-g M-n: hold Alt, press g, then Alt n.',
          },
          {
            text: 'main.rs borrows the name',
            file: { path: 'polyglot/src/main.rs', contains: 'greet(&name)' },
            hint: 'With the cursor on name, type &, then C-x C-s to save.',
          },
          {
            text: 'the build ran',
            event: { command: 'compile' },
            hint: 'C-c C-c asks for a compile command; RET runs cargo build.',
          },
        ],
      },
      {
        id: 'score',
        goal: 'Open the Python scoring tool',
        detail: 'Open `tools/score.py`.',
        demo: 'C-x C-f "~/polyglot/tools/score.py" RET',
        conditions: [
          {
            text: 'score.py is open',
            elisp: '(equal (buffer-file-name) "/work/polyglot/tools/score.py")',
            hint: 'C-x C-f ~/polyglot/tools/score.py RET.',
          },
        ],
      },
      {
        id: 'proof',
        goal: 'Open the OCaml proof',
        detail: 'Open `proof/term.ml`.',
        demo: 'C-x C-f "~/polyglot/proof/term.ml" RET',
        conditions: [
          {
            text: 'term.ml is open',
            elisp: '(equal (buffer-file-name) "/work/polyglot/proof/term.ml")',
            hint: 'C-x C-f ~/polyglot/proof/term.ml RET.',
          },
        ],
      },
      {
        id: 'report',
        goal: 'Retitle the report and watch it rebuild',
        detail:
          'Open `docs/report.typ`, change the `=` heading, save. The preview updates in place.',
        demo: 'C-x C-f "~/polyglot/docs/report.typ" RET M-< C-n C-n C-n C-e DEL DEL DEL DEL DEL DEL DEL DEL "measured twice" C-x C-s',
        conditions: [
          {
            text: 'report.typ is open',
            elisp: '(equal (buffer-file-name) "/work/polyglot/docs/report.typ")',
            hint: 'C-x C-f, then the path. TAB completes each directory.',
          },
          {
            text: 'the heading changed and was saved',
            file: {
              path: 'polyglot/docs/report.typ',
              not_contains: '= Polyglot: a tiny service, measured\n',
            },
            hint: 'Lines starting with = are headings in Typst. Edit the text after =, then C-x C-s.',
          },
        ],
      },
      {
        id: 'compile-per-root',
        goal: 'Save everything',
        detail: '`C-x s` offers to save every modified buffer.',
        demo: 'C-x s',
        conditions: [
          {
            text: 'save-some-buffers ran',
            event: { command: 'save-some-buffers' },
            hint: 'C-x then s.',
          },
        ],
      },
      {
        id: 'deploy',
        goal: 'Read the site page',
        detail: 'Preview `site/index.html` from the tree (the ◧ button).',
        demo: 'C-x C-f "~/polyglot/site/index.html" RET',
        conditions: [
          {
            text: 'index.html is open',
            elisp: '(equal (buffer-file-name) "/work/polyglot/site/index.html")',
            hint: 'Open it in Emacs with C-x C-f.',
          },
        ],
      },
      {
        id: 'layout',
        goal: 'Code above, compile below',
        detail: '`C-x 2` splits; `C-x o` moves; `C-x b *compilation* RET` shows the build there.',
        demo: 'C-x 1 C-x b "main.rs" RET C-x 2 C-x o C-x b "*compilation*" RET',
        conditions: [
          {
            text: 'the layout matches',
            layout: '(vsplit main.rs *compilation*)',
            hint: 'C-x 2 splits; C-x o moves; C-x b *compilation* RET shows the compile buffer there.',
          },
        ],
      },
    ],
  }),
}

export const LESSONS: Record<string, LessonBundle> = Object.fromEntries(
  [OPENING_FILES, GETTING_UNSTUCK, WINDOWS, POLYGLOT].map((b) => [b.id, b]),
)

/** A small runnable bundle for any other ready lesson in the index. */
export function genericLesson(ref: LessonRef): LessonBundle {
  return {
    id: ref.id,
    title: ref.title,
    locale: 'en',
    front: {
      title: ref.title,
      summary: `Served by the fake session. The real lesson lives in curriculum/${ref.id}.`,
      minutes: ref.minutes,
    },
    assets: [],
    markdown: `This placeholder keeps every course-map link working while you develop the app against \`pnpm dev:fake\`.${ref.vscode ? `\n\n:::vscode\nThis lesson replaces: ${ref.vscode}.\n:::` : ''}\n`,
    scenario: assign({
      id: ref.id,
      title: ref.title,
      minutes: ref.minutes,
      modules: ['base', 'agent'],
      setup: {
        workspace: 'fixtures/hello-rust',
        root: 'hello',
        open: ['hello/src/main.rs'],
        layout: 'single',
      },
      steps: [
        {
          id: 'open',
          goal: 'Open README.md',
          detail: '`C-x C-f`, type `README`, `TAB`, `RET`.',
          demo: 'C-x C-f "README" TAB RET',
          conditions: [
            {
              text: 'README.md is open',
              elisp: '(equal (buffer-file-name) "/work/hello/README.md")',
              hint: 'C-x C-f ~/hello/README.md RET.',
            },
          ],
        },
      ],
    }),
  }
}
