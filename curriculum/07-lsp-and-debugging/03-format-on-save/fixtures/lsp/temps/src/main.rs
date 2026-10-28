use std::fs;

fn main() {
    let text = fs::read_to_string("readings.csv").expect("cannot read readings.csv");
    let readings = temps::parse_readings(&text);
    match temps::average(&readings) {
        Some(mean) => println!("average {mean:.2} C ({:.2} F)", temps::to_fahrenheit(mean)),
        None => println!("no readings"),
    }
}
