;;; playground-compat.el --- Package knowledge of the playground agent  -*- lexical-binding: t; -*-

;;; Commentary:

;; What the agent knows about packages that change how Emacs reads
;; keys or shows lists (emacs.md section 10.3): loops that read keys of
;; their own (menus, avy, which-key's pages), transient menus, eldoc
;; and eglot messages, and how Dired, ibuffer and the buffer list are
;; made again after a restore.

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)
(require 'playground-transport)

(declare-function project-root "project" (project))
(declare-function dired-noselect "dired" (dir-or-list &optional switches))
(declare-function dired-get-filename "dired" (&optional localp no-error-if-not-filep))
(declare-function dired-remember-marks "dired" (beg end))
(declare-function dired-mark-remembered "dired" (alist))
(declare-function dired-unmark-all-marks "dired" ())
(declare-function dired-goto-file "dired" (file))
(declare-function ibuffer "ibuffer" (&optional other-window-p name qualifiers noselect shrink filter-groups formats))
(declare-function ibuffer-update "ibuffer" (arg &optional silent))
(declare-function ibuffer-current-state-list "ibuffer" (&optional pos))
(declare-function ibuffer-current-buffer "ibuffer" (&optional must-be-live))
(declare-function ibuffer-unmark-all-marks "ibuffer" ())
(declare-function ibuffer-mark-on-buffer "ibuf-ext" (func &optional ibuffer-mark-on-buffer-mark group))
(declare-function ibuffer-jump-to-buffer "ibuf-ext" (name))
(declare-function list-buffers-noselect "buff-menu" (&optional files-only buffer-list filter-predicate))
(declare-function transient-active-prefix "transient" (&optional prefixes))
(declare-function transient--emergency-exit "transient" (&optional id))
(declare-function eieio-oref "eieio-core" (obj slot))
(declare-function eieio-oset "eieio-core" (obj slot value))
(declare-function playground--schedule-state "playground-observe" (&rest _))
(declare-function playground--echo "playground-observe" (text))
(defvar dired-directory)
(defvar ibuffer-filtering-qualifiers)
(defvar transient--prefix)
(defvar playground--suppress-echo)

(defvar playground--waiting nil
  "The entry of `playground-agent-waiting-loops' whose loop is running, or nil.
Bound around the loop, so timers that run while it waits for a key
see it.")

(defun playground--menu-name ()
  "Name of the transient menu that is open (\"my-menu\"), or nil.
A transient prefix (lesson 13.5) is no loop: it pushes its keymap over
every other and its keys go to the command loop as usual.  Nil while
the menu is hidden or suspended (a suffix reading from the minibuffer):
then the minibuffer is what waits.  C-g closes the menu (in a nested
one it goes back to the outer menu)."
  (when-let* (((featurep 'transient))
              (prefix (transient-active-prefix)))
    (symbol-name (eieio-oref prefix 'command))))

(defun playground--menu-what (name)
  "\"the NAME menu\"."
  (format "the %s menu" name))

(defun playground--close-menu ()
  "Close the transient menu, if any; return \"closed the NAME menu\" or nil.
Visible, hidden or suspended alike: `transient--prefix' is set from
the menu's start to its end.  `transient--emergency-exit' takes its
keymaps, hooks and window away and drops the outer menus of a nested
one; it needs no key and works outside the command loop.  A suspended
suffix would put the hooks back when it returns, so the prefix first
forgets how (its `unwind-suffix')."
  (when-let* ((prefix (bound-and-true-p transient--prefix)))
    (let ((name (symbol-name (eieio-oref prefix 'command))))
      (playground--safely "close menu"
        (eieio-oset prefix 'unwind-suffix nil)
        (transient--emergency-exit))
      (concat "closed " (playground--menu-what name)))))

(defun playground--waiting-advice (entry)
  "Around advice marking the loop of ENTRY, from `playground-agent-waiting-loops'."
  (lambda (orig &rest args)
    (let ((playground--waiting entry))
      (playground--schedule-state)
      (unwind-protect
          (apply orig args)
        (playground--schedule-state)))))

(defun playground--eldoc-quietly (orig &rest args)
  "Around advice: eldoc's echo-area messages are not reported."
  (let ((playground--suppress-echo t))
    (apply orig args)))

(defun playground--eglot-connecting (_managed-modes project _class contact &rest _)
  "Before advice for `eglot--connect': say which server starts for PROJECT.
CONTACT is the server's command line when it is a list of strings.
Eglot's own \"[eglot] Connected!\" message, which follows once the
server answers, reaches sessiond like any other message."
  (playground--safely "eglot"
    (let ((program (and (consp contact) (stringp (car contact))
                        (file-name-nondirectory (car contact))))
          (name (ignore-errors
                  (file-name-nondirectory (directory-file-name (project-root project))))))
      (playground--echo (format "Starting the language server%s for %s…"
                                (if program (concat " " program) "")
                                (or name "this project"))))))

(defun playground--list-buffer-props (name &optional pos)
  "Window-state properties that let a restore make list buffer NAME again.
For a Dired listing: its directory, the file on the line at POS (the
window's point) and its marks; for ibuffer: its filters, its marks and
the buffer on the line at POS; for the buffer list: a flag.  Nil for
other buffers.  A step's demo kills or changes these buffers (2.6
moves files in Dired, 2.5 filters ibuffer), and a reset setup kills
them, so the window would otherwise come back on another buffer."
  (when-let* ((b (get-buffer name)))
    (with-current-buffer b
      (ignore-errors
        (save-excursion
          (when (integerp pos) (goto-char (max (point-min) (min pos (point-max)))))
          (cond
           ((and (derived-mode-p 'dired-mode) dired-directory)
            `((dired . ,(expand-file-name (if (consp dired-directory)
                                               (car dired-directory)
                                             dired-directory)))
              (dired-file . ,(dired-get-filename nil t))
              (dired-marks . ,(dired-remember-marks (point-min) (point-max)))))
           ((derived-mode-p 'ibuffer-mode)
            `((ibuffer . t)
              (ibuffer-filters . ,ibuffer-filtering-qualifiers)
              (ibuffer-at . ,(let ((cur (ibuffer-current-buffer)))
                               (and (buffer-live-p cur) (buffer-name cur))))
              (ibuffer-marks
               . ,(delq nil (mapcar (lambda (e)
                                      (and (buffer-live-p (car e))
                                           (characterp (cdr e))
                                           (not (eq (cdr e) ?\s))
                                           (cons (buffer-name (car e)) (cdr e))))
                                    (ibuffer-current-state-list))))))
           ((derived-mode-p 'Buffer-menu-mode)
            '((buffer-menu . t)))))))))

(defun playground--rebuild-list-buffer (name props)
  "The list buffer that window-state PROPS describe, made again if needed.
A Dired listing is visited again and reverted, since a restore has just
put its files back, with the marks it had; ibuffer gets its filters and
marks back; the buffer list is regenerated.  NAME is the buffer's name
when the state was saved.  Nil when PROPS describe no list buffer or the
directory is gone."
  (playground--safely "rebuild list buffer"
    (cond
     ((assq 'dired props)
      (let ((dir (alist-get 'dired props)))
        (when (and (stringp dir) (file-directory-p dir))
          (require 'dired)
          (let ((b (dired-noselect dir)))
            (with-current-buffer b
              (dired-unmark-all-marks)
              (revert-buffer)
              (when-let* ((marks (alist-get 'dired-marks props)))
                (dired-mark-remembered marks)))
            b))))
     ((assq 'ibuffer props)
      (require 'ibuffer)
      (require 'ibuf-ext)
      (let ((b (get-buffer name)))
        (unless (and b (eq (buffer-local-value 'major-mode b) 'ibuffer-mode))
          (save-window-excursion (ibuffer nil name nil t))
          (setq b (get-buffer name)))
        (when b
          (with-current-buffer b
            (setq ibuffer-filtering-qualifiers (alist-get 'ibuffer-filters props))
            (ibuffer-update nil t)
            (ibuffer-unmark-all-marks)
            (dolist (m (alist-get 'ibuffer-marks props))
              (when-let* ((mb (and (stringp (car m)) (get-buffer (car m)))))
                (ibuffer-mark-on-buffer (lambda (x) (eq x mb)) (cdr m)))))
          b)))
     ((assq 'buffer-menu props)
      (list-buffers-noselect)))))

(defun playground--list-buffer-point (b props)
  "PROPS with point on the line of the file or buffer they name in B."
  (let ((pos (ignore-errors
               (with-current-buffer b
                 (save-excursion
                   (cond
                    ((stringp (alist-get 'dired-file props))
                     (and (dired-goto-file (alist-get 'dired-file props)) (point)))
                    ((stringp (alist-get 'ibuffer-at props))
                     (goto-char (point-min))
                     (ibuffer-jump-to-buffer (alist-get 'ibuffer-at props))
                     (point))))))))
    (if (integerp pos)
        (cons (cons 'point pos) (assq-delete-all 'point (copy-alist props)))
      props)))

(provide 'playground-compat)
;;; playground-compat.el ends here
