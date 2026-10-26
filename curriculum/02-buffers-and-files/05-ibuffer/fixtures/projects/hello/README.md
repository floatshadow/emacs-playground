# hello

A tiny Rust program that greets you, then shouts goodbye.

- `cargo run` prints a greeting
- `cargo run -- Ada` greets Ada
- `cargo test` checks the shouting

The build script `build.sh` prints the same output every time; the
window lessons use it so their layouts do not depend on how long cargo
takes.
