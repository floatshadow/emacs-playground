;;; my-keys.el --- my key bindings  -*- lexical-binding: t; -*-

;; Loaded once init.el has loaded the modules (the cookie below), so
;; these bindings win over the modules'.  C-c followed by a letter is
;; reserved for you: Emacs and well-behaved packages never bind it.
;;
;; Try a change at once: M-x eval-buffer.

;;;###autoload
(add-hook 'after-init-hook (lambda () (require 'my-keys)))

;; My prefix map: C-c u, then one key.
(defvar-keymap my-map
  :doc "My own commands, under C-c u."
  "d" #'duplicate-line
  "w" #'whitespace-mode
  "l" #'display-line-numbers-mode)
(keymap-global-set "C-c u" my-map)
(which-key-add-key-based-replacements "C-c u" "my commands")

;; More keys in my map:


(provide 'my-keys)
;;; my-keys.el ends here
