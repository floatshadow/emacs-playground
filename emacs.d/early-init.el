;;; early-init.el --- before the first frame exists  -*- lexical-binding: t; -*-

;; Emacs reads this file before it draws anything and before packages
;; are loaded.  Keep it to settings that must be in place that early:
;; garbage collection during startup, where compiled code lives, and
;; what frames look like.  Everything else is in init.el and modules/.
;; (Part 13.1 of the course walks through this file.)

;;;; Garbage collection

;; Collect less often while starting up, then settle on a moderate
;; threshold so typing never pauses for a collection.
(setq gc-cons-threshold most-positive-fixnum
      gc-cons-percentage 0.5)
(add-hook 'emacs-startup-hook
          (lambda ()
            (setq gc-cons-threshold (* 32 1024 1024)
                  gc-cons-percentage 0.1)))

;;;; Packages

;; init.el activates packages itself, after deciding where they live
;; (emacs.md section 8): the pinned set ships read-only in the image,
;; packages the learner installs go to emacs.d/elpa.
(setq package-enable-at-startup nil)

;; Do not let packages' autoloads add global keys the course has not
;; reached yet; the git module turns Magit's C-x g on (Part 5).
(defvar magit-define-global-key-bindings)
(setq magit-define-global-key-bindings nil)

;;;; Native compilation

;; The image ships native code for its packages, and the trampolines
;; they need, in /opt/playground/eln-cache.  Code compiled later
;; (packages the learner installs, files in user-lisp/) goes to
;; ~/.cache so that the workspace, which is a git repository, stays
;; free of binaries.
;;
;; Native code must be loadable from where it is written.  On a server
;; the root filesystem is read-only and ~/.cache is a tmpfs, which
;; Docker mounts noexec by default; Emacs would compile there and then
;; fail to load the result ("Native elisp load failed").  So the
;; writable cache is the first of ~/.cache and the workspace's private
;; .playground/ directory that allows it; if neither does, nothing is
;; compiled at run time (byte code runs instead) and only the shipped
;; trampolines are used.

(defun playground--existing-parent (dir)
  "DIR, or its nearest parent that exists."
  (let ((path (expand-file-name dir)))
    (while (not (file-exists-p path))
      (setq path (file-name-directory (directory-file-name path))))
    path))

(defvar playground--mountinfo "/proc/self/mountinfo"
  "The kernel's list of mounts (a variable so tests can supply their own).")

(defun playground--mount-options (dir)
  "Mount options of the filesystem holding DIR, from `playground--mountinfo'."
  (when (file-readable-p playground--mountinfo)
    (let ((path (file-name-as-directory
                 (file-truename (playground--existing-parent dir))))
          (best "") opts)
      (with-temp-buffer
        (insert-file-contents playground--mountinfo)
        (dolist (line (split-string (buffer-string) "\n" t))
          ;; ID PARENT MAJ:MIN ROOT MOUNT-POINT OPTIONS ...; a later
          ;; mount on the same point hides the earlier one.
          (let* ((fields (split-string line " "))
                 (mount (file-name-as-directory
                         (replace-regexp-in-string "\\\\040" " " (nth 4 fields)))))
            (when (and (string-prefix-p mount path)
                       (>= (length mount) (length best)))
              (setq best mount
                    opts (split-string (nth 5 fields) ","))))))
      opts)))

(defun playground--eln-dir-usable-p (dir)
  "Non-nil when native code written to DIR could be loaded back."
  (and (file-writable-p (playground--existing-parent dir))
       (not (member "noexec" (playground--mount-options dir)))))

(defun playground--shipped-trampoline-p (subr-name)
  "Non-nil when a trampoline for SUBR-NAME exists already.
Before-while advice for `comp-subr-trampoline-install' when nothing
can be compiled: advising a primitive then still works for calls
from byte code, instead of failing on a trampoline that cannot load."
  (and (fboundp 'comp--trampoline-search)
       (comp--trampoline-search subr-name)))

;; Defined by comp-run.el, which loads later.
(defvar native-comp-async-report-warnings-errors)
(defvar native-comp-warning-on-missing-source)
(defvar native-comp-jit-compilation)
(declare-function comp--trampoline-search "comp-run" (subr-name))

(defun playground--no-native-compile (&rest _)
  "Override for `native-compile-async' when nothing compiled could load."
  nil)

(when (featurep 'native-compile)
  (setq native-comp-async-report-warnings-errors 'silent
        native-comp-warning-on-missing-source nil)
  (let ((candidates
         (list (expand-file-name "emacs/eln-cache/" (or (getenv "XDG_CACHE_HOME") "~/.cache"))
               (expand-file-name ".playground/eln-cache/" (or (getenv "PG_WORKSPACE") "/work"))))
        (usable nil))
    (dolist (dir candidates)
      (when (and (not usable) (playground--eln-dir-usable-p dir))
        (setq usable dir)))
    (when (fboundp 'startup-redirect-eln-cache)
      (startup-redirect-eln-cache (or usable (car candidates))))
    (unless usable
      (setq native-comp-jit-compilation nil)
      ;; Emacs 31 native-compiles user-lisp/ files whatever the JIT
      ;; setting says; their byte code is used instead.
      (advice-add 'native-compile-async :override #'playground--no-native-compile)
      (with-eval-after-load 'comp-run
        (advice-add 'comp-subr-trampoline-install :before-while
                    #'playground--shipped-trampoline-p))))
  (let ((shipped (or (getenv "PG_ELN_CACHE") "/opt/playground/eln-cache/")))
    (when (file-directory-p shipped)
      ;; After the writable cache, before the system's own directory.
      (setq native-comp-eln-load-path
            (append (list (car native-comp-eln-load-path) shipped)
                    (cdr native-comp-eln-load-path))))))

;;;; Frames

;; No tool bar and no scroll bars; the menu bar stays (F10 or a click
;; opens it, which helps while the keys are new).  GUI frames use
;; Iosevka Nerd Font at 12 pt when it is installed (the image has it,
;; emacs.md section 6); terminal frames use the browser terminal's font.
(setq default-frame-alist
      (append '((tool-bar-lines . 0)
                (vertical-scroll-bars . nil)
                (horizontal-scroll-bars . nil))
              (when (file-directory-p "/usr/local/share/fonts/iosevka-nerd")
                '((font . "Iosevka Nerd Font-12")))
              default-frame-alist))
(setq tool-bar-mode nil
      scroll-bar-mode nil
      frame-inhibit-implied-resize t
      frame-resize-pixelwise t
      inhibit-startup-screen t)

;; Grammars are prebuilt into the image; never offer to download one.
(defvar treesit-auto-install-grammar)
(setq treesit-auto-install-grammar 'never)

;;; early-init.el ends here
