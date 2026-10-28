---
title: Debugging with dape
summary: Debug a Rust program with dape and codelldb, set a breakpoint, step in and out, watch a value, and find the line that is wrong.
minutes: 15
---

dape is Emacs's client for the Debug Adapter Protocol, the debugger side of what LSP is for language servers: the same adapters VS Code uses, driven from Emacs. The image has three: codelldb for Rust, debugpy for Python, and ocamlearlybird for OCaml bytecode.

:::vscode
Run and Debug (F5) is `M-x dape`, which offers the configurations for the current buffer's language, like launch.json without the file. F9 toggles a breakpoint: `C-x C-a b`. F10, F11 and Shift+F11 (step over, into, out) are `C-x C-a n`, `C-x C-a s` and `C-x C-a o`, and F5 to continue is `C-x C-a c`. The Variables, Watch and Call Stack views are dape's info windows on the right, and the Debug Console is *dape-repl* at the bottom.
:::

## Breakpoints and sessions

`C-x C-a` is dape's prefix. :try[C-x C-a b] toggles a :jargon[breakpoint] on the current line, shown as `B` in the margin; `C-x C-a B` removes them all. `M-x dape` asks which configuration to run and offers the right one: `codelldb-rust` debugs the crate's `target/debug` binary, so build first with `C-c C-c`. The session opens its windows: the source with an arrow `=>` at the current line, values and the call stack on the right, the REPL and the program's output at the bottom.

## Stepping

| Keys | Does |
|---|---|
| `C-x C-a n` | next line, stepping over calls |
| `C-x C-a s` | step into the call on this line |
| `C-x C-a o` | step out, back to the caller |
| `C-x C-a c` | continue to the next breakpoint or the end |
| `C-x C-a q` | quit: end the session and close its windows |

After one `C-x C-a` key, repeat-mode keeps the map active: `n n n` steps three lines, and any other key ends it. In *dape-repl* the same commands are words (`next`, `step`, `out`, `continue`), or just their first letter and `RET`.

## Values

The right-hand windows show the local variables of the current frame: `RET` on a `+` expands a value, and `RET` on a name adds it to the watch list. After a step out, `[return value]` shows what the function returned. :try[C-x C-a w] on a name adds it to the watch list (and removes it again), so you can follow it through every step.

The program in `/work/lsp/temps` should print the average of four readings, 20.50 °C, but prints 21.17. Find out why by stepping, without reading `lib.rs` first.
