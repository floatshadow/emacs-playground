---
title: Pipe a region through any tool
summary: Send the region to a shell command, see its output or replace the region with it, and insert a command's output at point.
minutes: 10
---

Every program on the system that reads text and writes text is an editing command you already have. Select some lines, hand them to `sort`, `jq` or `pandoc`, and take back what comes out. Emacs does not need a package for JSON or for table conversion when a command-line tool already does it.

:::vscode
In VS Code each of these is an extension or a command palette entry: Sort Lines, Format Document for JSON, a Markdown-to-Org converter if you find one. Here it is one key, :try[M-|], followed by any shell command. Anything you can type in a terminal pipe works, including your own scripts.
:::

## Look first, then replace

:try[M-|] runs a command with the region as its input and shows the output: in the echo area when it is short, otherwise in a buffer called *Shell Command Output*. The buffer you are editing does not change, so it is a safe way to try a command.

With a prefix, `C-u M-|` replaces the region with the output. That is the version you use once the command is right. If the output is wrong, `C-/` puts the region back.

The region is what you selected, newline included. Select whole lines, from the start of the first one to the start of the line after the last (`C-a`, `C-SPC`, then down), so the command sees complete lines.

## Inserting output

:try[M-!] runs a command without any input and shows its output; `C-u M-!` inserts the output at point instead. It is the quickest way to put a date, a file list or a version number into a document.

:::aside{title="Which shell"}
The command runs in your shell, in the buffer's directory (`default-directory`), so relative paths are relative to the file you are editing. In a remote file (Part 12), it runs on the remote machine.
:::

The exercise is an Org file of raw material. You will sort a list, pretty-print a JSON blob and convert a Markdown table to an Org table, all in place, and stamp the file with today's date.

::steps
