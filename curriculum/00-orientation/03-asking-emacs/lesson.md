---
title: Asking Emacs
summary: Find out what any key does, read any command's documentation, list the keys of the current mode, and open the source of any command.
minutes: 12
---

Emacs documents itself while it runs. Every key runs a named :jargon[command], every command has documentation, and the documentation links to the Lisp that implements it. Once you know four help keys, you never need to remember anything you can ask for.

:::vscode
Hovering for documentation and searching the command palette both live under the `C-h` prefix. `C-h k` is "what does this shortcut do?", `C-h f` and `C-h v` are hover help for any command or setting, and `C-h b` is the keyboard shortcuts editor, read-only. Unlike an extension's docs, the source is one keypress away.
:::

## What does this key do?

:try[C-h k] asks for a key, then explains it: the command it runs, where it is bound, and its documentation. The *Help* window takes the cursor, so `q` closes it when you are done. Asking about a key never runs it, so you can ask about anything, even `C-x C-c`.

## What is this command, this setting, this mode?

:try[C-h f] describes a function or command by name and :try[C-h v] a variable, which is what Emacs calls a setting. Both prompts complete names with `TAB`. :try[C-h o] takes either. :try[C-h m] describes the buffer you are in: its major mode, every minor mode that is on, and the keys each adds.

When you do not know what to ask, :try[C-h ?] lists every help command on one page; `b` in it is the same as `C-h b`, every key that works right now.

## Read the source

At the top of a help page, the file name where the command is defined is a link. `s` in the help window opens the source at the definition. This is the real code that runs when you press the key, and you can read it, search it and, later in the course, change it while Emacs runs.

:::aside{title="Links in help"}
Help pages are full of links: to related commands, to variables, to the manual. `TAB` moves from link to link, `RET` follows one, `l` goes back and `r` forward again, like a browser.
:::

::steps
