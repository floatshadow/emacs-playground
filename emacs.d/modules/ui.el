;;; ui.el --- icons, mode line, highlights  -*- lexical-binding: t; -*-

;; Module: ui.  Unlocked in Part 13 (Making it yours), where you decide
;; which of these you want.
;;
;; Icons from Iosevka Nerd Font in dired, completion lists, the corfu
;; popup and ibuffer (nerd-icons-*); a breadcrumb header line (project,
;; file, function); a brief highlight of the line you jumped to
;; (pulsar); line numbers in code; ligatures in the GUI frame; helpful,
;; a richer *Help* buffer; ef-themes, more themes to try with
;; M-x load-theme.
;;
;; Keys added here:
;;   C-h f / C-h v / C-h k / C-h x   helpful versions of describe-*
;;   C-c C-d   helpful documentation of the symbol at point

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

;;;; Helpful

(playground-with-package helpful
  (keymap-global-set "C-h f" #'helpful-callable)
  (keymap-global-set "C-h v" #'helpful-variable)
  (keymap-global-set "C-h k" #'helpful-key)
  (keymap-global-set "C-h x" #'helpful-command)
  (keymap-global-set "C-c C-d" #'helpful-at-point))

;;; ui.el ends here
