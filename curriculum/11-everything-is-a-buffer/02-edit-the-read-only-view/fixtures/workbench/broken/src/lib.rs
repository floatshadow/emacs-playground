//! Temperature readings: parse, convert and summarise.

/// One reading in degrees Celsius.
pub struct Reading {
    pub place: String,
    pub celsius: f64,
}

// TODO: accept "21.5C" as well as "21.5".
pub fn parse(place: &str, text: &str) -> Option<Reading> {
    let celsius = text.trim().parse().ok()?;
    Some(Reading { place: place.to_string(), celsius })
}

pub fn fahrenheit(r: &Reading) -> f64 {
    r.celsius * 9.0 / 5.0 + 32.0
}

// TODO: return None for an empty slice instead of NaN.
pub fn mean(readings: &[Reading]) -> f64 {
    let total: f64 = readings.iter().map(|r| r.celsius).sum();
    total / readings.len() as f64
}
