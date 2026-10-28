# cargo-lab

The brief: a command-line temperature converter in Rust, `tempconv`.

```
$ cargo run -- 100 -40
100 C = 212 F
-40 C = -40 F
converted 2 values
```

1. Scaffold the crate with `cargo new tempconv` (from Eshell) and commit
   the untouched scaffold.
2. Run it: code above, output below.
3. `c_to_f` and a loop over the command-line arguments.
4. Print how many values were converted (and fix what the compiler says).
5. A test for `c_to_f`.
6. `f_to_c`, formatted by rustfmt on save.
7. Commit the code and the test separately.
