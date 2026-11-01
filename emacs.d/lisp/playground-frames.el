;;; playground-frames.el --- Frames and windows of the playground agent  -*- lexical-binding: t; -*-

;;; Commentary:

;; Which frame and window are the learner's, frames opening and
;; closing (and the windows a new frame inherits), the window helpers
;; the other parts share, and the ops that open a GUI frame and let go
;; of the GUI display (emacs.md section 10.3).

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)
(require 'playground-transport)

(defvar playground--pending-layout)
(declare-function playground--emit-state "playground-observe" (&optional force))
(declare-function playground--schedule-state "playground-observe" (&rest _))
(declare-function playground--apply-pending-layout "playground-editor-state" (frame))
(declare-function x-display-list "xfns.c" ())
(declare-function x-close-connection "xfns.c" (terminal))
(declare-function x-hide-tip "xfns.c" ())

(defun playground--frame-kind (&optional frame)
  "\"gui\" or \"terminal\" for FRAME."
  (if (display-graphic-p frame) "gui" "terminal"))

(defvar playground--learner-frame nil
  "The frame the learner's last command ran in (or the newest client frame).")

(defun playground--usable-frame-p (frame)
  "Non-nil when FRAME is a live, real frame the learner can see.
The daemon's initial frame is not one."
  (and (playground--top-level-frame-p frame)
       (not (and (daemonp) (eq frame terminal-frame)))))

(defun playground--gui-frames ()
  "The graphical frames the learner can see, newest first."
  (seq-filter (lambda (f) (and (playground--usable-frame-p f) (display-graphic-p f)))
              (frame-list)))

(defun playground--learner-frame ()
  "The frame the learner works in.
That is the frame of the last command when it still exists, unless
it is a terminal frame while a graphical frame exists: graphical
frames exist only while sessiond's GUI mode is on (it deletes them
when it leaves it), and the browser then shows the GUI frame, so the
newest graphical frame is the learner's.  Calls from sessiond run
from timers, between commands, with whatever frame Emacs last
selected (the GUI frame just made, the daemon's own frame after a
client frame closed), which is not always the one the learner is
looking at."
  (let ((last playground--learner-frame)
        (gui (playground--gui-frames)))
    (cond ((and (playground--usable-frame-p last)
                (or (display-graphic-p last) (null gui)))
           last)
          (gui (car gui))
          ((playground--usable-frame-p (selected-frame)) (selected-frame))
          ((seq-find #'playground--usable-frame-p (frame-list)))
          (t (selected-frame)))))

(defun playground--note-learner-frame (&optional frame)
  "Remember FRAME (default: the selected one) as the learner's frame."
  (let ((frame (or frame (selected-frame))))
    (when (playground--usable-frame-p frame)
      (setq playground--learner-frame frame))))

(defun playground--user-window ()
  "The window the learner is working in.
That is the selected window of the learner's frame
\(`playground--learner-frame'), or, while a minibuffer is open
there, the window that was selected when it was entered."
  (let* ((frame (playground--learner-frame))
         (w (frame-selected-window frame)))
    (if (window-minibuffer-p w)
        (let ((m (minibuffer-selected-window)))
          (if (and m (window-live-p m) (eq (window-frame m) frame))
              m
            (frame-first-window frame)))
      w)))

;;;; Frames

(defun playground--top-level-frame-p (frame)
  "Non-nil when FRAME is a real frame (not a child frame or tooltip)."
  (and (frame-live-p frame)
       (not (frame-parameter frame 'parent-frame))
       (not (frame-parameter frame 'tooltip))))

(defun playground--frame-event (event frame)
  "Emit a frame EVENT (\"opened\" or \"closed\") for FRAME."
  (when (playground--top-level-frame-p frame)
    (playground--emit `((t . "frame") (event . ,event) (kind . ,(playground--frame-kind frame))))))

(defun playground--copy-windows (state to)
  "Give frame TO the windows of STATE, a `window-state-get' value.
TO's selected window becomes the one STATE has selected."
  (when (frame-live-p to)
    (playground--safely "copy windows"
      (with-selected-frame to
        (let ((ignore-window-parameters t))
          (playground--only-window)
          (window-state-put state (frame-root-window to) 'safe))))
    (playground--schedule-state)))

(defun playground--frame-windows (frame)
  "The window state of FRAME, to put into another frame.
Without pixel sizes, so the windows are sized by their proportions: a
GUI frame's pixels mean nothing in a terminal frame, where
`window-state-put' would find every window too small for them."
  (playground--drop-pixel-sizes (window-state-get (frame-root-window frame))))

(defun playground--drop-pixel-sizes (state)
  "Window STATE without its pixel-width and pixel-height entries."
  (if (proper-list-p state)
      (mapcar #'playground--drop-pixel-sizes
              (seq-remove (lambda (e) (memq (car-safe e) '(pixel-width pixel-height))) state))
    state))

(defun playground--on-make-frame ()
  "An emacsclient frame opened (it is selected)."
  (playground--safely "make-frame"
    (let* ((frame (selected-frame))
           ;; The frame of the last command, not `playground--learner-frame',
           ;; which already prefers this frame when it is graphical.
           (previous (and (playground--usable-frame-p playground--learner-frame)
                          playground--learner-frame)))
      ;; The browser shows the frame just made: the terminal frame after
      ;; a restart, the GUI frame when the learner switches to it.
      (playground--note-learner-frame frame)
      (playground--frame-event "opened" frame)
      (playground--emit-state t)
      ;; After the server has finished setting the frame up (its initial
      ;; buffer): restore a layout kept by `open' during a restart, else
      ;; give the new frame the windows of the frame the learner was in,
      ;; so switching to the GUI frame keeps the lesson's windows.
      (cond
       (playground--pending-layout
        (run-at-time 0 nil #'playground--apply-pending-layout frame))
       ((and previous (not (eq previous frame)))
        (run-at-time 0 nil (lambda ()
                             (when (playground--usable-frame-p previous)
                               (playground--copy-windows
                                (playground--frame-windows previous) frame)))))))))

(defun playground--last-frame-on-terminal-p (frame)
  "Non-nil when no other real frame shares FRAME's terminal.
\[make-frame-command] in the terminal frame adds a second frame to the
same tty; deleting it again with \[delete-frame] closes nothing the
learner would call \"the terminal frame\"."
  (let ((term (frame-terminal frame)))
    (not (seq-some (lambda (f)
                     (and (not (eq f frame))
                          (eq (frame-terminal f) term)
                          (playground--top-level-frame-p f)))
                   (frame-list)))))

(defun playground--on-delete-frame (frame)
  "FRAME is about to be deleted.
When it is the last graphical frame (sessiond leaves GUI mode, or C-x
C-c in it), its windows go back to the learner's terminal frame: what
the learner arranged in the GUI frame is still there."
  (playground--safely "delete-frame"
    (when (playground--last-frame-on-terminal-p frame)
      (playground--frame-event "closed" frame))
    (when (and (display-graphic-p frame)
               (playground--usable-frame-p frame)
               (equal (playground--gui-frames) (list frame)))
      (when-let* ((to (seq-find (lambda (f)
                                  (and (not (eq f frame)) (playground--usable-frame-p f)))
                                (frame-list))))
        (when (eq playground--learner-frame frame)
          (setq playground--learner-frame to))
        (run-at-time 0 nil #'playground--copy-windows (playground--frame-windows frame) to)))
    (playground--schedule-state)))

(defmacro playground--in-user-window (&rest body)
  "Run BODY with the learner's window selected (not the minibuffer).
The window is in the learner's frame (`playground--learner-frame'),
so `selected-window', `frame-first-window' and the like mean what the
learner sees, whichever frame Emacs happens to have selected."
  (declare (indent 0) (debug t))
  `(let ((w (playground--user-window)))
     (if (eq w (selected-window))
         (progn ,@body)
       (with-selected-window w ,@body))))

(defun playground--main-window (frame)
  "A window of FRAME to keep: one visiting a file if any, never a side window."
  (let* ((user (playground--user-window))
         (wins (window-list frame 'nomini (frame-first-window frame)))
         (normal (seq-remove (lambda (w) (window-parameter w 'window-side)) wins)))
    (or (and (memq user normal) (buffer-file-name (window-buffer user)) user)
        (seq-find (lambda (w) (buffer-file-name (window-buffer w))) normal)
        (and (memq user normal) user)
        (car normal)
        user)))

(defun playground--leave-side-window ()
  "Select a normal window of the frame when a side window is selected.
Deleting the other windows from a side window (with
`ignore-window-parameters', as layouts and rescue do) leaves it as
the frame's only window, still marked as a side window; splitting it
then recurses until `max-lisp-eval-depth', so C-x 2 and C-x 3 fail.
The learner is in a side window after a help popup with
`help-window-select' (lesson 3.5's popups.el) or in dirvish-side.
Returns the selected window."
  (let ((w (selected-window)))
    (when (window-parameter w 'window-side)
      (let ((main (playground--main-window (window-frame w))))
        (when (and (window-live-p main) (not (window-parameter main 'window-side)))
          (select-window main))))
    (selected-window)))

(defun playground--only-window ()
  "Leave one ordinary window in the selected frame.
That is the selected window, or a normal window when a side window is
selected (`playground--leave-side-window')."
  (let ((ignore-window-parameters t))
    (playground--leave-side-window)
    (delete-other-windows)
    (playground--unside-sole-window)))

(defun playground--unside-sole-window (&optional frame)
  "Make FRAME's only window an ordinary window again.
A side window that became the only window (see
`playground--leave-side-window') keeps its `window-side' parameter
and its dedication, and nothing can be split.  Clear them.  Returns
non-nil when there was something to repair."
  (let ((wins (window-list frame 'nomini)))
    (when (and (= (length wins) 1)
               (or (window-parameter (car wins) 'window-side)
                   (window-parameter (car wins) 'window-slot)))
      (let ((w (car wins)))
        (set-window-parameter w 'window-side nil)
        (set-window-parameter w 'window-slot nil)
        (set-window-dedicated-p w nil)
        t))))

(defun playground--op-frame (args &rest _)
  "Open a GUI frame on the display in ARGS.
No `fullscreen' parameter: it needs a window manager, and there is
none; the base module fits GUI frames to their display."
  (let* ((display (or (alist-get 'display args) (getenv "PG_DISPLAY") ":1"))
         (frame (make-frame-on-display display)))
    (playground--frame-event "opened" frame)
    `((frame . ,(frame-parameter frame 'name)))))


(defun playground--op-leave-display (&rest _)
  "Delete the X frames (the GUI frame) and close the X display connections.
Stopping Xvnc afterwards then does not make Emacs report \"Connection
lost to X server\" in the terminal frame: deleting the frames alone
keeps the connection open.  The session image's Emacs is a Lucid
build, where `x-close-connection' is safe.  Replies {closed, detail}:
closed is false when a display stayed open, and detail says why (the
display is then left running)."
  (when (fboundp 'x-hide-tip) (ignore-errors (x-hide-tip)))
  (dolist (f (frame-list))
    (when (eq (framep f) 'x) (delete-frame f t)))
  (let ((failed nil))
    (when (fboundp 'x-display-list)
      (dolist (d (x-display-list))
        (condition-case err (x-close-connection d)
          (error (push (format "%s: %s" d (error-message-string err)) failed)))))
    `((closed . ,(playground--bool (null failed)))
      (detail . ,(mapconcat #'identity failed "; ")))))

(defun playground--op-resume-loop (&rest _)
  "Make the command loop start waiting for keys again; reply {}.
After the GUI frame's terminal is gone, the loop that was waiting for
a key from it swallows the next input event from the terminal frame
\(a key, or the ESC of an escape sequence, whose rest is then typed as
text: \"[I\"); a `top-level' from a timer restarts the wait on the
terminal frame first."
  (run-at-time 0 nil #'top-level)
  (make-hash-table))

(provide 'playground-frames)
;;; playground-frames.el ends here
