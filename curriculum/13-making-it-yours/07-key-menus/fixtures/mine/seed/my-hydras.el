;;; my-hydras.el --- my key menus  -*- lexical-binding: t; no-byte-compile: t; -*-

;; A hydra is a menu of keys that stays open.  It lists its keys at the
;; bottom of the frame; each key (a head) runs a command.  Red heads,
;; the default, keep the menu open; blue heads (:exit t) close it; q
;; closes it, and so does C-g, always.
;;
;; pretty-hydra-define lays the heads out in titled columns: a column
;; title, then a list of heads, each (KEY COMMAND "hint").  Group them
;; the way you think about your commands, and move a head to another
;; column whenever you change your mind.
;;
;; Try a change at once: C-M-x inside a form, or M-x eval-buffer.
;;
;; no-byte-compile (first line): Emacs compiles user-lisp/ before init.el
;; has made the packages available, and pretty-hydra-define is a macro
;; from a package.

;; Loaded after init.el (the cookie below).  A mistake in this file then
;; shows as a message at startup instead of stopping it.
;;;###autoload
(add-hook 'after-init-hook
          (lambda () (with-demoted-errors "my-hydras.el: %S" (require 'my-hydras))))

(require 'pretty-hydra)
(require 'my-keys)                      ; my-map, the C-c u prefix of lesson 13.3

;;;; My menu

(pretty-hydra-define my-menu
  (:title "My menu (q closes it)" :quit-key "q")
  ("Windows"
   (("o" other-window "other window")
    ("2" split-window-below "split below")
    ("3" split-window-right "split right")
    ("0" delete-window "close this one")
    ("1" delete-other-windows "only this one"))
   "Buffers"
   (("b" switch-to-buffer "switch buffer" :exit t)
    ("f" find-file "open a file")
    ("s" save-buffer "save"))
   "Toggles"
   (("w" whitespace-mode "whitespace" :toggle t)
    ("v" visual-line-mode "wrap at words" :toggle t))
   "Misc"
   (("d" duplicate-line "duplicate line")
    ("l" display-line-numbers-mode "line numbers" :toggle t)
    ("c" count-words "count words"))))

;;;; Window sizes

;; defhydra writes the hint itself: a docstring that starts with a
;; newline, where _x_ marks the key x.  Pink: keys that are not heads
;; still work, so you can type while it is open.
(defhydra my-sizes (:color pink :hint nil)
  "
 Window size: _<left>_ narrower  _<right>_ wider  _<up>_ taller  _<down>_ shorter  _q_ done"
  ("<left>" shrink-window-horizontally)
  ("<right>" enlarge-window-horizontally)
  ("<up>" enlarge-window)
  ("<down>" shrink-window)
  ("q" nil))

;;;; My keys

;; A binding whose command is ("label" . COMMAND) shows the label in
;; which-key's popup.
(keymap-set my-map "s" '("window sizes" . my-sizes/body))

;; My key for the menu:


(provide 'my-hydras)
;;; my-hydras.el ends here
