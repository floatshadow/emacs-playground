;;; assumed-config.el --- what the Parts 0-3 lessons assume of the modules  -*- lexical-binding: t; -*-

;; Loaded only by the play harness (curriculum/tools/play/play.py) in
;; place of the real configuration, so that every lesson demo can be
;; replayed against a real Emacs 31.1 before the session image and
;; `playctl curriculum play' can do it.
;;
;; It mirrors the behaviour-relevant settings of emacs.d/modules/{base,
;; editing,files,windows}.el as written by the Emacs agent (worktree
;; snapshot of 2026-10-26).  When those modules change a key binding or
;; a default that a lesson relies on, change it here too and replay:
;;
;;   python3 curriculum/tools/play/play.py --all
;;
;; The settings the lessons depend on most are marked LESSON.  The
;; curriculum's side of the contract is listed in
;; curriculum/tools/README.md ("What the lessons assume").
;;
;; `pg-play-enable-module' is called once per module named in a
;; scenario's `modules', in order.

(require 'package)
(setq package-user-dir "/opt/play/elpa"
      package-native-compile nil
      native-comp-jit-compilation nil
      package-archives nil)
(package-initialize)
(require 'treesit)
(add-to-list 'treesit-extra-load-path "/opt/play/tree-sitter")
;; The session image ships the rust grammar, so .rs files open in
;; rust-ts-mode before lang-rust is enabled.
(add-to-list 'auto-mode-alist '("\\.rs\\'" . rust-ts-mode))

(defvar pg-play-modules nil "Modules enabled so far.")

(defun pg-play--base ()
  "Module base."
  (setq inhibit-startup-screen t
        ring-bell-function #'ignore
        echo-keystrokes 0.25
        ;; LESSON 0.2, 0.3: help windows take the focus, so q closes them.
        help-window-select t
        ;; LESSON 2.4, 2.6: every confirmation is y or n.
        use-short-answers t
        enable-recursive-minibuffers nil
        uniquify-buffer-name-style 'post-forward-angle-brackets
        auto-revert-verbose nil
        global-auto-revert-non-file-buffers t
        history-length 500
        recentf-max-saved-items 200
        auto-save-visited-interval 5
        auto-save-default nil
        make-backup-files nil
        create-lockfiles nil
        require-final-newline t
        sentence-end-double-space nil
        scroll-conservatively 101)
  (setq-default indent-tabs-mode nil tab-width 4 fill-column 80)
  (line-number-mode 1)
  (column-number-mode 1)
  (minibuffer-depth-indicate-mode 1)
  (setq which-key-idle-delay 0.8 which-key-idle-secondary-delay 0.05)
  (which-key-mode 1)
  (repeat-mode 1)
  ;; LESSON 1.x: typing replaces an active region.
  (delete-selection-mode 1)
  ;; LESSON 0.1, 2.3, 2.4, 2.5: files save themselves 5 s after typing
  ;; stops.  Lessons that teach the modified flag turn this off in
  ;; setup.eval.
  (auto-save-visited-mode 1)
  (global-auto-revert-mode 1)
  (savehist-mode 1)
  (recentf-mode 1)
  (save-place-mode 1)
  (global-so-long-mode 1)
  ;; LESSON 0.2: C-z and C-x C-z do nothing.
  (keymap-global-unset "C-z")
  (keymap-global-unset "C-x C-z")
  (xterm-mouse-mode 1))

(defun pg-play--editing ()
  "Module editing."
  (when (fboundp 'consult-yank-pop)
    (keymap-global-set "M-y" #'consult-yank-pop))
  (keymap-global-set "C-?" #'undo-redo)
  (keymap-global-set "C-M-_" #'undo-redo)
  (keymap-global-set "C-x u" #'vundo)
  ;; LESSON 1.5: M-% is visual-replace, one prompt for both fields.
  (visual-replace-global-mode 1)
  (add-hook 'visual-replace-minibuffer-mode-hook #'visual-replace-toggle-query)
  (keymap-global-set "C-;" #'iedit-mode)
  (setq mc/list-file "/tmp/mc-lists.el")
  (keymap-global-set "C->" #'mc/mark-next-like-this)
  (keymap-global-set "C-<" #'mc/mark-previous-like-this)
  (keymap-global-set "C-c m l" #'mc/edit-lines)
  (keymap-global-set "C-c m a" #'mc/mark-all-like-this)
  (keymap-global-set "C-=" #'expreg-expand)
  (keymap-global-set "C--" #'expreg-contract)
  (keymap-global-set "M-j" #'avy-goto-char-timer)
  (setq avy-timeout-seconds 0.4 avy-style 'at-full avy-background t)
  ;; LESSON 1.8: consult is installed with the completion module, so
  ;; M-g i is consult-imenu even before Part 4.
  (keymap-global-set "M-g i" (if (fboundp 'consult-imenu) #'consult-imenu #'imenu))
  (when (fboundp 'consult-outline)
    (keymap-global-set "M-g o" #'consult-outline))
  (add-hook 'prog-mode-hook #'ws-butler-mode)
  (add-hook 'text-mode-hook #'ws-butler-mode))

(defun pg-play--ibuffer-group-by-project ()
  "Copy of `playground-ibuffer-group-by-project' (files.el)."
  (let (groups)
    (dolist (b (buffer-list))
      (when-let* ((dir (buffer-local-value 'default-directory b))
                  ((not (string-prefix-p " " (buffer-name b))))
                  (proj (with-current-buffer b (ignore-errors (project-current nil dir))))
                  (root (expand-file-name (project-root proj)))
                  (name (format "Project: %s" (file-name-nondirectory (directory-file-name root)))))
        (unless (assoc name groups)
          (push (list name `(filename . ,(concat "\\`" (regexp-quote root)))) groups))))
    (setq ibuffer-filter-groups
          (append (sort groups (lambda (a b) (string< (car a) (car b))))
                  '(("Dired" (mode . dired-mode))
                    ("Help and messages" (name . "\\`\\*")))))))

(defun pg-play--files ()
  "Module files."
  ;; LESSON 2.1, 2.5: C-x C-b is ibuffer; D kills without asking.
  (keymap-global-set "C-x C-b" #'ibuffer)
  (setq ibuffer-show-empty-filter-groups nil ibuffer-expert t)
  (add-hook 'ibuffer-hook #'pg-play--ibuffer-group-by-project)
  ;; LESSON 2.8: C-x C-r opens a recent file.
  (keymap-global-set "C-x C-r" #'recentf-open)
  (setq dired-listing-switches "-alh --group-directories-first"
        dired-dwim-target t
        dired-auto-revert-buffer t
        dired-kill-when-opening-new-dired-buffer t
        dired-recursive-copies 'always
        dired-recursive-deletes 'top
        dired-create-destination-dirs 'ask
        delete-by-moving-to-trash nil)
  (setq dirvish-use-header-line t
        dirvish-attributes '(subtree-state file-size)
        dirvish-side-width 32
        dirvish-reuse-session t)
  (dirvish-override-dired-mode 1)
  (autoload 'dirvish-side "dirvish-side" nil t)
  (keymap-global-set "C-x C-j" #'dirvish-side)
  (with-eval-after-load 'dirvish
    (keymap-set dirvish-mode-map "TAB" #'dirvish-subtree-toggle)
    (keymap-set dirvish-mode-map "?" #'dirvish-dispatch))
  ;; LESSON 2.7: a directory with Cargo.toml is a project root.
  (setq project-vc-extra-root-markers '("Cargo.toml" "pyproject.toml" "dune-project")
        project-switch-commands '((project-find-file "Find file")
                                  (project-find-regexp "Find regexp")
                                  (project-find-dir "Find directory")
                                  (project-vc-dir "VC-Dir")
                                  (project-eshell "Eshell"))
        xref-search-program (if (executable-find "rg") 'ripgrep 'grep)
        bookmark-save-flag 1))

(defvar pg-play-display-buffer-rules
  '(((or (derived-mode . compilation-mode)
         (derived-mode . occur-mode)
         (derived-mode . xref--xref-buffer-mode)
         "\\`\\*Async Shell Command\\*")
     (display-buffer-reuse-mode-window display-buffer-reuse-window display-buffer-at-bottom)
     (window-height . 0.33)))
  "Copy of `playground-display-buffer-rules' (windows.el).")

(defun playground-project-tab (dir)
  "Copy of the windows module's `playground-project-tab'."
  (interactive (list (funcall project-prompter)))
  (let ((name (file-name-nondirectory (directory-file-name dir))))
    (if (tab-bar--tab-index-by-name name)
        (tab-bar-switch-to-tab name)
      (tab-bar-new-tab)
      (tab-bar-rename-tab name)
      (project-switch-project dir))))

(defun pg-play--windows ()
  "Module windows."
  (setq windmove-wrap-around t)
  (windmove-default-keybindings 'shift)
  ;; LESSON 3.2: ace-window labels are home-row letters.
  (setq aw-keys '(?a ?s ?d ?f ?g ?h ?j ?k ?l)
        aw-scope 'frame
        aw-dispatch-always nil
        aw-background t)
  (keymap-global-set "M-o" #'ace-window)
  ;; LESSON 3.3, 3.5: build output opens at the bottom, a third high.
  (dolist (rule (reverse pg-play-display-buffer-rules))
    (add-to-list 'display-buffer-alist rule))
  (setq popper-reference-buffers
        '("\\*Messages\\*" "Output\\*$" "\\*Async Shell Command\\*" "\\*Warnings\\*"
          help-mode helpful-mode compilation-mode grep-mode occur-mode
          "^\\*eshell.*\\*$" eshell-mode "^\\*eat.*\\*$" eat-mode)
        popper-display-control nil
        popper-group-function nil)
  (keymap-global-set "C-`" #'popper-toggle)
  (keymap-global-set "M-`" #'popper-cycle)
  (keymap-global-set "C-M-`" #'popper-toggle-type)
  (popper-mode 1)
  (popper-echo-mode 1)
  ;; LESSON 3.6: C-c <left> undoes a layout change.
  (setq winner-dont-bind-my-keys nil)
  (winner-mode 1)
  ;; LESSON 3.7: the tab bar appears with the second tab; C-x p p opens
  ;; a project in a tab of its own.
  (setq tab-bar-show 1
        tab-bar-new-tab-choice "*scratch*"
        tab-bar-close-button-show nil
        tab-bar-new-button-show nil
        tab-bar-tab-hints t)
  (tab-bar-history-mode 1)
  (with-eval-after-load 'project
    (keymap-set project-prefix-map "p" #'playground-project-tab))
  (require 'project)
  (setq olivetti-body-width 100))

;; The agent module's rescue command (emacs.md §10.3), reduced to what
;; the lessons exercise: leave prompts and searches, stop macros, widen,
;; and keep one window, preferring one that shows a file.
(defun playground-rescue ()
  "Stand-in for the agent's `playground-rescue'."
  (interactive)
  (when (bound-and-true-p isearch-mode) (isearch-exit))
  (when defining-kbd-macro (kmacro-end-macro nil))
  (overwrite-mode -1)
  (widen)
  (let ((w (or (seq-find (lambda (w) (buffer-file-name (window-buffer w))) (window-list))
               (selected-window))))
    (select-window w)
    (delete-other-windows w))
  (deactivate-mark))

(defun pg-play-enable-module (m)
  "Enable module M (a string)."
  (push m pg-play-modules)
  (pcase m
    ("base" (pg-play--base))
    ("editing" (pg-play--editing))
    ("files" (pg-play--files))
    ("windows" (pg-play--windows))
    (_ nil)))

;;; assumed-config.el ends here
