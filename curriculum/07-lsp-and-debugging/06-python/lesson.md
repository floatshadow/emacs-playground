---
title: Python
summary: Let pet find the project's virtual environment, fix code with basedpyright's code actions, explore in a Python REPL fed from the buffer, and run pytest through compile.
minutes: 15
---

Python needs to know which interpreter and which packages a project uses. uv keeps them in the project's :jargon[virtual environment], `.venv`. pet finds it when you open a file, so everything that runs Python for this buffer agrees: the basedpyright language server, the REPL, and the tools you run.

:::vscode
The Python extension's interpreter picker is automatic here: pet looks for `.venv`, uv, poetry, pipenv or conda the way the extension does, and the mode line says `Pet`. Pylance is basedpyright, an open-source fork of the same type checker. The Python REPL with Run Selection is `C-c C-p` and `C-c C-c`. Testing's run button is pytest through `M-x compile`, with failures as links. ruff formats on save (lesson 7.3).
:::

## The environment

`M-x pet-verify-setup` shows what pet found for the current buffer: the project root, the virtual environment, and the interpreter the REPL will run, here `/work/lsp/tips/.venv/bin/python3`. When a project has no environment yet, `uv sync` in a shell (`M-!`) creates it from `pyproject.toml`.

## Code actions

basedpyright checks types as strictly as Pylance in strict mode, so it reports names it cannot resolve and values whose type it cannot know. `C-c l a` on a problem offers its fixes: `import math` for a module used without an import, or a `# pyright: ignore` comment when you know better.

## The REPL

:try[C-c C-p] starts a Python REPL for the project in another window. From the source, :try[C-c C-c] sends the whole buffer (definitions only; code under `if __name__ == "__main__":` does not run), `C-c C-r` sends the region, and `C-c C-z` moves to the REPL. There, `RET` runs a line, `M-p` brings back earlier input, and `TAB` completes names. Edit a function, `C-c C-c` again, and the REPL has the new version.

## pytest

`M-x compile` with `uv run pytest -q` runs the tests in the project's environment; a failure line such as `tests.py:5: AssertionError` is a link for `M-g M-n` (lesson 6.1).

The project `/work/lsp/tips` splits a restaurant bill. Both of its tests fail.
