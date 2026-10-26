;;; init.el --- load the configuration modules  -*- lexical-binding: t; -*-

;; This file is the same for everyone and the course never edits it.
;; It does three things:
;;
;;  1. Makes packages available: the pinned set that ships with the
;;     image (read-only, in /opt/playground/elpa) and the ones you
;;     install yourself (in elpa/ next to this file).
;;  2. Reads modules.el, which names the configuration modules to load.
;;     The playground rewrites modules.el when a lesson unlocks more
;;     (emacs.md section 4); you can edit it too.
;;  3. Loads each module from modules/ in that order.  A module that
;;     fails is reported (a warning, and the playground's agent tells
;;     the browser) and the others still load.
;;
;; Settings live in the modules, one file per topic, each introduced by
;; a lesson.  Your own additions belong in custom.el (Customize) or in
;; user-lisp/ (Emacs 31 compiles and autoloads files there).

;;;; Where the configuration is

(defvar playground-config-dir
  (file-name-directory (file-truename (or load-file-name buffer-file-name)))
  "Directory holding init.el, modules/ and lisp/.")

(defvar playground-modules '(base agent)
  "Modules to load, in order.  modules.el sets this.")

(defvar playground-modules-loaded nil
  "Modules that loaded without error, in load order.")

(defvar playground-module-errors nil
  "Alist of (MODULE . MESSAGE) for modules that failed to load.")

(defvar playground-current-module nil
  "The module being loaded, while it loads.")

(defun playground-p ()
  "Non-nil when running as the playground (not standalone).
Set PLAYGROUND=0 in the environment to use this configuration as a
plain Emacs setup: the agent stays off and quitting is normal."
  (not (equal (getenv "PLAYGROUND") "0")))

(defun playground-module-p (module)
  "Non-nil when MODULE is loaded."
  (memq module playground-modules-loaded))

(add-to-list 'load-path (expand-file-name "lisp" playground-config-dir))

;;;; Packages (emacs.md section 8)

(require 'package)

;; Packages you install with M-x package-install go here.
(setq package-user-dir (expand-file-name "elpa" user-emacs-directory))

;; The pinned packages that ship with the image.
(let ((site (or (getenv "PG_SITE_ELPA") "/opt/playground/elpa")))
  (when (file-directory-p site)
    (add-to-list 'package-directory-list site)))

(setq package-archives
      '(("gnu" . "https://elpa.gnu.org/packages/")
        ("nongnu" . "https://elpa.nongnu.org/nongnu/")
        ("melpa" . "https://melpa.org/packages/"))
      package-archive-priorities
      '(("gnu" . 3) ("nongnu" . 2) ("melpa" . 1)))

(package-activate-all)

(defun playground-package-p (package)
  "Non-nil when PACKAGE is installed (or built in)."
  (or (package-installed-p package)
      (locate-library (symbol-name package))))

(defun playground--missing (package)
  "Note that PACKAGE, wanted by the current module, is not installed."
  (display-warning 'playground
                   (format "Module %s: package %s is not installed; skipping its settings"
                           playground-current-module package)
                   :warning))

(defmacro playground-with-package (package &rest body)
  "Run BODY when PACKAGE is installed; otherwise warn and skip it."
  (declare (indent 1) (debug t))
  `(if (playground-package-p ',package)
       (progn ,@body)
     (playground--missing ',package)))

;; Tree-sitter grammars prebuilt into the image (rust, python, toml,
;; json, bash, yaml).
(with-eval-after-load 'treesit
  (let ((dir (or (getenv "PG_TREESIT_DIR") "/opt/playground/tree-sitter")))
    (when (file-directory-p dir)
      (add-to-list 'treesit-extra-load-path dir))))

;;;; Modules (emacs.md sections 3 and 4)

;; modules.el is evaluated form by form with lexical binding rather than
;; loaded, so a copy without the `lexical-binding' cookie (Emacs 31
;; warns about those) or with an error in it never stops the startup.
(defun playground-read-modules-file (file)
  "Evaluate the forms in FILE, normally modules.el; report errors."
  (condition-case err
      (with-temp-buffer
        (insert-file-contents file)
        (goto-char (point-min))
        (condition-case nil
            (while t (eval (read (current-buffer)) t))
          (end-of-file nil)))
    (error
     (push (cons 'modules.el (error-message-string err)) playground-module-errors)
     (display-warning 'playground
                      (format "%s: %s; loading %S" file (error-message-string err)
                              playground-modules)
                      :error))))

(let ((file (expand-file-name "modules.el" playground-config-dir)))
  (when (file-exists-p file)
    (playground-read-modules-file file)))

;; PG_MODULES="base agent editing" overrides modules.el (handy for
;; trying a module set without editing files).
(let ((env (getenv "PG_MODULES")))
  (when (and env (not (string-empty-p env)))
    (setq playground-modules (mapcar #'intern (split-string env "[ ,]+" t)))))

(unless (playground-p)
  (setq playground-modules (delq 'agent (copy-sequence playground-modules))))

(dolist (module playground-modules)
  (let ((file (expand-file-name (format "modules/%s.el" module) playground-config-dir))
        (playground-current-module module))
    (condition-case err
        (progn
          (load file nil t)
          (push module playground-modules-loaded))
      (error
       (push (cons module (error-message-string err)) playground-module-errors)
       (display-warning 'playground
                        (format "Module %s failed to load: %s"
                                module (error-message-string err))
                        :error)))))

(setq playground-modules-loaded (nreverse playground-modules-loaded)
      playground-module-errors (nreverse playground-module-errors))

;;;; Your settings

;; Customize writes here instead of appending to this file.
(setq custom-file (expand-file-name "custom.el" user-emacs-directory))
(load custom-file 'noerror 'nomessage)

;;; init.el ends here
