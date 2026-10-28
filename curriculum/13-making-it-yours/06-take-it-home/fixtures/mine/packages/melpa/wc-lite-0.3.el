;;; wc-lite.el --- Word count in the mode line  -*- lexical-binding: t; -*-

;; Author: Emacs Playground
;; Version: 0.3
;; Package-Requires: ((emacs "29.1"))
;; Keywords: convenience, wp
;; URL: https://github.com/playground-course/wc-lite

;;; Commentary:

;; A minor mode that shows the buffer's word count in the mode line.
;; Written for lesson 13.2 of Emacs Playground, which serves it from a
;; small local package archive.

;;; Code:

(defgroup wc-lite nil
  "Word count in the mode line."
  :group 'convenience)

(defcustom wc-lite-format " %dw"
  "Mode-line text for the word count; %d is the number of words."
  :type 'string)

(defun wc-lite--lighter ()
  "The mode-line text for the current buffer."
  (format wc-lite-format (count-words (point-min) (point-max))))

;;;###autoload
(define-minor-mode wc-lite-mode
  "Show the buffer's word count in the mode line."
  :lighter (:eval (wc-lite--lighter)))

(provide 'wc-lite)
;;; wc-lite.el ends here
