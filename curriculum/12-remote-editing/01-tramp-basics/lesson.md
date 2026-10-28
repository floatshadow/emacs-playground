---
title: TRAMP basics
summary: Open, edit and save files on another machine from your own Emacs, browse its folders in Dired, and check the result from a terminal.
minutes: 12
---

Your Emacs stays here, with your configuration, keys and buffers. :jargon[TRAMP], built into Emacs, makes files on other machines look like local ones: a path that starts with `/ssh:box:` is a file on the machine called *box*, and every command that works on files works on it.

:::vscode
Remote-SSH installs a server on the other machine and runs extensions there. TRAMP installs nothing: it logs in with plain `ssh`, reads and writes files through that connection, and your editor never leaves your machine. Remote Explorer becomes `C-x C-f /ssh:box:` plus `TAB`.
:::

## The remote path

A TRAMP path has three parts between slashes and colons: `/ssh:box:/home/deploy/` is the method (`ssh`), the host (`box`, an alias from `~/.ssh/config`, so no password is asked) and the path on that host. Press :try[C-x C-f] and type it after the offered folder: the `/` starts a fresh path, and `TAB` completes method, host and file names. The first connection takes a second or two; the bottom line says *Tramp: Opening connection for box using ssh*.

A folder opens in :jargon[Dired], with every key from lesson 2.6. A file opens in a normal buffer. Its mode line starts `-:--@` where a local file's starts `-:---`: the `@` marks a file on another machine.

Inside a remote buffer, `C-x C-f` offers the remote folder, and a `/` typed after it starts from the root *of the box*, so `/etc/hosts` there is the box's. A whole path of another kind, local or `/ssh:...`, needs an empty prompt: `C-a C-k` clears the offered folder first.

## Saving

`C-x C-s` writes the file back over the same connection. The playground saves local files for you a few seconds after you stop typing; it leaves remote files alone, because a save over a slow link would interrupt your typing. Save remote files yourself.

## One connection, reused

The first file opens a connection; every later file, listing and save on that host reuses it, so the second file opens instantly. When a connection goes stale (you closed your laptop, the box restarted), `M-x tramp-cleanup-all-connections` forgets every connection; TRAMP reconnects at the next remote file you touch. Buffers stay open.

## Terminals follow the buffer

A shell or terminal starts in the current buffer's folder, so from a remote buffer `C-u M-x eat` opens a terminal on the box itself (lesson 12.2 builds on that). To look at the box from the outside, start from a local buffer such as `*scratch*` and use `ssh box`, as you would in any terminal.

::steps
