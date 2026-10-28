---
title: Async shell commands and Dired integration
summary: Run one-off shell commands without leaving your buffer, in the background with M-&, on marked files in Dired, and on the region.
minutes: 10
---

Not every command needs a terminal. When you want one answer from the shell, or want a slow job to run while you keep editing, Emacs runs the command for you and puts the output where it is useful: the echo area, a buffer, or back into your text.

:::vscode
Where you would open the terminal, type one command and close it again, use `M-!`. Run in Terminal for a long job is `M-&`, which leaves the job running while you edit. Right-click a selection of files in the Explorer and run a command on them is Dired's `!` on marked files.
:::

## One command, now or in the background

:try[M-!] asks for a shell command, runs it in the current buffer's folder and waits. Short output appears in the echo area; longer output in the *Shell Command Output* buffer. :try[M-&] runs the command in the background instead: you can keep typing, and the output collects in *Async Shell Command*, which opens at the bottom once there is something to show. A second `M-&` while the first still runs gets a buffer of its own.

## On files in Dired

In Dired, `!` runs a command on the marked files, or on the file under the cursor when none is marked, and `&` does the same in the background. The files are added at the end of the command, so `mogrify -format jpg` with three marked pictures runs `mogrify -format jpg forest.png harbour.png sunset.png`. A `*` alone in the command stands for all the files at that spot, and `?` runs the command once per file with the file in its place. Dired does not refresh by itself afterwards: `g` shows the new files.

`% m` marks every file whose name matches a :jargon[regexp], such as `\.png$`.

## On the region

With a prefix, the output replaces what the command read: `C-u M-|` sends the region through a command and puts its output in place of the region. `C-x h C-u M-| sort RET` sorts a whole buffer. Part 10 builds on this: any program becomes an editing command.

The folder `/work/terminals/images` has three pictures to convert, a slow `report.sh`, and a list of names to sort.
