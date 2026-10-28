;;; all-modules-test.el --- ERT tests with every module on  -*- lexical-binding: t; -*-

;; Run from emacs.d/ in the session image (modules-test.el runs it in a
;; child Emacs, so `make emacs-test' covers it):
;;
;;   emacs --batch -l early-init.el -l test/all-modules-test.el \
;;     -f ert-run-tests-batch-and-exit
;;
;; Loads init.el with every module, the set Part 13 runs with (modules
;; unlock monotonically, so every lesson must also work with it), and
;; checks that each module loads without errors or missing packages,
;; the keys the lessons of Parts 4 to 13 teach, the compiled parts of
;; vterm, jinx and pdf-tools, the toolchains the modules call, the
;; language servers they register, and a few behaviours that are easy
;; to break: Org's Rust blocks, compile's error regexps, citations in
;; Typst.

;;; Code:

(require 'ert)
(require 'seq)

(defvar pg-all-test-dir
  (file-name-directory (directory-file-name
                        (file-name-directory (or load-file-name buffer-file-name))))
  "The emacs.d directory under test.")

(defconst pg-all-modules
  '(base agent editing files windows completion git terminals lsp
    lang-rust lang-ocaml lang-python writing input org power remote ui)
  "Every module, in the order the course unlocks them.")

(defvar playground-module-errors)
(defvar playground-modules-loaded)

(unless (featurep 'pg-all-test-init)
  (let ((process-environment
         (append (list (concat "PG_MODULES=" (mapconcat #'symbol-name pg-all-modules " "))
                       (concat "PG_WORKSPACE=" (make-temp-file "pg-all-work" t)))
                 process-environment)))
    (load (expand-file-name "init.el" pg-all-test-dir) nil t))
  (provide 'pg-all-test-init))

;;;; Loading

(ert-deftest pg-all/every-module-loads ()
  "Every module loads, in order, without errors."
  (should-not playground-module-errors)
  (should (equal playground-modules-loaded pg-all-modules)))

(ert-deftest pg-all/no-missing-packages ()
  "No module skipped settings because a package is missing."
  (let ((warnings (get-buffer "*Warnings*")))
    (should-not (and warnings
                     (with-current-buffer warnings
                       (save-excursion
                         (goto-char (point-min))
                         (re-search-forward "is not installed" nil t)))))))

;;;; Global keys

(defconst pg-all-global-keys
  '(;; Part 4: completion
    ("C-x b" . consult-buffer) ("M-y" . consult-yank-pop) ("M-s l" . consult-line)
    ("M-s r" . consult-ripgrep) ("M-g i" . consult-imenu) ("C-." . embark-act)
    ("M-+" . tempel-insert)
    ;; Part 5: git
    ("C-x g" . magit-status) ("C-x v t" . git-timemachine)
    ;; Part 7: lsp and dape
    ("C-c l r" . eglot-rename) ("C-c l a" . eglot-code-actions)
    ("C-x C-a b" . dape-breakpoint-toggle)
    ;; Part 8: writing and input
    ("M-$" . jinx-correct) ("C-M-$" . jinx-languages) ("C-x 8 RET" . insert-char)
    ("C-\\" . toggle-input-method)
    ;; Part 9: org
    ("C-c a" . org-agenda) ("C-c c" . org-capture) ("C-c n f" . org-roam-node-find)
    ("C-c n i" . org-roam-node-insert) ("C-c n l" . org-roam-buffer-toggle)
    ("C-c n d" . org-roam-dailies-goto-today)
    ;; Parts 1 to 3 keep theirs
    ("C-x u" . vundo) ("C-;" . iedit-mode) ("M-j" . avy-goto-char-timer)
    ("C-x C-b" . ibuffer) ("C-x C-j" . dirvish-side) ("M-o" . ace-window)
    ("C-`" . popper-toggle)
    ;; Part 13: ui
    ("C-h f" . helpful-callable))
  "Global keys and the commands the lessons say they run.")

(ert-deftest pg-all/global-keys ()
  "The keys of Parts 1 to 13 run the commands the lessons name."
  (dolist (entry pg-all-global-keys)
    (should (equal (cons (car entry) (keymap-lookup global-map (car entry)))
                   entry))))

;;;; Mode keys and hooks

(defun pg-all--key (map-symbol key)
  "The command KEY runs in the keymap held by MAP-SYMBOL."
  (keymap-lookup (symbol-value map-symbol) key))

(ert-deftest pg-all/ocaml-keys ()
  "Lesson 7.5 and P2: dune, utop, Merlin's type and destruct."
  (require 'tuareg)
  (require 'utop)
  (require 'ocaml-eglot)
  (should (eq (pg-all--key 'tuareg-mode-map "C-c C-c") 'playground-dune-build))
  (should (eq (pg-all--key 'tuareg-mode-map "C-c C-p") 'playground-dune-promote))
  (should (eq (pg-all--key 'utop-minor-mode-map "C-c C-e") 'utop-eval-phrase))
  (should (eq (pg-all--key 'utop-minor-mode-map "C-c C-s") 'utop))
  (should (eq (pg-all--key 'ocaml-eglot-mode-map "C-c C-t") 'ocaml-eglot-type-enclosing))
  (should (eq (pg-all--key 'ocaml-eglot-mode-map "C-c |") 'ocaml-eglot-destruct))
  (should-not (pg-all--key 'ocaml-eglot-mode-map "C-c C-p"))
  (should (memq 'playground-eglot-ensure tuareg-mode-hook))
  (should (memq 'ocaml-eglot-mode tuareg-mode-hook)))

(ert-deftest pg-all/writing-keys ()
  "Lessons 8.2, 8.3 and 8.6: compile Typst, build LaTeX, insert citations."
  (require 'typst-ts-mode)
  (require 'latex)
  (require 'markdown-mode)
  (require 'citar)
  (should (eq (pg-all--key 'typst-ts-mode-map "C-c C-c") 'typst-ts-compile))
  (should (eq (pg-all--key 'typst-ts-mode-map "C-c C-w") 'typst-ts-watch-mode))
  (dolist (map '(typst-ts-mode-map LaTeX-mode-map markdown-mode-map))
    (should (eq (pg-all--key map "C-c C-x @") 'citar-insert-citation)))
  (should (memq 'eglot-ensure typst-ts-mode-hook))
  (should (memq 'playground--latex-setup LaTeX-mode-hook))
  (should (equal TeX-command-default "LaTeXMk"))
  (should (equal (cdr (assq 'output-pdf TeX-view-program-selection)) '("PDF Tools")))
  (should (memq 'jinx-mode text-mode-hook)))

(ert-deftest pg-all/git-keys ()
  "Lesson 5.5: C-x v n / p move between hunks, C-x v r reverts one."
  (require 'diff-hl)
  (should (eq (keymap-lookup diff-hl-command-map "n") 'diff-hl-next-hunk))
  (should (eq (keymap-lookup diff-hl-command-map "p") 'diff-hl-previous-hunk))
  (should (eq (keymap-lookup diff-hl-command-map "r") 'diff-hl-revert-hunk)))

(ert-deftest pg-all/power-keys ()
  "Lesson 11.2: C-x C-q makes grep results editable (wgrep)."
  (require 'grep)
  (should (eq (keymap-lookup grep-mode-map "C-x C-q") 'wgrep-change-to-wgrep-mode))
  (should wgrep-auto-save-buffer))

(ert-deftest pg-all/input-methods ()
  "Lesson 8.7: TeX is the default input method; pyim is offered."
  (should (equal default-input-method "TeX"))
  (should (assoc "pyim" input-method-alist))
  (should (assoc "TeX" input-method-alist)))

;;;; Native parts and grammars

(ert-deftest pg-all/vterm-module ()
  "vterm's module was compiled with the image and loads without asking."
  (should (locate-library (concat "vterm-module" module-file-suffix)))
  (require 'vterm)
  (should (featurep 'vterm-module))
  (should (fboundp 'vterm--new)))

(ert-deftest pg-all/jinx-module ()
  "jinx's module loads and enchant finds the English dictionaries."
  (require 'jinx)
  (should (locate-library (concat "jinx-mod" module-file-suffix)))
  (jinx--load-module)
  (let ((langs (mapcar #'car (jinx--mod-langs))))
    (should (member "en_US" (mapcar (lambda (l) (format "%s" l)) langs)))))

(ert-deftest pg-all/epdfinfo ()
  "pdf-tools' epdfinfo server was built with the image and runs."
  (require 'pdf-info)
  (should (file-executable-p pdf-info-epdfinfo-program))
  (should (string-prefix-p "/opt/playground/elpa/pdf-tools-" pdf-info-epdfinfo-program))
  ;; It signals an error when epdfinfo cannot run.
  (should-not (condition-case err (progn (pdf-info-check-epdfinfo) nil)
                (error err))))

(ert-deftest pg-all/grammars ()
  "The tree-sitter grammars of the later parts load."
  (require 'treesit)
  (dolist (lang '(rust python toml json bash yaml typst html css))
    (should (treesit-ready-p lang t))))

;;;; Toolchains and language servers

(ert-deftest pg-all/tools-on-path ()
  "The programs the modules call are on PATH."
  (dolist (program '("cargo" "rust-analyzer" "cargo-nextest" "codelldb"
                     "uv" "basedpyright-langserver" "ruff"
                     "ocaml" "dune" "utop" "ocamllsp" "ocamlformat" "ocamlearlybird" "odoc"
                     "typst" "tinymist" "pdflatex" "latexmk" "biber" "texlab"
                     "marksman" "pandoc" "bash-language-server" "shellcheck" "delta"
                     "rg" "fd" "git" "magick" "pdftotext"))
    (should (equal (list program (and (executable-find program) t)) (list program t))))
  (should (file-executable-p "/opt/debugpy/bin/python")))

(defun pg-all--server (mode)
  "The command line `eglot-server-programs' gives for MODE."
  (require 'eglot)
  (cdr (seq-find (lambda (entry)
                   (let ((modes (car entry)))
                     (seq-some (lambda (m) (eq mode (if (consp m) (car m) m)))
                               (if (and (consp modes) (not (keywordp (cadr modes))))
                                   modes
                                 (list modes)))))
                 eglot-server-programs)))

(ert-deftest pg-all/language-servers ()
  "Each language's buffers talk to the server shipped for it."
  (should (equal (pg-all--server 'rust-ts-mode) '("rust-analyzer")))
  (should (equal (pg-all--server 'python-ts-mode) '("basedpyright-langserver" "--stdio")))
  (should (equal (pg-all--server 'tuareg-mode) '("ocamllsp")))
  (should (equal (pg-all--server 'typst-ts-mode) '("tinymist")))
  (should (equal (pg-all--server 'LaTeX-mode) '("texlab"))))

(ert-deftest pg-all/dape-adapters ()
  "Lesson 7.7: dape knows codelldb for Rust and the image's debugpy."
  (require 'dape)
  (let ((rust (alist-get 'codelldb-rust dape-configs))
        (python (alist-get 'debugpy dape-configs)))
    (should (equal (plist-get rust 'command) "codelldb"))
    (should (equal (plist-get python 'command) "/opt/debugpy/bin/python"))
    (should (eq (plist-get python :python) #'playground-project-python))))

;;;; Behaviour

(ert-deftest pg-all/org-rust-block ()
  "Lesson 9.4: a Rust block compiles with rustc and sees its :var values."
  (require 'org)
  (require 'ob)
  (let ((org-babel-temporary-directory (make-temp-file "pg-ob" t)))
    (should (equal (org-babel-execute:rust
                    "println!(\"{}\", xs.iter().sum::<i64>() * k);"
                    '((:var . (xs . (1 2 3))) (:var . (k . 2))
                      (:result-params "output") (:results . "output")))
                   "12\n"))
    (should (equal (cdr (playground-ob-rust--literal '((1 2.5) hline (3 4))))
                   "&[&[1.0, 2.5], &[3.0, 4.0]]"))
    (should (equal (playground-ob-rust--literal '("a" 1)) '("&[&str]" . "&[\"a\", \"1\"]")))))

(ert-deftest pg-all/org-babel-languages ()
  "Lesson 9.4: shell, Python and OCaml blocks may run, without asking."
  (require 'org)
  (dolist (lang '(shell python ocaml emacs-lisp))
    (should (alist-get lang org-babel-load-languages)))
  (should-not org-confirm-babel-evaluate))

(ert-deftest pg-all/compile-error-regexps ()
  "Lesson 6.1: cargo (rustc), dune (OCaml) and pytest locations are recognised."
  (require 'compile)
  (should (memq 'rust compilation-error-regexp-alist))
  (should (string-match (car (alist-get 'rust compilation-error-regexp-alist-alist))
                        "error[E0308]: mismatched types\n  --> src/main.rs:4:5\n"))
  (let ((caml (car (alist-get 'python-tracebacks-and-caml compilation-error-regexp-alist-alist)))
        (pytest (car (alist-get 'pytest compilation-error-regexp-alist-alist))))
    (should (string-match caml "File \"bin/main.ml\", line 3, characters 4-9:"))
    (should (string-match pytest "tests/test_score.py:12: AssertionError"))
    (should (equal (match-string 1 "tests/test_score.py:12: AssertionError") "tests/test_score.py"))
    (should (string-match pytest "tests/test_score.py:7: in test_mean"))))

(ert-deftest pg-all/typst-citation ()
  "Lesson 8.6: citar inserts @key citations in Typst."
  (with-temp-buffer
    (playground-citar-typst-insert-citation '("knuth" "lamport"))
    (should (equal (buffer-string) "@knuth @lamport"))))

(ert-deftest pg-all/quiet-mode-line ()
  "Housekeeping minors leave the mode line alone: it reads (Rust Eglot)."
  (require 'jinx)
  (require 'ocaml-eglot)
  (should-not apheleia-mode-lighter)
  (should (equal (cdr (assq 'jinx-mode minor-mode-alist)) '("")))
  (should (equal (cdr (assq 'ocaml-eglot-mode minor-mode-alist)) '(""))))

(ert-deftest pg-all/pdf-mode-chooses-by-frame ()
  "Lesson 8.4: PDFs open through `playground-pdf-mode'."
  (should (eq (cdr (assoc "\\.[pP][dD][fF]\\'" auto-mode-alist)) 'playground-pdf-mode)))

(provide 'all-modules-test)
;;; all-modules-test.el ends here
