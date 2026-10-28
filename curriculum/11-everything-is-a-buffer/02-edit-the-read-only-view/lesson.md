---
title: "C-x C-q: edit the read-only view"
summary: Rename files by editing a Dired listing, fix typos across files from a grep buffer, edit through an occur list, and change a file inside a tarball, all with one habit.
minutes: 12
---

`C-x C-q` toggles a buffer between read-only and editable. On a file, that is all it does. On a listing that *shows* other things (files in a folder, matches in files, lines of a buffer, members of an archive), the packages behind those listings made it mean the same thing: edit the view as text, then write the edits back to what it shows. Packages that share no code agree on one key; occur is the exception that proves the rule, with `e` for the same job.

:::vscode
Renaming many files at once, replacing across files from the search panel, or changing a file inside a zip without extracting it each needs its own feature or extension in VS Code. Here they are one habit: open the list, `C-x C-q`, edit with any command you know (replace, macros, multiple cursors), apply.
:::

## Four listings, one habit

| Listing | Made by | Edit with | Apply | Cancel |
|---|---|---|---|---|
| a folder | Dired | `C-x C-q` (:jargon[wdired]) | `C-c C-c` | `C-c ESC` |
| matches in files | grep, ripgrep exported with Embark | `C-x C-q` (wgrep) | `C-c C-c` | `C-c C-k` |
| matching lines of a buffer | occur, `M-s o` | `e` | `C-c C-c` | `C-/` in the file |
| members of a tarball or zip | tar-mode, archive-mode | `RET` opens a member | `C-x C-s` twice | `C-x k` |

In wdired, only the file names are editable; the rest of each line stays read-only, so a replace over the whole buffer cannot damage the dates or sizes. The same goes for wgrep: the `file:line:` prefix of each match is protected and only the matched text can change. In the playground wgrep also saves every file it changed, so `C-c C-c` is the last step.

## Regexp replace on file names

:try[C-M-%] is replace with a regular expression. A group `\(...\)` catches part of each match and `\1` puts it back in the replacement, so `IMG_\([0-9]+\)\.JPG` → `trip-\1.jpg` keeps each photo's number. At each match `y` replaces, `n` skips, and `!` replaces all the rest (lesson 1.5). If your keyboard will not send `C-M-%`, `M-x query-replace-regexp` is the same command.

## Archives are listings too

`C-x C-f` on a `.tar` file shows its members in tar-mode; a `.zip` or `.jar` opens in archive-mode. `RET` on a member opens it as a buffer named after the member and the archive, `index.md (site.tar)`. Saving that buffer writes it back into the archive's buffer, which then shows as modified until you save the archive itself.

The workbench has all four: six camera-named photos, docs with a typo, a task list, and a small website packed in `site.tar`.

::steps
