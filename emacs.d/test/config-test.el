;;; config-test.el --- ERT tests for early-init.el's helpers  -*- lexical-binding: t; -*-

;; Run from emacs.d/:
;;
;;   emacs --batch -l early-init.el -l test/config-test.el \
;;     -f ert-run-tests-batch-and-exit
;;
;; early-init.el decides where native code compiled at run time goes:
;; somewhere writable whose mount allows executing it (a noexec tmpfs
;; does not), else nowhere.  These tests feed it a made-up mount table.
;;
;; In the session image this file also loads modules-test.el, the tests
;; of the configuration modules themselves.

;;; Code:

(require 'ert)

(defmacro pg-config-with-mounts (spec &rest body)
  "Run BODY with a temporary directory and a fake mount table.
SPEC is (DIR MOUNTS): DIR is bound to the directory, MOUNTS is a list
of (RELATIVE-MOUNT-POINT . OPTIONS) mounted under it, in order."
  (declare (indent 1))
  (let ((dir (car spec)) (mounts (cadr spec)))
    `(let* ((,dir (file-name-as-directory (file-truename (make-temp-file "pg-mounts" t))))
            (playground--mountinfo (expand-file-name "mountinfo" ,dir)))
       (unwind-protect
           (progn
             (with-temp-file playground--mountinfo
               (insert "22 1 0:21 / / rw,relatime - overlay overlay rw\n")
               (let ((id 30))
                 (dolist (m ,mounts)
                   (let ((point (directory-file-name (expand-file-name (car m) ,dir))))
                     (make-directory point t)
                     (insert (format "%d 22 0:%d / %s %s - tmpfs tmpfs rw\n"
                                     id id (string-replace " " "\\040" point) (cdr m)))
                     (setq id (1+ id))))))
             ,@body)
         (delete-directory ,dir t)))))

(ert-deftest pg-config/mount-options-longest-match ()
  "The options of the innermost mount holding a directory are used."
  (pg-config-with-mounts (d '(("cache" . "rw,nosuid,nodev,noexec,relatime")
                              ("work" . "rw,relatime")
                              ("with space" . "rw,noexec")))
    (should (member "noexec" (playground--mount-options (expand-file-name "cache/emacs/eln-cache/" d))))
    (should-not (member "noexec" (playground--mount-options (expand-file-name "work/.playground/eln-cache/" d))))
    (should (member "noexec" (playground--mount-options (expand-file-name "with space/x/" d))))
    ;; Outside every listed mount: the root's options.
    (should (equal (playground--mount-options "/") '("rw" "relatime")))))

(ert-deftest pg-config/later-mount-hides-earlier ()
  "A second mount on the same point wins, as in the kernel's table."
  (pg-config-with-mounts (d '(("cache" . "rw,noexec") ("cache" . "rw,exec")))
    (should-not (member "noexec" (playground--mount-options (expand-file-name "cache/" d))))))

(ert-deftest pg-config/eln-dir-usable ()
  "Usable means writable and on a mount that allows exec."
  (pg-config-with-mounts (d '(("cache" . "rw,noexec") ("work" . "rw")))
    (should-not (playground--eln-dir-usable-p (expand-file-name "cache/emacs/eln-cache/" d)))
    (should (playground--eln-dir-usable-p (expand-file-name "work/.playground/eln-cache/" d)))
    (set-file-modes (expand-file-name "work" d) #o555)
    (unwind-protect
        (unless (zerop (user-uid))      ; root can write anyway
          (should-not (playground--eln-dir-usable-p (expand-file-name "work/.playground/eln-cache/" d))))
      (set-file-modes (expand-file-name "work" d) #o755))))

(ert-deftest pg-config/no-mount-table ()
  "Without /proc, nothing is known to be noexec."
  (let ((playground--mountinfo "/nonexistent/mountinfo"))
    (should-not (playground--mount-options "/"))
    (should (eq (playground--eln-dir-usable-p temporary-file-directory)
                (file-writable-p temporary-file-directory)))))

;; The modules' own tests load init.el with the packages of the image;
;; they run with these (`make emacs-test') when those packages exist.
(when (file-directory-p (or (getenv "PG_SITE_ELPA") "/opt/playground/elpa"))
  (load (expand-file-name "modules-test.el" (file-name-directory (or load-file-name buffer-file-name)))
        nil t))

(provide 'config-test)
;;; config-test.el ends here
