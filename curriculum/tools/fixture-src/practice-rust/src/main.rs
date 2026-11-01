use std::collections::HashMap;
use std::io::Read;

/// Prints the five most frequent words of standard input.
fn main() {
    let mut text = String::new();
    std::io::stdin()
        .read_to_string(&mut text)
        .expect("standard input is text");
    for (word, n) in tally::top(&text, 5) {
        println!("{n:>4} {word}");
    }
}
