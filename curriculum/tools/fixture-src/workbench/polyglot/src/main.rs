use polyglot::{Sample, describe, greet, worst};

fn main() {
    println!("{}", greet("team"));
    let rust = Sample { name: "rust".into(), p50: 0.4, p99: 1.1 };
    let python = Sample { name: "python".into(), p50: 3.2, p99: 9.8 };
    let ocaml = Sample { name: "ocaml".into(), p50: 0.9, p99: 2.7 };
    println!("{}", describe(rust));
    println!("{}", describe(python));
    println!("{}", describe(ocaml));
    if let Some(w) = worst(&[rust, python, ocaml]) {
        println!("slowest: {}", w.name);
    }
}
