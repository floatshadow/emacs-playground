;;; windows.el --- windows, layouts and workspaces  -*- lexical-binding: t; -*-

;; Module: windows.  Unlocked in Part 3 (Windows, layouts and workspaces).
;;
;; Moving between windows (windmove, ace-window), undoing layout changes
;; (winner), where pop-up buffers appear (display-buffer-alist, popper),
;; named layouts (burly), workspaces (tab-bar, one tab per project), and
;; focus (olivetti, follow-mode).
;;
;; Keys added here, by lesson:
;;   3.1  C-x 2 / 3 / 0 / 1 / o, C-x 4 0, C-x 5 2 / 0 (built in)
;;   3.2  S-<left> S-<right> S-<up> S-<down>   move to the window there;
;;        M-o   ace-window: pick a window by its label;
;;        C-x o o o   repeat (repeat-mode, in base)
;;   3.3  C-x ^ { } - +, C-x w r / f / t rotate, flip, transpose (built in)
;;   3.4  C-x 4 f / b / d / c / 4 / 1, C-x 5 f (built in)
;;   3.5  C-`  toggle the latest pop-up (popper);  M-`  cycle pop-ups;
;;        C-M-`  make a buffer a pop-up or a normal window;
;;        C-x w s  hide or show side windows (built in)
;;   3.6  C-c <left> / C-c <right>   undo / redo layout changes (winner);
;;        C-x r w / C-x r j   window configuration registers;
;;        M-x burly-bookmark-windows   a named layout
;;   3.7  C-x t 2 / RET / 0 / o / r / b (built in);
;;        C-x p p   switch project in its own tab
;;   3.8  M-x olivetti-mode, M-x follow-mode, C-x C-+ / C-x C-M-+

;;;; Lesson 3.2  Moving between windows

(setq windmove-wrap-around t)
(windmove-default-keybindings 'shift)

(playground-with-package ace-window
  (setq aw-keys '(?a ?s ?d ?f ?g ?h ?j ?k ?l)
        aw-scope 'frame
        aw-dispatch-always nil
        aw-background t)
  (keymap-global-set "M-o" #'ace-window))

;;;; Lesson 3.5  Taming pop-ups

;; Where buffers appear.  Each rule is (WHICH-BUFFERS ACTIONS PARAMETERS):
;; build output, grep and occur results and other command output open
;; in a window at the bottom, a third of the frame high, reusing a
;; window that already shows one of them.  Help keeps Emacs's default
;; (it splits the selected window), and shells open where you are.
;; Edit these rules, or add your own in custom.el, to change where
;; things go.
(defvar playground-display-buffer-rules
  '(((or (derived-mode . compilation-mode)
         (derived-mode . occur-mode)
         (derived-mode . xref--xref-buffer-mode)
         "\\`\\*Async Shell Command\\*")
     (display-buffer-reuse-mode-window display-buffer-reuse-window display-buffer-at-bottom)
     (window-height . 0.33)))
  "The playground's `display-buffer-alist' rules.")

(dolist (rule (reverse playground-display-buffer-rules))
  (add-to-list 'display-buffer-alist rule))

;; Pop-ups: popper remembers which buffers are "pop-ups" so one key
;; shows or hides the latest one, like VS Code's panel.  Where they
;; appear is left to the rules above.
(playground-with-package popper
  (setq popper-reference-buffers
        '("\\*Messages\\*" "Output\\*$" "\\*Async Shell Command\\*" "\\*Warnings\\*"
          help-mode helpful-mode compilation-mode grep-mode occur-mode
          "^\\*eshell.*\\*$" eshell-mode "^\\*eat.*\\*$" eat-mode)
        popper-display-control nil
        popper-group-function nil)
  (keymap-global-set "C-`" #'popper-toggle)
  (keymap-global-set "M-`" #'popper-cycle)
  (keymap-global-set "C-M-`" #'popper-toggle-type)
  (popper-mode 1)
  (popper-echo-mode 1))

;;;; Lesson 3.6  Undo and save layouts

(setq winner-dont-bind-my-keys nil)
(winner-mode 1)

;; burly-bookmark-windows saves the current layout as a bookmark (its
;; commands are autoloaded; nothing to turn on).

;;;; Lesson 3.7  Workspaces with tab-bar

;; The tab bar appears once there are two tabs.
(setq tab-bar-show 1
      tab-bar-new-tab-choice "*scratch*"
      tab-bar-close-button-show nil
      tab-bar-new-button-show nil
      tab-bar-tab-hints t)
(tab-bar-history-mode 1)

(defun playground-project-tab (dir)
  "Switch to the project in DIR in a tab of its own.
The tab is named after the project; if it exists already, switch to
it, so each project keeps its own window layout."
  (interactive (list (funcall project-prompter)))
  (let ((name (file-name-nondirectory (directory-file-name dir))))
    (if (tab-bar--tab-index-by-name name)
        (tab-bar-switch-to-tab name)
      (tab-bar-new-tab)
      (tab-bar-rename-tab name)
      (project-switch-project dir))))
(with-eval-after-load 'project
  (keymap-set project-prefix-map "p" #'playground-project-tab))

;;;; Lesson 3.8  Focus and layout recipes

(playground-with-package olivetti
  (setq olivetti-body-width 100))

;; follow-mode is built in: two windows on one buffer scroll as one page.

;; TODO(emacs, emacs.md section 3): golden-ratio (off by default) is not
;; in packages.lock yet.

;;; windows.el ends here
