---
title: Which-key and the key ticker
summary: Explore a prefix map with the which-key popup instead of memorising it, run any command by name, and read the ticker and the Where am I panel.
minutes: 8
---

Nobody memorises Emacs's keys up front. You learn a handful, and discover the rest when you need them: a :jargon[prefix key] shows you what can follow it, and `M-x` runs anything by name. This lesson is about those two habits, and about the playground's panels that watch your keys.

:::vscode
The keyboard shortcut reference card is built in and context sensitive. Press any :jargon[prefix key], wait a moment, and the :jargon[which-key] popup lists every key that can follow, with the command it runs. `M-x` is the command palette: every command has a name, and the name is how you run anything without a key.
:::

## Explore a prefix

Press :try[C-x r] and wait. The popup lists the commands under it: rectangles, :jargon[registers] and :jargon[bookmarks] live here, each one key away. Press a key to run that command, or `C-g` to put the popup away. `C-h` after any prefix gives the same list as a help buffer you can search.

A bookmark is a named place in a file that survives restarts; a register is a one-letter slot for a position, kept for this session. Both get their own lesson in Part 2; here they are just things to find in a popup.

## Run a command by name

Every key runs a named command, and :try[M-x] runs a command by its name. The name is often easier to remember than the key: `display-line-numbers-mode` turns line numbers on and off, and you do not need a key for something you do once a week. Run it twice and the numbers come and go.

## Watch your keys

The :jargon[ticker] under the screen shows each key sequence you press and the command it ran, newest first, with a short explanation. When a key does something surprising, the ticker tells you what it was. When it says *undefined*, the key does nothing here.

The *Where am I* panel on the right reads Emacs's state after every command. When a prompt is open, a region is active, a macro is recording or an extra window has appeared, it says so in one line and gives the one key that gets you out. *Did you mean* translates VS Code habits: press `C-s` hoping to save, and a card explains what happened instead.

::steps
