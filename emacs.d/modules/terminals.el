;;; terminals.el --- terminals and running things  -*- lexical-binding: t; -*-

;; Module: terminals.  Unlocked in Part 6 (Terminals and running things).
;;
;; compile with coloured output, eat (a full terminal inside a buffer;
;; vterm is the heavier alternative), Eshell (a shell written in Emacs
;; Lisp, where Emacs commands are shell commands), and asynchronous
;; shell commands.
;;
;; Keys added here, by lesson:
;;   6.1  C-c C-c  cargo build (Rust buffers);  M-x compile;
;;        M-g M-n / M-g M-p and C-x ` (next-error), g in *compilation*
;;   6.2  C-x p t   eat in the project's root;  M-x eat;  M-x vterm
;;   6.3  C-x p e   Eshell in the project (built in);  M-x eshell
;;   6.4  M-&       async shell command;  & and ! in dired

;;;; Lesson 6.1  compile and next-error

(setq compilation-scroll-output 'first-error
      compilation-ask-about-save nil
      compilation-always-kill t
      next-error-message-highlight t)
(add-hook 'compilation-filter-hook #'ansi-color-compilation-filter)

;; C-c C-c builds a Rust crate without asking, like VS Code's Run Build
;; Task; M-x compile asks for the command.  lang-rust (Part 7) defines
;; the same command and adds run, test and clippy.
(defun playground-cargo-build ()
  "cargo build in the crate's root, through `compile'."
  (interactive)
  (let ((default-directory (or (locate-dominating-file default-directory "Cargo.toml")
                               default-directory)))
    (compile "cargo build")))
(with-eval-after-load 'rust-ts-mode
  (keymap-set rust-ts-mode-map "C-c C-c" #'playground-cargo-build))

;; Error locations in *compilation*: Emacs knows cargo's (rust,
;; rust-panic) and OCaml's and dune's File "x.ml", line 3, characters
;; 4-9 (python-tracebacks-and-caml); pytest's "tests/test_x.py:12: in
;; test_name" and assertion lines are added here.
(with-eval-after-load 'compile
  (setf (alist-get 'pytest compilation-error-regexp-alist-alist)
        '("^\\([^ \t\n:]+\\.py\\):\\([0-9]+\\): \\(?:in \\|[A-Za-z]*Error\\)" 1 2))
  (add-to-list 'compilation-error-regexp-alist 'pytest))

;; cargo and dune name files from the project's root (src/count.rs), so
;; M-x compile started in src/ would ask where each file is: errors are
;; also looked up from the nearest Cargo.toml, dune-project or
;; pyproject.toml.  (A start hook: compilation-start sets the search
;; path after the mode hooks.)
(defun playground--compilation-search-root (_process)
  "Let this compilation's file names resolve from the project root too."
  (when-let* ((root (seq-some (lambda (marker) (locate-dominating-file default-directory marker))
                              '("Cargo.toml" "dune-project" "pyproject.toml"))))
    (setq-local compilation-search-path (list nil root))))
(add-hook 'compilation-start-hook #'playground--compilation-search-root)

;;;; Lesson 6.2  eat: a terminal in a buffer

(playground-with-package eat
  (setq eat-kill-buffer-on-exit t
        eat-enable-mouse t)
  (with-eval-after-load 'project
    (keymap-set project-prefix-map "t" #'eat-project))
  ;; Full-screen programs (htop, less) started from Eshell run in eat.
  (add-hook 'eshell-load-hook #'eat-eshell-mode)
  (add-hook 'eshell-load-hook #'eat-eshell-visual-command-mode))

;; vterm is the heavier alternative: libvterm in C (its module is
;; compiled with the image), faster with programs that redraw a lot.
;; M-x vterm; C-c C-t switches to copy mode (move and copy like in a
;; buffer), C-c C-t again goes back.
(playground-with-package vterm
  (setq vterm-always-compile-module nil
        vterm-kill-buffer-on-exit t
        vterm-max-scrollback 10000
        vterm-shell "/bin/bash"))

;;;; Lesson 6.3  Eshell

(setq eshell-scroll-to-bottom-on-input 'this
      eshell-history-size 10000
      eshell-hist-ignoredups t
      eshell-banner-message
      "Eshell: a shell inside Emacs.  Emacs commands work here too (try: find-file README.md).\n\n")

;; In shells and REPLs (Eshell, M-x shell, the Python shell) completion
;; pops up only on TAB: an automatic popup would take the RET that
;; should run the command line.
(defun playground--no-auto-completion ()
  "Complete only on request (TAB) in this buffer."
  (setq-local corfu-auto nil))
(add-hook 'eshell-mode-hook #'playground--no-auto-completion)
(add-hook 'comint-mode-hook #'playground--no-auto-completion)

;;;; Lesson 6.4  Async shell commands and Dired integration

(setq async-shell-command-buffer 'new-buffer
      shell-command-prompt-show-cwd t
      async-shell-command-display-buffer nil)

;;; terminals.el ends here
