---
title: Embark
summary: Act on whatever is at point or highlighted in a list (a file, a buffer, a URL, a symbol), and export a list of search results into a buffer of its own.
minutes: 10
---

Everything so far picks a candidate and does the one thing the command was for: `C-x C-f` opens a file, `C-x b` switches to a buffer. :jargon[embark] adds the other things. :try[C-.] means "act on this": on the highlighted candidate in a list, or on whatever is at point in a buffer (a file name, a URL, a symbol). Embark works out what kind of thing it is and waits for one key naming the action.

:::vscode
`C-.` is the right-click context menu, from the keyboard and inside every list: delete a file straight from the open-file prompt, close a buffer from the switcher, copy a link from the text. "Open in search editor" is `C-. E`: it turns any list of results into a buffer you can keep.
:::

## Acting

After `C-.` the prompt starts with *Act*. One key picks the action; which keys exist depends on the kind of target:

| Target | Keys after `C-.` |
|---|---|
| file | `d` delete, `r` rename, `c` copy, `=` ediff with another file |
| buffer | `k` kill (asks first), `o` show in another window |
| URL | `w` copy it, `b` open it in a browser, `e` read it in eww |
| identifier in code | `o` occur in this buffer, `r` find references, `C-s` search for it |
| Lisp symbol | `h` its help |
| any | `w` copy, `E` export, `S` collect |

You do not need to remember them: `C-h` after `C-.` lists every action for this target, and you pick one by name. Destructive actions ask first. After an action in a prompt, the prompt closes; `C-g` at the *Act* prompt changes nothing.

In a prompt, `C-;` runs the default action for the kind of candidate, which in most prompts is what `RET` does; in buffers `C-;` stays iedit (lesson 1.7). `C-h B` lists every key binding of the current buffer through the same kind of list.

## Exporting

`C-. E` on a list exports all of its candidates to a normal buffer of the matching kind. Ripgrep results become a grep buffer, where `RET` visits a match and `M-g M-n` walks them; files become a Dired listing; buffers an ibuffer. `C-. S` collects the candidates into a plain list buffer instead. Part 11 edits an exported grep buffer to rename across a whole project.

::steps
