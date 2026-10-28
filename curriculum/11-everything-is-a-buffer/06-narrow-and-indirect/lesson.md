---
title: Narrow and indirect buffers
summary: Edit a code block of a Markdown or Org document in its language's own mode, narrow a file to one function, and keep two views of one file with an indirect clone.
minutes: 10
---

A buffer does not have to show all of a file, and a file's text does not have to live in only one buffer. Three tools build on that: edit buffers for code inside documents, :jargon[narrowing], and :jargon[indirect buffers]. Each gives a package a buffer that holds exactly the text it should work on.

:::vscode
VS Code edits a fenced code block inside the Markdown file, with whatever help the Markdown extension gives. Here `C-c '` opens the block in a buffer of its own, in the language's major mode: Rust highlighting, indentation and commands, then `C-c C-c` puts it back. Folding everything but one function is narrowing, and "Open to the side" on the same file with a different scroll position and folding is an indirect clone.
:::

## Code blocks in their own buffer

In a Markdown file, :try[C-c '] inside a fenced block (markdown-edit-code-block, through the edit-indirect package) copies the block into a new buffer in the mode its fence names: a fence that says `rust` gives rust-ts-mode. `C-c C-c` writes it back into the document, `C-c C-k` discards. In Org, `C-c '` on a source block (org-edit-special) does the same for any language Babel knows, and `C-c '` again returns (lesson 9.4).

The edit buffer has the language's major mode, so highlighting, indentation, `M-;`, tempel snippets and the mode's keys all work. It visits no file, though, and a language server needs a file inside a project: Eglot and Flymake stay off in these buffers. To have the compiler's help, keep real code in real files and narrow instead.

## Narrowing

`C-x n d` narrows to the function around the cursor, `C-x n n` to the region, and `C-x n w` widens back; Narrow in the mode line says the buffer is narrowed (lesson 9.1 did the same with `C-x n s` on an Org subtree). The buffer is still the file's buffer, narrowed, so Eglot, Flymake, apheleia and saving go on as before, while search, replace and `M-<` see only the visible part. That makes narrowing a scope for any command: a replace inside one function, a macro run to the end of a section.

## Indirect buffers

:try[C-x 4 c] (clone-indirect-buffer-other-window) makes a second buffer, `tally.el<2>`, that shares its text with the first. Edits in either show in both, but each has its own point, narrowing and even major mode. One window can stay narrowed to the function you are changing while the other shows the whole file. Kill the clone with `C-x k`; the file's buffer stays.
