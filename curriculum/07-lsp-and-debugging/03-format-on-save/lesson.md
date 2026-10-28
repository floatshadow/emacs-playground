---
title: Format on save with apheleia
summary: Let apheleia run each language's formatter when you save, format on demand, and turn it off for one buffer.
minutes: 8
---

A :jargon[formatter] rewrites code into its language's standard layout: indentation, spacing, line breaks, the order of imports. apheleia runs the right one every time a file is saved, in the background, and patches only the lines that changed, so the cursor and the window stay where they were.

:::vscode
editor.formatOnSave is on, with the standard formatter per language: rustfmt for Rust, ocamlformat for OCaml, ruff for Python (it also sorts imports, like isort). Format Document (Shift+Alt+F) is `C-c l f`. There is no setting to find: `M-x apheleia-mode` turns it off and on for the current buffer.
:::

## On save, and on demand

Every save you make formats: `C-x C-s`. The automatic save a few seconds after you stop typing does not, as in VS Code, so text never moves under your cursor while you type. The change shows the moment the formatter answers; apheleia stays quiet in the mode line. :try[C-c l f] formats the buffer now, without waiting for a save: it runs `M-x apheleia-format-buffer`, and where apheleia has no formatter for the language, it asks the language server, and failing that indents the buffer.

## Which formatter

The table `apheleia-mode-alist` maps each major mode to a formatter, and `apheleia-formatters` says how to run each one: :try[C-h v] `apheleia-mode-alist` shows it. Formatters read their project's settings, such as a `rustfmt.toml`, a `.ocamlformat` or the `[tool.ruff]` section of `pyproject.toml`, so a team's style applies here as it does in CI.

## Off for one buffer

`M-x apheleia-mode` turns formatting off in the current buffer, for a file whose layout you want to keep, and on again. The echo area says which. `M-x apheleia-global-mode` does it everywhere.

The folder `/work/lsp/tidy` holds three badly formatted files: `src/main.rs`, `shapes.ml` and `report.py`.
