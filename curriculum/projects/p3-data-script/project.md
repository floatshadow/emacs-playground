---
title: Data script
summary: A numpy script from uv init to a tested, debugged first commit, with the project's own Python everywhere.
minutes: 75
---

You write `stats.py`, a script that summarises two weeks of temperatures in `data/temps.csv`. The project uses uv for its virtual environment, and pet makes sure every tool in Emacs uses that environment: basedpyright checks types against the numpy installed in `.venv`, the Python shell imports it, pytest and the debugger run with it. The brief is `README.md` in the preview.

:::vscode
This is the Python extension's job split among small tools. The interpreter picker is pet, which finds `.venv` by itself. Pylance is basedpyright, the open-source fork of the same checker. Black or Ruff on save is ruff through apheleia, "Run Selection in Terminal" is `C-c C-c` into a :jargon[REPL] beside the code, the Testing view is pytest in the :jargon[compilation buffer], and Run and Debug is dape, which speaks the same debug adapter protocol to the same debugpy.
:::

## The keys of this project

| Keys | Does |
|---|---|
| `C-x p t` | a terminal (eat) in the project root |
| `M-n` / `C-h .` | next diagnostic / the diagnostic at point |
| `C-c C-p` / `C-c C-c` / `C-c C-z` | start Python / send the buffer / go to the shell |
| `C-x p c` | a command in the project root; it remembers the last one |
| `M-g M-n` | next error of the last build, from anywhere |
| `C-x C-a b` / `M-x dape` | toggle a breakpoint / start the debugger |
| `M-x dape-repl` | the debugger's console |
| `C-x g` | Magit status |

## One environment

uv keeps the project's Python and packages in `.venv`, next to `pyproject.toml`; `uv run` runs a command in it, for instance in the terminal :try[C-x p t] opens in the project root. pet looks up from each Python file for such an environment and tells the other packages about it, so there is nothing to activate and nothing to select. The mode line's *Pet* says it is on.

:::aside{title="The debugger's single keys"}
After a dape command, single keys act on the debugger: `n` next line, `s` step in, `o` step out, `c` continue, `q` quit. The echo area lists them. Any other key ends that, so typing in the code works as before.
:::

Each task saves a checkpoint when it starts; the list on the left restores one. When every task is done, **Again, without hints** runs the whole project once more against the clock.

::steps
