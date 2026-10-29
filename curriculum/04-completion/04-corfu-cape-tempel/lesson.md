---
title: "Corfu, cape and tempel"
summary: Complete words, names and file paths from a popup as you type, and write code from snippets whose fields you fill in with TAB.
minutes: 10
---

Completion is not only for prompts. In a buffer, :jargon[corfu] pops up candidates under the cursor after you type two characters and pause. Where they come from is decided by the buffer's completion sources: a language server once Part 7 turns them on, and from now on the words of your open buffers and file names, added by :jargon[cape]. :jargon[tempel] inserts :jargon[snippets]: templates with fields to fill in.

:::vscode
The IntelliSense popup and snippets, with the same keys: the popup appears as you type, `RET` takes the highlighted entry, and in a snippet `TAB` jumps to the next field. Ctrl+Space is `C-M-i` here (or `TAB` after the indentation). Emmet-style abbreviations and user snippets are tempel templates, plain text files you can edit.
:::

## The popup

| Key | Does |
|---|---|
| `C-n` `C-p` | move the highlight (the arrows too) |
| `RET` | insert the highlighted candidate |
| `TAB` | complete as far as all candidates agree |
| `M-SPC` | add a space to the input to filter with a second word, as in lesson 4.1 |
| `C-g` | close the popup and keep what you typed |

Typing anything that is not part of a name (a bracket, a space) closes the popup and inserts the character as usual. Remember that while the popup is open, `RET` inserts a candidate instead of a new line: `C-g` first if you wanted the new line. In prose (Org, Markdown, plain text, commit messages) the popup does not open by itself: `C-M-i` asks for it, so `RET` at the end of a sentence always starts a new line.

`TAB` first indents the line; on a line that is already indented it completes. `C-M-i` always completes, and opens the popup at once without waiting for two characters.

## Where candidates come from

Cape adds two sources to every buffer: *Dabbrev*, the words of the buffers in the same mode (so a name defined in another Rust file completes here), and *File*, which completes a path once you have typed a `/` in it, relative to the buffer's folder. The annotation at the right of each candidate names its source.

## Snippets

:try[M-+] lists the snippets for the current mode and inserts the one you pick. In Rust, `fn` gives `fn _(_) -> _ { }` with three fields: type the name, `TAB`, the parameters, `TAB`, the return type, `TAB`, and the cursor lands in the body. `S-TAB` goes back a field. Inside a field `TAB` always moves on, even when the popup is open; `RET` takes a candidate from it. `M-*` completes a snippet name typed in the buffer instead. The snippets live in `/work/emacs.d/templates`; Part 13 adds your own.

::steps
