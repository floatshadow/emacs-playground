---
title: Key menus with hydra
summary: Open a menu of keys that stays on screen, bind it to your prefix, group its keys by what they do, and choose which keys close it.
minutes: 15
---

A prefix map (lesson 13.3) waits for one more key, then forgets. A :jargon[hydra] is a menu of keys that stays: it lists its keys at the bottom of the frame, each key (a :jargon[head]) runs a command, and the menu waits for the next key until you close it. You will open a starter menu, put it on `C-c u m`, sort its keys into columns of your own and decide which keys close it.

:::vscode
The Command Palette groups commands by category (View:, File:, Git:), and chords such as Ctrl+K Ctrl+C put keys under a first key. A hydra does both: one key opens your menu, its columns are the categories you choose, and it shows every key, so there is nothing to remember or type. The VSpaceCode extension brings the same idea to VS Code. Unlike a chord, a head can repeat: `o o o` walks through the windows.
:::

## A menu that stays

The starter file `user-lisp/my-hydras.el` defines `my-menu` with `pretty-hydra-define`: a list of options, then the columns, each a title followed by a list of heads `("KEY" COMMAND "hint")`. Two of its four columns:

```elisp
(pretty-hydra-define my-menu
  (:title "My menu (q closes it)" :quit-key "q")
  ("Windows"
   (("o" other-window "other window")
    ("3" split-window-right "split right"))
   "Buffers"
   (("b" switch-to-buffer "switch buffer" :exit t)
    ("f" find-file "open a file"))))
```

The menu is a command like any other, `my-menu/body`: `M-x my-menu/body` opens it in a small window above the echo area, which goes away when the menu closes. Each head becomes a command too, named after the menu: `my-menu/other-window` is what the ticker shows when you press `o` in it. `:toggle t` on a head that turns a mode on and off adds `(*)` or `( )` after its hint: whether the mode is on in this buffer.

To open it with a key, bind it in your prefix map: `(keymap-set my-map "m" '("my menu" . my-menu/body))`. A string paired with the command is the label which-key shows after `C-c u`. Saving the file runs nothing: `M-x eval-buffer`, or `C-M-x` with the cursor inside one form, applies it, and evaluating `pretty-hydra-define` again replaces the menu. Once bound, :try[C-c u m] opens it.

The file's first line says `no-byte-compile: t`: Emacs compiles `user-lisp/` before the packages are available, and `pretty-hydra-define` comes from a package (pretty-hydra, which builds on hydra).

## Red, blue and the way out

Each head has a colour that says what happens after it, and the menu's options can set one for all of them:

| Colour | After one of its keys | After any other key |
|---|---|---|
| red (the default) | stays open | closes, and the key does its usual job |
| blue (`:exit t` on a head) | closes | closes, and the key does its usual job |
| pink (`:color pink`) | stays open | the key does its usual job, and the menu stays |
| amaranth (`:color amaranth`) | stays open | the key is ignored, with a warning |

A mouse click counts as any other key: clicking into Emacs closes a red menu. Leave red the keys you press several times in a row, such as moving between windows or toggling. Make blue the keys that finish what you opened the menu for, such as opening a file or switching buffers; `:color blue` in the options makes every head blue. `q` closes this menu (its `:quit-key`), and :try[C-g] closes any hydra, whatever its colour. Rescue me closes it too. Escape alone does not: in the terminal it waits for another key (it starts the Meta keys), and which-key lists them under the menu; `ESC ESC ESC` (lesson 0.2) closes both.

## Your own groups

A column is a title over a list, so regrouping is moving a line. `C-a C-k C-k` cuts a whole line, line break included, and `C-y` pastes it above the line the cursor is on. Save, evaluate, and the next `C-c u m` shows the new layout. Name the columns the way you think: Windows, Buffers, Toggles, and Mine for your own commands (`("h" my-dated-header "dated heading")` from lesson 13.4 belongs there). A head's key works only while its menu is open, so it never clashes with the keys outside.

The last head of a column ends with the parentheses that close the column. Move a line from the middle, or carry those parentheses along. A lost one shows when you save: saving re-indents the file (format on save, lesson 7.3), so the lines below shift right, and evaluating fails with "End of file during parsing". Press `C-/` until the line is back; the first one undoes the re-indenting. A head pasted after a column's closing parentheses is outside the column: evaluating fails with "Eager macro-expansion failure", and the old menu stays. Cut it again and paste it between two heads.

## defhydra

`pretty-hydra-define` builds on hydra's own `defhydra`, whose hint you write yourself as a docstring: `_k_` marks the key `k`, and the docstring starts with a newline (without it, hydra stops with "Not enough arguments for format string"). The starter's `my-sizes`, on `C-c u s`, resizes windows with the arrow keys:

```elisp
(defhydra my-sizes (:color pink :hint nil)
  "
 Window size: _<left>_ narrower  _<right>_ wider  _q_ done"
  ("<left>" shrink-window-horizontally)
  ("<right>" enlarge-window-horizontally)
  ("q" nil))
```

`:hint nil` leaves out the automatic list of heads, since the docstring names them. It is pink, so you can type while it is open, which also makes it easy to forget: while its line is on screen, the arrows resize instead of moving the cursor. `:pre` and `:post` run code when a hydra opens and closes; `:timeout 5` closes it after five idle seconds.

:::aside{title="hydra, transient and repeat maps"}
Magit's menus are :jargon[transient] menus, built into Emacs: keys that take arguments and remember values, defined with `transient-define-prefix`. They take more to write and suit commands with options; a hydra suits a handful of keys you press often. A repeat map (lesson 13.3) is a hydra without the menu. Inside a hydra, `repeat-mode` stands aside (the ui module arranges it), so `o o` stays in the menu instead of switching to `C-x o`'s repeat map.
:::

::steps
