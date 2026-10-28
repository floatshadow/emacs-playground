---
title: Paper with citations
summary: A short paper in Typst with a figure and citations, a LaTeX appendix proofread through SyncTeX, and both PDFs released under a git tag.
minutes: 75
---

You write a short paper on how Lantern's build time grows: the paper itself in Typst, its appendix of measurements in LaTeX, one `refs.bib` for both. Every tool is one from Part 8; here they share a project, a bibliography and a release. The brief is `README.md` in the preview; once the PDFs exist, the preview shows the one that belongs to the file you are editing.

:::vscode
Tinymist and LaTeX Workshop are the same tools here: tinymist is Typst's language server in Emacs too, and AUCTeX with latexmk builds LaTeX as LaTeX Workshop's recipe does. The PDF viewer tab is pdf-tools in the GUI frame, with :jargon[SyncTeX] both ways. Source Control's "Create Tag" is Magit's `t`, which also makes the :jargon[annotated tag] a release usually gets.
:::

## The keys of this project

| Keys | Does |
|---|---|
| `C-c C-w` | Typst: watch, rebuild the PDF on every save |
| `M-x image-dired` | thumbnails of a folder; `RET` full size, `w` copies the file name |
| `C-c C-x @` | insert a citation from `refs.bib` (Typst and LaTeX) |
| `C-c C-s` / `C-c C-e` | LaTeX: a section / an environment |
| `C-c C-c` | LaTeX: build with latexmk (it runs biber for the bibliography) |
| `C-c C-v` | LaTeX: show the cursor's line in the PDF |
| `M-s s` | in a PDF search: jump back to the source |
| `C-x g` | Magit status; `C-u S` stages everything, `t` opens the tag menu |

## Two languages, one bibliography

Typst rebuilds as fast as you save, so the paper's PDF can follow every edit (`C-c C-w` in paper.typ); LaTeX builds on request with `C-c C-c`, and `C-c C-v` shows the line you are on in its PDF. Magit closes the project: :try[C-x g] opens it from any file here.

citar reads every `.bib` file at the top of the project, so `C-c C-x @` offers the same references in `paper.typ` and in `appendix.tex` and writes each one's syntax: `@knuth1984` in Typst, `\cite{knuth1984}` in LaTeX.

:::aside{title="The GUI frame"}
Images and PDF pages need the GUI :jargon[frame]: the screen switches to it for tasks 2 and 5. It is the same Emacs, with the same buffers; the Frame switch above the screen goes back whenever you like. In the terminal frame image-dired's thumbnails stay blank and a PDF opens as its text.
:::

Each task saves a checkpoint when it starts; the list on the left restores one. When every task is done, **Again, without hints** runs the whole project once more against the clock.

::steps
