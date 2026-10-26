//! Rectangle helpers used by the report.
//! Keep the functions in this order: area, perimeter, describe.
// FIXME: delete this line once the functions are in order.

pub fn describe(w: f64, h: f64) -> String {
    format!("{w} x {h}: area {}, perimeter {}", area(w, h), perimeter(w, h))
}

pub fn area(w: f64, h: f64) -> f64 {
    w * h
}

pub fn perimeter(w: f64, h: f64) -> f64 {
    2.0 * (w + h)
}
