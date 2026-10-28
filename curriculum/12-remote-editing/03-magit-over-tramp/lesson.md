---
title: Magit over TRAMP and sudo
summary: Commit to a repository on the box with Magit, and edit a file only root may change by hopping through sudo.
minutes: 12
---

Magit runs `git`, and like every program it runs in the buffer's folder. In a remote folder that means `git` on the box: `C-x g` in `/ssh:box:~/site/` shows the box's repository, and staging, committing and pushing happen there. Nothing is copied to your machine.

:::vscode
Remote source control in VS Code needs the Remote-SSH server on the other side. Magit needs only `git` on the box. For root-owned files, VS Code asks you to save with elevated rights through a prompt; TRAMP chains a `sudo` hop into the file's path.
:::

## Magit on the box

Open the folder (`C-x C-f /ssh:box:~/site/`) and press :try[C-x g]. Everything from Part 5 works: `S` stages every change, `c c` opens the commit message, `C-c C-c` commits. Each Magit command is a `git` run over the connection, so a slow link makes Magit slower; the connection itself is shared, as in lesson 12.1.

## Root's files: a sudo hop

`/etc/app/app.env` on the box belongs to root. As *deploy* you can read it but not write it: the mode line shows `%%`, read-only. A :jargon[multi-hop] path chains a second method after the first, with `|`:

`/ssh:box|sudo::/etc/app/app.env`

reads "ssh to box, then sudo on it (as root, the default), then this file". The box lets *deploy* use sudo without a password, so nothing is asked. TRAMP remembers the chain and shows the buffer's file as `/sudo:root@box:/etc/app/app.env`.

Once you know the shape, a shortcut saves typing it: in a read-only remote buffer, `M-x tramp-revert-buffer-with-sudo` reopens the same file through sudo. For a file on your own machine, `/sudo::/etc/hosts` is the one-hop version.

:::aside{title="Be careful as root"}
A root buffer can break the box like a root shell can. Close it (`C-x k`) when you are done, and `M-x tramp-cleanup-all-connections` drops the root connection too.
:::

::steps
