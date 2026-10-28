;;; tally.el --- draw a histogram of the region  -*- lexical-binding: t; -*-

;; Glue between any buffer and the tally binary: select lines of
;; numbers anywhere (a log, a table, shell output) and M-x tally-region.

(defvar tally-directory (file-name-directory (or load-file-name buffer-file-name))
  "The tally crate, where `cargo run' builds and runs the binary.")

(defun tally-region (start end)
  "Draw a histogram of the numbers between START and END."
  (interactive "r")
  (let ((default-directory tally-directory))
    (shell-command-on-region start end "cargo run -q" "*tally*")))

(provide 'tally)
;;; tally.el ends here
