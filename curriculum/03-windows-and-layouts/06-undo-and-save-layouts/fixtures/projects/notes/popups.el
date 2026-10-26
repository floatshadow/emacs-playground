;;; popups.el --- where pop-up buffers appear (lesson 3.5)  -*- lexical-binding: t; -*-

;; `display-buffer-alist' is a list of rules.  Each rule says WHICH
;; buffers it is about, HOW to show them (a list of display functions,
;; tried in order) and WITH WHAT parameters:
;;
;;   (WHICH-BUFFERS (DISPLAY-FUNCTION ...) (PARAMETER . VALUE) ...)
;;
;; The playground's windows module already has one rule: build output,
;; grep, occur and xref results open at the bottom, a third of the
;; frame high.  Read it in /work/emacs.d/modules/windows.el.
;;
;; The rule below is for *Help*.  It uses a side window: a window glued
;; to one edge of the frame that C-x 1 does not delete and C-x w s
;; hides and shows.  Two values are wrong on purpose.  Fix them, save,
;; and evaluate this file with M-x eval-buffer.

(add-to-list 'display-buffer-alist
             '("\\*Help\\*"
               (display-buffer-reuse-window display-buffer-in-side-window)
               (side . bottom)          ; 1. Help belongs on the right
               (window-width . 0.25)))  ; 2. two fifths of the width: 0.4

;;; popups.el ends here
