---
title: Incremental search
summary: Search as you type, step through matches in both directions, search for a symbol or a regular expression, and list every match with occur.
minutes: 12
---

Searching is the fastest way to move in Emacs. You do not open a find box and then navigate to a result: the cursor jumps while you type, and when you see where you want to be, you stop. Most experienced users move by search more than by any motion key.

:::vscode
Ctrl+F is `C-s`, and F3 is pressing `C-s` again. Shift+F3 is `C-r`. There is no box: the search prompt is the bottom line, and `RET` leaves the cursor at the match while `C-g` takes it back to where you started. The match counter, *2/4*, is in the prompt. Find All in a panel is `M-s o`.
:::

## As you type

:try[C-s] starts an :jargon[isearch]. Each character you type narrows it, and the cursor sits on the first match after where you started. `C-s` again goes to the next match, `C-r` to the previous one; past the last match, one more `C-s` wraps to the top. `DEL` removes the last character you searched for.

Two ways out, and they mean different things. `RET` stops the search and leaves the cursor where it is. `C-g` cancels it and puts the cursor back where the search began, which makes a search a safe way to look something up. The mark is set where you started, so `C-x C-x` gets you back later too.

## Smarter matching

Lower-case text matches any case; type a capital and the search becomes exact. `M-c` toggles this while searching. :try[M-s .] searches for the symbol under the cursor as a whole word, so `greet` does not match `greet_all`. :try[C-M-s] searches for a :jargon[regexp], such as `let [a-z]+ =`. `M-s SPC` toggles whether a space matches any run of whitespace.

## Every match at once

:try[M-s o] (occur) lists every line that matches a regexp in an *Occur* window, with line numbers. `RET` on a line goes there. From any buffer, `M-g M-n` visits the next match in the list and `M-g M-p` the previous one: the same keys walk through build errors and project-wide search results later in the course.

::steps
