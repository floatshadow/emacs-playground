---
title: "eat: a terminal in a buffer"
summary: Run a real terminal inside Emacs with eat, switch between its input modes, and copy from its output with Emacs keys.
minutes: 12
---

Some programs want a real terminal: test runners with colours, `htop`, `less`, an interactive installer. eat is a :jargon[terminal emulator] written in Emacs Lisp: a shell runs in a buffer, full-screen programs draw in it, and because it is a buffer you can search and copy its output with the keys you already know.

:::vscode
The integrated terminal (Ctrl+`) is `C-x p t`, which opens a terminal in the project's root, or `M-x eat` in the current file's folder. Toggling the panel is ``C-` `` (popper, lesson 3.5). The difference: selecting text with the mouse is replaced by switching the terminal to Emacs keys, where `C-s`, `C-SPC` and `M-w` work on the output like on any buffer.
:::

## Three input modes

A terminal and Emacs both want your keys, so eat has modes that decide who gets them:

| Mode | Keys go to | Enter it with |
|---|---|---|
| semi-char (the default) | the program, except `C-x`, `M-x`, `C-g`, `C-h`, `C-c` | `C-c C-j` |
| emacs | Emacs: the output is a read-only buffer to move, search and copy in | `C-c C-e` |
| char | the program, every key (for programs that need `C-x` themselves) | `C-c M-d`; `M-RET` leaves it |

In semi-char mode `C-r` is the shell's history search and `C-a` goes to the start of the command line, as in any terminal. Switch to emacs mode with :try[C-c C-e] and the same keys move and search in the output; :try[C-c C-j] gives them back to the shell. `C-c C-k` in emacs mode kills the program.

The mode line shows the mode in brackets: *Eat[semi-char]*, *Eat[emacs]* or *Eat[char]*. *POP* at its left means popper counts the terminal as a :jargon[pop-up], so ``C-` `` hides and shows it like VS Code's panel.

## Leaving and coming back

`C-x b` leaves the terminal running in the background, like a hidden panel; `C-x p t` again goes back to the same one, and `C-u C-x p t` opens another. Typing `exit` closes the shell and its buffer.

:::aside{title="vterm"}
vterm is the heavier alternative: the terminal is libvterm, written in C, so programs that redraw a lot of screen are faster. It is installed too (`M-x vterm`), with `C-c C-t` as its copy mode. eat needs no compiled module and also works inside Eshell (lesson 6.3), which is why the playground uses it.
:::

The crate `/work/terminals/tally` has a failing test. You will run the tests in a terminal and copy the failing test's name into the FIXME comment at the top of `src/lib.rs`.
