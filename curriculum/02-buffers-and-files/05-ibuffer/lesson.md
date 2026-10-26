---
title: Many buffers with ibuffer
summary: Mark, filter and act on many buffers at once in ibuffer, grouped by project.
minutes: 10
---

After an hour of work you have thirty buffers: source files from two projects, help pages, build output, a few lists. :jargon[ibuffer] is a buffer list you can act on in bulk: mark the buffers you mean, then save, kill or open them with one key.

:::vscode
There is no real equivalent in VS Code; the closest are Close All Saved and the editor's Open Editors view. Think of ibuffer as a file manager for open buffers: marks, filters and commands that apply to everything marked.
:::

## Reading the list

:try[C-x C-b] opens it, grouped by project, with help and messages at the end. The first columns are flags: `*` for :jargon[modified], `%` for :jargon[read-only], `>` for a mark. Then come the name, the size, the major mode and the file. `n` and `p` move, `RET` opens the buffer on the line, `q` leaves.

## Marking

`m` marks the buffer on the line and `u` unmarks it; `t` swaps marked and unmarked. Marking by hand is fine for three buffers. For more, ibuffer marks by kind: `* m` marks modified buffers, `* h` help buffers, `* s` special buffers, and `% f` asks for a regexp and marks every buffer whose file name matches.

## Acting

Capital letters act on the marked buffers. `D` kills them (in the playground without asking, so mark carefully), `S` saves them and `V` shows them. Nothing happens to the buffers you did not mark.

## Filtering

A filter hides lines without touching the buffers. `/ f` filters by file name with a regexp, `/ m` by major mode, and filters stack: each new one narrows the list further. The active filters show at the top. `/ /` removes them all. Filtering first and then marking with `* m` is how you save only the modified Rust files and nothing else.

::steps
