---
title: LaTeX with AUCTeX
summary: Insert sections and environments with AUCTeX, type maths fast with cdlatex, reference an equation with RefTeX, build with latexmk and jump to errors.
minutes: 15
---

AUCTeX is the LaTeX mode most Emacs users write papers in. It inserts sections and environments for you, builds with latexmk, and walks you through errors. Two helpers ride along: cdlatex, which turns short keys into maths, and RefTeX, which keeps track of every label so you never type one twice.

:::vscode
LaTeX Workshop's Build LaTeX project is `C-c C-c`, which asks which command to run and offers latexmk, the same tool the extension uses. Snippets for environments are `C-c C-e`, and Go to next error is ``C-c ` `` after a build. The texlab language server from Part 7's Eglot gives completion and warnings as you type.
:::

## Structure

:try[C-c C-s] inserts a sectioning command. It asks for the level (`RET` takes the one offered, usually `section`), the title, and a :jargon[label], offering one made from the title such as `sec:model`. :try[C-c C-e] inserts an environment: type its name, `equation`, `itemize`, `figure`, with completion, and AUCTeX writes `\begin` and `\end` and, for numbered ones, a label like `eq:1`.

## Maths with cdlatex

Inside maths, the backquote is a prefix for symbols: `` `a `` types `\alpha`, `` `b `` `\beta`, `` `> `` `\rightarrow`, `` `8 `` `\infty`. Pause after the backquote and a table of every symbol appears. The apostrophe changes the character before it: `n'^` makes `\hat{n}`, `x'b` makes `\mathbf{x}`. `TAB` jumps out of the braces you are in.

## References

:try[C-c )] inserts a reference. RefTeX first asks for the format: `RET` for `\ref`, `e` for `\eqref`. Then it lists the labels of that kind with their context, the cursor already on the nearest one: `RET` takes it. Labels you rename later are found the same way, by what they label, not by remembering their names.

## Building and errors

:try[C-c C-c] asks which command to run, offering `LaTeXMk` when the PDF is older than the source: `RET`. latexmk runs pdflatex, biber for the bibliography, and pdflatex again as often as needed; the bottom line ends with the number of pages. When the source is up to date, `C-c C-c` offers `View` instead.

When the build fails, the bottom line says so, and ``C-c ` `` jumps to the line of the first error and opens a `*TeX Help*` window that explains it. Press it again for the next error. Fix the line and build again.

The paper is `/work/writing/paper.tex`.

::steps
