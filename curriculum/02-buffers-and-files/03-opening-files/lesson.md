---
title: Opening files
summary: Open files from the minibuffer with completion, save them, and find your way back from the buffer list and from Dired.
minutes: 12
---

Emacs asks for a path in the :jargon[minibuffer], the one-line area at the bottom of the screen. Once you trust that line, most of Emacs stops being mysterious.

:::vscode
Ctrl+P and the Explorer both become `C-x C-f`. Files you have opened are :jargon[buffers], and `C-x b` switches between them the way Ctrl+Tab does. There is no tab bar by default.
:::

Press :try[C-x C-f]. Emacs shows *Find file:* with the current directory already typed. Type the first letters of a name and press `TAB` to complete it. `RET` opens the file.

If the file does not exist, Emacs opens an empty buffer and says *(New file)*. Nothing is written to disk until you save with :try[C-x C-s].

When something looks wrong, `C-g` cancels. It is always safe. The panel on the right tells you which state you are in and how to leave it.

## Paths in the prompt

The offered directory is only a starting point. Typing `/` after it starts a fresh path from the root, and `~/` starts from your home directory: the old text dims to show it no longer counts. `TAB` completes as far as it can and, when several names fit, lists them. :try[C-x C-r] lists files you opened recently, and `C-x d` opens a folder.

## Saving

The mode line shows `-:**-` while the buffer has changes that are not on disk, and `-:---` once it is saved. `C-x C-s` saves the buffer you are in. The playground normally saves files for you a few seconds after you stop typing; this lesson switches that off so you can watch the stars.

## Two ways to get lost

`C-x C-b` lists every buffer and takes over the window. It is the most common "what just happened?" moment, so you will do it on purpose. `q` puts the list away.

If you press `RET` in the *Find file* prompt before typing a name, you asked for the directory itself, and Emacs shows a :jargon[Dired] listing of it. That is lesson 2.6; for now, `q` leaves it and you are back in your file.

::steps
