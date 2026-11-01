# Part 13: making it yours

Your configuration is `/work/emacs.d`. Part 13 changes it for real.

- `seed/` holds starter files. A lesson copies the one it needs into
  `emacs.d/user-lisp/` when that file does not exist there yet; the copy
  in `user-lisp/` is yours from then on: `my-settings.el` (lessons 13.1
  and 13.6), `my-packages.el` (13.2), `my-keys.el` (13.3),
  `my-commands.el` (13.4) and `my-menus.el` (13.5).
- `journal/` is a place to try commands on Markdown files.
- `packages/` stands in for the internet in lesson 13.2: `melpa/` is a
  small package archive and `forge/cheer/` a git repository, because the
  playground has no network.
