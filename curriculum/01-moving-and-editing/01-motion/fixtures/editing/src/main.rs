//! A small command-line tool that greets people and prints a report.
//! Lesson 1.1 marks five spots with (1) to (5) in the comments.

mod latency;
mod report;
mod shapes;

use std::env;

// (1) the end of the next line
fn greet(name: &str) -> String {
    // (2) just after the word Hello on the next line
    format!("Hello, {name}!")
}

/// Greets everybody named on the command line, or the world.
fn greet_all(names: &[String]) -> Vec<String> {
    if names.is_empty() {
        return vec![greet("world")];
    }
    names.iter().map(|n| greet(n)).collect()
}

// (3) the start of the next line, where fn main begins
fn main() {
    let args: Vec<String> = env::args().skip(1).collect();
    let greetings = greet_all(&args);
    // (4) just after the closing brace of the next loop
    for line in &greetings {
        println!("{line}");
    }
    let total = greetings.len();
    let widest = greetings.iter().map(|g| g.len()).max().unwrap_or(0);
    // (5) the next line is line 35: reach it by number
    println!("{}", report::summary(total, widest));
    println!("{}", shapes::describe(3.0, 4.0));
    println!("{}", greet("reader"));
}
