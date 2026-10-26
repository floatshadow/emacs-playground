use hello::shout;

#[test]
fn shouts() {
    assert_eq!(shout("hi"), "HI");
}

// TODO: test greet once it moves into the library.
