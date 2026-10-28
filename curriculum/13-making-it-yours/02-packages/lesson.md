---
title: "Packages: install, update, pin, remove"
summary: Browse and install packages from an archive and from a git repository, configure them with use-package, and remove one cleanly.
minutes: 15
---

Emacs packages come from :jargon[package archives]: GNU ELPA and NonGNU ELPA (vetted, enabled by default) and MELPA (built from thousands of git repositories). `package.el` is built in: it downloads, byte-compiles and activates them. A package that is in no archive can be installed straight from its git repository.

:::vscode
The Extensions view becomes `M-x list-packages`, *Install* becomes `i` then `x`, and installing a VSIX from a file has a cousin in `package-vc-install` from a git URL. Where VS Code records extensions in its settings, you write a `use-package` form per package, which installs it if missing and configures it in one place.
:::

:::aside{title="No internet in the playground"}
Sessions have no network. For this lesson the archive named *melpa* points at a small local copy in `/work/mine/packages/melpa/`, and `https://github.com/playground-course/` is redirected to local git repositories. The commands are exactly the ones you would use at home.
:::

## The package list

`M-x list-packages` lists every package the archives offer. In the list: `RET` describes a package, `i` marks it for installation, `d` for deletion, `U` marks every available upgrade, `x` does what is marked, `q` leaves. :try[C-h P] describes any package by name, installed or not. `M-x package-install` installs one by name, `M-x package-upgrade-all` upgrades everything, `M-x package-delete` removes one.

The packages this course ships are pinned: `emacs.d/packages.lock` lists each one's version and checksum, and the image installs exactly those, read-only, in `/opt/playground/elpa`. What you install goes to `emacs.d/elpa/`, yours to keep or delete.

## From a repository

`M-x package-vc-install` takes a git URL, clones it into `emacs.d/elpa/` and installs the package from the clone. You can edit it there and send the author a patch; `M-x package-vc-upgrade` pulls new commits.

## use-package

A `use-package` form keeps a package's whole story in one place:

| Keyword | Does |
|---|---|
| `:ensure t` | install from an archive if missing |
| `:vc (:url "…")` | install from a git repository if missing |
| `:pin melpa` | take it only from that archive |
| `:custom (var value)` | set an option |
| `:bind ("C-c y" . cmd)` | bind a key (and load the package on first use) |
| `:hook (text-mode . mode)` | turn a mode on with another |
| `:config …` | run code after the package loads |

The starter file `user-lisp/my-packages.el` loads after the modules, like `my-settings.el`. Its first form shows each keyword at work. Its first line says `no-byte-compile: t`: Emacs compiles `user-lisp/` before `init.el` runs, and compiling a form with `:ensure` would look for packages before `init.el` has said where they live. Keep `:ensure` forms in that file.

To remove a package for good, delete its form first, then the package: a form with `:ensure` or `:vc` would install it again at the next start.

:::aside{title="elpaca and straight"}
Two popular replacements for package.el. *straight.el* clones every package as a git repository and pins them all in a lock file. *elpaca* does the same, installs in parallel and in the background. Both work with use-package (`:straight`, `:ensure (:host github …)`). Start with package.el; switch when you want every package as a repository.
:::

::steps
