# data-lab

The brief: `stats.py`, a small Python script that summarises two weeks of
daily temperatures (`data/temps.csv`) with numpy, tested with pytest.

```
$ uv run python stats.py
{'mean': 23.64..., 'max': 31.4, 'min': 18.2, 'range': 13.19...}
```

1. `uv init --bare`, then `uv add numpy` and `uv add --dev pytest`.
2. `load` and `summarize` in `stats.py`, checked by basedpyright.
3. Explore the data in a Python shell beside the code.
4. ruff formats and sorts imports on save.
5. A pytest test, run in the compile window.
6. Stop at a breakpoint with dape.
7. Commit.
