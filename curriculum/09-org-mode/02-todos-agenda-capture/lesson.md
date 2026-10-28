---
title: "TODOs, scheduling, the agenda and capture"
summary: Turn headings into tasks, tag, schedule and set deadlines, capture tasks from any buffer without leaving it, and read the week in the agenda.
minutes: 15
---

Any Org heading becomes a task when it starts with a keyword: TODO, then DONE. Give it a date and it shows up in the :jargon[agenda], a view Emacs builds from every Org file in `/work/org`. And :jargon[capture] lets you jot a task down from whatever buffer you are in, filed into `inbox.org`, with a link back to where you were.

:::vscode
Todo Tree collects `TODO` comments; Org tasks are headings with a state, dates and :jargon[tags], and the agenda is a calendar of them. Capture is the quick note you would otherwise lose: one key from any file, the note filed away, and you are back where you were.
:::

## States, tags and dates

On a heading, :try[C-c C-t] cycles the state: none, TODO, DONE. `S-<right>` and `S-<left>` step through the same states. When a task becomes DONE, Org adds a `CLOSED:` line with the time. `C-c C-q` sets tags, words between colons at the end of the heading.

`C-c C-s` schedules a task (the day you plan to work on it) and `C-c C-d` sets a deadline (the day it is due). Both ask for a date, with a calendar to look at:

| Type | Means |
|---|---|
| `+1`, `+3` | tomorrow, in three days |
| `fri`, `mon` | the next Friday, Monday |
| `2026-12-01` | that day |
| `S-<right>`, `S-<left>` | move the calendar's day |

The answer becomes a :jargon[timestamp] under the heading: `SCHEDULED: <2026-11-02 Mon>`.

:::aside{title="Shift and the arrows"}
Since Part 3, Shift and an arrow moves between windows. In Org files, the agenda and the date prompt, Org's own meaning wins where it has one (states on a heading, days on a date); elsewhere in the file, Shift and an arrow still moves to the next window.
:::

## Capture

:try[C-c c] shows the capture templates: `t` for a Task, `n` for a Note. A small window opens on the new entry in `inbox.org`; type it, then `C-c C-c` files it and puts you back, or `C-c C-k` throws it away. The entry links to the buffer you started from: `C-c C-o` on the link goes there.

Captured tasks pile up in the inbox until you move them where they belong: `C-c C-w` on one refiles it under any heading of any Org file in `/work/org`.

## The agenda

:try[C-c a] opens the agenda menu; `a` shows the week from today, with every scheduled task on its day and deadlines warning in advance. `t` lists every open TODO instead, and `m` the headings with a tag you name. The agenda is a view, not a file: in it, `n` and `p` move, `RET` visits the task, `t` changes its state in the file it lives in, `S-<right>` moves its date a day, and `q` puts your windows back.

`blog.org` is the launch plan from 9.1, now with some tasks. `about.md` and `nginx.conf` are drafts open in other buffers.

::steps
