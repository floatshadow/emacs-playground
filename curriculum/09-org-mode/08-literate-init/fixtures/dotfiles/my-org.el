;;; my-org.el --- my Org settings  -*- lexical-binding: t; -*-
;; Written by hand.  init.org is taking over: once it tangles, this
;; file is made from it.
(provide 'my-org)

;; Keep tags right after the heading text.
(setq org-tags-column 0)

;; No table of contents in exports.
(setq org-export-with-toc nil)
