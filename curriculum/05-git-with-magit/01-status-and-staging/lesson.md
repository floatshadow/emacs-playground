---
title: Status and staging hunks
summary: Read a repository's status in Magit, stage single hunks instead of whole files, discard what you do not want, and commit.
minutes: 15
---

Part 5 turns on the git module and :jargon[Magit], a Git client that lives in a buffer. Its status buffer is the Source Control view, and nearly everything happens there with single letters: no command names, no `git add -p`.

:::vscode
`C-x g` is the Source Control view (Ctrl+Shift+G). Where VS Code stages a file with **+** and a single change only from the diff editor, Magit stages a :jargon[hunk] with `s` wherever the cursor is in it. The commit message box is a buffer of its own, confirmed with `C-c C-c`.
:::

## The status buffer

:try[C-x g] in any file of a repository opens its status: the current branch and the last commit at the top, then sections such as *Untracked files*, *Unstaged changes*, *Staged changes* and *Recent commits*. Sections fold: `TAB` opens or closes the one the cursor is in, and on a file it shows the file's diff, hunk by hunk. `M-1` to `M-4` fold the whole buffer to one depth: `M-2` shows file names only, `M-4` every hunk. `n` and `p` move from section to section; `RET` on a file or a hunk visits that place in the file. `g` refreshes (Magit refreshes by itself after its own commands) and `q` buries the buffer.

Files in a repository also show the branch in their mode line: *Git-main* when the file matches the last commit, *Git:main* once it has changes.

## Staging

Git commits what is in the :jargon[staging area], not everything you changed. In Magit the key acts on what the cursor is on: a hunk, a file, or a whole section.

| Key | Does |
|---|---|
| `s` | stage the hunk, file or section at point |
| `u` | unstage it |
| `S` | stage every change |
| `U` | unstage everything |
| `k` | discard the change at point (asks first) |

With a region inside a hunk, `s` stages only the selected lines. `k` throws changes away for good, which is why it asks.

## Committing

`c` opens the commit menu: a list of keys and switches that stays at the bottom until you pick one. `c` again starts a commit; `C-g` closes the menu. Magit opens a buffer for the message and shows the staged diff beside it. Write a summary line, optionally a blank line and more, then `C-c C-c`. `C-c C-k` cancels without committing.

Every menu works the same way, and `?` in the status buffer opens the menu of all menus. You never have to remember more than the first letter.

::steps
