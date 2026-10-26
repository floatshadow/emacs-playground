;;; files.el --- buffers, files, projects  -*- lexical-binding: t; -*-

;; Module: files.  Unlocked in Part 2 (Buffers and files).
;;
;; Dired (the directory editor) with dirvish's nicer display and side
;; window, ibuffer instead of the plain buffer list, project.el (a
;; project is a git repository or a directory with Cargo.toml or
;; pyproject.toml), recent files, bookmarks, consult-dir.
;;
;; Keys added here, by lesson:
;;   2.1  C-x C-b      ibuffer, grouped by project
;;   2.3  C-x C-r      recent files;  C-x d  dired
;;   2.6  C-x C-j      dirvish side window (the Explorer);
;;        in dired: RET ^ m u d x C R + D ! & ( TAB as usual
;;   2.7  C-x p f/b/g/p/k/c   project commands (built in);
;;        M-s r ripgrep arrives with the completion module
;;   2.8  C-x r m / C-x r b / C-x r l   bookmarks (built in);
;;        C-x C-d      jump to a recent directory (consult-dir)

;;;; Lesson 2.1  Buffers are not tabs; 2.5 ibuffer

(keymap-global-set "C-x C-b" #'ibuffer)
(setq ibuffer-show-empty-filter-groups nil
      ibuffer-expert t)

(defun playground-ibuffer-group-by-project ()
  "Group ibuffer's list by project, special buffers last."
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
                    ("Help and messages" (name . "\\`\\*")))))
    ;; ibuffer runs this hook after drawing the list: draw it again.
    (ibuffer-update nil t)))
(add-hook 'ibuffer-hook #'playground-ibuffer-group-by-project)

;;;; Lesson 2.3  Opening files

(keymap-global-set "C-x C-r" #'recentf-open)

;;;; Lesson 2.6  Dired and the dirvish side window

(setq dired-listing-switches "-alh --group-directories-first"
      dired-dwim-target t
      dired-auto-revert-buffer t
      dired-kill-when-opening-new-dired-buffer t
      dired-recursive-copies 'always
      dired-recursive-deletes 'top
      dired-create-destination-dirs 'ask
      delete-by-moving-to-trash nil)

(playground-with-package dirvish
  ;; Every dired buffer gets dirvish's display (keys stay dired's).
  (setq dirvish-use-header-line t
        dirvish-attributes '(subtree-state file-size)
        dirvish-side-width 32
        dirvish-reuse-session t)
  (dirvish-override-dired-mode 1)
  ;; The side window, like VS Code's Explorer.
  (autoload 'dirvish-side "dirvish-side" nil t)
  (keymap-global-set "C-x C-j" #'dirvish-side)
  (with-eval-after-load 'dirvish
    (keymap-set dirvish-mode-map "TAB" #'dirvish-subtree-toggle)
    (keymap-set dirvish-mode-map "?" #'dirvish-dispatch)))

;;;; Lesson 2.7  Projects

(setq project-vc-extra-root-markers '("Cargo.toml" "pyproject.toml" "dune-project")
      project-switch-commands '((project-find-file "Find file")
                                (project-find-regexp "Find regexp")
                                (project-find-dir "Find directory")
                                (project-vc-dir "VC-Dir")
                                (project-eshell "Eshell"))
      xref-search-program (if (executable-find "rg") 'ripgrep 'grep))

;;;; Lesson 2.8  Recent files, bookmarks and registers

;; Bookmarks are saved as soon as they change.
(setq bookmark-save-flag 1)

(playground-with-package consult-dir
  (keymap-global-set "C-x C-d" #'consult-dir)
  (with-eval-after-load 'vertico
    (keymap-set minibuffer-local-filename-completion-map "C-x C-d" #'consult-dir)))

;;; files.el ends here
