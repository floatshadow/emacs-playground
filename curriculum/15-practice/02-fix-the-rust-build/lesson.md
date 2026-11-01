---
title: Fix the Rust build
summary: Make a small Rust crate build without warnings, then make its tests and clippy pass, with the tools of Part 7.
minutes: 15
---

Practice: the lesson says what to make, not which keys to press, and checks only the result. Part 7's tools are all here: :try[C-c C-c] builds, Flymake underlines, the compile window links each error to its line, and `C-h .` shows the message at point.

:::vscode
The Problems panel and the cargo tasks: the same messages from the same compiler, in a buffer whose lines are links.
:::

## The brief

`tally` counts words: `cargo run < README.md` prints the five most frequent words of a file. Someone left it broken.

1. **Build.** `cargo build` stops at two errors in `src/lib.rs`. Fix them, and the warning that shows up in `src/main.rs` once the errors are gone.
2. **Test.** `cargo test` has one failing test, and `cargo clippy -- -D warnings` two lints on one line. Fix the code; the tests are right.

The checks run cargo on the saved files.

::steps
