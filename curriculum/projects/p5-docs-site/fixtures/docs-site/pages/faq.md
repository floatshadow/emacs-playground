# FAQ

## Where are the totals kept?

In `~/.local/share/tally/history`, one line per run. Your `.tallyrc`
can move them elsewhere.

## Can I count several files at once?

Yes: tally prints a seperate line for each file and a total at the end.
See [Install](instal.md) for the settings that change the output.

## Why do I not recieve a total for piped text?

Standard input has no name, so tally cannot remember it. Save the text
to a file first.
