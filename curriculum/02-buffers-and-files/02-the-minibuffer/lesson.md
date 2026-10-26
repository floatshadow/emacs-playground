---
title: The minibuffer
summary: Complete names with TAB, run commands by name, reuse anything you typed before from the history, and give commands a numeric argument.
minutes: 10
---

Every time Emacs needs an answer, it asks in the :jargon[minibuffer], the last line of the screen. File names, buffer names, commands, search strings, replacements: one input line with the same keys everywhere. Learn it once and every prompt becomes familiar.

:::vscode
The command palette's input box is the minibuffer, but it is not only for commands: Ctrl+P, Ctrl+G, the search box and every "enter a name" dialog are all the same line. Arrow-up history is `M-p`. VS Code has nothing like `C-u`: a number or a variant given to the next command.
:::

## Completion

`TAB` completes. Type the start of a name and press it: Emacs fills in as much as is certain and, if several names fit, lists them in a *Completions* window. Type a little more and press `TAB` again. :try[M-x] uses this for command names: `count-w` and `TAB` becomes *count-words*. The list closes by itself when you finish.

`RET` accepts what is in the prompt. `C-g` cancels the whole command, so a prompt you opened by mistake costs nothing.

## History

Every prompt remembers what you entered, and the playground keeps that :jargon[history] across restarts. In a prompt, :try[M-p] brings back the previous entry and `M-n` goes forward again; the up and down arrows do the same. `M-r` searches the history: type part of an old entry and `RET`. Re-running a long command or reopening a file from yesterday is two keys.

## Arguments

:try[C-u] before a command gives it a :jargon[prefix argument]. With digits it is a count: `C-u 40 =` types forty equals signs, `C-u 3 C-x o` moves three windows on. Without digits it asks for a variant: `C-u C-SPC` jumps back to the previous mark instead of setting one, and `C-u M-x` passes the argument to the command you run by name. The help for each command (`C-h f`) says what its argument does.

:::aside{title="Nested prompts"}
A few keys open a second prompt from inside the first, `M-r` among them. Emacs shows the depth as *[2]* at the start of the line. `C-g` leaves the innermost prompt; `ESC ESC ESC` leaves them all.
:::

::steps
