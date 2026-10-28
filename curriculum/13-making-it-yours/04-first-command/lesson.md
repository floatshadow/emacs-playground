---
title: "Your first command, hooks and advice"
summary: Write a command in Emacs Lisp, test it as you go, bind it to your prefix, run it automatically from a hook, and change it with advice.
minutes: 15
---

A command is a Lisp function with an `(interactive)` line, which is what lets you run it with `M-x` or bind it to a key. You will write one that inserts a Markdown heading with today's date, bind it, have Emacs run it in every new Markdown file of a journal folder, and then change where it inserts without editing it.

:::vscode
In VS Code a new command means an extension: a project, a manifest, a build. Here it is five lines in `user-lisp/my-commands.el`, evaluated with one key, live in the editor you are typing in. Hooks are VS Code's events (`onDidOpenTextDocument`), and advice has no VS Code equivalent: it wraps any existing function, yours or Emacs's.
:::

## Try the pieces with M-:

:try[M-:] evaluates one expression and shows the result at the bottom. Build a command from expressions you have tried: `(format-time-string "# %Y-%m-%d")` gives today's heading as a string; `(insert …)` puts a string at the cursor.

## A command

```elisp
;;;###autoload
(defun my-dated-header ()
  "Insert a Markdown heading with today's date."
  (interactive)
  (insert (format-time-string "# %Y-%m-%d\n\n")))
```

The string under the name is the documentation `C-h f` shows. `C-M-x` with the cursor inside the form defines it now; the `;;;###autoload` line makes it available at every start without loading the file first. Bind it in your prefix map (lesson 13.3): `(keymap-set my-map "h" #'my-dated-header)`, then `C-x C-e` right after the closing parenthesis evaluates that one line.

## A hook

A :jargon[hook] is a list of functions Emacs runs at a moment: `markdown-mode-hook` runs in every buffer that turns Markdown mode on, `after-save-hook` after every save. `(add-hook 'markdown-mode-hook #'my-journal-header)` adds yours; the function checks that the buffer is empty and that its file is under a `journal/` folder, so other Markdown files are left alone. A named function, rather than a `lambda`, can be removed again with `remove-hook`.

## Advice

`advice-add` wraps a function without touching its definition. An `:around` advice receives the original function and decides when to call it:

```elisp
(defun my-at-top (orig &rest args)
  "Call ORIG with ARGS at the top of the buffer."
  (save-excursion
    (goto-char (point-min))
    (apply orig args)))
(advice-add 'my-dated-header :around #'my-at-top)
```

Now the heading goes to the top of the buffer wherever the cursor is, and the cursor stays put. `C-h f my-dated-header` mentions the advice; `advice-remove` takes it off. Use advice for functions you do not own; for your own, editing them is clearer.

::steps
