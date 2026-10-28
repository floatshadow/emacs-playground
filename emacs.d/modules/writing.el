;;; writing.el --- Markdown, Typst, LaTeX, PDF, citations, spelling  -*- lexical-binding: t; -*-

;; Module: writing.  Unlocked in Part 8 (Writing), with input.
;;
;; Markdown (markdown-mode; pandoc renders it; edit-indirect edits a
;; fenced block in its own mode), Typst (typst-ts-mode with the
;; tinymist language server), LaTeX (AUCTeX with latexmk, cdlatex,
;; RefTeX and the texlab language server), PDFs (pdf-tools in the GUI
;; frame, with SyncTeX between source and PDF), citations from a .bib
;; file (citar) and spell checking (jinx, with the hunspell
;; dictionaries through enchant).  The image ships the tools: pandoc,
;; typst, tinymist, TeX Live, texlab, marksman.
;;
;; Keys added here, by lesson:
;;   8.1  Markdown: C-c C-s b bold, C-c C-l link, M-RET list item,
;;        C-c C-c p preview (pandoc, shown in eww), C-c ' edit a
;;        fenced code block, TAB in a table aligns it (markdown-mode)
;;   8.2  Typst: C-c C-c compile, C-c C-w watch (the PDF follows every
;;        save), C-c C-p open the PDF; errors in Flymake (M-g n)
;;   8.3  LaTeX: C-c C-c build (latexmk), C-c C-e environment,
;;        C-c C-s section, ` and ' cdlatex symbols and accents,
;;        C-c ( label, C-c ) reference (RefTeX), C-c ` next error
;;   8.4  PDFs (GUI frame): n p SPC pages, o outline, C-s search,
;;        C-c C-v from the LaTeX source to the PDF, double-click in the
;;        PDF back to the source; in the terminal frame a PDF shows as
;;        text
;;   8.6  C-c C-x @  insert a citation (Typst, LaTeX, Markdown; Org's
;;        own key in the org module); M-x citar-open
;;   8.8  M-$ correct the misspelling near point (jinx), C-M-$ switch
;;        dictionaries; M-x count-words; olivetti (windows module)

;;;; Lesson 8.1  Markdown with live preview

;; markdown-mode's autoloads claim .md files from Part 0 on; this
;; module adds the renderer and the code-block editor.  C-c C-c p
;; writes the HTML and browses it; the container has no web browser,
;; so URLs open in eww, Emacs's own (C-c C-c l keeps an eww preview
;; that follows every save).
(setq markdown-command "pandoc --standalone --metadata title=Preview"
      markdown-fontify-code-blocks-natively t
      markdown-hide-urls nil
      markdown-enable-math t
      browse-url-browser-function #'eww-browse-url)

;; C-c ' edits the fenced block at point in a buffer of its language
;; (edit-indirect; markdown-mode uses it when it is installed).
(playground-with-package edit-indirect
  (autoload 'edit-indirect-region "edit-indirect" nil t))

;;;; Lesson 8.2  Typst with tinymist

;; typst-ts-mode's autoloads claim .typ files.  Its grammar ships with
;; the image (/opt/playground/tree-sitter).  The PDF is written next to
;; the source, where the browser's preview pane picks it up.
(playground-with-package typst-ts-mode
  ;; C-c C-p opens the PDF in Emacs (pdf-tools in the GUI frame).
  (setq typst-ts-preview-function #'find-file-other-window)
  (with-eval-after-load 'eglot
    (add-to-list 'eglot-server-programs '(typst-ts-mode . ("tinymist"))))
  (add-hook 'typst-ts-mode-hook #'eglot-ensure))

;;;; Lesson 8.3  LaTeX with AUCTeX

;; AUCTeX's LaTeX-mode for .tex files, latexmk as the build (it runs
;; biber or bibtex and reruns LaTeX as needed), PDF output with
;; SyncTeX, no question about the master file (each file is its own),
;; cdlatex for fast maths, RefTeX for labels, references and citations.
(playground-with-package auctex
  ;; TeX-auto-save would leave auto/ directories next to the sources.
  (setq TeX-auto-save nil
        TeX-parse-self t
        TeX-master t
        TeX-PDF-mode t
        TeX-save-query nil
        TeX-command-default "LaTeXMk"
        TeX-source-correlate-mode t
        TeX-source-correlate-method 'synctex
        TeX-view-program-selection '((output-pdf "PDF Tools"))
        reftex-plug-into-AUCTeX t
        reftex-default-bibliography nil
        LaTeX-electric-left-right-brace t)
  ;; latexmk is the default for C-c C-c in every LaTeX buffer.
  (defun playground--latex-setup ()
    "LaTeX buffers: latexmk by default, RefTeX, cdlatex, the language server."
    (setq-local TeX-command-default "LaTeXMk")
    (turn-on-reftex)
    (when (fboundp 'turn-on-cdlatex) (turn-on-cdlatex))
    (eglot-ensure))
  (add-hook 'LaTeX-mode-hook #'playground--latex-setup)
  ;; The PDF buffer reloads when a build finishes.
  (add-hook 'TeX-after-compilation-finished-functions #'TeX-revert-document-buffer)
  (with-eval-after-load 'eglot
    (add-to-list 'eglot-server-programs '((LaTeX-mode :language-id "latex") . ("texlab")))))

(playground-with-package cdlatex
  ;; In LaTeX buffers cdlatex's TAB expands its abbreviations (equ TAB
  ;; makes an equation environment) and jumps out of braces; its $ is
  ;; left to AUCTeX, which already pairs it.
  (setq cdlatex-takeover-dollar nil))

;;;; Lesson 8.4  pdf-tools and SyncTeX (GUI frame)

;; A PDF opens in pdf-view-mode in the GUI frame, rendered by epdfinfo
;; (built with the image).  A terminal frame cannot show images, so
;; there the PDF's text is shown instead (doc-view runs pdftotext and
;; shows the NAME.pdf/text buffer).
(defvar playground--pdf-tools-ready nil)

(defun playground-pdf-mode ()
  "Show a PDF: pdf-tools in a graphical frame, its text in a terminal."
  (interactive)
  (cond
   ((and (display-graphic-p) (playground-package-p 'pdf-tools))
    (unless playground--pdf-tools-ready
      (require 'pdf-tools)
      (add-hook 'pdf-view-mode-hook #'pdf-tools-enable-minor-modes)
      (setq playground--pdf-tools-ready t))
    (pdf-view-mode))
   (t
    ;; doc-view asks whether to show the text when it cannot draw.
    (cl-letf (((symbol-function 'y-or-n-p) #'always))
      (doc-view-mode)))))

(add-to-list 'auto-mode-alist '("\\.[pP][dD][fF]\\'" . playground-pdf-mode))
(add-to-list 'magic-mode-alist '("%PDF" . playground-pdf-mode))

(setq pdf-view-display-size 'fit-page
      pdf-view-resize-factor 1.1
      pdf-annot-activate-created-annotations t)

;;;; Lesson 8.5  Images, SVG and inline previews

;; image-mode and SVG are built in; image-dired makes thumbnails with
;; ImageMagick (in the image).  Org's inline images and LaTeX previews
;; are set up in the org module.
(setq image-dired-dir (playground-var "image-dired/")
      image-use-external-converter t)

;;;; Lesson 8.6  Citations with citar

;; The bibliography is every .bib file at the top of the project (or
;; next to the file), plus whatever a LaTeX file names with
;; \addbibresource or \bibliography and an Org file with
;; #+bibliography.  citar inserts @key in Typst, [@key] in Markdown
;; and \cite{key} in LaTeX, without asking for a citation style.
(defun playground-bib-files ()
  "The .bib files at the top of the current project, or in this directory."
  (let ((root (or (when-let* ((proj (project-current))) (project-root proj))
                  default-directory)))
    (directory-files root t "\\.bib\\'")))

(defun playground-citar-typst-insert-citation (keys &optional _invert)
  "Insert the citation KEYS in Typst syntax: @key, separated by spaces."
  (insert (mapconcat (lambda (key) (concat "@" key)) keys " ")))

(playground-with-package citar
  (setq citar-latex-prompt-for-cite-style nil
        citar-latex-prompt-for-extra-arguments nil
        citar-markdown-prompt-for-extra-arguments nil)
  (with-eval-after-load 'citar
    (add-to-list 'citar-major-mode-functions
                 '((typst-ts-mode)
                   (local-bib-files . playground-bib-files)
                   (insert-citation . playground-citar-typst-insert-citation)
                   (insert-keys . playground-citar-typst-insert-citation))))
  (defun playground--citar-local-bibliography ()
    "Let citar see the project's .bib files in this buffer."
    (setq-local citar-bibliography (playground-bib-files)))
  (dolist (hook '(LaTeX-mode-hook markdown-mode-hook typst-ts-mode-hook))
    (add-hook hook #'playground--citar-local-bibliography))
  (with-eval-after-load 'latex
    (keymap-set LaTeX-mode-map "C-c C-x @" #'citar-insert-citation))
  (with-eval-after-load 'markdown-mode
    (keymap-set markdown-mode-map "C-c C-x @" #'citar-insert-citation))
  (with-eval-after-load 'typst-ts-mode
    (keymap-set typst-ts-mode-map "C-c C-x @" #'citar-insert-citation)))

(playground-with-package citar-embark
  (with-eval-after-load 'embark
    (with-eval-after-load 'citar
      (citar-embark-mode 1))))

;;;; Lesson 8.8  Focused writing

;; jinx checks the words on screen as you type, in prose buffers
;; (Markdown, Org, LaTeX, Typst, commit messages), with American and
;; British English dictionaries.  M-$ replaces ispell-word.
(playground-with-package jinx
  (setq jinx-languages "en_US")
  (add-hook 'text-mode-hook #'jinx-mode)
  (keymap-global-set "M-$" #'jinx-correct)
  (keymap-global-set "C-M-$" #'jinx-languages)
  (with-eval-after-load 'jinx
    (setf (alist-get 'jinx-mode minor-mode-alist) '(""))))

;;; writing.el ends here
