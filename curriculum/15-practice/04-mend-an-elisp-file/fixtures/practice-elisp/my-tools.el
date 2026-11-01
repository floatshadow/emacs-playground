;;; my-tools.el --- small text tools  -*- lexical-binding: t; -*-

;; Three small commands for prose: count the words of the buffer, wrap
;; a region to a narrow column, and put a title in a box of dashes.
;;
;; The file is broken in three places.  Make it compile, make
;; `my-tools-wrap-region' wrap the region, and make it a command you
;; can run with M-x.

;;; Code:

(defun my-tools-word-count (start end)
  "Return the number of words between START and END."
  (count-words start end))

(defun my-tools-count-buffer ()
  "Show how many words the buffer has."
  (interactive)
  (message "%d words" (my-tools-word-count (point-min) (point-max)))

(defun my-tools-wrap (text width)
  "Return TEXT filled to WIDTH columns."
  (with-temp-buffer
    (insert text)
    (let ((fill-column width))
      (fill-region (point-min) (point-max)))
    (buffer-string)))

(defun my-tools-wrap-region (start end)
  "Wrap the text between START and END to 40 columns."
  (let ((text (buffer-substring start end)))
    (delete-region start end)
    (goto-char start)
    (insert (my-tools-wrap 40 text))))

(defun my-tools-boxed (title)
  "Return TITLE between two lines of dashes as long as it."
  (let ((rule (make-string (length title) ?-)))
    (concat rule "\n" title "\n" rule)))

(provide 'my-tools)
;;; my-tools.el ends here
