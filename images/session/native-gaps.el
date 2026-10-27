;;; native-gaps.el --- native-compile the Lisp files the AOT build missed  -*- lexical-binding: t; -*-

;; Run once at image build time, as root, after `make install':
;;
;;   emacs --batch -l native-gaps.el
;;
;; Emacs 31.1 built from the release tarball with
;; --with-native-compilation=aot leaves about 200 of its own Lisp files
;; without native code (cl-seq, dabbrev, misearch, xt-mouse, ...).  At
;; run time Emacs then compiles them on first use into the writable eln
;; cache (~/.cache), which on a read-only root with a noexec tmpfs
;; cannot be loaded back, so `require' fails (emacs.md section 1).
;; This compiles every installed .el that has a .elc but no .eln into
;; the system native-lisp directory, then checks none is left.

;;; Code:

(require 'comp)
(require 'comp-run)
(require 'cl-lib)

(defun native-gaps--opted-out-p (file)
  "Non-nil when FILE says `no-native-compile: t' (the generated loaddefs)."
  (with-temp-buffer
    (insert-file-contents file)
    (re-search-forward "no-native-compile: *t" nil t)))

(defun native-gaps--missing ()
  "Installed Lisp files with byte code but no native code."
  (let (todo)
    ;; Sources are installed compressed; the .eln name and hash are
    ;; computed from the .el.gz file itself, as `load' does.
    (dolist (gz (directory-files-recursively lisp-directory "\\.el\\.gz\\'"))
      (when (and (file-exists-p (concat (substring gz 0 -3) "c"))
                 (not (comp-lookup-eln gz))
                 (not (native-gaps--opted-out-p gz)))
        (push gz todo)))
    (nreverse todo)))

(let* ((native-comp-eln-load-path (last native-comp-eln-load-path))
       (native-comp-async-jobs-number (max 1 (min 32 (num-processors))))
       (native-comp-async-report-warnings-errors 'silent)
       (todo (native-gaps--missing)))
  (message "native-gaps: %d files without native code, compiling into %s"
           (length todo) (car native-comp-eln-load-path))
  (when todo
    (native-compile-async todo)
    (while (or comp-files-queue (> (comp--async-runnings) 0))
      (accept-process-output nil 0.5)))
  (let ((left (native-gaps--missing)))
    (message "native-gaps: %d left%s" (length left)
             (if left (format ": %S" left) ""))
    (when left (kill-emacs 1))))

;;; native-gaps.el ends here
