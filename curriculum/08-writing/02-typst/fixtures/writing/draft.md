# Why Lantern has no cache

Every build of Lantern starts from nothing. It reads every note, parses it, and writes every page again, even when only one note has changed. People are often surprised by this, and some of them write to ask whether it is a mistake. It is not: it is the simplest design that could work, and it has kept the tool small enough to read in an afternoon.

A cache would keep the parsed notes between builds. The second build would then recieve most of its work for free and only parse the notes that changed. The price is a seperate directory of state, which can go stale, which has to be cleaned, and which is definately the first thing anyone suspects when a page looks wrong.

The measurements in the report show where the time goes. Parsing is almost all of it; writing pages and copying images hardly registers. So far no one has complained untill their folder reached several thousand notes, which is why the problem never occured to us in the first year.

If you want to help, the best place to start is the parser. Send a patch, or write to the adress in the README, and tell us about your enviroment: how many notes, how large, and how long a build takes.
