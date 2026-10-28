---
title: The one abstraction
summary: See why a Dired listing, a Magit log, build output and a shell all take the same search, copy and macro keys, and name the three mechanisms that let packages compose.
minutes: 10
---

Ten parts in, you have used packages written by different people over thirty years: Dired, Magit, compile, Eshell, consult, Embark. None of them was designed with the others in mind, and yet they work together. This part shows how, one pattern per lesson. The reason fits in a sentence: every one of them produces or consumes a :jargon[buffer].

:::vscode
In VS Code each view is its own widget: you cannot search the Source Control list with Ctrl+F, record a macro in the Explorer, or copy ten commit hashes from the Timeline. In Emacs those views are buffers of text with a :jargon[major mode] on top, so the keys you learned for files work in all of them.
:::

## Three kinds of glue

1. **Major modes over text.** A Dired listing is the output of `ls` in a buffer; a Magit log is the output of `git log`. The mode adds keys (`w` in Dired copies a file name, `n` in Magit moves to the next commit), but the text stays text: `C-s`, `C-SPC`, `M-w` and macros apply as everywhere.
2. **:jargon[Hooks].** Every mode runs a hook when it starts, every save runs `after-save-hook`, every build runs `compilation-finish-functions`. A package plugs into another by adding a function to its hook, without either knowing the other (lesson 11.8).
3. **Thing at point.** Commands ask "what is under the cursor?" through shared functions: a file name, a URL, a symbol. Embark's `C-.` (Part 4) is the general form of this (lesson 11.7).

## Text keys in special buffers

In the listing on the left, :try[C-s] searches the file names as you type, exactly as it searches a file; `RET` stops on the match. The same works in `*compilation*`, in a help buffer and in the Magit log you open later in this lesson. `C-SPC` and `M-w` copy any part of a line; `w` is Dired's shortcut for "copy this file name".

A keyboard macro records keys, not commands of one package, so a macro recorded in Dired can switch windows, paste into a Markdown file and come back. :try[F3] starts recording and `F4` stops, then replays (lesson 1.6). Whatever the listing, the recipe is the same: put the cursor on the first item, record one round that ends on the next item, replay.

The checklist on the right, `notes/release.md`, collects what goes into a release: three file names from `assets/` and three commits from the log.

::steps
