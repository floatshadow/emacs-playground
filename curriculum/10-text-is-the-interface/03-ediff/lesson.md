---
title: ediff
summary: Compare two files side by side one difference at a time, copy changes from one side to the other, and merge two versions against their common ancestor.
minutes: 12
---

:jargon[ediff] puts two versions next to each other and walks you through their differences one at a time. You decide, difference by difference, which side wins. With a third file, the version both started from, it becomes a merge tool: changes made on only one side are taken for you, and you decide only where both sides changed the same lines.

:::vscode
This is the diff editor (Compare Selected) and the merge editor. The difference: ediff is driven from a small control window at the bottom, with single keys, and the cursor never has to leave it. `n` and `p` are Next and Previous Change; `a` and `b` are the arrows that copy a change across.
:::

## Starting a comparison

:try[M-x] `ediff-files` asks for two files, A and B. `M-x ediff-buffers` compares two buffers, which may be unsaved; `M-x ediff-directories` compares two folders and lists the files that differ, and from there you open any pair. Magit uses ediff too (`e` on a file in the status buffer).

The screen splits into A on the left and B on the right, with the **control window**, *Ediff Control Panel*, as a thin strip at the bottom. Keys typed there act on the comparison. Its mode line says where you are: `diff 2 of 4`.

## In the control window

| Key | Does |
|---|---|
| `n` / `p` | next / previous difference; both sides scroll to it |
| `j` | jump to a difference by number: `3 j` |
| `a` / `b` | copy A's version of this difference into B / B's into A |
| `r a` / `r b` | restore A / B's version of this difference after a copy |
| `\|` | side by side or one above the other |
| `w a` / `w b` | save A / B |
| `?` | list every key |
| `q` | quit (it asks first) |

## Merging with an ancestor

`M-x ediff-merge-files-with-ancestor` takes three files: your version (A), their version (B) and the ancestor both started from. A third window, C, holds the result. Where only one side changed, C already has that change; where both changed the same lines, C shows both between `<<<<<<<` and `>>>>>>>` and waits for you: `a` or `b` puts that side's version into C. `w c` saves C under a name you choose.

`M-x ediff-merge-files` does the same without an ancestor, so every difference is yours to decide.

The exercise compares two configuration files and takes one change across, then merges two edits of the same shell function, keeping parts of each.

::steps
