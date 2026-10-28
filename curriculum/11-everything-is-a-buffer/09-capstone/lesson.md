---
title: "Capstone: a pipeline with no new package"
summary: Rename a concept across Rust, Python, JSON and Typst, build, fix the errors with a macro over next-error, and commit by hunk, using only what earlier lessons taught.
minutes: 15
---

This lesson adds nothing new. The `polyglot` project in `/work/workbench/polyglot` (a copy for this lesson, not the one in `/work/projects`) measures latency in three languages, and the team has renamed the 99th percentile from *p99* to *tail*. The rename touches Rust, Python, JSON, Typst and Markdown; the build has a mistake of its own; and someone left a debugging line in the working tree that must not be committed.

:::vscode
In VS Code this is Replace in Files, the Problems panel, a manual fix three times, and Stage Selected Ranges in the diff view. Here every step is a buffer handed to the next package, and the repeated fix is one macro that walks the error list.
:::

## The plan

1. **Search and export:** `M-s r` from a file of the project, `C-.` `E` (lessons 11.3, 11.4).
2. **Rename:** `C-x C-q`, replace, `C-c C-c` in the grep buffer (lesson 11.2).
3. **Edit and build:** the source above, the build below, `C-c C-c` (lesson 3.8's first recipe).
4. **Macro over next-error:** a macro whose first key is `M-g M-n` fixes the error it lands on; replaying it walks the rest (lessons 1.6 and 11.3).
5. **Stage by hunk:** in Magit, `TAB` expands a file's diff, `s` and `u` stage and unstage the hunk under the cursor (lesson 5.1).
6. **Commit:** `c c` and `C-c C-c`.

A macro that starts with :try[M-g M-n] is a useful shape whenever a build reports the same mistake in several places: each replay moves to the next location before editing, so `F4` once per error, or `C-u 0 F4` to repeat until next-error runs out, finishes the job.

The steps below say what to achieve, not which keys to press; the hints and Watch it are there if you need them.

::steps
