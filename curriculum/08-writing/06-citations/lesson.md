---
title: Citations with citar
summary: Keep references in one BibTeX file, cite them from Typst and from LaTeX with the same key, and open a reference's notes.
minutes: 10
---

A `.bib` file is a list of references, each with a key such as `knuth1984`. Typst, LaTeX, Markdown with pandoc and Org all read the same file, so one bibliography serves every document. citar is the picker: it lists the references by author, year and title, and inserts the right citation syntax for the file you are in.

:::vscode
Citation pickers such as the Citation Picker for Zotero insert keys from a list; here `C-c C-x @` does it, with the same completion as `M-x`, in Typst, LaTeX and Markdown alike. The `.bib` file itself gets a major mode, bibtex-mode, with templates for new entries and a formatter.
:::

## The bibliography

`refs.bib` opens in bibtex-mode. Each entry is `@type{key, field = {value}, ...}`. :try[C-c C-c] tidies the entry the cursor is in: one field per line, the `=` signs aligned, braces and commas made consistent. Saving the file tidies every entry the same way. `C-c C-b` asks for an entry type and inserts a template with its fields; `C-c C-e` and a letter go straight to one, `C-c C-e C-a` for an article.

## Citing

In a Typst, LaTeX or Markdown buffer, :try[C-c C-x @] opens the reference list. Type any part of an author, title, year or key to narrow it, then `RET` to cite one reference, or `TAB` on each of several and `RET` at the end. citar writes what the file type expects:

| File | Inserted |
|---|---|
| Typst | `@knuth1984 @lamport1994` |
| LaTeX | `\cite{knuth1984,lamport1994}` |
| Markdown | `[@knuth1984; @lamport1994]` |

Typst finds the file through `#bibliography("refs.bib")`, usually at the end of the document, and LaTeX with biblatex through `\addbibresource{refs.bib}` in the preamble and `\printbibliography` where the list goes. Both number the references and print the list when you build.

## Notes and files

`M-x citar-open` picks a reference and offers what belongs to it: its PDF, its links and your notes. Here, notes are files named after the key in `notes/`, next to the `.bib` file; when there are none yet, citar offers to create them.

:::aside{title="RefTeX can cite too"}
In LaTeX buffers RefTeX also cites, with `C-c [`. citar does the same in every format, which is why this course uses it.
:::

The files are `refs.bib`, `report.typ` and `paper.tex` in `/work/writing`.

::steps
