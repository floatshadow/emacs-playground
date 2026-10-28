---
title: "Hello, cargo"
summary: A small Rust command-line tool from cargo new to two clean commits, without leaving Emacs.
minutes: 75
---

You build `tempconv`, a temperature converter, the way Rust projects really start: `cargo new` in a shell, a first run, code written with the language server's help, a compiler error fixed from the build output, a test, formatting, and commits. Every tool is one you met in Parts 4 to 7; here they work together on one crate. The brief is `README.md` in the preview.

:::vscode
rust-analyzer is the same server VS Code's Rust extension runs, so completions, hovers and diagnostics match what you know. The Run task and the integrated terminal become `C-c C-r`, `C-c C-c` and `C-c C-t`, whose output lands in the :jargon[compilation buffer] below the code, where every error is a link. Source Control's "Stage Selected Ranges" is Magit's `s` on a selection.
:::

## The keys of this project

| Keys | Does |
|---|---|
| `C-x p e` | Eshell in the project root |
| `C-c C-r` / `C-c C-c` / `C-c C-t` | cargo run / build / test, in the compile window |
| `M-x compile` | any command, such as `cargo run -- 100 -40` |
| `M-g M-n` / `M-g M-p` | next / previous error of the last build, from anywhere |
| `M-g n` / `M-g p` | next / previous underline (Flymake) in the code |
| `M-+` | insert a snippet (tempel); `TAB` moves to its next field |
| `C-c l f` | format the buffer now; saving formats too |
| `C-x g` | Magit status; `s` stages a file, a hunk or a selection |

## Shell, compile window, editor

The shell is for one-off commands such as `cargo new` and `git commit -m`; :try[C-x p e] opens one in the project root. :jargon[Eshell] is a shell written in Emacs Lisp, so Emacs commands work in it as if they were programs: `find-file src/main.rs` opens a file.

The compile window is for commands you repeat: cargo's output appears below the code, the cursor stays where it was, and :try[M-g M-n] walks the errors in order, moving the cursor to each one in the code.

The editor is where rust-analyzer helps: completions pop up as you type (`RET` or `TAB` takes one, `C-g` closes the popup), errors are underlined, `C-c l h` shows the documentation at point and `M-.` goes to a definition.

:::aside{title="Why commit the scaffold"}
Committing `cargo new`'s output untouched means every later diff is only your own work. Magit then shows `main.rs` as one hunk, because your code replaced cargo's three lines, and the last task splits that hunk into two commits with a selection.
:::

Each task saves a checkpoint when it starts; the list on the left restores one. When every task is done, **Again, without hints** runs the whole project once more against the clock.

::steps
