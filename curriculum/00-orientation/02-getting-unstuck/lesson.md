---
title: C-g and the three ways to get lost
summary: Cancel a prompt, a search and a half-typed key, close an extra window and leave a help buffer, on purpose, so getting lost stops being scary.
minutes: 12
---

Nearly every "Emacs is stuck" moment is one of three things: Emacs is waiting for you to finish something, an extra window has appeared, or you are in a buffer you did not mean to visit. Each has a one-key way out. In this lesson you get into all three on purpose and leave again.

:::vscode
`C-g` is your Esc. It cancels whatever is in progress (a prompt, a search, a key sequence you started) and it is always safe: it never deletes text or closes files. VS Code closes a panel with Esc; in Emacs extra windows go away with `C-x 1` or with `q` inside them.
:::

## Waiting for you

When the bottom line shows a prompt, Emacs is waiting for an answer, and every key you type goes into the prompt. Open one with :try[C-x C-f] and cancel it with :try[C-g]. The echo area says *Quit*; nothing else happened.

A search is the same kind of wait. :try[C-s] starts one, and while *I-search* shows at the bottom, your keys extend the search instead of editing. When the text cannot be found, the first `C-g` removes the part that failed and the second ends the search and puts the cursor back where it started.

A half-typed key sequence waits too. Press `C-x` and stop: the echo area shows *C-x-* and Emacs waits for the next key. `C-g` cancels that as well.

## An extra window

Some commands show their result in a second window without moving your cursor there. :try[C-x C-b], the buffer list, is the classic: suddenly the screen is split. `C-x 1` keeps only the window you are in. Nothing is closed; the list is still a buffer, just not shown. Inside a list like this one, `q` closes it too. (Once Part 2 turns on the files module, `C-x C-b` opens ibuffer, which takes over the current window; `q` closes it the same way.)

## The wrong buffer

Help buffers take the cursor with them: after `C-h k` the cursor is in *Help*, and `q` closes it and takes you back. Most special buffers (help, lists, logs) close with `q`. The tutorial, :try[C-h t], is different: it is an ordinary text buffer, so `q` would just type a *q*. You leave it like any buffer, by killing it with `C-x k`.

## When everything is wrong at once

`ESC ESC ESC` (Esc three times) gets out of one layer at a time: first the prompt, then the extra windows. And the *Rescue me* button on the right does all of it in one go, without touching your files.

:::aside{title="C-z is not undo"}
In a terminal, `C-z` would suspend Emacs to the shell. The playground switches it off, so pressing it out of habit does nothing. Undo is `C-/`.
:::

::steps
