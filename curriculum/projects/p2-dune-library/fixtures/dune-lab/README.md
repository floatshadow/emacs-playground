# dune-lab

The brief: `wordstat`, an OCaml library that splits text into words,
built with dune and tested with expect tests.

```ocaml
# Wordstat.words "The cat, the hat.";;
- : string list = ["the"; "cat"; "the"; "hat"]
```

1. `dune init proj wordstat --kind=lib --inline-tests --ppx ppx_expect`
2. A first `words` in `lib/wordstat.ml`, tried in utop beside it.
3. The expect tests in `brief/words_tests.ml`, then a `words` that
   passes them.
4. A `summary` type, its cases filled in by Merlin.
5. A house style in `.ocamlformat`.
6. Documentation comments, built into HTML by odoc.
7. Commit.
