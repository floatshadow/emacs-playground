use std::env;

use hello::shout;

fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

fn main() {
    // TODO: accept several names and greet each of them.
    let name = env::args().nth(1).unwrap_or_else(|| "world".into());
    println!("{}", greet(&name));
    println!("{}", shout("and goodbye"));
}
