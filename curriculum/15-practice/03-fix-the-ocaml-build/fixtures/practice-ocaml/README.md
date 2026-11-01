# practice-ocaml

Lesson 15.3. The `stats` library has two type errors, so `dune build`
fails. Once it builds, `dune test` runs `test/test_stats.ml` and
compares what it prints with `test/test_stats.expected`: one line
differs. `dune fmt` formats the code as `.ocamlformat` says.
