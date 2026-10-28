/// The area of a rectangle.
fn area(width: f64, height: f64) -> f64 {
    let perimeter = 2.0 * (width + height);
    width * height
}

fn main() {
    let mut counts = HashMap::new();
    for shape in ["square", "circle", "square"] {
        *counts.entry(shape).or_insert(0) += 1;
    }
    let total: i32 = area(3.0, 4.0);
    println!("area {total}, shapes {counts:?}");
}
