;;; cheer.el --- A word of encouragement  -*- lexical-binding: t; -*-

;; Author: Emacs Playground
;; Version: 1.0
;; Package-Requires: ((emacs "29.1"))
;; URL: https://github.com/playground-course/cheer

;;; Commentary:

;; M-x cheer shows a short encouragement in the echo area.  Written for
;; lesson 13.2 of Emacs Playground, which installs it from a local git
;; repository standing in for GitHub.

;;; Code:

(defvar cheer-lines
  '("Nice work." "You are getting faster." "That was a clean edit."
    "Emacs is starting to feel like yours.")
  "What `cheer' may say.")

;;;###autoload
(defun cheer ()
  "Show a word of encouragement."
  (interactive)
  (message "%s" (nth (random (length cheer-lines)) cheer-lines)))

(provide 'cheer)
;;; cheer.el ends here
