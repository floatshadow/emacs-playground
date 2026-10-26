---
title: Replace
summary: Replace text match by match with a live preview, preserve case, and rename with a regular expression and a capture group.
minutes: 12
---

Replacing in Emacs is a conversation. You say what to replace and with what, and Emacs stops at each match and asks. Usually you answer `y` a few times and `!` for the rest; the point is that you see every change before it happens, so a careless pattern cannot quietly damage the file.

:::vscode
Ctrl+H is `M-%`. The find and replace fields are one prompt at the bottom: type the text, `RET`, the replacement, `RET`. Instead of Replace and Replace All buttons you answer at each match: `y` replace, `n` skip, `!` replace all the rest. Regex mode is a separate key, `C-M-%`, and `$1` is written `\1`.
:::

## Match by match

:try[M-%] starts with the text to find. As you type, every match in the buffer is highlighted, so you see the damage before you do it. `RET` moves on to the replacement, and a second `RET` starts at the first match after the cursor. Then:

- `y` replaces this match and moves on; `n` skips it;
- `!` replaces this and every remaining match without asking;
- `^` goes back to the previous match, `.` replaces this one and stops;
- `q` or `RET` stops; what is done stays done, and `C-/` undoes it one replacement at a time.

Case is handled for you. Lower-case search text matches any case, and the replacement follows the match: *Colour* becomes *Color*, *COLOUR* becomes *COLOR*. Starting at the top with `M-<` makes sure no match is behind you.

## With a regular expression

:try[C-M-%] does the same with a :jargon[regexp]. Parentheses capture, and in Emacs they are written with backslashes: `\(` and `\)`. In the replacement, `\1` is what the first group matched and `\&` is the whole match. So `p\([0-9]+\)_ms` matches *p50_ms* and *p99_ms*, and `p\1_millis` turns them into *p50_millis* and *p99_millis*, while *p99_budget* does not match at all. If your keyboard will not send `C-M-%`, `M-x query-replace-regexp` is the same command.

:::aside{title="Backslashes"}
Emacs regexps treat a plain `(` as a literal parenthesis and `\(` as a group, the opposite of most languages. When a regexp does not match, a missing backslash is the usual reason.
:::

::steps
