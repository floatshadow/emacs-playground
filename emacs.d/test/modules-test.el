;;; modules-test.el --- ERT tests for the configuration modules  -*- lexical-binding: t; -*-

;; Run from emacs.d/ in the session image (config-test.el loads this
;; file, so `make emacs-test' runs it too):
;;
;;   emacs --batch -l early-init.el -l test/modules-test.el \
;;     -f ert-run-tests-batch-and-exit
;;
;; Loads init.el with the base and editing modules, the set Part 1
;; runs with, using the packages the image ships, and checks settings
;; the lessons depend on: what Home, End and Delete arrive as in the
;; terminal frame, the M-x messages, the mode-line colours, the mode
;; line's minor modes, the M-y binding, the search's match count, and
;; how the GUI frame is fitted to its display.  One test loads the
;; files module on top, for ibuffer's groups.  The last one runs
;; all-modules-test.el in a child Emacs: every module on, the set Part
;; 13 reaches.

;;; Code:

(require 'ert)

(defvar pg-modules-test-dir
  (file-name-directory (directory-file-name
                        (file-name-directory (or load-file-name buffer-file-name))))
  "The emacs.d directory under test.")

(defvar modus-vivendi-tinted-palette-overrides)
(declare-function modus-themes-get-color-value "modus-themes" (color &optional with-overrides theme))
(declare-function playground--kkp-decode "base")
(declare-function playground-fit-frame-to-display "base")
(declare-function playground--frame-fit "base")
(defvar ibuffer-filter-groups)
(defvar isearch-lazy-count-current)
(defvar isearch-lazy-count-total)
(defvar isearch-lazy-highlight-forward)
(defvar isearch-suspended)

(unless (featurep 'pg-modules-test-init)
  (let ((process-environment (cons "PG_MODULES=base editing" process-environment)))
    (load (expand-file-name "init.el" pg-modules-test-dir) nil t))
  (provide 'pg-modules-test-init))

(defvar playground-module-errors)
(defvar playground-modules-loaded)

(ert-deftest pg-modules/loaded ()
  "base and editing load without errors."
  (should-not playground-module-errors)
  (should (equal playground-modules-loaded '(base editing))))

;;;; Terminal keys

(defun pg-modules--decode (sequence)
  "The key the terminal frame reads for ESC [ SEQUENCE."
  (require 'kkp)
  (let ((unread-command-events (string-to-list (substring sequence 1))))
    (playground--kkp-decode (aref sequence 0))))

(ert-deftest pg-modules/home-end-delete ()
  "tmux's Home, End and Delete arrive as the keys their names say."
  (should (equal (pg-modules--decode "4~") [end]))
  (should (equal (pg-modules--decode "1~") [home]))
  (should (equal (pg-modules--decode "3~") [deletechar]))
  ;; Modified keys keep their names: C-<delete> is kill-word.
  (should (equal (pg-modules--decode "3;5~") [C-delete]))
  (should (equal (pg-modules--decode "1;5F") [C-end]))
  (should (equal (pg-modules--decode "1;5H") [C-home]))
  ;; Other ~ keys are unchanged.
  (should (equal (pg-modules--decode "6~") [next]))
  (should (equal (pg-modules--decode "2~") [insert]))
  (should (equal (pg-modules--decode "15~") [f5]))
  (should (equal (pg-modules--decode "7~") [home]))
  (should (equal (pg-modules--decode "8~") [end]))
  ;; And they run the commands a VS Code user expects.
  (should (eq (key-binding [end]) 'move-end-of-line))
  (should (eq (key-binding [home]) 'move-beginning-of-line))
  (should (eq (key-binding [deletechar]) 'delete-forward-char))
  (should (eq (key-binding [C-delete]) 'kill-word)))

;;;; M-x messages

(ert-deftest pg-modules/no-abbreviation-tips ()
  "M-x suggests key bindings but not abbreviations such as M-x em-ve."
  (should-not extended-command-suggest-shorter)
  (should suggest-key-bindings))

;;;; Mode lines

(defun pg-modules--luminance (hex)
  "WCAG relative luminance of the colour HEX (#rrggbb)."
  (let ((channel (lambda (i)
                   (let ((c (/ (string-to-number (substring hex i (+ i 2)) 16) 255.0)))
                     (if (<= c 0.03928) (/ c 12.92) (expt (/ (+ c 0.055) 1.055) 2.4))))))
    (+ (* 0.2126 (funcall channel 1))
       (* 0.7152 (funcall channel 3))
       (* 0.0722 (funcall channel 5)))))

(defun pg-modules--contrast (a b)
  "WCAG contrast ratio between colours A and B."
  (let ((la (pg-modules--luminance a)) (lb (pg-modules--luminance b)))
    (/ (+ (max la lb) 0.05) (+ (min la lb) 0.05))))

(ert-deftest pg-modules/selected-mode-line-stands-out ()
  "The selected window's mode line is clearly brighter than the others."
  (should (memq 'modus-vivendi-tinted custom-enabled-themes))
  (let ((active (modus-themes-get-color-value 'bg-mode-line-active t))
        (inactive (modus-themes-get-color-value 'bg-mode-line-inactive t))
        (fg-active (modus-themes-get-color-value 'fg-mode-line-active t))
        (fg-inactive (modus-themes-get-color-value 'fg-mode-line-inactive t))
        (bg-main (modus-themes-get-color-value 'bg-main t)))
    (should (equal active "#7a58c4"))
    (should (> (pg-modules--luminance active) (pg-modules--luminance inactive)))
    (should (>= (pg-modules--contrast active inactive) 2.0))
    ;; Both stay readable, and an inactive mode line still shows against
    ;; the buffer background.
    (should (>= (pg-modules--contrast fg-active active) 4.5))
    (should (>= (pg-modules--contrast fg-inactive inactive) 4.5))
    (should (> (pg-modules--luminance inactive) (pg-modules--luminance bg-main)))))

(ert-deftest pg-modules/no-ws-butler-lighter ()
  "ws-butler does its housekeeping without a \" wb\" in the mode line."
  (require 'ws-butler)
  (with-temp-buffer
    (prog-mode)
    (ws-butler-mode 1)
    (should ws-butler-mode)
    ;; (`format-mode-line' draws nothing in batch; read the entry.)
    (should (equal (cdr (assq 'ws-butler-mode minor-mode-alist)) '("")))
    ;; Other lighters are untouched.
    (should (equal (cdr (assq 'abbrev-mode minor-mode-alist)) '(" Abbrev")))))

;;;; Keys of the editing module

(ert-deftest pg-modules/m-y-is-yank-pop ()
  "Part 1's M-y is the yank-pop lesson 1.2 teaches; consult's comes in Part 4."
  (should (eq (key-binding (kbd "M-y")) 'yank-pop)))

;;;; Searching

(ert-deftest pg-modules/isearch-counts-matches ()
  "Lesson 1.4's search prompt shows the match count (\"1/4 I-search: \")."
  (should isearch-lazy-count)
  (with-temp-buffer
    (insert "greet greet greet greet\n")
    (goto-char (point-min))
    (let ((isearch-lazy-count-current 1) (isearch-lazy-count-total 4)
          (isearch-lazy-highlight-forward t) (isearch-forward t)
          (isearch-error nil) (isearch-suspended nil) (isearch-success t)
          (isearch-wrapped nil) (isearch-opoint (point)))
      (should (string-prefix-p "1/4 I-search"
                               (substring-no-properties (isearch-message-prefix)))))))

;;;; The files module

(ert-deftest pg-modules/ibuffer-groups-on-first-use ()
  "The first C-x C-b after Emacs starts already groups by project."
  (load (expand-file-name "modules/files.el" pg-modules-test-dir) nil t)
  (should-not (featurep 'ibuf-ext))
  (save-window-excursion
    (ibuffer nil "*pg-ibuffer*")
    (unwind-protect
        (with-current-buffer "*pg-ibuffer*"
          (should (featurep 'ibuf-ext))
          (should ibuffer-filter-groups)
          (should (save-excursion
                    (goto-char (point-min))
                    (search-forward "[ Help and messages ]" nil t))))
      (kill-buffer "*pg-ibuffer*"))))

(ert-deftest pg-modules/dired-menus-go-with-dirvish ()
  "q in dirvish recomputes the menu bar, which showed Dired's menus until then."
  (load (expand-file-name "modules/files.el" pg-modules-test-dir) nil t)
  (should (advice-member-p 'playground--update-menu-bars 'dirvish-quit)))

;;;; The GUI frame

(ert-deftest pg-modules/gui-frame-follows-the-display ()
  "The GUI frame is fitted when it is made and when the display changes size."
  (should (memq #'playground--fit-new-frame after-make-frame-functions))
  (should (memq #'playground--fit-frames-on display-monitors-changed-functions))
  ;; Terminal frames are left alone.
  (should-not (playground-fit-frame-to-display (selected-frame))))

(ert-deftest pg-modules/gui-frame-fit-arithmetic ()
  "The outer frame, menu bar included, covers the monitor exactly."
  ;; 20 px of internal border and fringes, a 40 px menu bar.
  (should (equal (playground--frame-fit '(0 0 1280 900) '(1000 600) '(980 560))
                 '(0 0 1260 860)))
  (should (equal (playground--frame-fit '(0 0 1600 900) '(1600 900) '(1580 860))
                 '(0 0 1580 860)))
  (should (equal (playground--frame-fit '(0 0 10 10) '(1000 600) '(980 560))
                 '(0 0 1 1))))

;;;; Every module (in a child Emacs)

(ert-deftest pg-modules/all-modules ()
  "all-modules-test.el passes: every module on, in a fresh Emacs."
  (let* ((buffer (generate-new-buffer " *pg-all-modules*"))
         (status (call-process (expand-file-name invocation-name invocation-directory)
                               nil buffer nil "--batch"
                               "-l" (expand-file-name "early-init.el" pg-modules-test-dir)
                               "-l" (expand-file-name "test/all-modules-test.el" pg-modules-test-dir)
                               "-f" "ert-run-tests-batch-and-exit"))
         (output (with-current-buffer buffer (buffer-string))))
    (kill-buffer buffer)
    ;; The child's report: one line per test, then the summary.
    (message "%s" (mapconcat #'identity
                             (seq-filter (lambda (line)
                                           (string-match-p "\\`\\(   passed\\|FAILED\\|Ran \\|  \\)" line))
                                         (split-string output "\n"))
                             "\n"))
    (unless (eq status 0) (message "%s" output))
    (should (eq status 0))))

(provide 'modules-test)
;;; modules-test.el ends here
