---
title: Outlining and structure editing
summary: Fold an Org file to the outline you need, move headings up, down, in and out with everything under them, cut and paste whole subtrees, and narrow to one of them.
minutes: 12
---

An :jargon[Org] file is plain text with headings: a line starting with one star is a top heading, two stars a child of the heading above it, and so on. Everything up to the next heading is that heading's body. Org's keys treat a heading and everything under it, its :jargon[subtree], as one thing you can fold, move, cut and paste.

:::vscode
The Outline view and folding arrows of a Markdown file, with the structure editing that VS Code leaves to you: Org moves a section with its subsections, renumbers its level as it goes and never breaks the nesting.
:::

## Folding

:try[S-TAB] (Shift and Tab) cycles the whole file: OVERVIEW shows only the top headings, CONTENTS every heading, SHOW ALL everything. The echo area names each view. `TAB` on a heading cycles just that subtree: folded, its children, all of it. A heading followed by `...` has folded text under it. `C-u C-u C-u TAB` shows everything at once, whatever was folded.

:try[C-c C-n] and `C-c C-p` move to the next and previous heading; `C-c C-f` and `C-c C-b` stay on the same level, and `C-c C-u` goes up to the parent.

## Moving and reshaping

With the cursor anywhere on a heading line:

| Keys | Does |
|---|---|
| `M-<right>`, `M-<left>` | one level deeper or shallower (one more or one fewer star) |
| `M-S-<right>`, `M-S-<left>` | the same for the heading and all its children |
| `M-<up>`, `M-<down>` | swap the subtree with the one above or below, at the same level |
| `M-RET` | a new heading at this level |
| `C-c C-x C-w`, `C-c C-x C-y` | cut the subtree, paste it at the level of the heading before the cursor |

`M-<up>` moves a whole area past another whole area: it never mixes their children. A selection that starts at the beginning of a heading line makes `M-<right>` and `M-<left>` act on every heading in it.

:::aside{title="Refile"}
`C-c C-w` moves the subtree under any heading you pick by name, in this file or another Org file: a cut and paste in one step. It is how you empty an inbox of captured tasks (lesson 9.2).
:::

## Narrowing

:try[C-x n s] narrows the buffer to the current subtree: searches, replacements and `M->` then stay inside it, and Narrow shows in the mode line. `C-x n w` widens again. Narrowing hides nothing from the file: saving writes all of it.

`plan.org` was typed at a meeting, every line at the same level. Give it three levels.

::steps
