;;; input.el --- special characters and input methods  -*- lexical-binding: t; -*-

;; Module: input.  Unlocked in Part 8 (lesson 8.7), with writing.
;;
;; Characters that are not on the keyboard: C-x 8 RET inserts any
;; character by name, C-x 8 followed by a short sequence types common
;; ones (C-x 8 _ n is an en dash, C-x 8 ' e is é).  An input method
;; turns what you type into other characters while it is on: C-\
;; toggles the default one, TeX (\alpha gives α, \to gives →), and
;; C-u C-\ picks another, such as pyim for Chinese (type pinyin, then
;; the number or SPC of a candidate).  C-u C-x = describes the
;; character at point.
;;
;; Keys, lesson 8.7:
;;   C-x 8 RET  insert a character by name     C-\  input method on/off
;;   C-x 8 _ n  en dash (C-x 8 then a short sequence, C-x 8 C-h lists them)
;;   C-u C-\    choose an input method (TeX, pyim, ...)
;;   C-u C-x =  everything about the character at point

;;;; The TeX input method

;; C-\ turns it on; the mode line shows "\" while it is active.
(setq default-input-method "TeX")

;;;; Chinese: pyim

;; pyim is a Chinese input method written in Emacs Lisp, with the basic
;; pinyin dictionary (pyim-basedict); it works in the terminal frame
;; too, showing candidates in the echo area.  C-u C-\ pyim RET turns it
;; on.  Its caches go to var/ with the other state files.
(playground-with-package pyim
  (setq pyim-dcache-directory (playground-var "pyim/dcache/")
        pyim-default-scheme 'quanpin
        pyim-page-tooltip 'minibuffer
        pyim-page-length 7
        pyim-cloudim nil)
  (with-eval-after-load 'pyim
    (when (playground-package-p 'pyim-basedict)
      (require 'pyim-basedict)
      (pyim-basedict-enable))))

;;; input.el ends here
