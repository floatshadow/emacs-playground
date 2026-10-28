fn main() {
    println!("{}", greeter::greet("world"));
    for line in greeter::greet_all(&["Ada", "Grace", "Linus"]) {
        println!("{line}");
    }
}
