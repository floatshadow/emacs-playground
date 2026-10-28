---
title: "Remote compile, Eshell and LSP"
summary: Build on the box from your own Emacs, jump to a remote error, ask the box's rust-analyzer, and open a shell on the box in its folder.
minutes: 15
---

Lesson 12.1 edited a file on the box. The rest of Emacs comes along: a command that runs a program runs it *where the buffer's folder is*. In a buffer whose folder is `/ssh:box:~/greeter/`, a build runs `cargo` on the box, `next-error` opens the box's files, Eglot starts the box's `rust-analyzer`, and Eshell opens with the box as its working directory. No setting says "remote"; the :jargon[default directory] does.

:::vscode
In VS Code the terminal, tasks and extensions run on the remote side once you connect with Remote-SSH. Here the same happens per buffer: every buffer has a folder, local or remote, and processes start there. Your keys, completion and layout stay local.
:::

## Compile on the box

In a Rust buffer :try[C-c C-c] runs `cargo build` in the crate's root (lesson 7.4). From a remote buffer the build runs on the box, with the box's toolchain, and the output arrives in `*compilation*` here. The error lines name the crate's files, so :try[M-g M-n] (`next-error`) opens `/ssh:box:~/greeter/src/main.rs` at the error, exactly as it would locally (with Eglot on, `M-g n` walks the buffer's diagnostics instead). `M-x compile` runs any command the same way, on the box, in the buffer's own folder; `C-x p c` runs it in the project's root.

## Eglot on the box

Opening a remote Rust file starts Eglot as it does for a local one (lesson 7.1), but the server it runs is the box's `rust-analyzer`, talking to your Emacs over the SSH connection. It sees the box's crates and toolchain, not yours. The mode line shows `[eglot:greeter]` once it is ready, and then `M-.`, `M-,`, `M-?` and the Flymake diagnostics work as usual. The first start takes a few seconds while the server indexes.

:::aside{title="When the box is slow"}
A language server over a slow link can make typing lag. `M-x eglot-shutdown` stops it for the project; compile and `next-error` still work without it.
:::

## Eshell on the box

Eshell starts in the current buffer's folder, so from a remote buffer it starts on the box: the prompt reads `/ssh:box:~/greeter/src $`, external programs such as `cargo` or `id` run there, and Eshell's own commands (`cd`, `ls`, `find-file`) understand remote paths. A few of those built-ins answer from Emacs, not from the box: `whoami` prints your local name. A `*` in front (`*whoami`) runs the program instead. `M-x eshell` returns to the Eshell you already have; `C-u M-x eshell` starts a new one here. `C-x p e` starts one in the project's root, on the box too.

::steps
