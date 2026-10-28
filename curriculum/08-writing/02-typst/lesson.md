---
title: Typst with tinymist
summary: Compile a Typst report, let typst watch rebuild the PDF on every save, add a table and a figure, and find errors with Flymake.
minutes: 15
---

Typst is a typesetting language with Markdown-like markup, a real scripting language underneath and compile times in milliseconds. Emacs edits `.typ` files in typst-ts-mode, and Eglot connects it to tinymist, the Typst language server, for completion, documentation on hover and errors as you type.

:::vscode
The Tinymist extension's Export PDF is `C-c C-c`, and its live preview is `C-c C-w`, which runs `typst watch`: every save rebuilds the PDF, and the preview pane reloads it. Problems are Flymake's: the mode line counts them and `M-n` jumps to the next one, as with any language server since Part 7.
:::

## Compiling

:try[C-c C-c] runs `typst compile` on the current file and shows the output in a :jargon[compilation buffer]; the PDF is written next to the source, `report.pdf` for `report.typ`. The preview pane shows it as soon as it exists. `C-c C-p` opens the PDF in Emacs itself: as text in this terminal, drawn as pages in the graphical frame (lesson 8.4).

## Watching

:try[C-c C-w] turns on watch mode for the buffer: a `typst watch` process keeps running in the background and rebuilds the PDF every time the file is saved, so the preview pane follows your writing. The mode line shows `[Watch]` while it runs. When a build fails, the errors appear in a small `*Typst-Watch*` window at the bottom, which goes away once the file builds again. `C-c C-w` once more stops watching.

The playground saves edited files by itself a few seconds after you stop typing, so with watch mode on you hardly need `C-x C-s`.

## Errors as you type

tinymist checks the document while you type, before anything is saved. Mistakes are underlined, the mode line's Flymake counter shows how many there are, and :try[M-n] and `M-p` move between them. With the cursor on one, its message appears at the bottom; `C-h .` shows it in full and `C-c l d` lists every problem in the buffer.

:::aside{title="Typst in a minute"}
`= Title` and `== Section` are headings, `*bold*` and `_italic_` are markup, and `#` switches to code: `#table(columns: 2, [a], [b])` calls the table function with content in square brackets. `#figure(image("plot.svg"), caption: [...])` numbers the figure and adds a caption.
:::

The report is `/work/writing/report.typ`.

::steps
