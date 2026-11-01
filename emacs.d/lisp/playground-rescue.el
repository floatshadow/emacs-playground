;;; playground-rescue.el --- Rescue for the playground agent  -*- lexical-binding: t; -*-

;;; Commentary:

;; The Rescue button (emacs.md section 10.3): leave prompts, recursive
;; edits, searches, macros, narrowing, extra windows and waiting loops,
;; in two phases when a throw to the command loop is needed, and undo
;; what the debug signal leaves set.

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)
(require 'playground-transport)
(require 'playground-frames)
(require 'playground-observe)
(require 'playground-compat)

(declare-function isearch-done "isearch" (&optional nopush edit))
(declare-function iedit-mode "iedit" (&optional arg))
(declare-function multiple-cursors-mode "multiple-cursors-core" (&optional arg))
(defvar isearch--current-buffer)

;;;;; Rescue (emacs.md 10.3)

(defun playground--rescue-plan ()
  "Actions the first, unwinding phase of rescue will take."
  (let (did)
    (when (> (minibuffer-depth) 0) (push "cancelled the prompt" did))
    (when (> (- (recursion-depth) (minibuffer-depth)) 0) (push "left the recursive edit" did))
    did))

(defun playground--op-rescue (_args &optional id)
  "Get the learner out of any confusing state; reply {did:[...]}.
An open hydra is closed first (its keys would otherwise stay on top of
everything rescue leaves the learner in).  A loop reading keys of its
own (`playground-agent-waiting-loops') is left next, with the keys
that leave it, and the rest happens once it has ended.  Leaving
minibuffers and recursive edits needs a throw to the command loop, so
in that case the rest of the work runs from a timer at top level.  In
both cases the reply is sent from there."
  (let ((hydra (playground--close-hydra)))
    (if (not playground--waiting)
        (playground--rescue-from (and hydra (list hydra)) id)
      (let ((left (nth 3 playground--waiting)))
        (playground--quit-waiting
         (key-parse (nth 4 playground--waiting))
         (lambda ()
           (let ((value (playground--rescue-from (delq nil (list hydra left)) id)))
             (when (and id (not (eq value 'playground--deferred)))
               (playground--reply id t value)))))
        'playground--deferred))))

(defconst playground--quit-waiting-tries 40
  "Times, 50 ms apart, rescue checks whether a waiting loop has ended.")

(defun playground--quit-waiting (keys then &optional tries)
  "Make the running waiting loop read KEYS, then call THEN once it has ended.
The loop reads them as if typed.  Unwinding it with a throw instead
(as for prompts) would leave a tty menu drawn over a frame that no
longer redisplays.  TRIES counts the checks so far."
  (unless tries
    (setq unread-command-events (append (listify-key-sequence keys) unread-command-events)))
  (if (and playground--waiting (< (or tries 0) playground--quit-waiting-tries))
      (run-at-time 0.05 nil #'playground--quit-waiting keys then (1+ (or tries 0)))
    (funcall then)))

(defun playground--rescue-from (prior id)
  "Rescue from prompts, recursive edits and the rest, after PRIOR actions.
Returns the reply value, or `playground--deferred' when the reply to
call ID is sent later (see `playground--op-rescue')."
  ;; Newest first, as `playground--rescue-plan' lists them.
  (let ((did (append (playground--rescue-plan) (reverse prior))))
    (if (null (playground--rescue-plan))
        `((did . ,(vconcat (playground--rescue-rest (reverse did)))))
      ;; Phase 2 after unwinding.
      (run-at-time 0 nil (lambda ()
                           (let ((all (playground--rescue-rest (reverse did))))
                             (when id (playground--reply id t `((did . ,(vconcat all))))))))
      (funcall playground-agent-rescue-unwind-function)
      'playground--deferred)))

(defun playground--rescue-rest (did)
  "Second phase of rescue; DID lists actions so far.  Returns all actions."
  (let ((did (reverse did)))
    (playground--safely "rescue isearch"
      (when (bound-and-true-p isearch-mode)
        (with-current-buffer (or (bound-and-true-p isearch--current-buffer) (current-buffer))
          (isearch-done))
        (push "stopped the search" did)))
    (playground--safely "rescue macro"
      (when defining-kbd-macro
        (end-kbd-macro)
        (push "stopped recording the keyboard macro" did)))
    (let* ((frame (selected-frame))
           (keep (playground--main-window frame))
           (visible (mapcar #'window-buffer (window-list frame 'nomini))))
      (playground--safely "rescue buffers"
        (dolist (b (delete-dups visible))
          (with-current-buffer b
            (when overwrite-mode
              (overwrite-mode -1)
              (push "turned off overwrite mode" did))
            (when (buffer-narrowed-p)
              (widen)
              (push (format "widened %s" (buffer-name)) did))
            (when (bound-and-true-p multiple-cursors-mode)
              (multiple-cursors-mode -1)
              (push "left multiple cursors" did))
            (when (bound-and-true-p iedit-mode)
              (ignore-errors (iedit-mode -1))
              (push "left iedit" did)))))
      (playground--safely "rescue windows"
        (let ((others (1- (length (window-list frame 'nomini)))))
          (when (> others 0)
            (let ((ignore-window-parameters t))
              (delete-other-windows keep))
            (push (format "closed %d other window%s" others (if (= others 1) "" "s")) did))
          (when (playground--unside-sole-window frame)
            (push "made the window splittable again" did))
          (select-window keep)))
      (playground--safely "rescue special"
        (let ((shown (window-buffer keep)))
          (dolist (b (buffer-list))
            (when (and (buffer-live-p b)
                       (not (equal (buffer-name b) "*scratch*"))
                       (provided-mode-derived-p (buffer-local-value 'major-mode b) 'special-mode))
              (if (eq b shown)
                  (progn
                    (with-selected-window keep (bury-buffer))
                    (push (format "put away %s" (buffer-name b)) did))
                (bury-buffer b))))))
      (playground--safely "rescue mark"
        (with-current-buffer (window-buffer keep)
          (when (region-active-p)
            (deactivate-mark)
            (push "dropped the selection" did)))))
    (discard-input)
    (setq did (nreverse did))
    ;; Reported even when the last Rescue said the same.
    (setq playground--last-echo nil)
    (let ((playground--suppress-echo nil))
      (message "%s" (if did
                        (concat "Rescue: " (string-join did ", ") ".")
                      "Rescue: nothing to undo; you were already in a normal state.")))
    did))


(defun playground--op-reset-debug (&rest _)
  "Undo what the debug signal (SIGUSR2) leaves set; reply {}.
Emacs's handler for it sets `debug-on-quit', after which every C-g in
a prompt opens *Backtrace*."
  (setq debug-on-quit nil)
  (make-hash-table))

(provide 'playground-rescue)
;;; playground-rescue.el ends here
