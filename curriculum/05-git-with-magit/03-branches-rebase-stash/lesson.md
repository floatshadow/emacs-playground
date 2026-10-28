---
title: "Branches, rebase and stash"
summary: Set work aside with the stash, create and switch branches, squash commits with an interactive rebase, rebase a branch onto main and merge it.
minutes: 15
---

Branches are where Magit's menus save the most typing. Each Git operation is a letter that opens a menu and a second letter that runs the variant you want, with the branch names completed in the minibuffer as in Part 4.

:::vscode
The branch picker in the status bar is `b b`; "Create branch" is `b c`. Stash and pop are `z z` and `z p`. VS Code has no interactive rebase; in Magit it is a buffer listing the commits, where one key per line says what to do with each.
:::

## Stash

In the status (:try[C-x g]), `z` is the stash menu. `z z` stashes every change, staged or not, and leaves the tree clean; `z p` pops the newest stash back (`z a` applies it and keeps it). Stashes appear in their own section of the status, where `RET` shows one and `k` drops it.

## Branches

| Keys | Does |
|---|---|
| `b b` | check out a branch |
| `b c` | create a branch (asks where it starts) and check it out |
| `b m` | rename a branch |
| `b k` | delete a branch |
| `l a` | log of all branches, as a graph |

## Rebase and merge

`r` is the rebase menu and `m` the merge menu. `r e` replays the current branch on top of another ("elsewhere"); `m m` merges the branch you name into the current one. When the current branch is simply behind, Git fast-forwards it without a merge commit.

`r i` rebases interactively. Pick the oldest commit to rewrite, `C-c C-c`, and a list of the commits opens, oldest first, each line starting with `pick`. One key per line changes that:

| Key | Does |
|---|---|
| `s` | squash: meld into the commit above, combining the messages |
| `f` | fixup: meld, keeping only the message above |
| `w` | reword the message |
| `d` | drop the commit |
| `M-p` `M-n` | move the commit up or down |

`C-c C-c` runs the rebase; `C-c C-k` abandons it. If Git stops halfway (a conflict, or a commit to edit), the status says so: `r r` continues and `r a` aborts.

::steps
