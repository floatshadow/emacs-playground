# polyglot

One small service in four languages: a Rust CLI greets, a Python tool
scores latency, an OCaml file proves a function terminates, and a Typst
report shows the numbers. Project P6 builds it for real; Parts 2 and 3
use it as a second project to switch to.

- `src/` Rust (the build is broken on purpose: see main.rs)
- `tools/score.py` Python
- `proof/term.ml` OCaml
- `docs/report.typ` Typst
