//! Temperature readings: conversions and summaries.

/// Degrees Fahrenheit for `celsius`.
pub fn to_fahrenheit(celsius: f64) -> f64 {
    celsius * 9.0 / 5.0 + 32.0
}

/// Whether water freezes at `celsius`.
pub fn is_freezing(celsius: f64) -> bool {
    return celsius <= 0.0;
}

/// The readings in a CSV text: one number per line after the header.
pub fn parse_readings(text: &str) -> Vec<f64> {
    text.lines()
        .skip(2)
        .filter_map(|line| line.trim().parse().ok())
        .collect()
}

/// The mean of `readings`, or `None` when there are none.
pub fn average(readings: &[f64]) -> Option<f64> {
    if readings.is_empty() {
        return None;
    }
    let total: f64 = readings.iter().sum();
    let count = readings.len() as f64;
    Some(total / count)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn freezing_point() {
        assert!(is_freezing(0.0));
        assert!(!is_freezing(0.5));
    }

    #[test]
    fn average_of_two() {
        assert_eq!(average(&[1.0, 3.0]), Some(2.0));
    }
}
