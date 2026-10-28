mod seed;
mod usr;

use usr::get_usr;

fn main() {
    let db = seed::seed();
    println!("{} users", db.len());

    for name in ["carol", "zoe"] {
        match get_usr(&db, name) {
            Some(u) => println!("{} is {}", u.name, u.age),
            None => println!("{name}: no such user"),
        }
    }
}
