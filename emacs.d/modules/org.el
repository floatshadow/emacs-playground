;;; org.el --- Org  -*- lexical-binding: t; -*-

;; Module: org.  Unlocked in Part 9 (Org-mode).
;;
;; Org is built in; this module sets where notes, tasks and captures
;; go, which languages code blocks may run (shell, Python, OCaml, Rust,
;; Emacs Lisp), how exports find LaTeX and open their results,
;; citations through citar, linked notes with org-roam and slides with
;; org-present.
;;
;; Files: everything lives under /work/org (`org-directory'): the
;; agenda reads every .org file there, captures go to inbox.org, notes
;; to roam/.  The directories are made the first time they are needed.
;;
;; Keys added here, by lesson:
;;   9.2  C-c a agenda, C-c c capture (C-c C-c files it, C-c C-k
;;        cancels); in Org: C-c C-t, C-c C-s, C-c C-d, C-c C-q (built in)
;;   9.4  C-c C-c on a code block runs it without asking; C-c ' edits
;;        it in its language's mode
;;   9.6  C-c C-e export (h o HTML, l p PDF, l b Beamer, m m Markdown);
;;        M-x org-present, then SPC or <right> / <left> between
;;        slides, C-c C-q to stop
;;   9.7  C-c n f find or make a note, C-c n i link one here,
;;        C-c n l backlinks, C-c n d today's daily note
;;   8.5  C-c C-x C-v inline images, C-c C-x C-l LaTeX previews (GUI)
;;   8.6  C-c C-x @ cite (citar)

;;;; Where things go

(defvar playground-org-root
  (expand-file-name "org" (or (getenv "PG_WORKSPACE") "/work"))
  "Directory of the learner's Org files.")

(setq org-directory playground-org-root
      org-default-notes-file (expand-file-name "inbox.org" playground-org-root)
      org-agenda-files (list playground-org-root)
      org-id-locations-file (playground-var "org-id-locations")
      org-persist-directory (playground-var "org-persist/")
      org-preview-latex-image-directory
      (expand-file-name "org-latex-previews/" (or (getenv "XDG_CACHE_HOME") "~/.cache")))

(defun playground--org-make-directories (&rest _)
  "Make `org-directory' before the agenda or a capture needs it."
  (make-directory org-directory t))
(advice-add 'org-agenda :before #'playground--org-make-directories)
(advice-add 'org-capture :before #'playground--org-make-directories)

;;;; Lesson 9.1  Outlining and structure editing

(setq org-special-ctrl-a/e t
      org-M-RET-may-split-line '((default . nil))
      org-insert-heading-respect-content t
      org-refile-targets '((nil :maxlevel . 3) (org-agenda-files :maxlevel . 2))
      org-refile-use-outline-path 'file
      org-outline-path-complete-in-steps nil)

;;;; Lesson 9.2  TODOs, scheduling, the agenda and capture

(keymap-global-set "C-c a" #'org-agenda)
(keymap-global-set "C-c c" #'org-capture)

;; windmove (lesson 3.2) puts S-<arrows> on window motion everywhere.
;; In Org buffers, the agenda and Org's date prompt, Org's own
;; S-<arrows> (TODO states, priorities, dates) win; they still move
;; between windows where Org has nothing to change.
(defun playground--org-shift-keys ()
  "Let Org's S-<arrow> keys shadow windmove's in this buffer."
  (setq-local minor-mode-overriding-map-alist
              (cons (cons 'windmove-mode (make-sparse-keymap))
                    minor-mode-overriding-map-alist)))
(add-hook 'org-mode-hook #'playground--org-shift-keys)
(add-hook 'org-agenda-mode-hook #'playground--org-shift-keys)
(advice-add 'org-read-date :around
            (lambda (read &rest args)
              (defvar windmove-mode)
              (let ((windmove-mode nil)) (apply read args)))
            '((name . playground-org-shift-keys)))
(with-eval-after-load 'org
  (when (fboundp 'windmove-up)
    (add-hook 'org-shiftup-final-hook #'windmove-up)
    (add-hook 'org-shiftdown-final-hook #'windmove-down)
    (add-hook 'org-shiftleft-final-hook #'windmove-left)
    (add-hook 'org-shiftright-final-hook #'windmove-right)))

(setq org-log-done 'time
      org-agenda-span 'week
      org-agenda-start-on-weekday nil
      org-agenda-window-setup 'current-window
      org-agenda-restore-windows-after-quit t
      ;; %a links back to where the capture started (a file and line).
      org-capture-templates
      '(("t" "Task" entry (file+headline org-default-notes-file "Tasks")
         "* TODO %?\n  %a")
        ("n" "Note" entry (file+headline org-default-notes-file "Notes")
         "* %?\n  %U\n  %a")))

;;;; Lesson 9.3  Tables and spreadsheet formulas

;; Built in: | and TAB, C-c = formulas, C-u C-c * recalculates,
;; C-c } shows the coordinates.

;;;; Lesson 9.4  Babel: run code, pass results between languages

;; The languages code blocks may run.  Rust has no Babel support in Org
;; (ob-rust needs rust-script), so `org-babel-execute:rust' below
;; compiles a block with rustc.
(setq org-confirm-babel-evaluate nil
      org-babel-python-command "python3"
      org-src-window-setup 'current-window
      org-src-preserve-indentation t
      org-edit-src-content-indentation 0)

(with-eval-after-load 'org
  (org-babel-do-load-languages
   'org-babel-load-languages
   '((emacs-lisp . t) (shell . t) (python . t) (ocaml . t) (calc . t))))

(with-eval-after-load 'org-src
  (add-to-list 'org-src-lang-modes '("rust" . rust-ts))
  (add-to-list 'org-src-lang-modes '("ocaml" . tuareg)))

(defvar org-babel-default-header-args:rust '((:results . "output"))
  "Rust blocks print their result.")

(defun playground-ob-rust--literal (value)
  "VALUE (a number, string or list from Org) as a Rust literal and type.
Returns (TYPE . LITERAL).  Lists become slices; a table (a list of
lists) becomes a slice of slices; mixed cells become strings."
  (cl-labels
      ((num-type (xs) (if (seq-every-p #'integerp xs) "i64" "f64"))
       (num (x type) (if (equal type "f64") (format "%s" (float x)) (format "%d" x)))
       (str (x) (format "%S" (if (stringp x) x (format "%s" x)))))
    (cond
     ((integerp value) (cons "i64" (format "%d" value)))
     ((numberp value) (cons "f64" (format "%s" value)))
     ((and (consp value) (seq-every-p #'consp (remq 'hline value)))
      (let* ((rows (remq 'hline value))
             (cells (apply #'append rows)))
        (if (seq-every-p #'numberp cells)
            (let ((type (num-type cells)))
              (cons (format "&[&[%s]]" type)
                    (format "&[%s]"
                            (mapconcat (lambda (row)
                                         (format "&[%s]" (mapconcat (lambda (x) (num x type)) row ", ")))
                                       rows ", "))))
          (cons "&[&[&str]]"
                (format "&[%s]"
                        (mapconcat (lambda (row) (format "&[%s]" (mapconcat #'str row ", ")))
                                   rows ", "))))))
     ((consp value)
      (if (seq-every-p #'numberp value)
          (let ((type (num-type value)))
            (cons (format "&[%s]" type)
                  (format "&[%s]" (mapconcat (lambda (x) (num x type)) value ", "))))
        (cons "&[&str]" (format "&[%s]" (mapconcat #'str value ", ")))))
     (t (cons "&str" (str value))))))

;; Org's macro, so this file indents the same (on save) before ob-core loads.
(put 'org-babel-result-cond 'lisp-indent-function 1)

(defun org-babel-execute:rust (body params)
  "Compile the Rust block BODY with rustc and run it; PARAMS are its headers.
:var NAME=VALUE becomes a constant NAME visible to the block; a block
without `fn main' is wrapped in one.  The output is the result."
  (let* ((src (org-babel-temp-file "ob-rust-" ".rs"))
         ;; Not in /tmp: the session mounts it noexec.
         (bin (make-temp-name (playground-var "ob-rust/bin-")))
         (vars (mapconcat
                (lambda (pair)
                  (pcase-let ((`(,type . ,literal) (playground-ob-rust--literal (cdr pair))))
                    (format "#[allow(non_upper_case_globals, dead_code)]\nconst %s: %s = %s;\n"
                            (car pair) type literal)))
                (org-babel--get-vars params) ""))
         (code (concat vars
                       (if (string-match-p "fn[ \t\n]+main[ \t\n]*(" body)
                           body
                         (concat "fn main() {\n" body "\n}\n")))))
    (with-temp-file src (insert code))
    (org-babel-eval (format "rustc --edition 2024 -o %s %s"
                            (shell-quote-argument bin) (shell-quote-argument src))
                    "")
    ;; A compile error is already shown (*Org-Babel Error Output*).
    (when (file-exists-p bin)
      (let ((output (unwind-protect (org-babel-eval (shell-quote-argument bin) "")
                      (delete-file bin))))
        (org-babel-result-cond (cdr (assq :result-params params))
          output
          (let ((tmp (org-babel-temp-file "ob-rust-out-")))
            (with-temp-file tmp (insert output))
            (org-babel-import-elisp-from-file tmp)))))))

(defun org-babel-prep-session:rust (_session _params)
  "Rust blocks have no sessions."
  (error "Rust code blocks do not support sessions"))

;;;; Lesson 9.6  Export

;; LaTeX exports build with latexmk (it runs biber when citations need
;; it) in Latin Modern; HTML colours code blocks (htmlize).  An exported
;; HTML file opens in eww and a PDF in Emacs (pdf-tools in the GUI
;; frame): the container has no desktop to hand them to.
(setq org-latex-compiler "pdflatex"
      org-latex-pdf-process
      '("latexmk -pdf -interaction=nonstopmode -output-directory=%o %f")
      org-latex-packages-alist '(("" "lmodern" nil))
      org-html-validation-link nil
      org-export-with-smart-quotes t
      org-file-apps '((auto-mode . emacs)
                      (directory . emacs)
                      ("\\.x?html?\\'" . (lambda (file _link) (eww-open-file file)))
                      ("\\.pdf\\'" . emacs)))

(with-eval-after-load 'ox
  (require 'ox-beamer)
  (require 'ox-md))

;; org-present: slides from top-level headings, in the current window.
(playground-with-package org-present
  (defun playground--org-present-start ()
    "Bigger text and no mode-line clutter while presenting."
    (org-present-big)
    (org-display-inline-images)
    (org-present-hide-cursor)
    (org-present-read-only))
  (defun playground--org-present-end ()
    "Undo `playground--org-present-start'."
    (org-present-small)
    (org-remove-inline-images)
    (org-present-show-cursor)
    (org-present-read-write))
  (add-hook 'org-present-mode-hook #'playground--org-present-start)
  (add-hook 'org-present-mode-quit-hook #'playground--org-present-end))

;;;; Lesson 8.5  Inline images and LaTeX previews

(setq org-image-actual-width '(400)
      org-startup-with-inline-images nil)
(with-eval-after-load 'org
  (setq org-format-latex-options (plist-put org-format-latex-options :scale 1.5)))

;;;; Lesson 8.6  Citations

;; org-cite with citar: C-c C-x @ inserts [cite:@key]; #+bibliography:
;; names the .bib file, else the project's .bib files are used.
(playground-with-package citar
  (setq org-cite-insert-processor 'citar
        org-cite-follow-processor 'citar
        org-cite-activate-processor 'citar)
  (add-hook 'org-mode-hook
            (lambda ()
              (when (fboundp 'playground-bib-files)
                (setq-local citar-bibliography (playground-bib-files))))))

;;;; Lesson 9.7  Notes with org-roam

(playground-with-package org-roam
  (setq org-roam-directory (expand-file-name "roam" playground-org-root)
        org-roam-db-location (playground-var "org-roam.db")
        org-roam-dailies-directory "daily/"
        org-roam-completion-everywhere nil)
  (defun playground--org-roam-directory (&rest _)
    "Make `org-roam-directory' before org-roam needs it."
    (make-directory org-roam-directory t))
  (advice-add 'org-roam-db-sync :before #'playground--org-roam-directory)
  (defun playground--org-roam-dailies-directory (&rest _)
    "Make the daily notes' directory: C-c n d fails without it."
    (make-directory (expand-file-name org-roam-dailies-directory org-roam-directory) t))
  (advice-add 'org-roam-dailies--capture :before #'playground--org-roam-dailies-directory)
  ;; Notes also change behind Emacs's back (a checkpoint restore, a git
  ;; checkout): bring the database up to date before offering notes.
  ;; With a few hundred notes this takes well under a second.
  (defun playground--org-roam-sync (&rest _)
    "Update the org-roam database from the files."
    (org-roam-db-sync))
  (advice-add 'org-roam-node-read :before #'playground--org-roam-sync)
  (keymap-global-set "C-c n f" #'org-roam-node-find)
  (keymap-global-set "C-c n i" #'org-roam-node-insert)
  (keymap-global-set "C-c n l" #'org-roam-buffer-toggle)
  (keymap-global-set "C-c n d" #'org-roam-dailies-goto-today)
  (keymap-global-set "C-c n c" #'org-roam-capture)
  ;; The database follows the files once org-roam is in use.
  (with-eval-after-load 'org-roam
    (playground--org-roam-directory)
    (org-roam-db-autosync-mode 1)))

;;;; Lesson 9.8  A literate init.org

;; Built in: C-c C-v t (org-babel-tangle) writes every block with a
;; :tangle header to its file.

;;; org.el ends here
