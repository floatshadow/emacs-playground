;;; lang-ocaml.el --- OCaml  -*- lexical-binding: t; -*-

;; Module: lang-ocaml.  Unlocked in Part 7 (lesson 7.5), with lsp.
;;
;; tuareg is the major mode for .ml and .mli files; ocaml-lsp-server
;; (ocamllsp) talks to Eglot for completion, types, rename and
;; diagnostics; ocaml-eglot adds Merlin's commands on top of it (the
;; type of the expression at point, destructuring a match); dune-mode
;; edits dune files; utop is the toplevel, in a buffer beside the code;
;; ocamlformat formats on save (apheleia, lsp module).  The tools come
;; from the image's opam switch (/opt/opam/default, on PATH).
;;
;; Keys in OCaml buffers, by lesson:
;;   7.5  C-c C-c  dune build          C-c C-t  type at point (C-c C-t
;;                                              again: the enclosing one)
;;        C-c C-s  start utop          C-c C-e  send the phrase to utop
;;        C-c C-b  send the buffer     C-c C-z  go to utop
;;        C-c |    destruct: fill a match with every case (P2)
;;        C-c C-l  definition          C-c C-a  .ml <-> .mli
;;        C-c C-r  dune runtest        C-c C-p  dune promote (accept
;;                                              an expect test's output)

;;;; Major modes

;; Nothing to turn on: tuareg's autoloads claim .ml, .mli, .mly and
;; friends, dune's claim dune, dune-project and dune-workspace.

;;;; Eglot: ocaml-lsp-server

(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs
               '((tuareg-mode :language-id "ocaml") . ("ocamllsp"))))
(add-hook 'tuareg-mode-hook #'eglot-ensure)

;; ocaml-eglot speaks Merlin's extra requests through ocamllsp.
;; Its " OCaml-eglot" lighter is left out: the mode line already says
;; Eglot.
(playground-with-package ocaml-eglot
  (add-hook 'tuareg-mode-hook #'ocaml-eglot-mode)
  (with-eval-after-load 'ocaml-eglot
    (setf (alist-get 'ocaml-eglot-mode minor-mode-alist) '(""))
    ;; ocaml-eglot moves between phrases with C-c C-p and C-c C-n; here
    ;; C-c C-p promotes expect-test output (below), as P2 teaches.
    (keymap-unset ocaml-eglot-mode-map "C-c C-p" t)))

;;;; dune through compile

(defun playground-dune-root ()
  "The directory of the nearest dune-project above the current file."
  (or (locate-dominating-file default-directory "dune-project")
      default-directory))

(defun playground-dune (args)
  "Run dune with ARGS in the project root through `compile'."
  (let ((default-directory (playground-dune-root)))
    (compile (concat "dune " args))))

(defun playground-dune-build () "dune build." (interactive) (playground-dune "build"))
(defun playground-dune-test () "dune runtest." (interactive) (playground-dune "runtest"))
(defun playground-dune-promote ()
  "dune promote: accept the output expect tests printed."
  (interactive)
  (playground-dune "promote"))

(defun playground-ocaml-setup ()
  "Make M-x compile default to dune build in OCaml buffers."
  (when (locate-dominating-file default-directory "dune-project")
    (setq-local compile-command "dune build")))
(add-hook 'tuareg-mode-hook #'playground-ocaml-setup)

(with-eval-after-load 'tuareg
  (keymap-set tuareg-mode-map "C-c C-c" #'playground-dune-build)
  (keymap-set tuareg-mode-map "C-c C-r" #'playground-dune-test)
  (keymap-set tuareg-mode-map "C-c C-p" #'playground-dune-promote))

;;;; utop

;; In a dune project utop starts through `dune utop', so the project's
;; libraries are loaded; elsewhere it is plain utop.  utop-minor-mode
;; gives the source buffer the keys that send code to it.
(playground-with-package utop
  (defun playground-utop-command ()
    "The utop command for the current buffer."
    (if (locate-dominating-file default-directory "dune-project")
        "dune utop . -- -emacs"
      "utop -emacs"))

  (defun playground--utop-setup ()
    "Turn on `utop-minor-mode' with the right utop command."
    (setq-local utop-command (playground-utop-command))
    (utop-minor-mode 1))
  (add-hook 'tuareg-mode-hook #'playground--utop-setup)
  (setq utop-edit-command nil))

;;;; Formatting

;; Nothing to set: apheleia (lsp module) maps tuareg-mode to
;; ocamlformat, with --enable-outside-detected-project so a project
;; without an .ocamlformat file is formatted with the default profile.

;;; lang-ocaml.el ends here
