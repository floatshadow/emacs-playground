;;; power.el --- power tools  -*- lexical-binding: t; -*-

;; Module: power.  Unlocked in Part 10 (Power tools).
;;
;; Planned: wgrep (edit grep results, save to every file), wdired
;; (rename files by editing the dired buffer), ediff with a sane layout,
;; calc, ielm, re-builder, edit-indirect for fenced code blocks (11.6),
;; line tools (sort, uniq, align).
;;
;; TODO(emacs, emacs.md section 3): not in slice 1; wgrep and
;; edit-indirect are not in packages.lock yet.  The ediff settings below
;; are kept because ediff's default opens its control panel in a
;; separate frame, which gets lost without a window manager.

(setq ediff-window-setup-function #'ediff-setup-windows-plain
      ediff-split-window-function #'split-window-horizontally)

;; wdired: C-x C-q in dired edits file names, and permissions too.
(setq wdired-allow-to-change-permissions t)

;;; power.el ends here
