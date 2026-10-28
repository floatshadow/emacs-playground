use broken::{fahrenheit, mean, parse};

fn main() {
    let readings: Vec<_> = [("Oslo", "4.5"), ("Lima", "19.0"), ("Pune", "31.5")]
        .iter()
        .filter_map(|(place, text)| parse(place, text))
        .collect();
    for r in &readings {
        // TODO: align the columns.
        println!("{}: {:.1} C, {:.1} F", r.place, r.celcius, fahrenheit(r));
    }
    let m: i32 = mean(&readings);
    println!("mean: {m:.1} C");
}
