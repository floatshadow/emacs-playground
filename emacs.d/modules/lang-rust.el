;;; lang-rust.el --- Rust  -*- lexical-binding: t; -*-

;; Module: lang-rust.  Unlocked in Part 7 (lesson 7.4), with lsp.
;;
;; rust-ts-mode (built in, tree-sitter), rust-analyzer through Eglot
;; with clippy as its checker (diagnostics arrive in Flymake), rustfmt
;; on save through apheleia, and cargo through M-x compile.
;;
;; Keys in Rust buffers:
;;   C-c C-c  cargo build      C-c C-r  cargo run
;;   C-c C-t  cargo test       C-c C-k  cargo clippy
;; (*compilation* then works with M-g n / M-g p and RET on an error.)

;; rust-ts-mode is used for .rs files from Part 0 on (base relies on the
;; built-in association); this module adds the tooling.
(add-to-list 'major-mode-remap-alist '(rust-mode . rust-ts-mode))

;;;; Eglot: rust-analyzer, with clippy as the checker

(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs '((rust-ts-mode rust-mode) . ("rust-analyzer")))
  (setq-default eglot-workspace-configuration
                (plist-put (default-value 'eglot-workspace-configuration)
                           :rust-analyzer '(:check (:command "clippy")))))
(add-hook 'rust-ts-mode-hook #'eglot-ensure)

;;;; Cargo through compile

(defun playground-cargo (subcommand)
  "Run cargo SUBCOMMAND in the crate's root through `compile'."
  (let ((default-directory (or (locate-dominating-file default-directory "Cargo.toml")
                               default-directory)))
    (compile (concat "cargo " subcommand))))

(defun playground-cargo-build () "cargo build." (interactive) (playground-cargo "build"))
(defun playground-cargo-run () "cargo run." (interactive) (playground-cargo "run"))
(defun playground-cargo-test () "cargo test." (interactive) (playground-cargo "test"))
(defun playground-cargo-clippy () "cargo clippy." (interactive) (playground-cargo "clippy"))

(defun playground-rust-setup ()
  "Make M-x compile default to cargo build in Rust buffers."
  (when (locate-dominating-file default-directory "Cargo.toml")
    (setq-local compile-command "cargo build")))
(add-hook 'rust-ts-mode-hook #'playground-rust-setup)

(with-eval-after-load 'rust-ts-mode
  (keymap-set rust-ts-mode-map "C-c C-c" #'playground-cargo-build)
  (keymap-set rust-ts-mode-map "C-c C-r" #'playground-cargo-run)
  (keymap-set rust-ts-mode-map "C-c C-t" #'playground-cargo-test)
  (keymap-set rust-ts-mode-map "C-c C-k" #'playground-cargo-clippy))

;; rustfmt on save: apheleia (lsp module) already maps rust-ts-mode to
;; rustfmt, using the edition from Cargo.toml.

;;; lang-rust.el ends here
