;;; lsp.el --- language servers, diagnostics, formatting  -*- lexical-binding: t; -*-

;; Module: lsp.  Unlocked in Part 7 (Language servers and debugging),
;; together with lang-rust, lang-ocaml and lang-python.
;;
;; Eglot talks to language servers (rust-analyzer, basedpyright,
;; ocaml-lsp) for completion, go-to-definition, rename and code
;; actions; Flymake shows their diagnostics; eldoc shows documentation
;; (in a popup in the GUI frame, eldoc-box); apheleia formats on save
;; without moving the cursor; consult-eglot searches symbols.
;; The language modules choose the servers and formatters.
;;
;; Keys added here, by lesson:
;;   7.1  M-. M-, M-?  definition, back, references (built in);
;;        C-c l r rename, C-c l a code actions, C-c l f format,
;;        C-c l s workspace symbols, C-c l h documentation at point
;;   7.2  M-n / M-p  next / previous diagnostic (in Flymake buffers);
;;        C-c l d   all diagnostics of the buffer;  C-h .  the one here
;;   7.3  formatting on save (apheleia), C-c l f formats now
;;   7.7  dape: M-x dape (TODO: debug adapters are not in the image yet)

;;;; Lesson 7.1  Eglot

(setq eglot-autoshutdown t
      eglot-extend-to-xref t
      eglot-events-buffer-config '(:size 0 :format full)
      eglot-report-progress t
      eglot-sync-connect 1)

;; These commands are not autoloaded by their packages; declaring them
;; lets C-c l work before the first Eglot buffer loads the library.
(autoload 'eglot-rename "eglot" nil t)
(autoload 'eglot-code-actions "eglot" nil t)
(autoload 'eglot-format-buffer "eglot" nil t)
(autoload 'flymake-show-buffer-diagnostics "flymake" nil t)

(defvar playground-lsp-map
  (let ((map (make-sparse-keymap)))
    (keymap-set map "r" #'eglot-rename)
    (keymap-set map "a" #'eglot-code-actions)
    (keymap-set map "f" #'playground-format-buffer)
    (keymap-set map "d" #'flymake-show-buffer-diagnostics)
    (keymap-set map "h" #'playground-doc-at-point)
    map)
  "Language-server commands under C-c l.")
(keymap-global-set "C-c l" playground-lsp-map)

(playground-with-package consult-eglot
  (keymap-set playground-lsp-map "s" #'consult-eglot-symbols))

(defun playground-doc-at-point ()
  "Show the documentation of the thing at point.
In the GUI frame it pops up next to point (eldoc-box); in the terminal
frame it opens the *eldoc* buffer."
  (interactive)
  (if (and (display-graphic-p) (fboundp 'eldoc-box-help-at-point))
      (eldoc-box-help-at-point)
    (eldoc-doc-buffer t)))

;;;; Lesson 7.2  Flymake and diagnostics

(with-eval-after-load 'flymake
  (keymap-set flymake-mode-map "M-n" #'flymake-goto-next-error)
  (keymap-set flymake-mode-map "M-p" #'flymake-goto-prev-error)
  (setq flymake-mode-line-lighter "Flymake"
        flymake-show-diagnostics-at-end-of-line nil))

;;;; Lesson 7.3  Format on save with apheleia

(playground-with-package apheleia
  (apheleia-global-mode 1))

(defun playground-format-buffer ()
  "Format the buffer with apheleia's formatter, or the language server's."
  (interactive)
  (cond ((and (fboundp 'apheleia-format-buffer) (bound-and-true-p apheleia-mode)
              (ignore-errors (apheleia--get-formatters)))
         (call-interactively #'apheleia-format-buffer))
        ((bound-and-true-p eglot--managed-mode) (eglot-format-buffer))
        (t (indent-region (point-min) (point-max)))))
(declare-function apheleia--get-formatters "apheleia")

;;;; Lesson 7.7  Debugging with dape

(setq dape-buffer-window-arrangement 'right
      dape-inlay-hints t)
;; TODO(emacs, emacs.md section 2): codelldb (Rust) and debugpy (Python)
;; are not installed in the image yet, so dape has no adapters to run.

;;; lsp.el ends here
