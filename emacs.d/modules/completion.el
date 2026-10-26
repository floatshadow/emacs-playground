;;; completion.el --- completion everywhere  -*- lexical-binding: t; -*-

;; Module: completion.  Unlocked in Part 4 (Completion everywhere).
;;
;; The minibuffer becomes a fuzzy, annotated list (vertico, orderless,
;; marginalia); consult adds searching commands with live preview;
;; embark acts on whatever is at point or selected; corfu, cape and
;; tempel complete code and expand snippets in buffers.
;;
;; Keys added here, by lesson:
;;   4.1  M-x and every prompt: vertical list, space-separated terms
;;        in any order, !term excludes, =term is literal, M-RET accepts
;;        what you typed
;;   4.2  C-x b consult-buffer (b SPC buffers, f SPC files), M-s l line,
;;        M-s r ripgrep, M-s d find files, M-g g line, M-g i imenu,
;;        M-g o outline, M-g m marks, M-y kill ring, C-x r j registers
;;   4.3  C-. act on the thing at point or the candidate (embark);
;;        in the minibuffer C-; does the default action; C-h B lists
;;        every binding
;;   4.4  TAB completes in buffers, M-SPC separates orderless terms in
;;        the popup, M-+ inserts a snippet, M-* completes a snippet name

;;;; Lesson 4.1  The minibuffer, upgraded

(playground-with-package vertico
  (setq vertico-cycle t
        vertico-count 12)
  (vertico-mode 1))

(playground-with-package orderless
  (setq completion-styles '(orderless basic)
        completion-category-defaults nil
        ;; File names keep prefix completion so ~/ and // work as usual.
        completion-category-overrides '((file (styles basic partial-completion)))))

(playground-with-package marginalia
  (marginalia-mode 1))

;;;; Lesson 4.2  Consult

(playground-with-package consult
  (keymap-global-set "C-x b" #'consult-buffer)
  (keymap-global-set "C-x 4 b" #'consult-buffer-other-window)
  (keymap-global-set "C-x 5 b" #'consult-buffer-other-frame)
  (keymap-global-set "C-x t b" #'consult-buffer-other-tab)
  (keymap-global-set "C-x r b" #'consult-bookmark)
  (keymap-global-set "C-x r j" #'consult-register)
  (keymap-global-set "M-y" #'consult-yank-pop)
  (keymap-global-set "M-g g" #'consult-goto-line)
  (keymap-global-set "M-g M-g" #'consult-goto-line)
  (keymap-global-set "M-g i" #'consult-imenu)
  (keymap-global-set "M-g o" #'consult-outline)
  (keymap-global-set "M-g m" #'consult-mark)
  (keymap-global-set "M-g f" #'consult-flymake)
  (keymap-global-set "M-s l" #'consult-line)
  (keymap-global-set "M-s L" #'consult-line-multi)
  (keymap-global-set "M-s r" #'consult-ripgrep)
  (keymap-global-set "M-s d" #'consult-fd)
  (keymap-global-set "M-s g" #'consult-grep)
  (keymap-set minibuffer-local-map "M-r" #'consult-history)
  (setq consult-narrow-key "<"
        register-preview-function #'consult-register-format
        xref-show-xrefs-function #'consult-xref
        xref-show-definitions-function #'consult-xref)
  (with-eval-after-load 'isearch
    (keymap-set isearch-mode-map "M-s l" #'consult-line)))

;;;; Lesson 4.3  Embark

(playground-with-package embark
  (keymap-global-set "C-." #'embark-act)
  ;; C-; is iedit in buffers (lesson 1.7); in the minibuffer it runs
  ;; the default action of the candidate (embark-dwim).
  (keymap-set minibuffer-local-map "C-;" #'embark-dwim)
  (keymap-global-set "C-h B" #'embark-bindings)
  (setq embark-indicators '(embark-minimal-indicator
                            embark-highlight-indicator
                            embark-isearch-highlight-indicator)))

(playground-with-package embark-consult
  (with-eval-after-load 'embark
    (require 'embark-consult)
    (add-hook 'embark-collect-mode-hook #'consult-preview-at-point-mode)))

;;;; Lesson 4.4  Corfu, cape and tempel

;; TAB indents, and completes when the line is already indented.
(setq tab-always-indent 'complete
      text-mode-ispell-word-completion nil)

;; Corfu pops up candidates as you type.  Emacs 31 draws child frames
;; in terminals too, so the popup works in the terminal frame.
(playground-with-package corfu
  ;; Language servers annotate candidates with long signatures; a
  ;; popup wider than 60 columns would not fit beside point in a
  ;; 120-column terminal and would be pushed to the left, away from
  ;; what you are typing.
  (setq corfu-auto t
        corfu-auto-delay 0.2
        corfu-auto-prefix 2
        corfu-cycle t
        corfu-preview-current nil
        corfu-max-width 60)
  (global-corfu-mode 1)
  (with-eval-after-load 'corfu
    (corfu-popupinfo-mode 1)
    (corfu-history-mode 1)
    (keymap-set corfu-map "M-SPC" #'corfu-insert-separator)))

;; Cape adds completion sources: file names and words from open buffers.
(playground-with-package cape
  (add-hook 'completion-at-point-functions #'cape-file)
  (add-hook 'completion-at-point-functions #'cape-dabbrev 90))

;; Tempel expands snippets from emacs.d/templates.
(playground-with-package tempel
  (setq tempel-path (expand-file-name "templates" playground-config-dir))
  (keymap-global-set "M-+" #'tempel-insert)
  (keymap-global-set "M-*" #'tempel-complete))

;;; completion.el ends here
