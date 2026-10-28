---
title: "Themes, fonts, icons and the mode line"
summary: Try a theme family, set up a light and dark pair you can toggle, enlarge the text for writing, and switch icons and an alternative mode line on and off.
minutes: 12
---

How Emacs looks is configuration like everything else: a :jargon[theme] sets the colours of every :jargon[face], the font is one face attribute, icons come from packages, and the mode line is a variable you can replace. This lesson runs in the graphical frame, where fonts, sizes and ligatures show.

:::vscode
Color Theme, File Icon Theme and the font settings become `M-x load-theme` (or a family's own command), the nerd-icons packages and `set-face-attribute`. VS Code's "auto switch between light and dark" is a theme pair you toggle with one command.
:::

## Themes

The course uses *modus-vivendi-tinted*, one of the Modus themes built into Emacs and designed for legibility. The *ef-themes* package adds a family of more colourful ones: `M-x ef-themes-select` loads one by name. `M-x load-theme` loads any theme; themes from packages ask once whether you trust them, the family commands do not.

A pair to switch between, light for daytime and dark for night:

```elisp
(setq modus-themes-to-toggle '(modus-operandi-tinted modus-vivendi-tinted))
```

Then `M-x modus-themes-toggle` flips between them (`ef-themes-to-toggle` and `ef-themes-toggle` do the same for ef-themes). Put the `setq` in `user-lisp/my-settings.el` and bind the toggle in your prefix map.

## Fonts

:try[C-x C-+] and `C-x C--` make the text in this buffer bigger or smaller (`C-x C-0` resets); `C-x C-M-+` zooms every buffer. For good, set the default face:

```elisp
(set-face-attribute 'default nil :family "Iosevka Nerd Font" :height 140)
```

`:height` is in tenths of a point. A bigger font only for writing is a hook: `(add-hook 'markdown-mode-hook (lambda () (text-scale-set 1)))`. Chinese, Japanese and Korean text falls back to Noto CJK here; `set-fontset-font` picks another font for a script. The terminal frame uses the browser terminal's font and size, so none of this changes it.

:::gui-only
Ligatures are on in this frame: `->`, `=>` and `!=` in code are drawn as single glyphs. `M-x global-ligature-mode` turns them off.
:::

## Icons and the mode line

The ui module adds icons from Iosevka Nerd Font to Dired, completion lists, the corfu popup and ibuffer. Each is a minor mode: `nerd-icons-completion-mode` for completion lists, `nerd-icons-dired-mode` for Dired. The breadcrumb header line is `breadcrumb-mode` (lesson 13.1 turned it off in ui.el; `M-x breadcrumb-mode` brings it back for the session).

*doom-modeline* is a popular replacement for the mode line, with icons and compact segments. The course describes the built-in mode line, so it stays off; `M-x doom-modeline-mode` tries it, and again goes back.

::steps
