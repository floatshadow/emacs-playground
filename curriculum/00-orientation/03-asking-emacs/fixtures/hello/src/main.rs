use std::env;

fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

fn main() {
    let name = env::args().nth(1).unwrap_or_else(|| "world".into());
    println!("{}", greet(&name));
}
