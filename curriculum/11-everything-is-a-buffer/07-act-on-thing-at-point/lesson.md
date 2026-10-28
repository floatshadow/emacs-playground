---
title: Act on the thing at point
summary: Open paths, follow symbols, browse URLs, jump to Dired and read help from whatever is under the cursor, in any buffer, with Embark and the find-file prompt.
minutes: 10
---

The third kind of glue from lesson 11.1 is "the thing at point". Emacs has shared functions that answer "what is under the cursor?": a file name, a URL, a symbol, an email address. Any command can ask them, so a path printed by a build script, a link in a commit message and a name in a grep result can all be acted on in place, without retyping them.

:::vscode
Ctrl+click opens a link or a path in the terminal, F12 follows a symbol, and the right-click menu offers actions for a selection. :try[C-.] (embark-act) is all of these in one key, in every buffer: it works out what the cursor is on and shows the actions for that kind of thing.
:::

## One key, many kinds of things

`C-.` shows a menu of single-key actions for the target, and `RET` in it runs the default action. Which actions appear depends on what Embark found:

| Thing at point | Where you meet it | Default action (`RET`) | Other keys |
|---|---|---|---|
| a file name | build output, a README, a grep line | open it | `j` Dired on it, `o` other window, `r` rename |
| a symbol in code | any source buffer | its definition, through xref and Eglot | `r` its references, like `M-?` |
| a URL | commit messages, comments, mail | browse it | `e` eww, `w` copy it |
| an Emacs Lisp symbol | init files, `*scratch*` | find its definition | `h` help |

When the cursor could mean two things, `C-.` picks the most specific one; pressing `C-.` again in its menu cycles to the next target. `C-h` in the menu lists every action with its key.

## Without Embark

Two built-in ways read the thing at point too. In `C-x C-f`, :try[M-n] offers the file name under the cursor: Emacs fills the "future history" of a prompt with likely answers, and for a file prompt that is the name at point (the ffap package, find file at point). And `M-.` follows the symbol at point through xref, whatever the language.

In the playground `browse-url` opens links in eww, a web browser inside Emacs, since the sessions have no browser of their own; `q` leaves it. The sessions have no internet either, so actions that fetch documentation from the web, such as a crate's page on docs.rs, are left out here; Emacs Lisp's built-in help stands in for them. Lesson 7.1's `M-.` and lesson 2.6's Dired are what Embark runs: it adds no new features, it only connects the target to the command.

::steps
