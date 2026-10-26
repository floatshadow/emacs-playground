---
title: Open things elsewhere
summary: Open files, buffers and folders in another window, send any command's output to another window, and clone a buffer view.
minutes: 10
---

Most commands show their result in the selected window, replacing what was there. Often you want it beside your work instead. Emacs has a whole family of keys for that: put `4` after `C-x` and the command opens "in another window"; put `5` and it opens in another frame.

:::vscode
Open to the Side (Ctrl+Enter in Quick Open) is `C-x 4 f`. Open Editor to the Side for an open file is `C-x 4 b`, and revealing a folder beside the code is `C-x 4 d`. Open in New Window is the `C-x 5` family. Emacs adds a general version: `C-x 4 4` makes the next command, whatever it is, use another window.
:::

## The 4 family

:try[C-x 4 f] is `C-x C-f` in another window, `C-x 4 b` is `C-x b` there, and :try[C-x 4 d] is Dired there. If there is already another window, the least recently used one is reused; if there is only one window, Emacs splits wherever there is room, below in a narrow terminal and to the side on a wide one. To decide yourself, split first (`C-x 3` or `C-x 2`) and then use a `C-x 4` key.

:try[C-x 4 4] works with any command: press it, then the command, and the command's buffer appears in another window instead of replacing yours. `C-x 4 4 C-x C-b` shows the buffer list beside your work. `C-x 4 1` does the opposite, keeping the next command's buffer in this window.

`C-x 4 c` clones the current buffer into another window as an independent view with its own narrowing, which is useful for seeing two distant parts of a long file in different modes.

## The 5 family

The same letters after `C-x 5` open a new frame: `C-x 5 f` for a file, `C-x 5 b` for a buffer, `C-x 5 d` for a folder. In the terminal a new frame takes over the screen; in the graphical frame it is a new desktop window.

## Closing what you opened

`q` in a special buffer such as help, a list or a listing closes it and gives the window back to the buffer it showed before. `C-c <left>` (lesson 3.6) undoes any layout change.

::steps
