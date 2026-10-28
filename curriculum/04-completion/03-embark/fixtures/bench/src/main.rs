mod config;
mod report;
mod stats;

fn main() {
    let samples = vec![12.0, 15.5, 11.2, 48.9, 13.1, 14.7, 90.3, 12.8];
    let summary = report::summarise("search", &samples);
    println!("{}", report::render_line(&summary));
}
