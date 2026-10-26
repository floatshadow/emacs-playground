;;; lang-ocaml.el --- OCaml  -*- lexical-binding: t; -*-

;; Module: lang-ocaml.  Unlocked in Part 7 (lesson 7.5), with lsp.
;;
;; Planned: tuareg (major mode), ocaml-lsp-server through Eglot, merlin's
;; eldoc, dune integration, utop (the toplevel), ocamlformat on save.
;;
;; TODO(emacs, emacs.md sections 2 and 3): not in slice 1.  The image has
;; no opam switch yet, and packages.lock has no tuareg, dune, utop or
;; merlin-eldoc.  Until then this module only tells Eglot which server
;; OCaml buffers would use, so turning it on does no harm.

(with-eval-after-load 'eglot
  (add-to-list 'eglot-server-programs
               '((tuareg-mode caml-mode) . ("ocamllsp"))))

;;; lang-ocaml.el ends here
