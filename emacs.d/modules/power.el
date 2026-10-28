;;; power.el --- text is the interface  -*- lexical-binding: t; -*-

;; Module: power.  Unlocked in Part 10 (Text is the interface); Part 11
;; composes these with the packages of earlier parts.
;;
;; Mostly built in: shell commands on the region, registers, ediff,
;; calc, Eshell over TRAMP, ielm and re-builder, line tools, occur.
;; wgrep makes grep and ripgrep results editable, so a project-wide
;; change is an edit in one buffer; wdired and occur-edit do the same
;; for file names and occur lines.
;;
;; Keys added here, by lesson:
;;   10.1  M-| region through a command, C-u M-| replaces it (built in)
;;   10.3  M-x ediff-files / ediff-buffers: n p a b | in the control
;;         window, which sits at the bottom of the frame
;;   10.4  C-x * c calc, C-x * e embedded (built in)
;;   10.6  M-x ielm, M-x re-builder (string syntax: no double escaping)
;;   10.8  M-s o occur; e or C-x C-q edit the lines, C-c C-c finish
;;   11.2  C-x C-q makes a read-only listing editable: dired (wdired),
;;         grep and ripgrep results (wgrep), occur; C-c C-c applies,
;;         C-c C-k cancels.  wgrep saves the changed files itself.

;;;; Lesson 10.3  ediff

;; The control panel is a small window at the bottom of the same frame
;; (ediff's default is a separate frame, which gets lost without a
;; window manager), and the two versions sit side by side.  After q,
;; C-c <left> (winner, windows module) brings back the layout from
;; before.
(setq ediff-window-setup-function #'ediff-setup-windows-plain
      ediff-split-window-function #'split-window-horizontally)

;;;; Lesson 10.6  Change Emacs while it runs

(setq reb-re-syntax 'string)

;;;; Lesson 10.7  Line tools and dynamic abbreviations

;; Built in: sort-lines, reverse-region, align-regexp, keep-lines,
;; flush-lines, delete-duplicate-lines, M-/ and C-M-/ (dabbrev).
(setq dabbrev-case-replace nil)

;;;; Lesson 11.2  C-x C-q: edit the read-only view

;; wdired: file names, and permissions too.
(setq wdired-allow-to-change-permissions t
      wdired-create-parent-directories t)

;; wgrep in grep-mode buffers (M-x rgrep, consult-ripgrep exported
;; with embark), with C-x C-q like the other listings.
(playground-with-package wgrep
  (setq wgrep-enable-key (kbd "C-x C-q")
        wgrep-auto-save-buffer t
        wgrep-change-readonly-file nil)
  (with-eval-after-load 'grep
    (require 'wgrep)
    (keymap-set grep-mode-map "C-x C-q" #'wgrep-change-to-wgrep-mode)))

;; occur buffers already take C-x C-q (and e) for occur-edit-mode.

;;; power.el ends here
