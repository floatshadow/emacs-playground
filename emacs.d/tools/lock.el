;;; lock.el --- resolve packages.el into packages.lock  -*- lexical-binding: t; -*-

;; Produces emacs.d/packages.lock (emacs.md section 8): every package
;; listed in packages.el plus its dependencies, with the exact version,
;; the archive it comes from, the archive file name and the sha256 of
;; that file, in dependency order (install from top to bottom).
;;
;; Run it with the Emacs the image ships (31.1), so that "already built
;; in" is decided against the right versions, and with network access:
;;
;;   mkdir -p "$OUT" && chmod 777 "$OUT"
;;   docker run --rm -v "$PWD/emacs.d:/src:ro" -v "$OUT:/out" \
;;     ghcr.io/zsy/emacs-playground/session:dev \
;;     emacs --batch -l /src/tools/lock.el -f playground-lock-batch \
;;       /src/packages.el /out/packages.lock
;;
;; then replace emacs.d/packages.lock and emacs.d/vendor/ with
;; $OUT/packages.lock and $OUT/vendor/.  `playctl packages update' is
;; meant to wrap exactly this.
;;
;; Vendoring: MELPA and MELPA Stable serve only their latest file, so
;; a pinned MELPA tarball disappears as soon as the package changes.
;; Every package locked from those two archives is therefore also
;; copied to vendor/ next to the lock (they are small), and
;; tools/install.el uses the vendored copy, checked against the same
;; sha256.  GNU and NonGNU ELPA keep old releases and are downloaded.
;;
;; Resolution: archives are preferred in the order of
;; `playground-lock-archives' (GNU ELPA, NonGNU ELPA, MELPA Stable,
;; MELPA), except that packages listed under :prefer-snapshot in
;; packages.el rank MELPA before MELPA Stable, and packages listed
;; under :pin come only from the archive named there; the newest
;; version of the first archive whose version satisfies every
;; requirement seen so far wins.  A dependency that Emacs provides in a
;; sufficient version is not locked.

;;; Code:

(require 'package)
(require 'cl-lib)
(require 'subr-x)

(defvar playground-lock-archives
  '(("gnu" . "https://elpa.gnu.org/packages/")
    ("nongnu" . "https://elpa.nongnu.org/nongnu/")
    ("melpa-stable" . "https://stable.melpa.org/packages/")
    ("melpa" . "https://melpa.org/packages/"))
  "Archives in order of preference.")

(defvar playground-lock--chosen nil "Alist name -> package-desc chosen so far.")
(defvar playground-lock--order nil "Chosen packages, dependencies first (reversed).")
(defvar playground-lock--builtin nil "Built-in packages that satisfied a requirement.")
(defvar playground-lock--mins nil "Alist name -> highest minimum version required.")
(defvar playground-lock--snapshot nil "Packages that prefer MELPA over MELPA Stable.")
(defvar playground-lock--pins nil "Alist name -> the only archive a package may come from.")

(defconst playground-lock-vendored-archives '("melpa" "melpa-stable")
  "Archives whose files are copied to vendor/.")

(defun playground-lock--archive-rank (desc)
  "Preference rank of DESC's archive (lower is better)."
  (let ((archive (package-desc-archive desc)))
    (when (memq (package-desc-name desc) playground-lock--snapshot)
      (setq archive (pcase archive ("melpa" "melpa-stable") ("melpa-stable" "melpa") (a a))))
    (or (cl-position archive playground-lock-archives :key #'car :test #'equal)
        99)))

(defun playground-lock--candidates (name)
  "Package descs for NAME across all archives (or its pinned one), best first."
  (sort (let ((pin (cdr (assq name playground-lock--pins))))
          (seq-filter (lambda (d) (or (null pin) (equal (package-desc-archive d) pin)))
                      (cdr (assq name package-archive-contents))))
        (lambda (a b)
          (let ((ra (playground-lock--archive-rank a))
                (rb (playground-lock--archive-rank b)))
            (if (/= ra rb) (< ra rb)
              (version-list-< (package-desc-version b) (package-desc-version a)))))))

(defun playground-lock--resolve (name min stack)
  "Choose a version of NAME satisfying MIN (a version list); STACK detects cycles."
  (cond
   ((eq name 'emacs)
    (unless (version-list-<= min (version-to-list emacs-version))
      (error "Emacs %s is too old (%s needed by %s)"
             emacs-version (package-version-join min) (car stack))))
   ((memq name stack) nil)
   ((and (not (assq name playground-lock--chosen))
         (package-built-in-p name min))
    (cl-pushnew name playground-lock--builtin))
   (t
    (let* ((prev (cdr (assq name playground-lock--mins)))
           (need (if (and prev (version-list-< min prev)) prev min))
           (chosen (cdr (assq name playground-lock--chosen))))
      (setf (alist-get name playground-lock--mins) need)
      (if (and chosen (version-list-<= need (package-desc-version chosen)))
          nil
        (when chosen
          (error "Conflict: %s %s chosen but %s needs %s"
                 name (package-version-join (package-desc-version chosen))
                 (car stack) (package-version-join need)))
        (let ((desc (seq-find (lambda (d) (version-list-<= need (package-desc-version d)))
                              (playground-lock--candidates name))))
          (unless desc
            (error "No archive has %s >= %s (needed by %s)"
                   name (package-version-join need) (or (car stack) "packages.el")))
          (push (cons name desc) playground-lock--chosen)
          (dolist (req (package-desc-reqs desc))
            (playground-lock--resolve (car req) (cadr req) (cons name stack)))
          (push desc playground-lock--order)))))))

(defun playground-lock--file (desc)
  "Archive file name of DESC."
  (concat (package-desc-full-name desc) (package-desc-suffix desc)))

(defun playground-lock--url (desc)
  "Download URL of DESC."
  (concat (cdr (assoc (package-desc-archive desc) playground-lock-archives))
          (playground-lock--file desc)))

(defun playground-lock-sha256-file (file)
  "Hex sha256 of FILE's bytes."
  (with-temp-buffer
    (set-buffer-multibyte nil)
    (insert-file-contents-literally file)
    (secure-hash 'sha256 (current-buffer))))

(defun playground-lock-download (url file)
  "Download URL to FILE with curl; signal an error on failure."
  (let ((status (call-process "curl" nil nil nil "-fsSL" "--retry" "3" "-o" file url)))
    (unless (eq status 0)
      (error "Download failed (%s): %s" status url))))

(defun playground-lock-read-list (file)
  "Read the module/package list in FILE."
  (with-temp-buffer
    (insert-file-contents file)
    (read (current-buffer))))

(defun playground-lock (list-file lock-file)
  "Resolve LIST-FILE and write LOCK-FILE (and vendor/ next to it)."
  (let* ((package-archives playground-lock-archives)
         (package-user-dir (make-temp-file "pg-lock-elpa" t))
         (package-archive-priorities nil)
         (tmp (make-temp-file "pg-lock-dl" t))
         (spec (playground-lock-read-list list-file))
         (modules (plist-get spec :modules))
         (vendor (expand-file-name "vendor" (file-name-directory lock-file)))
         lines)
    (setq playground-lock--chosen nil playground-lock--order nil
          playground-lock--builtin nil playground-lock--mins nil
          playground-lock--snapshot (plist-get spec :prefer-snapshot)
          playground-lock--pins (plist-get spec :pin))
    (make-directory vendor t)
    (dolist (old (directory-files vendor t "\\.\\(tar\\|el\\)\\'"))
      (delete-file old))
    (package-refresh-contents)
    (dolist (m modules)
      (dolist (p (cdr m))
        (playground-lock--resolve p nil (list (car m)))))
    (dolist (desc (reverse playground-lock--order))
      (let* ((file (playground-lock--file desc))
             (dest (expand-file-name file tmp)))
        (playground-lock-download (playground-lock--url desc) dest)
        (when (member (package-desc-archive desc) playground-lock-vendored-archives)
          (copy-file dest (expand-file-name file vendor) t))
        (push (format "%s %s %s %s %s"
                      (package-desc-name desc)
                      (package-version-join (package-desc-version desc))
                      (package-desc-archive desc)
                      file
                      (playground-lock-sha256-file dest))
              lines)
        (message "locked %s" file)))
    (with-temp-file lock-file
      (insert "# packages.lock: Emacs packages installed into the session image (emacs.md section 8).\n"
              "# Generated by emacs.d/tools/lock.el from emacs.d/packages.el with Emacs "
              emacs-version " on " (format-time-string "%Y-%m-%d" nil t) ".\n"
              "# Do not edit; change packages.el and regenerate.  The image build installs these\n"
              "# files in order and fails when a download is missing or its sha256 differs.\n"
              "#\n"
              "# Built in, not locked: "
              (mapconcat #'symbol-name (sort playground-lock--builtin #'string<) " ") "\n"
              "#\n"
              "# @archive NAME URL\n"
              "# NAME VERSION ARCHIVE FILE SHA256\n")
      (dolist (a playground-lock-archives)
        (insert (format "@archive %s %s\n" (car a) (cdr a))))
      (dolist (l (nreverse lines))
        (insert l "\n")))
    (delete-directory tmp t)
    (delete-directory package-user-dir t)
    (message "wrote %s (%d packages)" lock-file (length playground-lock--order))))

(defun playground-lock-batch ()
  "Batch entry point: emacs --batch -l lock.el -f playground-lock-batch LIST LOCK."
  (let ((list-file (or (pop command-line-args-left)
                       (expand-file-name "../packages.el" (file-name-directory load-file-name))))
        (lock-file (or (pop command-line-args-left)
                       (expand-file-name "../packages.lock" (file-name-directory load-file-name)))))
    (condition-case err
        (playground-lock list-file lock-file)
      (error (message "lock.el: %s" (error-message-string err))
             (kill-emacs 1)))))

(provide 'playground-lock)
;;; lock.el ends here
