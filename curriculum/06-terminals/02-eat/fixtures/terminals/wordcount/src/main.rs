mod count;

use std::env;
use std::fs;

fn main() {
    let path = env::args().nth(1).unwrap_or_else(|| String::from("poem.txt"));
    let text = fs::read_to_string(&path).expect("cannot read the file");
    let limit: usize = "3";
    for (word, n) in count::top_words(&text, limit) {
        println!("{n:>3}  {word}");
    }
}
