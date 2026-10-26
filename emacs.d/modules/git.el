;;; git.el --- git with Magit  -*- lexical-binding: t; -*-

;; Module: git.  Unlocked in Part 5 (Git with Magit).
;;
;; Magit (status, staging hunks, commits, log, blame, branches, rebase),
;; diff-hl (changed lines marked in the margin, like VS Code's gutter),
;; git-timemachine (step through a file's history).
;;
;; Keys added here, by lesson:
;;   5.1  C-x g   Magit status (TAB s u S U k c c ? inside)
;;        C-c g   Magit commands for the repository; C-c f for this file
;;   5.2  l l, l a, b (blame), C-x v = (built in)
;;   5.4  smerge keys in conflicted files: C-c ^ (built in)
;;   5.5  C-x v t   git-timemachine;  [ and ] (diff-hl) jump between
;;        changed hunks with C-x v [ and C-x v ]

;;;; Lesson 5.1  Status and staging hunks

(playground-with-package magit
  (keymap-global-set "C-x g" #'magit-status)
  (keymap-global-set "C-c g" #'magit-dispatch)
  (keymap-global-set "C-c f" #'magit-file-dispatch)
  (setq magit-diff-refine-hunk t
        magit-save-repository-buffers 'dontask
        magit-display-buffer-function #'magit-display-buffer-same-window-except-diff-v1))

;; Commit messages are edited in a normal buffer: C-c C-c commits,
;; C-c C-k cancels (with-editor, part of Magit).

;;;; Lesson 5.5  diff-hl and git-timemachine

(playground-with-package diff-hl
  ;; The margin works in the terminal frame, which has no fringes.
  (global-diff-hl-mode 1)
  (diff-hl-margin-mode 1)
  (diff-hl-flydiff-mode 1)
  (add-hook 'dired-mode-hook #'diff-hl-dired-mode)
  (with-eval-after-load 'magit
    (add-hook 'magit-pre-refresh-hook #'diff-hl-magit-pre-refresh)
    (add-hook 'magit-post-refresh-hook #'diff-hl-magit-post-refresh)))

(playground-with-package git-timemachine
  (keymap-global-set "C-x v t" #'git-timemachine))

;; TODO(emacs, emacs.md section 3): forge (optional) is not in
;; packages.lock yet.

;;; git.el ends here
