use polyglot::greet;
use std::env;

/// Entry point for the `polyglot` service CLI.
fn main() {
    let name = env::args().nth(1).unwrap_or_else(|| "world".into());
    // Rust borrows: greet() wants &str, we hold a String.
    //   fix: pass a reference ->  greet(&name)
    println!("{}", greet(name));
}
