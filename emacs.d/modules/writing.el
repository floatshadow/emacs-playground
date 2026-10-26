;;; writing.el --- Markdown, Typst, LaTeX, PDF  -*- lexical-binding: t; -*-

;; Module: writing.  Unlocked in Part 8 (Writing: Markdown, Typst, LaTeX).
;;
;; Planned: markdown-mode with edit-indirect for code blocks,
;; typst-ts-mode with tinymist, AUCTeX with cdlatex and RefTeX, latexmk,
;; pdf-tools (epdfinfo built at image time) for PDFs in the GUI frame,
;; citar for citations, jinx for spell checking, olivetti for prose.
;;
;; TODO(emacs, emacs.md sections 2, 3 and 6): not in slice 1.  Only
;; markdown-mode is in packages.lock (it is active for .md files through
;; its autoloads whatever the modules); Typst, TeX Live, pdf-tools, jinx
;; and citar are not in the image yet.

(setq markdown-fontify-code-blocks-natively t
      markdown-hide-urls nil)

;;; writing.el ends here
