;;; ui.el --- icons, mode line, highlights  -*- lexical-binding: t; -*-

;; Module: ui.  Unlocked in Part 13 (Making it yours), where you decide
;; which of these you want.
;;
;; Icons from Iosevka Nerd Font in dired, completion lists, the corfu
;; popup and ibuffer (nerd-icons-*); a breadcrumb header line (project,
;; file, function); a brief highlight of the line you jumped to
;; (pulsar); line numbers in code; ligatures in the GUI frame; helpful,
;; a richer *Help* buffer; ef-themes, more themes to try with
;; M-x load-theme; doom-modeline, an alternative mode line to try.
;;
;; Keys added here:
;;   C-h f / C-h v / C-h k / C-h x   helpful versions of describe-*
;;   C-c C-d   helpful documentation of the symbol at point
;;
;; Commands, by lesson:
;;   13.1  the learner turns breadcrumb-mode off below, as the
;;         "edit a module" way of changing a setting
;;   13.5  M-x ef-themes-select, M-x modus-themes-toggle (the pair is
;;         modus-themes-to-toggle), C-x C-+ / C-x C-- / C-x C-0,
;;         M-x nerd-icons-completion-mode, M-x doom-modeline-mode,
;;         M-x global-ligature-mode (GUI frame)

;;;; Icons

(playground-with-package nerd-icons
  (setq nerd-icons-font-family "Iosevka Nerd Font"))

(playground-with-package nerd-icons-dired
  ;; dirvish (files module) draws its own icons; plain dired uses these.
  (if (bound-and-true-p dirvish-override-dired-mode)
      (setq dirvish-attributes (cons 'nerd-icons (remq 'nerd-icons dirvish-attributes)))
    (add-hook 'dired-mode-hook #'nerd-icons-dired-mode)))

(playground-with-package nerd-icons-completion
  (nerd-icons-completion-mode 1)
  (add-hook 'marginalia-mode-hook #'nerd-icons-completion-marginalia-setup))

(playground-with-package nerd-icons-corfu
  (with-eval-after-load 'corfu
    (add-to-list 'corfu-margin-formatters #'nerd-icons-corfu-formatter)))

(playground-with-package nerd-icons-ibuffer
  (add-hook 'ibuffer-mode-hook #'nerd-icons-ibuffer-mode))

;;;; Where you are

(playground-with-package breadcrumb
  (breadcrumb-mode 1))

(playground-with-package pulsar
  (setq pulsar-pulse t
        pulsar-delay 0.05)
  (pulsar-global-mode 1))

(add-hook 'prog-mode-hook #'display-line-numbers-mode)
(setq display-line-numbers-width 3)

;;;; Ligatures (GUI frame only)

(playground-with-package ligature
  (with-eval-after-load 'ligature
    (ligature-set-ligatures
     'prog-mode
     '("<---" "<--" "<<-" "<-" "->" "-->" "--->" "<->" "<-->" "<--->" "<---->" "<!--"
       "<==" "<===" "<=" "=>" "=>>" "==>" "===>" ">=" "<=>" "<==>" "<===>" "<====>" "<!---"
       "<~~" "<~" "~>" "~~>" "::" ":::" "==" "!=" "===" "!==" ":=" ":-" ":+" "<*" "<*>" "*>"
       "<|" "<|>" "|>" "+:" "-:" "=:" "<******>" "++" "+++")))
  ;; Terminals cannot draw ligatures, so they are turned on when the
  ;; first graphical frame opens.
  (defun playground--ligatures-for-gui (&optional frame)
    "Turn on ligatures once a graphical FRAME exists."
    (when (display-graphic-p (or frame (selected-frame)))
      (require 'ligature)
      (global-ligature-mode 1)))
  (add-hook 'after-make-frame-functions #'playground--ligatures-for-gui)
  (when (display-graphic-p) (playground--ligatures-for-gui)))

;;;; Themes

;; ef-themes 2.2.1 autoloads its commands as plain functions, so M-x
;; does not offer them until the package is loaded; declare them as
;; commands.
(playground-with-package ef-themes
  (dolist (cmd '(ef-themes-toggle
                 ef-themes-rotate ef-themes-select
                 ef-themes-select-dark ef-themes-select-light
                 ef-themes-load-random ef-themes-load-random-dark
                 ef-themes-load-random-light ef-themes-list-colors
                 ef-themes-list-colors-current))
    (when (or (not (fboundp cmd)) (autoloadp (symbol-function cmd)))
      (autoload cmd "ef-themes" nil t))))

;;;; The mode line

;; doom-modeline is the popular replacement mode line (icons, compact
;; segments).  It stays off: lessons describe the built-in one.
;; M-x doom-modeline-mode tries it; again turns it off.
(playground-with-package doom-modeline
  (setq doom-modeline-icon t
        doom-modeline-height 1
        doom-modeline-buffer-file-name-style 'relative-from-project))

;;;; Helpful

(playground-with-package helpful
  (keymap-global-set "C-h f" #'helpful-callable)
  (keymap-global-set "C-h v" #'helpful-variable)
  (keymap-global-set "C-h k" #'helpful-key)
  (keymap-global-set "C-h x" #'helpful-command)
  (keymap-global-set "C-c C-d" #'helpful-at-point))

;;; ui.el ends here
