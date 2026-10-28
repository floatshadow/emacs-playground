---
title: diff-hl and git-timemachine
summary: See uncommitted changes in the margin, jump between them and revert one, and step through a file's history to recover text from an old version.
minutes: 10
---

Two smaller tools round off Git in Emacs. :jargon[diff-hl] marks the lines you changed since the last commit, in every file of a repository, and gives you keys to move between those changes and undo one. :jargon[git-timemachine] shows a file as it was at any commit, one commit at a time, in an ordinary read-only buffer you can search and copy from.

:::vscode
The coloured gutter bars are the marks in the left margin: `+` added, `!` changed, `-` deleted below. Clicking a bar to see or revert the change is `C-x v r`. The Timeline view, opened on an old commit, is `C-x v t` with `p` and `n` to step through it.
:::

## Changes in the margin

:try[C-x v n] jumps to the next change in the file. The marks update as you type, before you save. They compare against the last commit, so they disappear when you commit.

| Keys | Does |
|---|---|
| `C-x v n` | next changed hunk (then `n` again repeats) |
| `C-x v p` | previous changed hunk |
| `C-x v r` | revert the hunk at the cursor, after showing it |
| `C-x v =` | the whole file's diff in a window |

`C-x v r` asks before it reverts; `C-/` undoes a revert like any other edit.

## Through the history

:try[C-x v t] opens the current file at its latest commit, in a buffer named *timemachine:* and the file name. The mode line shows the commit and its position (*2/4*); the echo area names the author and the summary.

| Key | Does |
|---|---|
| `p` | previous version |
| `n` | next version |
| `g` | go to version number N |
| `w` | copy the commit's short hash |
| `b` | blame this version (Magit) |
| `q` | close the time machine |

The buffer is read-only, but all the reading keys work in it: search with `C-s`, select and copy with `M-w`. Your file is never touched; after `q`, paste what you copied wherever it belongs.

::steps
