use ledger::{format_cents, parse_line, total};
use std::io::{self, BufRead};

fn main() {
    let expenses: Vec<_> = io::stdin()
        .lock()
        .lines()
        .map_while(Result::ok)
        .filter_map(|line| parse_line(&line))
        .collect();
    println!("total {}", format_cents(total(&expenses)));
}
