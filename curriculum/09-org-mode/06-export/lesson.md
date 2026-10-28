---
title: Export
summary: Turn one Org file into HTML, a PDF through LaTeX, Beamer slides and Markdown from the export menu, and present it straight from Emacs.
minutes: 12
---

An Org file is the source; :jargon[export] makes whatever the reader needs from it. Headings, lists, tables, links, code blocks and their results all carry over. The export menu writes the new file next to the Org file, so `plan.org` gives `plan.html`, `plan.pdf`, `plan.md`.

:::vscode
Markdown PDF and Marp in one menu, with LaTeX behind the PDF. The Org file stays the one you edit; the exports are output you can delete and make again.
:::

## The export menu

:try[C-c C-e] opens the dispatcher: a menu of back ends, each a letter, then a letter for what to make.

| Keys after `C-c C-e` | Makes |
|---|---|
| `h h`, `h o` | plan.html; `o` also opens it |
| `l l`, `l p`, `l o` | plan.tex; plan.pdf through LaTeX; and open it |
| `l b`, `l P` | Beamer slides: plan.tex; their PDF |
| `m m` | plan.md (Markdown) |
| `t u` | plain text |

In the menu, `C-s` limits the export to the subtree at the cursor, and `C-b` leaves out everything around the body (the HTML head, the LaTeX preamble). Lines such as `#+title:`, `#+author:` and `#+options: toc:nil num:nil` (no table of contents, no section numbers) at the top of the file set the defaults for every back end.

In the playground, HTML opens in eww (Emacs's web browser: `q` closes it) and PDFs open in Emacs, as text in the terminal frame. LaTeX runs through latexmk, and while it runs Emacs waits.

:::gui-only
In the GUI frame, `l o` shows the PDF in pdf-tools, as in Part 8.
:::

## Presenting

`M-x org-present` turns the file into slides, one top heading each, in big text and read-only. `SPC` or the right arrow moves on, the left arrow goes back, `C-c C-q` ends the presentation and gives the file back as it was.

:::aside{title="Code in exports"}
A code block's `:exports` header says what goes into the export: `code` (the default), `results`, `both` or `none`. Babel runs blocks again while exporting when their results are exported.
:::

`plan.org` is the blog's launch plan, ready to send around.

::steps
