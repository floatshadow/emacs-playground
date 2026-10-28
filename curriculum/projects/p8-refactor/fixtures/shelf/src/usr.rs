//! Users and the lookups on them.

pub struct User {
    pub name: &'static str,
    pub age: u32,
}

impl User {
    pub fn new(name: &'static str, age: u32) -> User {
        User { name, age }
    }
}

/// The old way to add a user; seed.rs still calls it everywhere.
pub fn add_usr(db: &mut Vec<User>, name: &'static str, age: u32) {
    db.push(User::new(name, age));
}

/// The user called `name`, if any.
pub fn get_usr<'a>(db: &'a [User], name: &str) -> Option<&'a User> {
    db.iter().find(|u| u.name == name)
}
