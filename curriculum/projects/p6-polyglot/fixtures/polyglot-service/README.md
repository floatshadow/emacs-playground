# polyglot-service

One service, four roots, four languages:

- `service/`: the Rust CLI `latency`; it reads response times in
  milliseconds (a file, or standard input) and prints p50, p99 and mean.
- `tools/`: `score.py` turns that output into a score out of 100.
- `proof/`: an OCaml model of the percentile, with a dune test that checks
  its invariants.
- `report/`: the Typst report with the numbers.

Each root is a repository and a project of its own. `.dir-locals.el`
gives each one its build command for `C-x p c`.
