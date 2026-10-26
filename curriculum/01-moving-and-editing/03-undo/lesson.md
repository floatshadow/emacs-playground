---
title: Undo that never loses work
summary: Undo and redo, undo only inside a region, and browse the whole history of a buffer as a tree.
minutes: 10
---

In most editors, undoing a few steps and then typing throws away whatever you undid. Emacs never throws history away. Undo is itself recorded as a change, so every state the buffer has ever been in stays reachable. This lesson uses that to rescue a paragraph you deleted before making other edits.

:::vscode
Ctrl+Z is `C-/` and Ctrl+Y is `C-?` (in terminals that cannot send it, `C-M-_`). Two things VS Code cannot do: undo only the changes inside a selected region, and show the full history as a tree you can walk, with `C-x u`.
:::

## Undo and redo

:try[C-/] undoes the last change; press it again to go further back. Some terminals send `C-_` when you press `C-/`; both run *undo*, as the ticker shows. :try[C-?] redoes what undo undid. Any other command in between ends the sequence: after that, the undos themselves become history that a later undo can undo. That is why nothing is ever lost, and also why it can be confusing at first.

## Undo in a region

With an active :jargon[region], `C-/` undoes only the changes that touched that region, leaving the rest of the buffer as it is. If you deleted a paragraph and then edited elsewhere, mark the place where the paragraph was and undo there: the paragraph comes back and the later edits stay.

## The tree

:try[C-x u] opens the :jargon[undo tree] (vundo) in a small window under the buffer. Each node is a state of the buffer; branches are where you undid and then changed something else. `b` and `f` walk back and forward, `n` and `p` switch branches, and the buffer follows as you move. `RET` keeps the state you are on; `q` leaves the tree and puts you back where you started.

:::aside{title="Undo is per buffer"}
Every buffer has its own history, so undo in one file never touches another. The history lasts as long as the buffer; the playground's checkpoints cover what happens across sessions.
:::

::steps
