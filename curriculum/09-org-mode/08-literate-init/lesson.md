---
title: A literate init.org
summary: Write configuration as an Org document whose code blocks tangle to an Emacs Lisp file, set the tangle target once with #+PROPERTY, and load the result.
minutes: 10
---

A :jargon[literate configuration] is an Org file that explains your settings in prose and keeps the code in blocks. :jargon[Tangle] it and Org writes the blocks, in order, to the `.el` file Emacs actually loads. You read and reorganise the Org file; Emacs never needs to know it exists.

:::vscode
A `settings.json` where every setting has room for a paragraph of why, sections you can fold and reorder, and code you can run on the spot with `C-c C-c`. The `.el` file is generated, like a build output.
:::

## One target for every block

A `:tangle` header on each block works, but one line at the top says it for all of them:

```
#+PROPERTY: header-args:emacs-lisp :tangle my-org.el
```

Org reads `#+` lines when it opens the file; after adding or changing one, `C-c C-c` on it makes Org read it again. A block can still say `:tangle no` to stay out, or name another file.

:try[C-c C-v t] (`org-babel-tangle`) writes every block with a tangle target. The blocks go to the file in the order they appear, so the sections of the Org file are the order of the code. `C-c C-c` in an Emacs Lisp block evaluates just that block, to try a setting before tangling.

## Where it fits

On your own machine, `~/.emacs.d/init.org` tangles to `init.el`, or `init.el` holds the single line `(org-babel-load-file "~/.emacs.d/init.org")`, which tangles and loads in one go. The playground's modules in `/work/emacs.d/modules` are plain files that could be tangled the same way; here you practise on a small file of your own in `/work/dotfiles`.

:::aside{title="Loading by hand"}
`M-x load-file` runs a file of Emacs Lisp now; `M-x eval-buffer` runs the buffer you are in. What they set lasts until Emacs restarts.
:::

`my-org.el` was written by hand; `init.org` has begun to take it over, section by section. Finish the move.

::steps
