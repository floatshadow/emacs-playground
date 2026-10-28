---
title: Rust
summary: Build, test and lint a Rust crate from its buffers with cargo through compile, read clippy's lints in Flymake, and run tests with cargo-nextest.
minutes: 15
---

Everything from the last lessons comes together for Rust: rust-analyzer through Eglot for navigation and diagnostics, rustfmt on save, and cargo through `compile`, so every error and warning cargo prints is a link that `M-g M-n` follows.

:::vscode
rust-analyzer is the same server, with the same inlay hints and the same check on save (here clippy instead of cargo check). Its Run Test code lens becomes `C-c C-t`, the cargo tasks are `C-c C-c` (build), `C-c C-r` (run) and `C-c C-k` (clippy), and the Testing view's runner can be `cargo nextest run` through `M-x compile`.
:::

## cargo from the buffer

In a Rust buffer:

| Keys | Runs, in the crate's root |
|---|---|
| :try[C-c C-c] | `cargo build` |
| :try[C-c C-r] | `cargo run` |
| :try[C-c C-t] | `cargo test` |
| :try[C-c C-k] | `cargo clippy` |

Each opens *compilation* at the bottom. A failing test prints `panicked at src/lib.rs:40:9`, an error or a lint `--> src/lib.rs:10:5`; both are links for `M-g M-n`.

## clippy in Flymake

clippy is Rust's linter: it knows hundreds of patterns that compile but could be simpler or are likely bugs. rust-analyzer runs it each time you save and its lints appear in Flymake as warnings with the lint's name, such as `clippy [needless_return]`; `C-h .` shows the explanation and a link to the lint's page. `C-c C-k` runs it in the compile window instead, to walk all of them with `M-g M-n`.

## Inlay hints

The grey `: f64` after a variable and `celsius:` before an argument are inlay hints: types and parameter names the server shows but the file does not contain. `M-x eglot-inlay-hints-mode` hides or shows them in the current buffer.

:::aside{title="cargo-nextest"}
cargo-nextest runs each test in its own process, in parallel, and prints a summary that is easier to read on large suites. It is installed: `M-x compile RET` with `cargo nextest run`. Its failures point at the same `src/lib.rs:40:9` locations, so `M-g M-n` works.
:::

The crate `/work/lsp/temps` converts and averages temperature readings. It has two tests and one clippy warning.
