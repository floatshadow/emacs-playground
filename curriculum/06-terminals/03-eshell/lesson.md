---
title: Eshell
summary: Use Eshell, a shell written in Emacs Lisp, where Lisp, Emacs commands, buffers and shell pipelines mix on one command line.
minutes: 15
---

:jargon[Eshell] looks like bash, but it is Emacs Lisp all the way down. `ls`, `cd`, `cp` and `grep` are Lisp functions that work the same on every machine; anything else runs as an ordinary program. Because the shell lives inside Emacs, a command line can call any Emacs function, write into a buffer, or take its arguments from a Lisp expression.

:::vscode
VS Code has nothing quite like it: imagine a terminal where `code file.txt` opens the file in the editor you are typing in, and `> clipboard` were a real place to send output. Eshell is that, plus Lisp. For programs that need a real terminal (htop, less), Eshell hands them to eat, the terminal from lesson 6.2.
:::

## A shell first

`M-x eshell` opens Eshell in the folder of the current file; `C-x p e` opens one in the project's root. Commands, pipes and globs work as you expect: `ls -S *.log | head -n 5` lists the five largest log files, biggest first (`-S` sorts by size). `TAB` completes file and command names (in shells the completion popup waits for `TAB`, so `RET` always runs the line), `C-c C-c` interrupts a running command, `M-p` and `M-n` step through earlier commands, and :try[M-r] searches them as you type, like `C-r` in bash: `RET` puts the match on the command line, where you can edit it before running it.

## Lisp on the command line

A line that starts with a parenthesis is Lisp: `(+ 1 2)` prints 3. A command name that is not a program but an Emacs function runs that function, so `find-file notes.txt` opens the file in Emacs. Three kinds of `$` fill in arguments:

| Syntax | Is replaced by |
|---|---|
| `$PWD`, `$HOME` | a variable: environment or Lisp |
| `$(length (buffer-list))` | the value of a Lisp expression |
| `${ls -S \| head -n 1}` | the output of a command |

So `find-file ${ls -S | head -n 1}` opens the biggest file in one command.

## Buffers as files

`>` redirects output into a file, and also into a buffer: `ls > #<buffer listing>` writes into a buffer called *listing*, made if needed. That buffer is ordinary text to edit, search or save.

## Aliases

`alias big 'ls -S $* | head -n 5'` defines a command `big`; `$*` stands for whatever you type after it, so `big *.log` works. Aliases are saved, and come back in every Eshell.

The folder `/work/terminals/logs` holds seven log files of different sizes.
