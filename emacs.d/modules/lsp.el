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
;;   7.2  M-g n / M-g p (also M-n / M-p)  next / previous diagnostic
;;        (in Flymake buffers; M-g M-n stays next-error);
;;        C-c l d   all diagnostics of the buffer;  C-h .  the one here
;;   7.3  formatting on save (apheleia), C-c l f formats now
;;   7.7  M-x dape; C-x C-a b breakpoint; in a session n s o c

;;;; Lesson 7.1  Eglot

(setq eglot-autoshutdown t
      eglot-extend-to-xref t
      eglot-events-buffer-config '(:size 0 :format full)
      eglot-report-progress t
      eglot-sync-connect 1)

;; Rename applies at once, as F2 does in VS Code, even in files that are
;; not open (they are saved by auto-save-visited-mode); other edits a
;; server proposes still show a summary first when they reach such files.
(setq eglot-confirm-server-edits '((eglot-rename . nil) (t . maybe-summary)))

;; `eglot-ensure' connects after the first command in the buffer.  Lesson
;; setups and restores open files without one, so the server (and its
;; diagnostics) would wait for the learner's first key: the language
;; modules use this instead, which runs that pending connection half a
;; second later unless a command already did.
(defun playground-eglot-ensure ()
  "Start Eglot for this buffer soon, as `eglot-ensure' does on a command."
  (let ((before (and (local-variable-p 'post-command-hook) post-command-hook)))
    (eglot-ensure)
    (when-let* ((pending (and (local-variable-p 'post-command-hook)
                              ;; t only means "run the global hook too".
                              (seq-remove (lambda (f) (or (eq f t) (memq f before)))
                                          post-command-hook))))
      (run-with-timer 0.5 nil #'playground--eglot-connect-pending (current-buffer) pending))))

(defun playground--eglot-connect-pending (buffer fns)
  "Run `eglot-ensure''s connection FNS in BUFFER if no command has run them."
  (when (buffer-live-p buffer)
    (with-current-buffer buffer
      (dolist (f fns)
        (when (memq f post-command-hook)
          (funcall f))))))

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
frame it shows the *eldoc* buffer in another window, asking the sources
afresh (`eldoc-doc-buffer' alone fails before eldoc has shown anything)."
  (interactive)
  (if (and (display-graphic-p) (fboundp 'eldoc-box-help-at-point))
      (eldoc-box-help-at-point)
    (eldoc t)))

;;;; Lesson 7.2  Flymake and diagnostics

(with-eval-after-load 'flymake
  (keymap-set flymake-mode-map "M-n" #'flymake-goto-next-error)
  (keymap-set flymake-mode-map "M-p" #'flymake-goto-prev-error)
  ;; M-g n / M-g p walk this buffer's diagnostics too; M-g M-n stays
  ;; next-error, for build output and search results (lesson 6.1).
  (keymap-set flymake-mode-map "M-g n" #'flymake-goto-next-error)
  (keymap-set flymake-mode-map "M-g p" #'flymake-goto-prev-error)
  (setq flymake-mode-line-lighter "Flymake"
        flymake-show-diagnostics-at-end-of-line nil))

;; A buffer reverted to new contents on disk (a lesson's Reset, a step's
;; checkpoint, a git checkout) keeps its modes, and Eglot closes and
;; reopens the document around the revert.  basedpyright then never
;; reports on that file again, and checks that run on save (clippy, for
;; rust-analyzer) keep the old file's results.  So the document stays
;; open: the new text goes as one full change, followed by a save.
(defun playground--eglot-close-before-revert ()
  "Close the document before a revert that will reset the modes."
  (unless revert-buffer-preserve-modes
    (eglot--signal-textDocument/didClose)))

(defun playground--eglot-resync-after-revert ()
  "Send a reverted buffer's text as a full change, then didSave."
  (when (and revert-buffer-preserve-modes (bound-and-true-p eglot--managed-mode))
    (cl-incf eglot--docver)
    (setq eglot--recent-changes :emacs-messup)
    (eglot--signal-textDocument/didSave)))

(defun playground--eglot-revert-setup ()
  "Replace Eglot's close and reopen around reverts in this buffer."
  (when (bound-and-true-p eglot--managed-mode)
    (remove-hook 'before-revert-hook #'eglot--signal-textDocument/didClose t)
    (remove-hook 'after-revert-hook #'eglot--after-revert-hook t)
    (add-hook 'before-revert-hook #'playground--eglot-close-before-revert nil t)
    (add-hook 'after-revert-hook #'playground--eglot-resync-after-revert nil t)))
(add-hook 'eglot-managed-mode-hook #'playground--eglot-revert-setup)

;;;; Lesson 7.3  Format on save with apheleia

(playground-with-package apheleia
  ;; Housekeeping that is on in every buffer: no " Apheleia" in the mode
  ;; line, which reads (Rust Eglot) as lesson 0.1 describes.  M-x
  ;; apheleia-mode still toggles it per buffer.
  (setq apheleia-mode-lighter nil)
  ;; ws-butler (editing module) puts back, after each save, trailing
  ;; whitespace it kept out of the file.  apheleia drops its result when
  ;; the buffer changes while the formatter runs, so it must start after
  ;; ws-butler: last in after-save-hook, behind the global functions.
  ;; (apheleia-mode is defined by the autoloads, before apheleia.el.)
  (define-advice apheleia-mode (:after (&rest _) playground-format-last)
    (when apheleia-mode
      (remove-hook 'after-save-hook #'apheleia-format-after-save 'local)
      (add-hook 'after-save-hook #'apheleia-format-after-save 90 'local)))
  ;; Only saves you make format: not auto-save-visited's (base module).
  (define-advice apheleia-format-after-save (:before-until (&rest _) playground-not-auto-save)
    (bound-and-true-p playground-auto-saving))
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

;; M-x dape offers the configurations for the current buffer's language;
;; the adapters ship with the image: codelldb for Rust (it debugs the
;; crate's target/debug binary, so build first), debugpy for Python (the
;; program runs with the project's Python, the one pet found), and
;; ocamlearlybird for OCaml bytecode (dune's .bc executables).
;; C-x C-a b toggles a breakpoint on the line (C-x C-a is dape's prefix,
;; ready before the first session); in the session's REPL n is next,
;; s step in, o step out, c continue.
(setq dape-buffer-window-arrangement 'right
      dape-inlay-hints t)
(autoload 'dape-breakpoint-toggle "dape" nil t)
(keymap-global-set "C-x C-a b" #'dape-breakpoint-toggle)

(defun playground-cargo-binary ()
  "The debug binary of the crate at point: target/debug/<package name>."
  (let* ((root (or (locate-dominating-file default-directory "Cargo.toml")
                   default-directory))
         (name (with-temp-buffer
                 (ignore-errors (insert-file-contents (expand-file-name "Cargo.toml" root)))
                 (goto-char (point-min))
                 (when (and (re-search-forward "^\\[package\\]" nil t)
                            (re-search-forward "^name *= *\"\\([^\"]+\\)\"" nil t))
                   (match-string 1)))))
    (expand-file-name (concat "target/debug/" (or name (file-name-nondirectory
                                                        (directory-file-name root))))
                      root)))

(defun playground-project-python ()
  "The project's Python (pet's choice), else python3."
  (or (and (fboundp 'pet-executable-find) (ignore-errors (pet-executable-find "python")))
      (executable-find "python3")))

(with-eval-after-load 'dape
  ;; Rust: codelldb, which dape no longer configures itself.
  (add-to-list 'dape-configs
               `(codelldb-rust
                 modes (rust-mode rust-ts-mode)
                 ensure dape-ensure-command
                 command "codelldb"
                 command-args ("--port" :autoport
                               "--settings" "{\"sourceLanguages\":[\"rust\"]}")
                 command-cwd dape-command-cwd
                 port :autoport
                 :type "lldb"
                 :request "launch"
                 :cwd "."
                 :program playground-cargo-binary
                 :args []
                 :stopOnEntry nil
                 ;; Containers forbid turning off address randomisation,
                 ;; which LLDB does before launching unless told not to.
                 :initCommands ["settings set target.disable-aslr false"]))
  ;; Python: debugpy lives in its own environment (/opt/debugpy); the
  ;; program runs with the project's Python.
  (dolist (name '(debugpy debugpy-module))
    (when-let* ((config (alist-get name dape-configs)))
      (setf (alist-get name dape-configs)
            (thread-first (copy-sequence config)
                          (plist-put 'command "/opt/debugpy/bin/python")
                          (plist-put :python #'playground-project-python))))))

;;; lsp.el ends here
