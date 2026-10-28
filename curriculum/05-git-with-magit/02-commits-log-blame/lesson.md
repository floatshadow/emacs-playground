---
title: "Commits, log and blame"
summary: Browse the history, open any commit, find who last changed a line and when, and rewrite the last commit.
minutes: 15
---

History is where Magit pays for itself. Every list of commits is a buffer you move through with the usual keys, and `RET` opens whatever is on the line: a commit, a file, a hunk.

:::vscode
GitLens's blame annotations, file history and commit details, without an extension. `C-c f b` puts the blame inline above each block of lines; `RET` opens that commit with its whole diff. Amending the last commit is two keys.
:::

## The log

In the status buffer, `l` opens the log menu:

| Keys | Shows |
|---|---|
| `l l` | the current branch |
| `l a` | every branch, as a graph |
| `l o` | another branch or revision |

In a log, `n` and `p` move between commits, `RET` shows the commit on the line in another window, and `SPC` scrolls that window. `q` closes the log.

## Blame

`C-c f` is Magit's menu for the file you are in; `C-c f b` blames it. Each block of lines gets a heading with the commit that last changed it: its summary, author and date. `RET` on a line opens that commit; `n` and `p` jump between blocks; `q` turns blame off. `C-c f b` again while blaming digs one level deeper: it shows the file as it was before the commit at point, and `q` climbs back out one level at a time. For the history of the whole file, `C-c f l` shows the log of just this file.

:try[C-x v =] is Emacs's built-in diff of the current file against the last commit. It works in any version control system, without opening Magit.

## Changing the last commit

The commit menu `c` has more than `c`:

| Keys | Does |
|---|---|
| `c a` | amend: add what is staged to the last commit and edit its message |
| `c e` | extend: add what is staged without touching the message |
| `c w` | reword: edit the message only |

These replace the last commit with a new one. That is harmless until you have pushed it; after a push, others may already have the old one.

::steps
