---
title: Windows and frames
summary: Split the screen into windows, show one buffer in two places, close windows, and tell a window from a frame.
minutes: 10
---

The words first, because they are the opposite of what your desktop taught you. A :jargon[frame] is what your desktop calls a window: the whole Emacs screen. A :jargon[window] is a pane inside the frame, and it shows one :jargon[buffer]. Windows come and go freely; buffers stay until you kill them.

:::vscode
Split Editor is `C-x 2` (one above the other) or `C-x 3` (side by side). Editor groups are windows, but any window can show any buffer, including the same one as its neighbour. Close Editor Group is `C-x 0`, and Close Other Groups is `C-x 1`. New Window is `C-x 5 2`, a new frame.
:::

## Splitting

:try[C-x 2] splits the selected window into two, one above the other; :try[C-x 3] splits it side by side. Both new windows show the buffer you were in, so you have two views of one text: edit in one and the other updates as you type, because there is only one buffer. Each window keeps its own cursor and scroll position, which is the point: the top of a file in one window, the function you are editing in the other.

:try[C-x o] moves to the next window, in reading order. Any command that shows a buffer, such as `C-x b` or `C-x C-f`, does so in the selected window.

## Closing

:try[C-x 0] deletes the selected window and :try[C-x 1] deletes all the others. Only windows go: every buffer stays open, a `C-x b` away. :try[C-x 4 0] is the one exception, closing the window and killing its buffer together, for the help page or listing you are done with.

## Frames

:try[C-x 5 2] makes a new frame. In a terminal only one frame is visible at a time, so the new one takes over the screen, with a single window; `C-x 5 0` deletes it and the frame you came from reappears exactly as you left it. `C-x 5 o` switches between frames without closing any. On your own desktop, a new graphical frame is a separate desktop window. Most people use one frame and many windows.

:::gui-only
On a desktop, `C-x 5 2` in a graphical Emacs opens a second desktop window on the same Emacs, which you can drag to another monitor; both show the same buffers. The playground's graphical screen has no window manager to place it, so the new frame lies exactly over the first and nothing seems to change. `C-x 5 0` closes it, and `C-x 5 o` moves between the two.
:::

::steps
