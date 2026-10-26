---
title: Projects
summary: Open files, search, switch buffers and build within the current project, and switch between projects.
minutes: 12
---

A :jargon[project] is a folder Emacs treats as one unit. It finds the root by looking up from the current file for a version control directory or a marker such as `Cargo.toml`. Once Emacs knows the root, the `C-x p` commands work on the whole project, whichever file you are in.

:::vscode
The workspace folder is the project, found automatically. Ctrl+P is `C-x p f`, Ctrl+Shift+F is `C-x p g`, and Open Recent for workspaces is `C-x p p`. There is no workspace file to create: switching projects is switching folders. Close Workspace is `C-x p k`, which kills the project's buffers.
:::

## Inside one project

:try[C-x p f] lists every file of the project, with paths relative to its root, so `src/main.rs` and `tests/shout.rs` are told apart at a glance. :try[C-x p g] searches the whole project for a :jargon[regexp] and lists the matches in an :jargon[xref] buffer: `RET` visits one, and from anywhere `M-g M-n` and `M-g M-p` walk through them in order. :try[C-x p b] switches only among this project's buffers.

:try[C-x p c] runs a build command in the project root, whatever directory the current file is in, and shows the output in a :jargon[compilation buffer]. It remembers the last command, so the next build is `C-x p c RET`.

## Between projects

:try[C-x p p] lists the projects Emacs knows, the ones you have opened files in, and asks what to do in the one you pick: `f` to find a file, `g` to search, `d` for a Dired listing, `e` for a shell. :try[C-x p k] kills every buffer of the current project after asking, which is the closest thing to closing a folder.

:::aside{title="Faster search later"}
Part 4 adds `M-s r`, a ripgrep search of the project that previews each match as you move through the results. `C-x p g` works everywhere today.
:::

This lesson's `hello` project has three TODO comments to find, and a second project, `polyglot`, to switch to and back from.

::steps
