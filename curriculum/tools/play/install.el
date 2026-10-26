;;; install.el --- packages and grammars for the curriculum play harness  -*- lexical-binding: t; -*-

;; Installs the third-party packages that lessons in Parts 0-3 assume the
;; playground modules provide (emacs.md §3, §8) into /opt/play/elpa, and
;; the Rust tree-sitter grammar into /opt/play/tree-sitter.

(require 'package)
(setq package-user-dir "/opt/play/elpa"
      package-native-compile nil
      package-archives '(("gnu" . "https://elpa.gnu.org/packages/")
                         ("nongnu" . "https://elpa.nongnu.org/nongnu/")
                         ("melpa" . "https://melpa.org/packages/")))
(package-initialize)
(package-refresh-contents)
(dolist (p '(avy iedit multiple-cursors expreg vundo visual-replace ws-butler
             consult dirvish markdown-mode
             ace-window popper burly olivetti))
  (unless (package-installed-p p)
    (condition-case err
        (package-install p)
      (error (message "install %s failed: %S" p err)))))

(require 'treesit)
(setq treesit-language-source-alist
      '((rust "https://github.com/tree-sitter/tree-sitter-rust" "v0.23.2")))
(treesit-install-language-grammar 'rust "/opt/play/tree-sitter")
(message "installed: %S" (mapcar #'car package-alist))
