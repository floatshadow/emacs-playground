;;; my-commands.el --- your own commands go here  -*- lexical-binding: t; -*-

;; Emacs 31 treats this directory (`user-lisp-directory') specially:
;; every .el file in it is byte-compiled, its ;;;###autoload cookies
;; are collected, and the directory is on `load-path' at startup.  Put
;; commands you write here (Part 13.4 of the course); mark the ones you
;; want available without `require' with an autoload cookie, like:
;;
;;   ;;;###autoload
;;   (defun my-insert-date ()
;;     "Insert today's date."
;;     (interactive)
;;     (insert (format-time-string "%Y-%m-%d")))
;;
;; This file only explains so far; it defines nothing yet.

(provide 'my-commands)
;;; my-commands.el ends here
