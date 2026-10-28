---
title: How init.el is organised
summary: Read how your configuration starts, find out where a setting comes from, and change three settings three ways, Customize, your own file and a module, applying each without a restart.
minutes: 12
---

Everything you have used in this course is configured in `/work/emacs.d`, and it is yours: from this part on you change it. Emacs reads it at startup in a fixed order, and knowing that order tells you where each kind of change belongs.

:::vscode
VS Code has `settings.json` and the Settings editor on top of it. Emacs has both too: Customize is the editor (it writes `custom.el`), and your own Lisp files are the `settings.json`, with the difference that a file of Emacs Lisp can do anything, not only set values.
:::

## The startup order

1. `early-init.el`: frame and garbage-collection settings, before anything is drawn.
2. Your `user-lisp/` folder: Emacs 31 compiles each file there and runs the forms marked with a `;;;###autoload` line.
3. `init.el`: makes packages available, reads `modules.el` (the list of modules the course turned on, in order) and loads each file from `modules/`.
4. `custom.el`: what Customize saved.

Open `init.el` and read it: it is short, and it is the same for everyone. The modules hold the settings, one file per topic, each with a header that lists its keys by lesson.

## Where does a value come from?

:try[C-h v] describes a variable: its value here, its default, whether it is buffer-local, and whether you customized it. `C-h o` does the same for any symbol, function or variable. To find which file sets it, search the configuration: `M-s r` in `/work/emacs.d`, or `C-x p g`.

## Three ways to change a setting

- **Customize** (`M-x customize-variable`): a form with fields and buttons; `C-x C-s` applies what you changed and saves it to `custom.el`. Good for trying an option's values. Less good for anything you want to read, comment or copy later, because Customize owns that file.
- **Your own file** in `user-lisp/`. The starter file `my-settings.el` loads itself after the modules, so a `setq` there wins over a module's. `M-x eval-buffer` applies the file now.
- **A module**: the course's files are yours too. Change one when you disagree with its choice, and keep the change small, so you can still tell your edits from the course's.

`C-x C-e` after a form, `C-M-x` inside one, or `M-x eval-buffer` applies Lisp at once. To see a clean start, the Session page has a *Restart Emacs* button.

:::aside{title="When a change breaks the start"}
A module that fails to load is reported and the others still load, so a typo never leaves you without Emacs. The *Warnings* buffer names the file; `emacs --debug-init` shows the exact form.
:::

::steps
