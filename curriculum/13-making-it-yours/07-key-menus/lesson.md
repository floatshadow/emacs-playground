---
title: Key menus with transient
summary: Open a menu of keys that stays on screen, bind it to your prefix, group its keys by what they do, and choose which keys close it.
minutes: 15
---

A prefix map (lesson 13.3) waits for one more key, then forgets. A :jargon[transient] menu is a menu of keys that stays: it lists its keys in a window at the bottom of the frame, each key (a :jargon[suffix]) runs a command, and the menu can wait for the next key until you close it. Magit's menus (Part 5) are transient menus, and transient is built into Emacs. You will open a starter menu, put it on `C-c u m`, sort its keys into groups of your own and decide which keys close it.

:::vscode
The Command Palette groups commands by category (View:, File:, Git:), and chords such as Ctrl+K Ctrl+C put keys under a first key. A transient menu does both: one key opens your menu, its groups are the categories you choose, and it shows every key, so there is nothing to remember or type. The VSpaceCode extension brings the same idea to VS Code. Unlike a chord, a key can repeat: `o o o` walks through the windows.
:::

## A menu that stays

The starter file `user-lisp/my-menus.el` defines `my-menu` with `transient-define-prefix`: a name, an empty argument list, a docstring, then the groups. A group is a vector: its title, then one list per suffix, `("KEY" "description" COMMAND)`. A vector of groups shows them side by side. Two of its four groups:

```elisp
(transient-define-prefix my-menu ()
  "Open my menu: windows, buffers, toggles; q closes it."
  [["Windows"
    ("o" "other window" other-window :transient t)
    ("3" "split right" split-window-right :transient t)]
   ["Buffers"
    ("b" "switch buffer" switch-to-buffer)
    ("f" "open a file" find-file :transient t)]]
  [("q" "close" transient-quit-one)])
```

The menu is a command like any other: `M-x my-menu` opens it in a window at the bottom, which goes away when the menu closes. A suffix runs its command as if you had typed its usual key, in the buffer you were in, so the ticker shows `other-window` when you press `o` in the menu.

To open it with a key, bind it in your prefix map: `(keymap-set my-map "m" '("my menu" . my-menu))`. A string paired with the command is the label which-key shows after `C-c u`. Saving the file runs nothing: `M-x eval-buffer`, or `C-M-x` with the cursor inside one form, applies it, and evaluating `transient-define-prefix` again replaces the menu. Once bound, :try[C-c u m] opens it.

## What a key does after it runs

Each suffix says what happens to the menu after its command runs, and the menu says what happens to keys it does not list:

| Key | After it runs |
|---|---|
| a suffix (the default) | the menu closes |
| a suffix with `:transient t` | the menu stays open |
| a key the menu does not list (the default) | nothing runs: *Unbound suffix*, and the menu stays |
| the same with `:transient-non-suffix 'transient--do-stay` | the key does its usual job, and the menu stays |
| the same with `:transient-non-suffix 'transient--do-leave` | the key does its usual job, and the menu closes |

Keep `:transient t` on the keys you press several times in a row, such as moving between windows or toggling. Leave it off the keys that finish what you opened the menu for, such as opening a file or switching buffers. `q` closes this menu (its `transient-quit-one` suffix), and :try[C-g] closes any transient menu, whatever its keys say; inside a menu opened from another menu, `C-g` goes back to the outer one. Rescue me closes it too. A suffix that asks a question, such as `f` for a file, hides the menu while the minibuffer waits; with `:transient t` the menu comes back once you answer.

## Your own groups

A group is a title followed by its suffixes, so regrouping is moving a line. `C-a C-k C-k` cuts a whole line, line break included, and `C-y` pastes it above the line the cursor is on. Save, evaluate, and the next `C-c u m` shows the new layout. Name the groups the way you think: Windows, Buffers, Toggles, and Mine for your own commands (`("h" "dated heading" my-dated-header)` from lesson 13.4 belongs there). A suffix's key works only while its menu is open, so it never clashes with the keys outside.

The last suffix of a group ends with the `]` that closes the group. Move a line from the middle, or carry the bracket along. A lost one shows when you save: saving re-indents the file (format on save, lesson 7.3), so the lines below shift, and evaluating fails with "End of file during parsing" or an invalid read syntax. Press `C-/` until the line is back; the first one undoes the re-indenting.

## A menu for typing

The starter's `my-sizes`, on `C-c u s`, resizes windows with the arrow keys:

```elisp
(transient-define-prefix my-sizes ()
  "Resize windows with the arrow keys; other keys still work, q closes."
  :transient-non-suffix 'transient--do-stay
  ["Window size"
   ("<left>" "narrower" shrink-window-horizontally :transient t)
   ("<right>" "wider" enlarge-window-horizontally :transient t)
   ("q" "done" transient-quit-one)])
```

Keys it does not list do their usual job and the menu stays, so you can type while it is open, which also makes it easy to forget: while its window is on screen, the arrows resize instead of moving the cursor.

:::aside{title="transient, hydra and repeat maps"}
hydra is a third-party package with the same idea: menus of keys that stay, defined with `defhydra`, where a key's colour (red, blue, pink, amaranth) says what this lesson's table says. transient comes with Emacs and also suits commands with options, which it shows and remembers, as Magit's menus do. A repeat map (lesson 13.3) is a menu without the window. Inside a transient menu, `repeat-mode` stands aside (the ui module arranges it), so `o o` stays in the menu instead of switching to `C-x o`'s repeat map.
:::

::steps
