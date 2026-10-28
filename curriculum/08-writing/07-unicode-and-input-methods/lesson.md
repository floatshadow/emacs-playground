---
title: Unicode and input methods
summary: Insert any character by name, type common symbols with C-x 8, write maths with the TeX input method and Chinese with pyim, and find out what a character is.
minutes: 12
---

Prose needs characters the keyboard does not have: dashes, arrows, Greek letters, accents, other scripts. Emacs has three ways in, from occasional to fluent: insert one character by name, type a short sequence after `C-x 8`, or switch on an :jargon[input method] that turns what you type into other characters until you switch it off.

:::vscode
There is no built-in VS Code equivalent of `C-x 8 RET`: people copy characters from the web or install an extension. Input methods replace the operating system's IME inside Emacs, so they work the same in a terminal, over SSH and in the browser.
:::

## One character

:try[C-x 8 RET] asks for a character by its Unicode name, with completion: type `almost equal` and the list narrows to `ALMOST EQUAL TO (≈)`. A hexadecimal code point such as `2248` works too. `C-x 8` followed by a short sequence types the common ones directly: `C-x 8 _ n` is an en dash (–), `C-x 8 _ m` an em dash (—), `C-x 8 ' e` é, `C-x 8 * x` ×, `C-x 8 o` °. `C-x 8 C-h` lists them all.

## Input methods

An input method stays on in the buffer until you turn it off. :try[C-\] toggles the one you used last, here TeX at first; `C-u C-\` asks which one to turn on. While one is on, the mode line starts with its indicator: `\` for TeX, `PYIM` for pyim.

With TeX on, LaTeX names become characters as you type them: `\alpha` gives α, `\to` gives →, `\le` ≤, `\in` ∈, `^2` ², `_1` ₁. Plain letters stay plain, so you can write a whole sentence of mathematics without leaving it.

pyim types Chinese from pinyin: type `nihao` and the candidates appear at the bottom, numbered: `SPC` takes the first, a digit takes another, `C-n` and `C-p` page. It works in the terminal as well as in the graphical frame.

## What is this character?

`C-x =` shows the character at point and its code in the bottom line. :try[C-u C-x =] opens a help window with everything about it: its Unicode name, the input method keys that type it, the font that draws it. `q` closes the window.

The file is `/work/writing/notation.txt`, three lines waiting to be filled in.

::steps
