---
title: Consult
summary: Search lines, files, the whole project, definitions, marks and the kill ring from one kind of list, with a live preview of every candidate.
minutes: 12
---

:jargon[consult] gives the upgraded minibuffer something to search. Each of its commands turns a source into a list of candidates (the lines of this buffer, every match of a project-wide search, the definitions in a file, the kill ring) and *previews* the highlighted candidate in the window as you move through the list. `RET` goes there; `C-g` puts you back exactly where you were.

:::vscode
Ctrl+P, Ctrl+Shift+O (go to symbol), Ctrl+G and Ctrl+Shift+F, merged into one habit: open a list, type a few words, watch the preview, `RET`. The project search shows its results in the same list instead of a side panel, and the preview replaces clicking through them.
:::

## The commands

| Key | Searches | VS Code |
|---|---|---|
| :try[M-s l] | lines of this buffer | Ctrl+F, as a list |
| :try[C-x b] | buffers, recent files, bookmarks | Ctrl+P, Ctrl+Tab |
| :try[M-s r] | the whole project, with ripgrep | Ctrl+Shift+F |
| :try[M-g i] | definitions in this file (imenu) | Ctrl+Shift+O |
| :try[M-g o] | headings of this file (outline) | outline view |
| :try[M-g m] | places you marked with `C-SPC` | Go back |
| `M-g g` | a line number, with preview | Ctrl+G |
| `C-x r j` | registers, showing their contents | |
| `M-y` | the kill ring, as a list | clipboard history |

The terms work as in lesson 4.1: words in any order, `!` to exclude.

## Narrowing C-x b

`C-x b` lists buffers first, then :jargon[recent files] and bookmarks, each under a heading. A letter and a space at the start of the input narrow the list to one group: `b SPC` buffers, `f SPC` files, `m SPC` bookmarks, `p SPC` the current project. `DEL` on the empty input widens again.

## Searching the project

`M-s r` runs ripgrep in the project root as you type and lists every match under its file name. Spaces separate terms that must all be on the line, so `p99 fn` keeps only the lines that also say *fn*: definitions, not uses. Files are searched in parallel, so the order of the groups can change from one search to the next. Moving through the matches previews each file at the matching line.

## The kill ring as a list

`M-y` now opens the kill ring as a list: type a word from the text you killed and `RET` inserts it, without cycling through everything in between. Right after a `C-y`, the entry you pick replaces what you pasted.

::steps
