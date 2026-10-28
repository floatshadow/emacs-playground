;;; git.el --- git with Magit  -*- lexical-binding: t; -*-

;; Module: git.  Unlocked in Part 5 (Git with Magit).
;;
;; Magit (status, staging hunks, commits, log, blame, branches, rebase),
;; diff-hl (changed lines marked in the margin, like VS Code's gutter),
;; git-timemachine (step through a file's history), forge (pull
;; requests and issues of GitHub and GitLab remotes; it needs the
;; network, so it stays out of the way until you call it).
;;
;; Keys added here, by lesson:
;;   5.1  C-x g   Magit status (TAB s u S U k c c ? inside)
;;        C-c g   Magit commands for the repository; C-c f for this file
;;   5.2  l l, l a, b (blame), C-x v = (built in)
;;   5.4  smerge keys in conflicted files: C-c ^ (built in)
;;   5.5  C-x v n / C-x v p   next / previous changed hunk (diff-hl;
;;        C-x v ] and C-x v [ too), C-x v r  revert the hunk at point,
;;        C-x v t   git-timemachine (p n to step, q to leave)

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
  ;; Lesson 5.5's keys under C-x v: n and p move between hunks, r
  ;; reverts one (diff-hl's own n is revert, which surprises).
  (with-eval-after-load 'diff-hl
    (keymap-set diff-hl-command-map "n" #'diff-hl-next-hunk)
    (keymap-set diff-hl-command-map "p" #'diff-hl-previous-hunk)
    (keymap-set diff-hl-command-map "r" #'diff-hl-revert-hunk))
  (with-eval-after-load 'magit
    (add-hook 'magit-pre-refresh-hook #'diff-hl-magit-pre-refresh)
    (add-hook 'magit-post-refresh-hook #'diff-hl-magit-post-refresh)))

(playground-with-package git-timemachine
  (keymap-global-set "C-x v t" #'git-timemachine))

;;;; forge

;; ' (or N) in a Magit buffer opens forge's menu; nothing loads before
;; that.  Its database goes to var/.
(playground-with-package forge
  (setq forge-database-file (playground-var "forge-database.sqlite")))

;;; git.el ends here
