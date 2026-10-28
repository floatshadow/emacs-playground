---
title: One list of locations
summary: Walk build errors, search matches and language-server diagnostics with the same two keys, and turn any list of candidates into such a list with embark-export.
minutes: 10
---

A build log, a search, a list of diagnostics and a list of references have one thing in common: each is a list of places in files. Emacs gives all of them one protocol, `next-error`. Any buffer that can say "here is the next location" plugs into it, and two keys walk whichever list you used last.

:::vscode
F8 and Shift+F8 walk the Problems panel; search results have their own F4; references have yet another view. In Emacs `M-g M-n` and `M-g M-p` walk all of them: compile output, grep and ripgrep results, occur, xref references and Flymake diagnostics.
:::

## The two keys

:try[M-g M-n] goes to the next location and :try[M-g M-p] to the previous one, from any buffer: the cursor lands on the spot in the file, and the list scrolls to show where you are. `` C-x ` `` is the old name for `M-g M-n` and does the same. `RET` on a line of any of these lists visits that one.

Which list? The one you used last. A new build makes `*compilation*` the current list; pressing `M-g M-n` or `RET` inside another list (a grep buffer, the Flymake list) makes that one current. So the habit is: open the list, press `M-g M-n` in it once, then keep pressing it wherever you are.

## Lists that speak the protocol

| List | Made by |
|---|---|
| `*compilation*` | `C-c C-c` in a Rust buffer, `M-x compile`, `C-x p c` |
| grep buffers | `M-x grep`, `M-x rgrep`, or any consult search exported with Embark |
| `*Occur*` | `M-s o` (lesson 1.4) |
| `*xref*` | `M-?` references (lesson 7.1) |
| Flymake diagnostics | `C-c l d` for this buffer (lesson 7.2) |

## Any search becomes a list

Consult's searches live in the minibuffer and vanish when you pick one. :try[C-.] then `E` (embark-export) keeps them: the candidates become a real buffer of the matching kind, so a ripgrep search becomes a grep buffer that next-error can walk, wgrep can edit (lesson 11.2) and you can save. Exporting files gives a Dired listing; exporting buffers gives an ibuffer list.

The `broken` crate fails to build in two places and carries three TODO comments. You will walk all three kinds of list without learning a new key.

::steps
