;;; my-settings.el --- my own settings  -*- lexical-binding: t; -*-

;; Emacs 31 compiles every file in user-lisp/ when it starts and runs
;; the forms that follow an autoload cookie (a ;;;###autoload line).
;; The one form below loads the rest of this file once init.el has
;; loaded the modules, so a setting here wins over a module's.
;;
;; Try a change at once: C-M-x on a form, or M-x eval-buffer for all.

;;;###autoload
(add-hook 'after-init-hook (lambda () (require 'my-settings)))

;;; Settings


(provide 'my-settings)
;;; my-settings.el ends here
