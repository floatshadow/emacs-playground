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

;;;; Lesson 10.1  Pipe a region through any tool

;; Corfu's popup also opens in shell-command prompts (M-!, M-|, M-&),
;; where RET would insert its first candidate instead of running the
;; command as typed (`-t org' became `notes.org').  There, completion
;; waits for TAB.
(defun playground-shell-prompt-no-auto-complete ()
  "Turn off Corfu's automatic popup in a shell-command prompt."
  (when (eq (current-local-map) minibuffer-local-shell-command-map)
    (setq-local corfu-auto nil)))
(add-hook 'minibuffer-setup-hook #'playground-shell-prompt-no-auto-complete)

;;;; Lesson 10.3  ediff

;; The control panel is a small window at the bottom of the same frame
;; (ediff's default is a separate frame, which gets lost without a
;; window manager), and the two versions sit side by side.  After q,
;; C-c <left> (winner, windows module) brings back the layout from
;; before.
(setq ediff-window-setup-function #'ediff-setup-windows-plain
      ediff-split-window-function #'split-window-horizontally)

;; ediff dedicates the control window to its buffer.  A comparison left
;; open would then keep the next lesson's layout out of the selected
;; window; ediff works the same without the dedication.
(define-advice ediff-setup-control-buffer (:after (&rest _) playground-undedicate)
  (set-window-dedicated-p (selected-window) nil))

;;;; Lesson 10.6  Change Emacs while it runs

(setq reb-re-syntax 'string)

;; re-builder dedicates its window too; left open, it would keep the
;; next lesson's layout out (see ediff above).
(define-advice re-builder (:after (&rest _) playground-undedicate)
  (when-let* ((w (and (boundp 'reb-buffer) (get-buffer-window reb-buffer))))
    (set-window-dedicated-p w nil)))

;;;; Lesson 10.7  Line tools and dynamic abbreviations

;; Built in: sort-lines, reverse-region, align-regexp, keep-lines,
;; flush-lines, delete-duplicate-lines, M-/ and C-M-/ (dabbrev).
(setq dabbrev-case-replace nil)

;;;; Lesson 10.8  occur and occur-edit

;; occur: e edits the lines (built in); C-x C-q does too, as in the
;; other listings, and in occur-edit-mode it goes back like C-c C-c.
;; M-% over an edited listing skips the read-only headings (they
;; contain the search string) instead of stopping at the first one.
(with-eval-after-load 'replace
  (keymap-set occur-mode-map "C-x C-q" #'occur-edit-mode)
  (keymap-set occur-edit-mode-map "C-x C-q" #'occur-cease-edit))
(setq query-replace-skip-read-only t)

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

;;; power.el ends here
