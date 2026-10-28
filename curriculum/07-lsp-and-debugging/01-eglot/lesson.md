---
title: Eglot
summary: Let a language server read your code through Eglot, then read documentation, jump to definitions and references, rename across files and search symbols.
minutes: 15
---

A :jargon[language server] is a program that understands one language: it parses the whole project, knows every type and where each name is defined and used. Editors talk to it over a standard protocol, LSP. :jargon[Eglot], built into Emacs, is the client: it starts the right server when you open a file of a project and turns its answers into ordinary Emacs features. The servers are the same as VS Code's: rust-analyzer for Rust, ocaml-lsp for OCaml, basedpyright for Python.

:::vscode
IntelliSense is the same server here. F12 Go to Definition is `M-.`, and Alt+Left back is `M-,`. Shift+F12 Find All References is `M-?`. F2 Rename Symbol is `C-c l r`, Ctrl+. Quick Fix is `C-c l a`, Ctrl+T Go to Symbol in Workspace is `C-c l s`. Hover documentation appears in the echo area as you move, and in full with `C-c l h`.
:::

## Connected

In this course Eglot starts by itself for Rust, OCaml and Python files that belong to a project. The mode line then shows `[eglot:greeter]`, the project the server manages, and `Flymake[0 0]`, the number of errors and warnings it reports (lesson 7.2). While the server reads the project for the first time, which takes a few seconds, it may not know everything yet. Elsewhere, `M-x eglot` starts it by hand.

## Reading and moving

As the cursor rests on a name, :jargon[eldoc] shows what the server knows about it in the echo area: the signature, the type. :try[C-c l h] opens the full documentation in an *eldoc* window, which `q` closes.

:::gui-only
In the GUI frame `C-c l h` shows the documentation in a box next to the cursor instead (eldoc-box); moving the cursor closes it.
:::

:try[M-.] jumps to the definition of the name at point, in whatever file it is, and :try[M-,] comes back. :try[M-?] lists every place the name appears, grouped by file, in the minibuffer: `C-n` and `C-p` move through the list and preview each place, typing narrows it like any completion (Part 4), and `RET` visits one. Lists like this are :jargon[xref] lists; `M-.` uses one too when a name has several definitions.

The grey annotations the server adds, such as `: String` after a variable or `names:` before an argument, are :jargon[inlay hints]: they are not in the file, and the cursor skips over them.

## Changing

:try[C-c l r] renames the symbol at point everywhere it is used, in every file of the project, including files that are not open; the server knows which `greet` is which, so a function in another crate with the same name is left alone. :try[C-c l a] asks for code actions at point: the fixes and refactorings the server offers, like the lightbulb. :try[C-c l s] searches the symbols of the whole project by name, with a preview as you move through them.

The crate `/work/lsp/greeter` has a function `greet`, used from `main.rs`, from `greet_all` and from a test.
