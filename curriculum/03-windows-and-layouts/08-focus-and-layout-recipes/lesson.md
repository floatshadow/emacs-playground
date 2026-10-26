---
title: Focus and layout recipes
summary: Read a long file in two columns, centre text for writing, and rebuild four everyday layouts from memory.
minutes: 10
---

The window commands of this part combine into a few layouts you will use every day. This lesson collects them as recipes, adds two modes that change how a window shows its text, and asks you to rebuild the recipes without hints.

:::vscode
Zen Mode and Centered Layout are olivetti, `M-x olivetti-mode`: the text gets a comfortable width in the middle of the window. Zoom In is `C-x C-+` in the graphical frame, for one buffer, and `C-x C-M-+` for all of them. There is no VS Code equivalent of follow-mode, which turns two windows into two columns of one page.
:::

## Two columns of one page

Split a long file side by side with `C-x 3`, then :try[M-x] `follow-mode`. The right window continues exactly where the left one ends, and moving or scrolling in either keeps both in step, so a wide screen shows twice as much of one file. `C-v` turns two pages at once.

## Centred text

`M-x olivetti-mode` draws the text in a column in the middle of the window and leaves the margins empty. Nothing in the file changes. It suits prose and notes; for code, a narrow window does the same job.

:::gui-only
In the graphical frame, :try[C-x C-+] makes the text larger in this buffer and `C-x C--` smaller; `C-x C-0` resets. `C-x C-M-+` zooms every buffer at once. A terminal cannot change its font size, so these keys only work here.
:::

## The recipes

1. **Edit and build.** The file you edit above, build output below: one window on the file, then `C-x p c`. The output takes the bottom third by itself.
2. **Implementation and test.** The code on the left, its test on the right: `C-x 1`, `C-x 3`, then the test in the right window.
3. **Writing.** One window, centred: `C-x 1`, then olivetti.
4. **Wide code, narrow notes.** Code on the left and notes on the right, with the code clearly wider: `C-x 3`, then `C-x }` and a few more `}`.

When a recipe goes wrong halfway, `C-c <left>` undoes the last layout change, and `C-x z` repeats the last command when a key has no repeat of its own.

::steps
