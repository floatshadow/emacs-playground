---
title: Flymake and diagnostics
summary: Read the errors and warnings a language server reports with Flymake, move between them, read them in full, apply code actions and clear a file of problems.
minutes: 12
---

While you type, the language server checks the file and reports :jargon[diagnostics]: errors, warnings and notes, each with a position and a message. Flymake, built into Emacs, shows them: a mark in the fringe or margin, an underline under the code, and a count in the mode line. rust-analyzer adds the compiler's own messages and clippy's lints each time you save.

:::vscode
The Problems panel is `C-c l d` for this file and `M-x flymake-show-project-diagnostics` for the project. F8 (next problem) is `M-g n`, Shift+F8 is `M-g p`. Hovering a squiggle is the echo area, or `C-h .` for the full text, and the lightbulb's Quick Fix is `C-c l a`.
:::

## Reading the counts

`Flymake[2 1 3]` in the mode line means two errors, one warning and three notes (the notes are the compiler's hints attached to an error). `Flymake:Wait` means a check is running. In the code, problems are underlined and their line gets a mark in the margin: `‼` for an error, `!` for a warning or a note. A `↯` marks the line where code actions are available (below).

## Moving and reading

:try[M-g n] moves to the next diagnostic in the buffer and :try[M-g p] to the previous one; `M-n` and `M-p` do the same. The message appears in the echo area as the cursor arrives, and :try[C-h .] opens the *eldoc* window with every message at point in full, followed by the documentation of the code there (`q` closes it). These keys stay within the buffer's diagnostics; `M-g M-n`, as in lesson 6.1, still walks build output and search results.

:try[C-c l d] lists the buffer's diagnostics in a window: one line per problem with its position and message, `RET` to go to it. The list updates as you fix things. `M-x flymake-show-project-diagnostics` lists those of every file of the project.

## Code actions

Many diagnostics come with fixes the server can apply: import the missing type, prefix an unused variable with `_`, change a type. :try[C-c l a] on the diagnostic offers them; `RET` takes the first one, the server's favourite.

The file `/work/lsp/shapes/src/main.rs` has three problems. Clear them with as few edits as you can: one code action and two small changes are enough.
