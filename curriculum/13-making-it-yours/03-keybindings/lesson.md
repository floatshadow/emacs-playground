---
title: Keybindings
summary: Bind a command to a key, check what a key runs, build a prefix map of your own with which-key labels, and make a key repeat without its prefix.
minutes: 15
---

A key binding is an entry in a :jargon[keymap]: this key runs that command. The global map holds keys that work everywhere; each mode has a map of its own that wins over the global one in its buffers. Changing a binding is one line of Lisp, and `C-h k` always tells you the truth about what a key does now.

:::vscode
`keybindings.json` becomes a few lines in `user-lisp/my-keys.el`. VS Code's chords such as Ctrl+K Ctrl+C are Emacs's prefix keys; here you make a prefix of your own and decide what follows it. "when" clauses become mode maps: `keymap-set` in `rust-ts-mode-map` binds a key in Rust buffers only.
:::

## One key at a time

`M-x keymap-global-set` asks for a key (press it) and a command (type its name), and binds it at once, for this session. In a file the same is `(keymap-global-set "C-c d" #'duplicate-line)`. For one mode: `(keymap-set markdown-mode-map "C-c d" #'duplicate-line)`. :try[C-h k] followed by the key shows what runs; `C-h b` lists every binding in the buffer.

Which keys are free? `C-c` followed by a letter is reserved for you: Emacs and well-behaved packages never bind those. `C-c` followed by anything else, and the function keys `F5` to `F9`, belong to modes and packages.

## A prefix map of your own

A prefix map groups your commands under one prefix, the way `C-x r` groups registers. The starter file `user-lisp/my-keys.el` defines `my-map` with `defvar-keymap`, puts it on `C-c u`, and names it for :jargon[which-key], so pressing `C-c u` and waiting shows *my commands* and their keys. Add a key with `(keymap-set my-map "c" #'count-words)`. Older configurations build the same thing with `define-prefix-command`, which makes a prefix command whose keymap you then fill with `keymap-set`.

## Keys that repeat

`repeat-mode` (lesson 3.2; on from the start) lets a key repeat without its prefix: `C-x o o o`. A keymap made with `:repeat t` does the same for your commands:

```elisp
(defvar-keymap my-repeat-map
  :repeat t
  "d" #'duplicate-line)
```

After `C-c u d`, each further `d` duplicates again, until you press another key.

:::aside{title="defvar-keymap and C-M-x"}
`defvar-keymap` defines its keymap only once: evaluating the form again with `M-x eval-buffer` keeps the old map. `C-M-x` on the form defines it anew. Adding keys with `keymap-set` lines, as below, avoids the question.
:::

::steps
