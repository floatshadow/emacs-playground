/// The greeting for `name`, repeated `times` times.
fn greeting(name: &str, times: usize) -> String {
    format!("Hello, {name}! ").repeat(times)
}

fn main() {
    let times: usize = "3";
    println!("{}", greeting("box", times));
}
