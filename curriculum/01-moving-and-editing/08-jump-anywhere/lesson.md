---
title: Jump anywhere
summary: Jump to any visible spot by typing it, to any definition by name, and back to bookmarked places, even in other files.
minutes: 12
---

Motion keys and search cover most moves. Three more cover the rest: jumping to something you can see, jumping to something you can name, and jumping to something you marked earlier, perhaps in another file.

:::vscode
Ctrl+G is `M-g g`, Ctrl+Shift+O (go to symbol in file) and the Outline view are `M-g i`, and breadcrumbs become a prompt you type into. Ctrl+Shift+\ (go to matching bracket) is `C-M-f` and `C-M-b`. VS Code has no equivalent of avy, `M-j`: it jumps to any spot on the screen in two or three keystrokes.
:::

## What you can see: avy

:try[M-j] asks for characters. Type the first few letters of where you want to go, `let` for example, and pause: every visible match gets a label, a letter drawn over it. Press the label and the cursor is there. The labels run *a*, *s*, *d*, *f* and on along the home row, from the top of the window. It sounds slow to read about and is very fast to do: look, type, press.

## What you can name: imenu

:try[M-g i] lists the definitions in the current file, each with its kind in front: *Fn greet*, *Fn main*, *Module report*. Type the start of one, `TAB` completes, `RET` jumps. It works in Rust, Python, Markdown headings and Lisp alike, because each major mode tells imenu what a definition is. In Markdown and Org, `M-g o` does the same for headings.

## What you marked: bookmarks

:try[C-x r m] sets a :jargon[bookmark] at point and asks for a name. :try[C-x r b] jumps to a bookmark by name, opening its file if needed, and Emacs remembers bookmarks across restarts. Use them for the few places in a project you keep coming back to: the main function, the config, the test you are fixing.

Two smaller jumps complete the set. Every jump sets the mark where you left from, and `C-u C-SPC` goes back to it, repeatedly, through your recent positions. And on a bracket, `C-M-f` and `C-M-b` jump to its partner.

::steps
