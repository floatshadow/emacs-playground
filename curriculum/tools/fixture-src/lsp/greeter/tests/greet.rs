#[test]
fn greets_by_name() {
    assert_eq!(greeter::greet("Ada"), "Hello, Ada!");
}

#[test]
fn greets_everyone() {
    assert_eq!(greeter::greet_all(&["A", "B"]).len(), 2);
}
