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
;;        save), C-c C-p open the PDF; errors in Flymake (M-n)
;;   8.3  LaTeX: C-c C-c build (latexmk), C-c C-e environment,
;;        C-c C-s section, ` and ' cdlatex symbols and accents,
;;        C-c ( label, C-c ) reference (RefTeX), C-c ` next error
;;   8.4  PDFs (GUI frame): n p SPC pages, o outline, C-s search,
;;        C-c C-v from the LaTeX source to the PDF, double-click in the
;;        PDF (or M-s s during C-s) back to the source, C-c C-a
;;        annotations; in the terminal frame a PDF shows as text
;;   8.5  image-mode: i + i - zoom, i r rotate, n p next and previous
;;        image; M-x image-dired thumbnails; in Org C-c C-x C-v images
;;        and C-c C-x C-l formulas (C-u C-u: the whole buffer)
;;   8.6  C-c C-x @  insert a citation (Typst, LaTeX, Markdown; Org's
;;        own key in the org module); M-x citar-open; in a .bib file
;;        C-c C-c tidies the entry at point (bibtex-mode)
;;   8.8  M-$ correct the misspelling near point (jinx), C-M-$ switch
;;        dictionaries; M-x count-words; olivetti (windows module)

;;;; Lesson 8.1  Markdown with live preview

;; markdown-mode's autoloads claim .md files from Part 0 on; this
;; module adds the renderer and the code-block editor.  C-c C-c p
;; writes the HTML and browses it; the container has no web browser,
;; so URLs open in eww, Emacs's own, in a window to the right of the
;; source (C-c C-c l keeps an eww preview that follows every save).
(setq markdown-command "pandoc --standalone --metadata title=Preview"
      markdown-fontify-code-blocks-natively t
      markdown-hide-urls nil
      markdown-enable-math t
      browse-url-browser-function #'eww-browse-url)
(add-to-list 'display-buffer-alist
             '("\\`\\*eww\\*"
               (display-buffer-reuse-window display-buffer-in-direction)
               (direction . right) (window-width . 0.5)))

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
  ;; C-u C-c C-w turns watching on whatever its state; one `typst
  ;; watch' at a time, not a second one writing the same PDF.
  (defun playground--typst-watch-running-p (&rest _)
    "Non-nil when a `typst watch' started by C-c C-w is still running."
    (seq-some (lambda (p)
                (and (process-live-p p)
                     (string-prefix-p typst-ts-watch-process-name (process-name p))))
              (process-list)))
  (with-eval-after-load 'typst-ts-watch-mode
    (advice-add 'typst-ts-watch-start :before-until #'playground--typst-watch-running-p))
  (with-eval-after-load 'eglot
    (add-to-list 'eglot-server-programs '(typst-ts-mode . ("tinymist"))))
  (add-hook 'typst-ts-mode-hook #'eglot-ensure))

;;;; Lesson 8.3  LaTeX with AUCTeX

;; AUCTeX's LaTeX-mode for .tex files, latexmk as the build (it runs
;; biber or bibtex and reruns LaTeX as needed), PDF output with
;; SyncTeX, no question about the master file (each file is its own),
;; cdlatex for fast maths, RefTeX for labels, references and citations.

;; biber is a packed Perl program that unpacks itself into $TMPDIR and
;; runs from there, which fails on a noexec /tmp: unpack under ~/.cache.
(let ((dir (expand-file-name "par" (or (getenv "XDG_CACHE_HOME") "~/.cache"))))
  (make-directory dir t)
  (setenv "PAR_GLOBAL_TMPDIR" dir))

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
    ;; A reverted file (a lesson's reset or Reset this step) forgets
    ;; the labels RefTeX collected, so new ones start at eq:1 again.
    (add-hook 'after-revert-hook #'reftex-reset-mode nil t)
    (when (fboundp 'turn-on-cdlatex) (turn-on-cdlatex))
    (eglot-ensure))
  (add-hook 'LaTeX-mode-hook #'playground--latex-setup)
  ;; The PDF buffer reloads when a build finishes.
  (add-hook 'TeX-after-compilation-finished-functions #'TeX-revert-document-buffer)
  ;; latexmk's output holds every LaTeX pass; judge only the last one,
  ;; or the first pass's "Label(s) may have changed" says to run LaTeX
  ;; again after latexmk already did.
  (defun playground--latexmk-last-pass (orig process name)
    "Run the LaTeX sentinel ORIG on the last LaTeX pass of a latexmk run."
    (let ((start (save-excursion
                   (goto-char (point-max))
                   (and (re-search-backward "^Latexmk: " nil t)
                        (re-search-backward "^Run number [0-9]+ of rule '[a-z]*latex" nil t)
                        (line-beginning-position)))))
      (if (not start)
          (funcall orig process name)
        (save-restriction
          (narrow-to-region start (point-max))
          (goto-char (point-min))
          (funcall orig process name)))))
  (with-eval-after-load 'tex
    (advice-add 'TeX-LaTeX-sentinel :around #'playground--latexmk-last-pass))
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
(defvar playground--pdf-mode-depth 0)

;; C-c C-v puts the PDF beside the source, and a window that narrow
;; truncates long lines (`truncate-partial-width-windows'): the source
;; scrolls sideways to point and every line starts with a cut.  Prose
;; wraps at word boundaries instead; only the display changes, so C-e
;; and C-k still work on whole lines.  Org keeps its truncation.
(defun playground--prose-wrap ()
  "Wrap long lines at word boundaries, also in side-by-side windows."
  (setq-local truncate-partial-width-windows nil
              word-wrap t))
(add-hook 'text-mode-hook #'playground--prose-wrap)

(defun playground--pdf-tools-setup (&rest _)
  "Load pdf-tools the first time a PDF is shown in a graphical frame."
  (unless playground--pdf-tools-ready
    (require 'pdf-tools)
    (add-hook 'pdf-view-mode-hook #'pdf-tools-enable-minor-modes)
    (setq playground--pdf-tools-ready t)))

;; C-c C-v searches forward only when pdf-sync is loaded, and
;; otherwise just shows the PDF's first page: load it first.  A PDF
;; shown for the first time starts on page 1, whatever page the search
;; found: search again from the source once the PDF is on screen.  A
;; terminal frame has no page to find: it shows the PDF's text, and
;; epdfinfo never opens (and holds on to) the file.
(defun playground--pdf-sync-view (orig &rest args)
  "Run the forward search ORIG with ARGS, twice if the PDF was not shown."
  (let* ((src (current-buffer))
         (win (selected-window))
         (pdf (ignore-errors
                (expand-file-name (TeX-active-master (TeX-output-extension)))))
         (buf (and pdf (find-buffer-visiting pdf)))
         (shown (and buf (get-buffer-window buf 'visible))))
    (cond
     ((not (display-graphic-p))
      (if (and pdf (file-exists-p pdf))
          (display-buffer (find-file-noselect pdf) pdf-sync-forward-display-action)
        (user-error "No PDF yet: build with C-c C-c first")))
     (t
      (playground--pdf-tools-setup)
      (require 'pdf-sync)
      (apply orig args)
      (unless shown
        (redisplay t)
        (with-selected-window win
          (with-current-buffer src
            (apply orig args))))))))
(with-eval-after-load 'tex
  (advice-add 'TeX-pdf-tools-sync-view :around #'playground--pdf-sync-view))

(defun playground-pdf-mode ()
  "Show a PDF: pdf-tools in a graphical frame, its text in a terminal.
A file doc-view cannot read (missing, empty, half written) falls back
to `fundamental-mode', which would choose this function again."
  (interactive)
  (cond
   ((or (> playground--pdf-mode-depth 0) (zerop (buffer-size)))
    (fundamental-mode))
   ((and (display-graphic-p) (playground-package-p 'pdf-tools))
    (playground--pdf-tools-setup)
    (pdf-view-mode))
   (t
    (let ((playground--pdf-mode-depth (1+ playground--pdf-mode-depth)))
      (doc-view-mode)))))

;; doc-view asks whether to show the text when it cannot draw (a
;; terminal, or no Ghostscript for the graphical frame), when the mode
;; starts and again whenever the file is reverted (a rebuild, a reset):
;; always yes, so nothing waits for an answer.
(defun playground--doc-view-show-text (orig &rest args)
  "Call ORIG with ARGS, answering doc-view's text question with yes."
  (cl-letf (((symbol-function 'y-or-n-p) #'always))
    (apply orig args)))
(advice-add 'doc-view-initiate-display :around #'playground--doc-view-show-text)

(add-to-list 'auto-mode-alist '("\\.[pP][dD][fF]\\'" . playground-pdf-mode))
(add-to-list 'magic-mode-alist '("%PDF" . playground-pdf-mode))

(setq pdf-view-display-size 'fit-page
      pdf-view-resize-factor 1.1
      pdf-annot-activate-created-annotations t
      ;; C-c C-v shows the PDF beside the source, where a page fits best.
      pdf-sync-forward-display-action
      '((display-buffer-reuse-window display-buffer-in-direction)
        (direction . right) (window-width . 0.5)))

;; Closing the outline with M-RET (go to the heading) or C-c C-q kills
;; its buffer; q only buries it, and RET leaves it open.
(defun playground--pdf-outline-kill (&rest _)
  "Arguments for `pdf-outline-quit' that kill the outline buffer."
  (list t))
(with-eval-after-load 'pdf-outline
  (advice-add 'pdf-outline-quit :filter-args #'playground--pdf-outline-kill))

;;;; Lesson 8.5  Images, SVG and inline previews

;; image-mode and SVG are built in; image-dired makes thumbnails with
;; ImageMagick (in the image).  Org is built in too, so its inline
;; images (C-c C-x C-v) and LaTeX previews (C-c C-x C-l) work before
;; the org module: images at most 400 pixels wide, formulas as SVG
;; (dvisvgm) drawn in the text's colour, cached outside the workspace.
;; image-mode, and Org for images with a width, hand images to
;; ImageMagick, which has no SVG reader here and draws a blank square:
;; SVG goes to Emacs's own librsvg instead, which scales it too.
(with-eval-after-load 'image
  (dolist (type '(SVG SVGZ))
    (add-to-list 'imagemagick-types-inhibit type)))
(defun playground--svg-without-imagemagick (args)
  "ARGS for `create-image', with an SVG file's type left to Emacs."
  (pcase args
    (`(,(and (pred stringp) file) imagemagick . ,rest)
     (if (string-match-p "\\.svgz?\\'" file) (cons file (cons nil rest)) args))
    (_ args)))
(advice-add 'create-image :filter-args #'playground--svg-without-imagemagick)

(setq image-dired-dir (playground-var "image-dired/")
      image-use-external-converter t
      org-image-actual-width '(400)
      org-preview-latex-default-process 'dvisvgm
      org-preview-latex-image-directory
      (expand-file-name "org-latex-previews/" (or (getenv "XDG_CACHE_HOME") "~/.cache")))
(with-eval-after-load 'org
  (setq org-format-latex-options (plist-put org-format-latex-options :scale 1.5)))

;;;; Lesson 8.6  Citations with citar

;; The bibliography is every .bib file at the top of the project (or
;; next to the file), plus whatever a LaTeX file names with
;; \addbibresource or \bibliography and an Org file with
;; #+bibliography.  citar inserts @key in Typst, [@key] in Markdown
;; and \cite{key} in LaTeX, without asking for a citation style.
;; bibtex-mode: C-c C-c tidies the entry at point (fields one per
;; line, = signs aligned, trailing comma), as apheleia does on save.
(setq bibtex-entry-format '(opts-or-alts required-fields numerical-fields
                                         whitespace realign last-comma delimiters)
      bibtex-align-at-equal-sign t
      bibtex-comma-after-last-field t)

(defun playground-bib-files ()
  "The .bib files in this directory and at the top of the current project."
  (let ((dirs (delete-dups
               (delq nil (list (expand-file-name default-directory)
                               (when-let* ((proj (project-current)))
                                 (expand-file-name (project-root proj))))))))
    (seq-mapcat (lambda (dir) (directory-files dir t "\\.bib\\'")) dirs)))

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
  ;; M-x citar-open offers a reference's files, links and notes; notes
  ;; are files named after the key in a notes/ folder next to the .bib.
  (defun playground--citar-local-bibliography ()
    "Let citar see the project's .bib files, and their notes/, in this buffer."
    (let ((bibs (playground-bib-files)))
      (setq-local citar-bibliography bibs)
      (when-let* ((notes (and bibs (expand-file-name "notes" (file-name-directory (car bibs)))))
                  ((file-directory-p notes)))
        (setq-local citar-notes-paths (list notes)))))
  (defvar playground--citar-modes '(LaTeX-mode markdown-mode typst-ts-mode bibtex-mode))
  (dolist (mode playground--citar-modes)
    (add-hook (intern (format "%s-hook" mode)) #'playground--citar-local-bibliography))
  ;; Look again when a command runs: a .bib file or notes/ made after
  ;; the buffer was opened counts too.
  (defun playground--citar-refresh (&rest _)
    "Update this buffer's bibliography and notes folder for citar."
    (when (apply #'derived-mode-p playground--citar-modes)
      (playground--citar-local-bibliography)))
  (dolist (cmd '(citar-insert-citation citar-open citar-open-entry citar-open-notes
                                       citar-create-note citar-open-files citar-open-links))
    (advice-add cmd :before #'playground--citar-refresh))
  (with-eval-after-load 'latex
    (keymap-set LaTeX-mode-map "C-c C-x @" #'citar-insert-citation))
  (with-eval-after-load 'markdown-mode
    (keymap-set markdown-mode-map "C-c C-x @" #'citar-insert-citation))
  (with-eval-after-load 'typst-ts-mode
    (keymap-set typst-ts-mode-map "C-c C-x @" #'citar-insert-citation)))

(playground-with-package citar-embark
  (with-eval-after-load 'embark
    (with-eval-after-load 'citar
      (citar-embark-mode 1)
      ;; Global, so its lighter would be on every mode line.
      (setf (alist-get 'citar-embark-mode minor-mode-alist) '("")))))

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
    (setf (alist-get 'jinx-mode minor-mode-alist) '(""))
    ;; Chinese, Japanese and Korean text is not English: no underline
    ;; under what pyim types (lesson 8.7).
    (setf (alist-get t jinx-exclude-regexps)
          (append (alist-get t jinx-exclude-regexps) '("\\cc+\\|\\cj+\\|\\ch+")))))

;;; writing.el ends here
