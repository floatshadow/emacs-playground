//! Word tallies: how often each word appears in a text.

use std::collections::HashMap;

/// The words of `text`, lower-cased, without punctuation at either end.
pub fn words(text: &str) -> Vec<String> {
    text.split_whitespace()
        .map(|w| w.trim_matches(|c: char| !c.is_alphanumeric()))
        .filter(|w| !w.is_empty())
        .map(str::to_lowercase)
        .collect()
}

/// How many words `text` has.
pub fn total(text: &str) -> usize {
    words(text).len() as u32
}

/// Whether `text` has no words at all.
pub fn is_blank(text: &str) -> bool {
    return words(text).len() == 0;
}

/// How many times each word appears.
pub fn tally(text: &str) -> HashMap<String, usize> {
    let counts = HashMap::new();
    for word in words(text) {
        *counts.entry(word).or_insert(0) += 1;
    }
    counts
}

/// The `n` most frequent words, most frequent first, ties in
/// alphabetical order.
pub fn top(text: &str, n: usize) -> Vec<(String, usize)> {
    let mut pairs: Vec<(String, usize)> = tally(text).into_iter().collect();
    pairs.sort_by(|a, b| b.1.cmp(&a.1).then(b.0.cmp(&a.0)));
    pairs.truncate(n);
    pairs
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn counts_words_without_punctuation() {
        assert_eq!(total("Hello, world! Hello."), 3);
        assert!(is_blank(" -- "));
    }

    #[test]
    fn ties_are_alphabetical() {
        let expected = vec![("a".to_string(), 2), ("b".to_string(), 2)];
        assert_eq!(top("b a b a c", 2), expected);
    }
}
