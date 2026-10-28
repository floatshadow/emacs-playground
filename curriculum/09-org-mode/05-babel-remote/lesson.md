---
title: Babel on a remote host
summary: Run a code block on another machine over SSH with :dir, bring its output back as a table to work on locally, and keep one remote shell across blocks with :session.
minutes: 10
---

A code block runs in the directory its `:dir` header names, and that directory may be on another machine: Emacs reaches it over SSH with TRAMP, the remote-file layer Part 12 covers in full. The output comes back into your file, where the next block, running locally, can work on it. Nothing needs installing on the server beyond the program the block calls.

:::vscode
Like a notebook attached to a remote kernel, without a kernel: each block opens an SSH connection (shared and kept open between blocks) and runs a plain command there. The results land in the local document, under version control with the rest of it.
:::

## Running elsewhere

```
#+begin_src sh :dir /ssh:box:/tmp :results table
awk '{ print $9 }' access.log | sort | uniq -c
#+end_src
```

`/ssh:box:/tmp` is a TRAMP file name: the method (`ssh`), the host (`box`, a name from your SSH configuration) and a path on it. `/ssh:box:` alone is your home directory there. :try[C-c C-c] runs the block on the box; the first connection takes a moment, later ones reuse it.

`:dir` works for any language the remote machine has (`python3` for a Python block). A block without `:dir` runs here, so a local block can read a remote block's result through `:var` as in 9.4.

## Keeping a shell

Each run normally starts a fresh shell: a `cd` or a variable set in one block is gone in the next. `:session NAME` keeps one shell running in a buffer called NAME, and every block with the same `:session` runs in it, one after the other, like typing into one terminal. `C-x b NAME` shows that shell; killing the buffer ends it.

:::aside{title="On your own machines"}
Any host in `~/.ssh/config` works the same way, including through a jump host. For root, TRAMP chains methods: `/ssh:box|sudo::/var/log`.
:::

`remote.org` asks the box what it served. Its access log is in `/tmp` on the box, and `/tmp/site` holds a few pages.

::steps
