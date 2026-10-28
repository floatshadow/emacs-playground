;;; stamp.el --- mark lines as reviewed  -*- lexical-binding: t; -*-

;; A tiny command to change while Emacs runs.  Evaluate this file
;; (M-x eval-buffer), then try M-x stamp-line in notes.txt.
;;
;; C-x C-e right after a closing parenthesis evaluates the expression
;; that ends there, even in a comment: (* 6 7)

(defun stamp-text ()
  "The text `stamp-line' puts at the end of a line."
  "[reviewed]")

(defun stamp-line ()
  "Append the stamp to the current line."
  (interactive)
  (save-excursion
    (end-of-line)
    (insert " " (stamp-text))))

;;; stamp.el ends here
