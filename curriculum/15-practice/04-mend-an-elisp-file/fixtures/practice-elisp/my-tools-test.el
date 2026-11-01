;;; my-tools-test.el --- tests for my-tools.el  -*- lexical-binding: t; -*-

;; In Emacs: evaluate my-tools.el and this file, then M-x ert RET t RET.
;; From a shell, in this directory:
;;   emacs -Q --batch -l my-tools.el -l my-tools-test.el -f ert-run-tests-batch-and-exit

;;; Code:

(require 'ert)
(require 'my-tools)

(defconst my-tools-test-text
  "one two three four five six seven eight nine ten eleven twelve")

(ert-deftest my-tools-test-wrap ()
  "`my-tools-wrap-region' wraps the region to 40 columns."
  (with-temp-buffer
    (insert my-tools-test-text)
    (my-tools-wrap-region (point-min) (point-max))
    (should (equal (buffer-string)
                   "one two three four five six seven eight\nnine ten eleven twelve"))))

(ert-deftest my-tools-test-command ()
  "M-x my-tools-wrap-region wraps the active region."
  (with-temp-buffer
    (transient-mark-mode 1)
    (insert my-tools-test-text)
    (push-mark (point-min) t t)
    (goto-char (point-max))
    (call-interactively #'my-tools-wrap-region)
    (should (string-search "\n" (buffer-string)))))

(ert-deftest my-tools-test-boxed ()
  "`my-tools-boxed' puts dashes above and below the title."
  (should (equal (my-tools-boxed "Notes") "-----\nNotes\n-----")))

(provide 'my-tools-test)
;;; my-tools-test.el ends here
