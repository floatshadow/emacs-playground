;;; terminals.el --- terminals and running things  -*- lexical-binding: t; -*-

;; Module: terminals.  Unlocked in Part 6 (Terminals and running things).
;;
;; compile with coloured output, eat (a full terminal inside a buffer),
;; Eshell (a shell written in Emacs Lisp, where Emacs commands are
;; shell commands), and asynchronous shell commands.
;;
;; Keys added here, by lesson:
;;   6.1  M-x compile, M-g n / M-g p (next-error), g in *compilation*
;;   6.2  C-x p t   eat in the project's root;  M-x eat
;;   6.3  C-x p e   Eshell in the project (built in);  M-x eshell
;;   6.4  M-&       async shell command;  & and ! in dired

;;;; Lesson 6.1  compile and next-error

(setq compilation-scroll-output 'first-error
      compilation-ask-about-save nil
      compilation-always-kill t
      next-error-message-highlight t)
(add-hook 'compilation-filter-hook #'ansi-color-compilation-filter)

;;;; Lesson 6.2  eat: a terminal in a buffer

(playground-with-package eat
  (setq eat-kill-buffer-on-exit t
        eat-enable-mouse t)
  (with-eval-after-load 'project
    (keymap-set project-prefix-map "t" #'eat-project))
  ;; Full-screen programs (htop, less) started from Eshell run in eat.
  (add-hook 'eshell-load-hook #'eat-eshell-mode)
  (add-hook 'eshell-load-hook #'eat-eshell-visual-command-mode))

;; TODO(emacs, emacs.md section 3): vterm as the alternative terminal
;; (compiled at image time; needs cmake and libvterm in the build stage).

;;;; Lesson 6.3  Eshell

(setq eshell-scroll-to-bottom-on-input 'this
      eshell-history-size 10000
      eshell-hist-ignoredups t
      eshell-banner-message
      "Eshell: a shell inside Emacs.  Emacs commands work here too (try: find-file README.md).\n\n")

;;;; Lesson 6.4  Async shell commands and Dired integration

(setq async-shell-command-buffer 'new-buffer
      shell-command-prompt-show-cwd t
      async-shell-command-display-buffer nil)

;;; terminals.el ends here
