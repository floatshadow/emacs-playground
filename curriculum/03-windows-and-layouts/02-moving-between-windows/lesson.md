---
title: Moving between windows
summary: Reach any window in one chord with repeat-mode, Shift and the arrows, or ace-window's letters.
minutes: 8
---

With two windows, `C-x o` is all you need. With four, cycling through them gets slow, and Emacs offers three faster ways: repeat the key without the prefix, move in a direction, or jump straight to a window by its label.

:::vscode
Ctrl+1, Ctrl+2 and Ctrl+3 become `M-o` followed by the letter shown on the window. Focus Left, Right, Above and Below Group are `S-<left>`, `S-<right>`, `S-<up>` and `S-<down>`. Ctrl+K Ctrl+Left and friends need no chord prefix here.
:::

## Repeat without the prefix

:try[C-x o] moves to the next window. Right after it, a plain `o` does it again, and again: `C-x o o o` visits four windows. This is repeat-mode, and many commands have it: after the first key, the last key alone repeats. Any other key ends it.

## Move in a direction

Shift with an arrow key moves to the window in that direction: :try[S-<up>] to the window above, `S-<left>` to the one on the left. Past the edge of the frame it wraps around to the other side. Direction is often how you think about a layout anyway ("the test is on the right"), so this needs no counting.

## Jump by label

:try[M-o] (ace-window) draws a letter in the corner of every window: *a*, *s*, *d*, *f*, in reading order. Press the letter and you are there. With only two windows it skips the labels and just switches. It is the fastest way across a big layout, and the labels make it impossible to land in the wrong window.

:::aside{title="Moving windows, not the cursor"}
`M-o` can do more than jump: press `?` after it to see its other actions, such as swapping two windows or deleting one, each followed by a label.
:::

::steps
