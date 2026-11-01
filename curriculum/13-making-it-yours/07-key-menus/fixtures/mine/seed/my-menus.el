;;; my-menus.el --- my key menus  -*- lexical-binding: t; -*-

;; A transient menu stays on screen and lists its keys in a window at
;; the bottom of the frame; each key (a suffix) runs a command.  A
;; suffix closes the menu after it runs, unless it says :transient t;
;; q closes this menu, and C-g closes any.  Magit's menus are made the
;; same way, with transient, which is built into Emacs.
;;
;; transient-define-prefix lays the suffixes out in groups: a vector
;; whose first element is the group's title, then one list per suffix,
;; ("KEY" "description" COMMAND).  A vector of groups shows them side
;; by side.  Group them the way you think about your commands, and move
;; a suffix to another group whenever you change your mind.
;;
;; Try a change at once: C-M-x inside a form, or M-x eval-buffer.

;; Loaded after init.el (the cookie below).  A mistake in this file then
;; shows as a message at startup instead of stopping it.
;;;###autoload
(add-hook 'after-init-hook
          (lambda () (with-demoted-errors "my-menus.el: %S" (require 'my-menus))))

(require 'transient)
(require 'my-keys)                      ; my-map, the C-c u prefix of lesson 13.3

;;;; My menu

(transient-define-prefix my-menu ()
  "Open my menu: windows, buffers, toggles; q closes it."
  [["Windows"
    ("o" "other window" other-window :transient t)
    ("2" "split below" split-window-below :transient t)
    ("3" "split right" split-window-right :transient t)
    ("0" "close this one" delete-window :transient t)
    ("1" "only this one" delete-other-windows :transient t)]
   ["Buffers"
    ("b" "switch buffer" switch-to-buffer)
    ("f" "open a file" find-file :transient t)
    ("s" "save" save-buffer :transient t)]
   ["Toggles"
    ("w" "whitespace" whitespace-mode :transient t)
    ("v" "wrap at words" visual-line-mode :transient t)]
   ["Misc"
    ("d" "duplicate line" duplicate-line :transient t)
    ("l" "line numbers" display-line-numbers-mode :transient t)
    ("c" "count words" count-words :transient t)]]
  [("q" "close" transient-quit-one)])

;;;; Window sizes

;; Keys the menu does not list still do their usual job, and the menu
;; stays (:transient-non-suffix), so you can type while it is open.
(transient-define-prefix my-sizes ()
  "Resize windows with the arrow keys; other keys still work, q closes."
  :transient-non-suffix 'transient--do-stay
  ["Window size"
   ("<left>" "narrower" shrink-window-horizontally :transient t)
   ("<right>" "wider" enlarge-window-horizontally :transient t)
   ("<up>" "taller" enlarge-window :transient t)
   ("<down>" "shorter" shrink-window :transient t)
   ("q" "done" transient-quit-one)])

;;;; My keys

;; A binding whose command is ("label" . COMMAND) shows the label in
;; which-key's popup.
(keymap-set my-map "s" '("window sizes" . my-sizes))

;; My key for the menu:


(provide 'my-menus)
;;; my-menus.el ends here
