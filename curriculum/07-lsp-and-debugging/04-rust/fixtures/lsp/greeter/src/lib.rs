//! Greetings for one person or a whole list.

/// A greeting for `name`.
pub fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

/// One greeting per name, in order.
pub fn greet_all(names: &[&str]) -> Vec<String> {
    names.iter().map(|name| greet(name)).collect()
}
