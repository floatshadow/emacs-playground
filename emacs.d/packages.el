;;; packages.el --- packages the configuration modules use  -*- lexical-binding: t; -*-

;; The top-level packages per module (emacs.md section 8, third-party.md
;; section 3).  tools/lock.el resolves their dependencies against GNU
;; ELPA, NonGNU ELPA, MELPA Stable and MELPA (in that order of
;; preference), skips what Emacs already ships in a recent enough
;; version, and writes packages.lock: name, version, archive, file and
;; sha256 of every package, dependencies first.  The session image
;; installs exactly packages.lock and fails on any mismatch.
;;
;; Edit this file, then regenerate the lock (see tools/lock.el).
;;
;; :modules         module name followed by the packages it configures
;; :pin             (PACKAGE . ARCHIVE) pairs: the package comes only from
;;                  that archive.  dirvish: the NonGNU ELPA tarball leaves
;;                  out the extensions/ directory (dirvish-side,
;;                  dirvish-subtree, dirvish-icons), which MELPA's recipe
;;                  includes.
;; :prefer-snapshot packages taken from MELPA rather than MELPA Stable
;;                  because their last stable tag is years old and
;;                  predates Emacs 30/31 or the companion packages
;;                  (nerd-icons 0.1.0 is from 2023; helpful 0.21 breaks
;;                  on Emacs 30's help-fns; consult-dir 0.1 predates
;;                  consult 1.0; citar 1.4.0's autoloads define a struct
;;                  before cl-lib is loaded; vterm has no stable tag).
;;
;; Native parts are built at image time, not by the packages:
;; vterm-module.so (libvterm), jinx-mod.so (enchant) and pdf-tools'
;; epdfinfo (poppler) are compiled in the session Dockerfile's `natives'
;; stage from the vendored tarballs and copied into the installed
;; package directories.

;; One entry per module that configures packages; agent and remote use
;; built-in libraries only.
(:modules
 ((base kkp clipetty)
  (editing avy expreg multiple-cursors iedit vundo visual-replace ws-butler)
  (completion vertico orderless marginalia consult embark embark-consult
              corfu cape tempel)
  (files dirvish consult-dir)
  (windows ace-window popper burly olivetti)
  (git magit forge diff-hl git-timemachine)
  (terminals eat vterm)
  (lsp eldoc-box apheleia dape consult-eglot)
  (lang-python pet)
  (lang-ocaml tuareg dune utop ocaml-eglot)
  (writing markdown-mode edit-indirect typst-ts-mode auctex cdlatex
           pdf-tools citar citar-embark jinx)
  (input pyim pyim-basedict)
  (org org-roam org-present htmlize)
  (power wgrep)
  (ui nerd-icons nerd-icons-dired nerd-icons-completion nerd-icons-corfu
      nerd-icons-ibuffer breadcrumb pulsar helpful ef-themes ligature
      doom-modeline))
 :pin
 ((dirvish . "melpa-stable"))
 :prefer-snapshot
 (nerd-icons nerd-icons-corfu nerd-icons-ibuffer consult-dir visual-replace
  helpful elisp-refs eldoc-box burly ligature vterm citar citar-embark))
