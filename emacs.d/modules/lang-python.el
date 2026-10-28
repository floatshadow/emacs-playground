;;; lang-python.el --- Python  -*- lexical-binding: t; -*-

;; Module: lang-python.  Unlocked in Part 7 (lesson 7.6), with lsp.
;;
;; python-ts-mode (built in, tree-sitter), basedpyright through Eglot,
;; ruff for formatting on save (apheleia), pet to find the project's
;; virtual environment (uv's .venv) so the language server, the shell
;; and the checkers all use the project's Python.
;;
;; Keys in Python buffers (python.el, built in):
;;   C-c C-p  start a Python shell     C-c C-c  send the buffer to it
;;   C-c C-r  send the region          C-c C-z  switch to the shell

(add-to-list 'major-mode-remap-alist '(python-mode . python-ts-mode))

(setq python-indent-guess-indent-offset-verbose nil
      python-shell-interpreter "python3"
      python-shell-dedicated 'project)

;;;; The project's environment (pet)

(playground-with-package pet
  ;; Runs before other hooks so they see the environment pet found.
  (add-hook 'python-base-mode-hook #'pet-mode -10))

;;;; Eglot: basedpyright

(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs
               '((python-ts-mode python-mode) . ("basedpyright-langserver" "--stdio"))))
(add-hook 'python-ts-mode-hook #'playground-eglot-ensure)

;;;; ruff on save

(with-eval-after-load 'apheleia
  (setf (alist-get 'python-ts-mode apheleia-mode-alist) '(ruff-isort ruff))
  (setf (alist-get 'python-mode apheleia-mode-alist) '(ruff-isort ruff)))

;;; lang-python.el ends here
