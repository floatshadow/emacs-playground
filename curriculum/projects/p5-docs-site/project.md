---
title: Docs site
summary: Write Markdown pages with a live preview, build them into a static site, rename a term on every page at once, reorder files, fix spelling, commit page by page and deploy to the box.
minutes: 75
---

The manual of `tally`, a small word-counting tool, lives in `/work/docs-site/pages/` as four Markdown pages. `make` turns them into HTML in `_site/` with pandoc (read `build.sh`: thirty lines of shell), and the box serves `/srv/www` at `http://box/`. You will finish the manual and publish it, using only tools from earlier parts.

:::vscode
Markdown Preview is markdown-mode's live preview, Search across files plus Replace All is ripgrep exported to a buffer you edit, renaming in the Explorer is editing the Dired listing, the spell checker's quick fix is `M-$`, and deploying with an SFTP extension is a Dired copy to a `/ssh:` path. Nothing here is a plug-in for documentation sites: each step is a general tool meeting Markdown files.
:::

## Write and preview

:try[C-c C-c l] turns on the live preview: pandoc renders the page into a second window (eww, Emacs's own browser) and renders it again on every save. :try[C-c C-c p] previews once. The preview pane beside the screen shows the built page from `_site/` once task 2 has built it.

## Build in compile

:try[C-x p c] runs `make` in the project root and shows the output in `*compilation*`. `build.sh` reports a broken link as `pages/FILE.md:LINE:`, a location `compile` understands: :try[M-g M-n] jumps to it from any window.

## Change every page at once

:try[M-s r] searches the project with ripgrep. In the results, `C-.` then `E` (embark-export) turns them into a buffer of matching lines; :try[C-x C-q] makes that buffer editable (wgrep), any edit works on it (`M-%` replaces), and `C-c C-c` writes every change back to its file and saves it. The same `C-x C-q` in Dired renames files by editing their names (wdired).

:::aside{title="Why numbers in file names"}
`build.sh` orders the navigation bar by file name and drops a leading `NN-` from the URL, a convention many site generators share. Renaming `install.md` to `02-install.md` moves it in the bar and keeps `install.html` working.
:::

## Spelling, commits, deploy

jinx underlines misspellings as you type; :try[M-$] corrects the one nearest the cursor, and a digit picks a suggestion. In Magit (:try[C-x g]) `s` stages the file or hunk at point and `c c` commits what is staged, so one page per commit is a matter of what you stage. To deploy, copy the built files in Dired with `C` to `/ssh:box:/srv/www/`: TRAMP opens the connection and the copy goes over SSH.

::steps
