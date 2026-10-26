---
title: Undo and save layouts
summary: Undo a layout change, keep layouts in registers for the session, save a named layout as a bookmark, and restore it after anything goes wrong.
minutes: 10
---

Building a good layout takes a dozen keys; destroying it takes one, `C-x 1`. Emacs keeps layouts the way it keeps text: every change can be undone, and a layout you like can be saved under a name and restored later, with the same files and cursor positions.

:::vscode
VS Code restores the workspace's layout when you reopen it, and that is all. In Emacs, `C-c <left>` undoes a layout change like `C-/` undoes an edit, a register holds a layout for the session, and a named bookmark holds it across restarts.
:::

## Undo a layout

:jargon[winner] remembers every window configuration. :try[C-c <left>] goes back to the previous one, and again to the one before; `C-c <right>` goes forward. A layout lost to `C-x 1`, to a pop-up or to Rescue comes back in one key.

## Keep a layout for the session

:try[C-x r w] stores the current layout in a :jargon[register], named by a letter, and `C-x r j` with the same letter restores it, buffers and all. Use it for the two or three arrangements you switch between during a session: "code and tests", "code and build".

## Save a layout by name

`M-x burly-bookmark-windows` saves the layout as a :jargon[bookmark]: which windows, which files, where the cursor was in each. The name starts with *Burly:*. Restore it with `C-x r b`, like any bookmark, after a restart, a Rescue, or a week away. `M-x window-swap-states` swaps the contents of two windows when the layout is right but the buffers are the wrong way round.

:::aside{title="Across restarts"}
`desktop-save-mode` saves every open buffer and the frame's layout when Emacs exits and restores them at the next start. The playground already restores buffers and windows when its Emacs restarts, so it leaves desktop off.
:::

::steps
