---
title: Merge conflicts with smerge
summary: Find the conflicted files of a merge in Magit, resolve each conflict by keeping one side or both, and conclude the merge.
minutes: 15
---

A merge stops when both branches changed the same lines. Git writes both versions into the file between markers and leaves the choice to you. Emacs recognises those markers: opening the file turns on :jargon[smerge], whose keys resolve one conflict at a time.

:::vscode
The merge editor's "Accept Current", "Accept Incoming" and "Accept Both" are `C-c ^ u`, `C-c ^ l` and `C-c ^ a`, acting on the conflict at the cursor. There is no separate view: you resolve in the file itself, and `C-c ^ E` opens a three-way view when you want one.
:::

## Reading a conflict

```
<<<<<<< HEAD
pub const CURRENCY: &str = "EUR";
=======
pub const CURRENCY: &str = "GBP";
>>>>>>> categories
```

The *upper* version, between `<<<<<<<` and `=======`, is the branch you are on (`HEAD`, here main). The *lower* version, down to `>>>>>>>`, comes from the branch being merged. During a merge Magit's status (:try[C-x g]) says *Merging* and marks each conflicted file *unmerged* (it is listed under both Unstaged and Staged changes); `RET` on one opens it.

## Resolving

Opening a conflicted file puts the cursor in its first conflict. All smerge keys start with `C-c ^`:

| Keys | Does |
|---|---|
| `C-c ^ n` `C-c ^ p` | next and previous conflict |
| `C-c ^ u` | keep the upper version |
| `C-c ^ l` | keep the lower version |
| `C-c ^ a` | keep all versions, one after the other |
| `C-c ^ RET` | keep the version the cursor is in |
| `C-c ^ E` | resolve this file in ediff, three-way |

Each key replaces the whole conflict, markers included, and `C-/` undoes it. You can also simply edit the text and delete the markers yourself. When a file is saved with no markers left, Emacs stages it for Git and turns smerge off.

:::aside{title="ediff"}
`C-c ^ E` shows the two versions side by side with the result below. `n` and `p` walk the differences, `a` or `b` copies one side into the result, `q` quits (it asks first). It is the closest thing to VS Code's three-pane merge editor.
:::

## Concluding

When every file is resolved, commit as usual: `c c` in the status. Git has prepared the message, *Merge branch 'categories'*, so `C-c C-c` is enough. To give up instead, `m a` aborts the merge and puts everything back.

::steps
