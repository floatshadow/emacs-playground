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
;;                  consult 1.0).
;;
;; Slice 1 covers the modules base, editing, completion, files,
;; windows, git, terminals, lsp, lang-rust, lang-python and ui.
;; TODO(emacs, emacs.md section 3): packages for lang-ocaml (tuareg,
;; dune, utop, merlin-eldoc), writing (edit-indirect, typst-ts-mode,
;; auctex, cdlatex, pdf-tools, citar, jinx), org (org-roam,
;; org-present, ob-rust), power (wgrep), terminals (vterm, needs
;; cmake and libvterm at build time), git (forge), input (rime).

(:modules
 ((base kkp clipetty)
  (editing avy expreg multiple-cursors iedit vundo visual-replace ws-butler)
  (completion vertico orderless marginalia consult embark embark-consult
              corfu cape tempel)
  (files dirvish consult-dir)
  (windows ace-window popper burly olivetti)
  (git magit diff-hl git-timemachine)
  (terminals eat)
  (lsp eldoc-box apheleia dape consult-eglot)
  (lang-python pet)
  (ui nerd-icons nerd-icons-dired nerd-icons-completion nerd-icons-corfu
      nerd-icons-ibuffer breadcrumb pulsar helpful ef-themes ligature)
  ;; Only markdown-mode for now: lesson fixtures open README.md files.
  (writing markdown-mode))
 :pin
 ((dirvish . "melpa-stable"))
 :prefer-snapshot
 (nerd-icons nerd-icons-corfu nerd-icons-ibuffer consult-dir visual-replace
  helpful elisp-refs eldoc-box burly ligature))
