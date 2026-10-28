;;; my-packages.el --- packages I install  -*- lexical-binding: t; no-byte-compile: t; -*-

;; Loaded once init.el has loaded the modules, like my-settings.el.
;; Each use-package form names a package, makes sure it is installed
;; (:ensure from an archive, :vc from a repository) and configures it.
;;
;; Try a change at once: M-x eval-buffer.
;;
;; no-byte-compile (first line): Emacs compiles user-lisp/ before init.el
;; runs, and compiling a use-package form with :ensure checks packages
;; then, before init.el has said where the course's packages live.

;;;###autoload
(add-hook 'after-init-hook (lambda () (require 'my-packages)))

;; wc-lite: the word count in the mode line of text buffers.
(use-package wc-lite
  :ensure t                        ; install it if it is missing
  :pin melpa                       ; and only from the archive named melpa
  :custom (wc-lite-format " %dw")  ; set an option
  :hook (text-mode . wc-lite-mode)) ; turn it on in every text buffer

;; More packages:


(provide 'my-packages)
;;; my-packages.el ends here
