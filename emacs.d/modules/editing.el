;;; editing.el --- moving and editing  -*- lexical-binding: t; -*-

;; Module: editing.  Unlocked in Part 1 (Moving and editing).
;;
;; Packages: avy (jump to any visible spot), expreg (grow the region by
;; syntax), multiple-cursors and iedit (edit several places at once),
;; vundo (the undo tree), visual-replace (replace with a live preview),
;; ws-butler (trim trailing spaces you added, nothing else).
;;
;; Keys added here, by lesson:
;;   1.2  M-y          built in: right after C-y, an older kill (yank-pop);
;;                     the completion module makes it consult-yank-pop (4.2)
;;   1.3  C-x u        vundo, the undo tree;  C-? / C-M-_  redo
;;   1.5  M-%          replace with a preview, then y n ! ^ . per match
;;   1.6  C-x C-k ...  keyboard macros (built in; nothing to configure)
;;   1.7  C-;          iedit;  C-> C-<  next/previous like this;
;;        C-c m l      a cursor on each line;  C-c m a  all like this;
;;        C-=  C--     grow / shrink the region (expreg)
;;   1.8  M-j          avy: jump to a visible character;
;;        M-g i  M-g o imenu / outline jumps (consult when available)

;;;; Lesson 1.2  Kill, yank and the kill ring

;; M-y stays the built-in yank-pop, the command lesson 1.2 teaches (on
;; its own it also lists the kill ring to pick from).  The completion
;; module binds consult-yank-pop (lesson 4.2); every package is in the
;; image, so binding it here whenever consult exists would give Part 1
;; a different command than its lessons describe.

;;;; Lesson 1.3  Undo that never loses work

;; C-/ undoes; C-? (or C-M-_ where the terminal cannot send C-?) redoes.
(keymap-global-set "C-?" #'undo-redo)
(keymap-global-set "C-M-_" #'undo-redo)

(playground-with-package vundo
  (keymap-global-set "C-x u" #'vundo)
  (setq vundo-glyph-alist vundo-unicode-symbols
        vundo-compact-display t))

;;;; Lesson 1.5  Replace

;; M-% opens one prompt for both the text and its replacement and shows
;; every match as you type (TAB switches between the two fields).  RET
;; then asks at each match with the usual query-replace keys:
;; y n ! ^ . q.  C-M-% stays the plain regexp version.
(playground-with-package visual-replace
  (visual-replace-global-mode 1)
  (add-hook 'visual-replace-minibuffer-mode-hook #'visual-replace-toggle-query))

;;;; Lesson 1.6  Rectangles and keyboard macros

;; Built in: C-x SPC (rectangle mark), C-x r t/k/y/N, F3/F4,
;; C-x C-k n/e/r.  Nothing to configure.

;;;; Lesson 1.7  Multiple cursors and iedit

(playground-with-package iedit
  ;; C-; edits every occurrence of the symbol at point; C-0 C-; limits
  ;; it to the current function.
  (keymap-global-set "C-;" #'iedit-mode))

(playground-with-package multiple-cursors
  (setq mc/list-file (playground-var "mc-lists.el"))
  (keymap-global-set "C->" #'mc/mark-next-like-this)
  (keymap-global-set "C-<" #'mc/mark-previous-like-this)
  (keymap-global-set "C-c m l" #'mc/edit-lines)
  (keymap-global-set "C-c m a" #'mc/mark-all-like-this))

(playground-with-package expreg
  (keymap-global-set "C-=" #'expreg-expand)
  (keymap-global-set "C--" #'expreg-contract))

;;;; Lesson 1.8  Jump anywhere

(playground-with-package avy
  (keymap-global-set "M-j" #'avy-goto-char-timer)
  (setq avy-timeout-seconds 0.4
        avy-style 'at-full
        avy-background t))

(keymap-global-set "M-g i" (if (fboundp 'consult-imenu) #'consult-imenu #'imenu))
(when (fboundp 'consult-outline)
  (keymap-global-set "M-g o" #'consult-outline))

;;;; Whitespace

;; Trailing whitespace is removed from the lines you edited, never from
;; the rest of the file (so diffs stay small).  It is housekeeping that
;; never asks anything of you, so its " wb" stays out of the mode line.
(playground-with-package ws-butler
  (add-hook 'prog-mode-hook #'ws-butler-mode)
  (add-hook 'text-mode-hook #'ws-butler-mode)
  (with-eval-after-load 'ws-butler
    (setf (alist-get 'ws-butler-mode minor-mode-alist) '(""))))

;;; editing.el ends here
