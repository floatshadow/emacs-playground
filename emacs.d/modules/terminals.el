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

;; cargo's messages take two lines (error[E0609]: …, then --> file:line:col).
;; compile.el parses output as it arrives (and again for redisplay) and
;; never looks at a complete line twice, so when the output stops between
;; the two lines for a moment the error is lost and M-g M-n skips it.
;; While the process runs, a last line that matched nothing stays unparsed.
(defvar compilation--parsed)
(defun playground--compilation-unparse-last-line (&rest _)
  "Leave the last complete output line to be parsed with the next one."
  (when-let* ((proc (get-buffer-process (current-buffer)))
              ((memq (process-status proc) '(run open)))
              ((markerp compilation--parsed)))
    (save-excursion
      (save-restriction
        (widen)
        (goto-char (process-mark proc))
        (forward-line 0)
        (forward-line -1)
        (when (and (< (point) compilation--parsed)
                   (not (text-property-not-all (point) (line-end-position) 'compilation-message nil)))
          (move-marker compilation--parsed (point)))))))
(advice-add 'compilation--ensure-parse :after #'playground--compilation-unparse-last-line)

;; A short remote job (Eshell's grep in /ssh:box:) can finish while
;; compilation-start still talks to TRAMP, before it adds the process to
;; `compilation-in-progress'; the sentinel has then already run, so the
;; dead process stays listed and [Compiling] stays in every mode line.
(defvar compilation-in-progress)
(declare-function compilation--update-in-progress-mode-line "compile")
(defun playground--compilation-drop-finished (buffer)
  "Forget compilation processes that finished before they were listed."
  (when (seq-some (lambda (p) (not (process-live-p p))) compilation-in-progress)
    (setq compilation-in-progress (seq-filter #'process-live-p compilation-in-progress))
    (compilation--update-in-progress-mode-line))
  buffer)
(advice-add 'compilation-start :filter-return #'playground--compilation-drop-finished)

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

;; The daemon has no SHELL, so terminals would run /bin/sh (dash: no
;; history or line editing, C-r prints ^R).  M-x shell and eat run bash.
(setq explicit-shell-file-name "/bin/bash")

(playground-with-package eat
  (setq eat-kill-buffer-on-exit t
        eat-enable-mouse t)
  ;; project-prefix-map exists (autoloaded) before project.el loads,
  ;; which may be never in this part: bind now, not after load.
  (require 'project)
  (keymap-set project-prefix-map "t" #'eat-project)
  (setq eat-shell explicit-shell-file-name)
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
