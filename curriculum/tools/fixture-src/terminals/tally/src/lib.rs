//! Summary statistics for a list of measurements.

// FIXME: the failing test is

/// The mean of `values`, or `None` for an empty list.
pub fn mean(values: &[f64]) -> Option<f64> {
    if values.is_empty() {
        return None;
    }
    Some(values.iter().sum::<f64>() / values.len() as f64)
}

/// The median of `values`, or `None` for an empty list.
pub fn median(values: &[f64]) -> Option<f64> {
    if values.is_empty() {
        return None;
    }
    let mut sorted = values.to_vec();
    sorted.sort_by(|a, b| a.total_cmp(b));
    Some(sorted[sorted.len() / 2])
}

/// The largest value minus the smallest, or `None` for an empty list.
pub fn range(values: &[f64]) -> Option<f64> {
    let max = values.iter().copied().reduce(f64::max)?;
    let min = values.iter().copied().reduce(f64::min)?;
    Some(max - min)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mean_of_three() {
        assert_eq!(mean(&[1.0, 2.0, 6.0]), Some(3.0));
    }

    #[test]
    fn median_of_odd_count() {
        assert_eq!(median(&[3.0, 1.0, 2.0]), Some(2.0));
    }

    #[test]
    fn median_of_even_count() {
        assert_eq!(median(&[4.0, 1.0, 3.0, 2.0]), Some(2.5));
    }

    #[test]
    fn range_of_four() {
        assert_eq!(range(&[4.0, 1.0, 3.0, 2.0]), Some(3.0));
    }

    #[test]
    fn empty_is_none() {
        assert_eq!(mean(&[]), None);
        assert_eq!(median(&[]), None);
        assert_eq!(range(&[]), None);
    }
}
