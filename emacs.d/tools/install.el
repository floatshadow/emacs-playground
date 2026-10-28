;;; install.el --- install packages.lock exactly  -*- lexical-binding: t; -*-

;; Installs every package of emacs.d/packages.lock into a package
;; directory and native-compiles it (emacs.md section 8).  The session
;; image runs it at build time:
;;
;;   emacs --batch -l tools/install.el -f playground-install-batch \
;;     EMACS_D ELPA_DIR ELN_DIR
;;
;; For each line of the lock, in order, the file is taken from
;; EMACS_D/vendor/ when it is vendored there (every locked file is, see
;; tools/lock.el) or else downloaded from its archive, and its sha256 must
;; equal the lock's; any difference, missing file or failed install
;; stops the build with a non-zero exit.  `package-archives' is empty
;; while installing, so package.el cannot fetch anything the lock does
;; not name: an unsatisfied dependency is an error too.
;;
;; Byte compilation runs once more, in a fresh Emacs, for every file
;; the install left without byte code: package.el compiles each package
;; in the installing Emacs, where `read-symbol-shorthands' from a
;; package that uses cond-let (closql, forge) can leak into later
;; packages and fail their compilation ("Invalid binding").  Files that
;; still do not compile are listed; a package's main file among them
;; fails the build.
;;
;; Native code goes to ELN_DIR (early-init.el adds it to
;; `native-comp-eln-load-path' at run time).

;;; Code:

(require 'package)
(require 'bytecomp)
(require 'cl-lib)
(require 'subr-x)

(defvar native-comp-async-report-warnings-errors)
(defvar native-comp-async-jobs-number)

(defun playground-install--read-lock (file)
  "Parse lock FILE into (ARCHIVES . ENTRIES).
ARCHIVES is an alist (NAME . URL); each entry is a list
\(NAME VERSION ARCHIVE FILE SHA256)."
  (let (archives entries)
    (with-temp-buffer
      (insert-file-contents file)
      (dolist (line (split-string (buffer-string) "\n" t "[ \t]+"))
        (unless (string-prefix-p "#" line)
          (let ((fields (split-string line "[ \t]+" t)))
            (cond
             ((equal (car fields) "@archive")
              (push (cons (nth 1 fields) (nth 2 fields)) archives))
             ((= (length fields) 5) (push fields entries))
             (t (error "Bad line in %s: %s" file line)))))))
    (cons (nreverse archives) (nreverse entries))))

(defun playground-install--sha256 (file)
  "Hex sha256 of FILE's bytes."
  (with-temp-buffer
    (set-buffer-multibyte nil)
    (insert-file-contents-literally file)
    (secure-hash 'sha256 (current-buffer))))

(defun playground-install--fetch (entry archives vendor tmp)
  "Return a verified local copy of ENTRY's file."
  (pcase-let* ((`(,name ,_version ,archive ,file ,sha) entry)
               (vendored (expand-file-name file vendor))
               (dest (expand-file-name file tmp)))
    (if (file-exists-p vendored)
        (copy-file vendored dest t)
      (let ((base (cdr (assoc archive archives))))
        (unless base (error "%s: unknown archive %s" name archive))
        (let ((status (call-process "curl" nil nil nil "-fsSL" "--retry" "3"
                                    "-o" dest (concat base file))))
          (unless (eq status 0)
            (error "%s: download of %s%s failed (curl exit %s); regenerate packages.lock"
                   name base file status)))))
    (let ((got (playground-install--sha256 dest)))
      (unless (equal got sha)
        (error "%s: sha256 mismatch for %s: lock has %s, file has %s" name file sha got)))
    dest))

(defun playground-install--wait-native ()
  "Wait until every queued native compilation has finished."
  (while (or (bound-and-true-p comp-files-queue)
             (and (fboundp 'comp--async-runnings) (> (comp--async-runnings) 0))
             (and (fboundp 'comp-async-runnings) (> (comp-async-runnings) 0)))
    (accept-process-output nil 1)))

(defun playground-install--uncompiled (elpa-dir)
  "Lisp files under ELPA-DIR without byte code, tests and generated files aside."
  (seq-remove
   (lambda (file)
     (or (file-exists-p (concat file "c"))
         (string-match-p "\\(-pkg\\|-autoloads\\|loaddefs\\|-shortdoc\\|-subtest\\|\\`\\.dir-locals\\)\\.el\\'"
                         (file-name-nondirectory file))
         (string-match-p "/tests?/" file)))
   (directory-files-recursively elpa-dir "\\.el\\'")))

(defun playground-install--recompile (elpa-dir)
  "Byte-compile the files the install left uncompiled, in fresh Emacsen.
One child Emacs per package directory, so nothing leaks between them."
  (dolist (dir (delete-dups (mapcar #'file-name-directory
                                    (playground-install--uncompiled elpa-dir))))
    (message "recompiling %s" (file-relative-name dir elpa-dir))
    (call-process
     (expand-file-name invocation-name invocation-directory) nil nil nil
     "--batch" "-Q"
     "--eval" (format "(progn (setq package-user-dir %S byte-compile-warnings nil) (package-activate-all) (byte-recompile-directory %S 0))"
                      (file-name-as-directory elpa-dir) dir)))
  (let ((left (playground-install--uncompiled elpa-dir)))
    (dolist (file left)
      (message "not byte-compiled: %s" (file-relative-name file elpa-dir)))
    (let ((main (seq-filter
                 (lambda (file)
                   (string-prefix-p (concat (file-name-base file) "-")
                                    (file-name-nondirectory
                                     (directory-file-name (file-name-directory file)))))
                 left)))
      (when main
        (error "Main package files did not byte-compile: %s"
               (mapconcat (lambda (f) (file-relative-name f elpa-dir)) main ", "))))))

(defun playground-install (emacs-d elpa-dir eln-dir)
  "Install EMACS-D/packages.lock into ELPA-DIR, native code into ELN-DIR."
  (let* ((lock (playground-install--read-lock (expand-file-name "packages.lock" emacs-d)))
         (archives (car lock))
         (entries (cdr lock))
         (vendor (expand-file-name "vendor" emacs-d))
         (tmp (make-temp-file "pg-install" t))
         (package-user-dir (file-name-as-directory elpa-dir))
         (package-directory-list nil)
         (package-archives nil)
         (package-native-compile nil)
         (byte-compile-warnings nil)
         (native-comp-async-report-warnings-errors 'silent))
    (make-directory package-user-dir t)
    (package-initialize)
    (dolist (entry entries)
      (let ((file (playground-install--fetch entry archives vendor tmp)))
        (message "installing %s %s (%s)" (nth 0 entry) (nth 1 entry) (nth 2 entry))
        (package-install-file file)
        (unless (package-installed-p (intern (nth 0 entry)))
          (error "%s did not install" (nth 0 entry)))))
    (delete-directory tmp t)
    (playground-install--recompile package-user-dir)
    ;; Every package activated: now compile them all to native code,
    ;; in parallel, into ELN-DIR.
    (when (and (featurep 'native-compile) eln-dir)
      (make-directory eln-dir t)
      (setq native-comp-eln-load-path (cons (file-name-as-directory eln-dir)
                                            (last native-comp-eln-load-path))
            native-comp-async-jobs-number (max 1 (min 32 (num-processors))))
      (native-compile-async (list package-user-dir) 'recursively)
      (playground-install--wait-native)
      (let ((n (length (directory-files-recursively eln-dir "\\.eln\\'"))))
        (message "native-compiled %d files into %s" n eln-dir)
        (when (zerop n) (error "native compilation produced nothing"))))
    (message "installed %d packages from packages.lock" (length entries))))

(defun playground-install-batch ()
  "Batch entry point: EMACS_D ELPA_DIR ELN_DIR from the command line."
  (let ((emacs-d (pop command-line-args-left))
        (elpa-dir (pop command-line-args-left))
        (eln-dir (pop command-line-args-left)))
    (condition-case err
        (playground-install emacs-d elpa-dir eln-dir)
      (error (message "install.el: %s" (error-message-string err))
             (kill-emacs 1)))))

(provide 'playground-install)
;;; install.el ends here
