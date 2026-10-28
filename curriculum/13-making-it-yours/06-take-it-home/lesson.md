---
title: Take it home
summary: Keep your configuration in git, push it to another machine, clone it there and start Emacs with it, the way you will on your own computer.
minutes: 12
---

Your configuration is a folder of text files, so the best way to carry it is git: commit what you change, push it to a remote, clone it on every machine. This lesson does exactly that with the remote box standing in for your laptop, a fresh machine with Emacs and the course's packages but no configuration.

:::vscode
Settings Sync uploads settings and the extension list to your account. Here the configuration is a repository you own: `git clone` it into `~/.emacs.d` on a new machine and Emacs starts with everything, packages pinned by `packages.lock`. History, branches and review come for free.
:::

## The repository

`/work/emacs.d` is now a git repository of its own (the lesson made it one with your files as the first commit). Its `.gitignore` leaves out what Emacs regenerates: `var/` (history, recent files), `elpa/` (installed packages), compiled files. Commit with Magit (:try[C-x g] in any of its files) as in Part 5 whenever you change something.

## On another machine

On your computer you would push to GitHub and clone from there. Here the box has an empty repository, `emacs.d.git`, which plays GitHub: add it as a remote called `box` with the URL `box:emacs.d.git` (the `box` alias again), and push.

Then on the box, as on any new machine:

```sh
git clone emacs.d.git ~/.emacs.d
TERM=xterm-256color PLAYGROUND=0 emacs -nw
```

`~/.emacs.d` is where Emacs looks for a configuration. `PLAYGROUND=0` turns off what only makes sense here: the agent that feeds the browser's ticker stays loaded but off, and `C-x C-c` quits as usual. The agent is optional: keep `agent` in `modules.el`, where it does nothing outside the playground, or delete it. `TERM=xterm-256color` is needed only because the terminal you run it from is eat inside Emacs, whose terminal type the box does not know.

To quit that inner Emacs from eat, send it the keys with `C-q`: `C-q C-x C-q C-c`.

## What your computer needs

- Emacs 31 (30 works with a few warnings), built with native compilation and tree-sitter.
- The packages: `emacs --batch -l ~/.emacs.d/tools/install.el -f playground-install-batch ~/.emacs.d ~/.emacs.d/elpa ~/.emacs.d/eln-cache` installs exactly the versions in `packages.lock`, checking each checksum.
- The toolchains the modules call: rust-analyzer, basedpyright and ruff, ocaml-lsp and ocamlformat, tinymist and typst, texlab, ripgrep, enchant with a dictionary. A module whose tools are missing still loads; only those features stay quiet.
- Iosevka Nerd Font for the icons.

wc-lite and cheer from lesson 13.2 exist only in the playground's stand-in archive and repository: delete their `use-package` forms, or point them at real packages, before you clone at home.

::steps
