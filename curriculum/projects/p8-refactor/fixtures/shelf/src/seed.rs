use crate::usr::{User, add_usr};

// The seed data: forty users, added one call at a time.
//
// Every call below still uses the old add_usr helper. The new style
// pushes a User directly:
//
//     db.push(User::new("alice", 31));
//
// which reads better and lets add_usr go away.

pub fn seed() -> Vec<User> {
    let mut db = Vec::new();
    add_usr(&mut db, "alice", 31);
    add_usr(&mut db, "bob", 25);
    add_usr(&mut db, "carol", 42);
    add_usr(&mut db, "dave", 37);
    add_usr(&mut db, "erin", 29);
    add_usr(&mut db, "frank", 51);
    add_usr(&mut db, "grace", 33);
    add_usr(&mut db, "heidi", 27);
    add_usr(&mut db, "ivan", 45);
    add_usr(&mut db, "judy", 39);
    add_usr(&mut db, "karl", 22);
    add_usr(&mut db, "laura", 48);
    add_usr(&mut db, "mallory", 35);
    add_usr(&mut db, "niaj", 26);
    add_usr(&mut db, "olivia", 41);
    add_usr(&mut db, "peggy", 30);
    add_usr(&mut db, "quentin", 58);
    add_usr(&mut db, "rupert", 44);
    add_usr(&mut db, "sybil", 36);
    add_usr(&mut db, "trent", 28);
    add_usr(&mut db, "ursula", 53);
    add_usr(&mut db, "victor", 32);
    add_usr(&mut db, "walter", 47);
    add_usr(&mut db, "xena", 24);
    add_usr(&mut db, "yusuf", 38);
    add_usr(&mut db, "zara", 43);
    add_usr(&mut db, "amir", 34);
    add_usr(&mut db, "bea", 23);
    add_usr(&mut db, "cyril", 55);
    add_usr(&mut db, "dora", 40);
    add_usr(&mut db, "emil", 21);
    add_usr(&mut db, "fay", 46);
    add_usr(&mut db, "gus", 50);
    add_usr(&mut db, "hana", 57);
    add_usr(&mut db, "igor", 49);
    add_usr(&mut db, "jana", 52);
    add_usr(&mut db, "kofi", 54);
    add_usr(&mut db, "lena", 59);
    add_usr(&mut db, "milo", 56);
    add_usr(&mut db, "nora", 60);
    db
}
