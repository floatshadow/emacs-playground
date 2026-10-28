---
title: Eshell meets TRAMP
summary: cd into a directory on another machine from Eshell, run commands there, and send their output straight into a local buffer.
minutes: 10
---

Eshell's `cd` understands :jargon[TRAMP] paths. `cd /ssh:box:/var/log` moves the shell onto the remote box: the prompt shows the remote directory, and the commands you type run there. The output, though, lands in Emacs, so it can go into any local buffer as easily as onto the screen.

:::vscode
In VS Code this takes Remote-SSH: a second window connected to the server, its own terminal, and copy and paste to get lines back to your local notes. Here one Eshell switches between machines with `cd`, and a redirection moves text from the server into the note you have open.
:::

## Going remote

`M-x eshell` opens Eshell (:try[C-x p e] opens one in the current project's root, as in Part 6). `cd /ssh:box:/var/log` connects to the machine called `box` over SSH and changes to `/var/log` there; the first connection takes a few seconds. `cd` with a local path, or plain `cd`, comes back. Inside the remote directory, relative file names are remote: `cat shop.log` reads the box's file.

Eshell's built-in commands, such as `ls`, `cat` and `cp`, are Lisp running in your Emacs; through TRAMP they read and write the remote files. Any other program, such as `uname`, `wc` or `df`, runs on the box. `grep` is a special case: Eshell runs the box's grep and shows the matches in a *grep* buffer, where `RET` on a line opens the remote file at that line. A `*` in front, `*grep`, runs the plain program and prints its output in the shell, like any other program.

## Output into a buffer

Eshell redirects into buffers as well as files. `#<buffer NAME>` names a buffer:

| Redirection | Does |
|---|---|
| `> #<buffer notes.org>` | replaces the buffer's whole text with the output |
| `>> #<buffer notes.org>` | appends the output at the end of the buffer |
| `>>> #<buffer notes.org>` | inserts the output at the buffer's cursor |

The buffer belongs to your Emacs, so remote output ends up in a local file without a copy step.

:::aside{title="Part 12"}
This lesson uses TRAMP from Eshell only. Part 12 opens remote files directly, edits them as root, and compiles on the box.
:::

The exercise collects the three ERROR lines of the shop's log on the box into a local Org file and turns them into a table.

::steps
