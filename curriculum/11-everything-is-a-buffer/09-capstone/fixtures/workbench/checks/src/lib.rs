//! Small text helpers, and the tests that keep them honest.

/// Title-case each word: "hello world" -> "Hello World".
pub fn title_case(s: &str) -> String {
    s.split(' ')
        .map(|w| {
            let mut c = w.chars();
            match c.next() {
                Some(f) => f.to_uppercase().collect::<String>() + c.as_str(),
                None => String::new(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

/// Count the words, ignoring runs of spaces.
pub fn word_count(s: &str) -> usize {
    s.split(' ').count()
}

/// Shorten to at most `max` characters, ending with "..." when cut.
pub fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        s.chars().take(max).collect::<String>() + "..."
    }
}

/// The text between the first pair of square brackets, if any.
pub fn bracketed(s: &str) -> Option<&str> {
    let open = s.find('[')?;
    let close = s[open..].find(']')? + open;
    Some(&s[open + 1..close])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn title_case_capitalises_words() {
        assert_eq!(title_case("hello world"), "Hello World");
    }

    #[test]
    fn word_count_ignores_extra_spaces() {
        assert_eq!(word_count("one  two   three"), 3);
    }

    #[test]
    fn truncate_keeps_short_text() {
        assert_eq!(truncate("short", 10), "short");
    }

    #[test]
    fn truncate_fits_the_limit() {
        assert_eq!(truncate("a long sentence", 8).chars().count(), 8);
    }

    #[test]
    fn bracketed_finds_the_tag() {
        assert_eq!(bracketed("see [docs] here"), Some("docs"));
    }

    #[test]
    fn bracketed_handles_empty_input() {
        assert_eq!(bracketed(""), Some(""));
    }
}
