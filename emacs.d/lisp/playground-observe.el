;;; playground-observe.el --- What the playground agent reports  -*- lexical-binding: t; -*-

;;; Commentary:

;; The reporting half of the agent (emacs.md section 10.2, api.md
;; section 5): one `key' event per command, the `state' object, echo-area
;; messages, minibuffer and isearch prompts, and the `eval' and `check'
;; ops lessons use to look at Emacs.

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)
(require 'playground-transport)
(require 'playground-frames)
(require 'playground-compat)

(declare-function flymake-diagnostics "flymake" (&optional beg end))
(declare-function flymake-diagnostic-type "flymake" (diag))
(declare-function flymake-diagnostic-text "flymake" (diag))
(declare-function flymake-diagnostic-beg "flymake" (diag))
(declare-function flymake-diagnostic-backend "flymake" (diag))
(declare-function flymake--lookup-type-property "flymake" (type prop &optional default))
(declare-function tab-bar-tabs "tab-bar" (&optional frame))
(declare-function isearch-message-prefix "isearch" (&optional ellipsis nonincremental))
(defvar isearch-string)
(defvar isearch-nonincremental)
(defvar flymake-mode)
(defvar playground-agent-mode)
(defvar playground-agent-check-timeout)
(defvar playground-agent-interesting-minor-modes)
(defvar playground-agent-echo-noise)
(defvar playground-agent-ignored-commands)
(defvar playground-agent-ignored-keys)

(defvar playground--last-state nil "The last state line sent, to suppress duplicates.")
(defvar playground--state-timer nil "Pending coalesced state emission.")
(defvar playground--early nil
  "Commands already reported when they opened a minibuffer.
A list of (DEPTH . COMMAND), where DEPTH is the minibuffer depth the
command runs at.  The command's own `post-command-hook' run is then
not reported a second time.")
(defvar playground--mx-reader nil
  "(DEPTH . KEYS) of the extended-command reader being used, e.g. M-x.")
(defvar playground--mx-armed nil
  "Minibuffer depth at which a command chosen by M-x is running.")
(defvar playground--exit-reported nil
  "Command already reported by `minibuffer-exit-hook'.
A prompt cancelled with C-g is followed by one more run of
`post-command-hook' for the same command, without its keys.")
(defvar playground--window-counter 0 "Last window id handed out.")
(defvar playground--suppress-echo nil "Non-nil while messages must not be reported.")
(defvar playground--in-hook nil "Guards against re-entering the agent from its own hooks.")

;;;; State snapshot (emacs.md 10.2, api.md 5)

(defun playground--window-id (window)
  "Small integer id of WINDOW, stable for the window's lifetime."
  (or (window-parameter window 'playground-id)
      (set-window-parameter window 'playground-id (cl-incf playground--window-counter))))

(defun playground--reported-window-p (window &optional selected)
  "Non-nil when WINDOW appears in states and snapshots.
Windows showing internal buffers (names starting with a space, such
as which-key's popup while a prefix is pending) are left out, unless
WINDOW is SELECTED."
  (or (eq window selected)
      (not (string-prefix-p " " (buffer-name (window-buffer window))))))

(defun playground--windows (frame selected)
  "Vector describing the live windows of FRAME; SELECTED is the learner's window."
  (vconcat
   (delq nil
         (mapcar (lambda (w)
                   (when (playground--reported-window-p w selected)
                     (let ((buf (window-buffer w))
                           (side (window-parameter w 'window-side)))
                       `((id . ,(playground--window-id w))
                         (buf . ,(buffer-name buf))
                         (mode . ,(symbol-name (buffer-local-value 'major-mode buf)))
                         (selected . ,(playground--bool (eq w selected)))
                         (dedicated . ,(playground--bool (window-dedicated-p w)))
                         (side . ,(if side (symbol-name side) :null))))))
                 (window-list frame 'nomini (frame-first-window frame))))))

(defun playground--prune-tree (node selected)
  "NODE of `window-tree' without unreported windows, or nil.
A combination left with one child is replaced by that child, and a
child split the same way as its parent is merged into it, so hiding
a popup never leaves a one-child split behind.  SELECTED is the
learner's window, always kept.  Combinations are (DIR CHILD...),
DIR non-nil when the children are stacked top to bottom."
  (if (windowp node)
      (and (playground--reported-window-p node selected) node)
    (let* ((dir (car node))
           (children
            (mapcan (lambda (child)
                      (let ((c (playground--prune-tree child selected)))
                        (cond ((null c) nil)
                              ((and (consp c) (eq (car c) dir)) (copy-sequence (cdr c)))
                              (t (list c)))))
                    (cddr node))))
      (cond ((null children) nil)
            ((null (cdr children)) (car children))
            (t (cons dir children))))))

(defun playground--tree (frame &optional selected)
  "Pruned window tree of FRAME; SELECTED defaults to the learner's window."
  (playground--prune-tree (car (window-tree frame))
                          (or selected (playground--user-window))))

(defun playground--layout-node (node)
  "Reduce a pruned window tree NODE to window ids and splits."
  (if (windowp node)
      (playground--window-id node)
    (vconcat (list (if (car node) "vsplit" "hsplit"))
             (mapcar #'playground--layout-node (cdr node)))))

(defun playground--layout (frame &optional selected)
  "Window tree of FRAME as nested vectors (vsplit stacks, hsplit is side by side)."
  (let ((tree (playground--tree frame selected)))
    (if tree (playground--layout-node tree) :null)))

(defun playground--frame-count ()
  "Number of frames, without child frames (corfu popups) and tooltips."
  (seq-count #'playground--top-level-frame-p (frame-list)))

(defun playground--tabs (frame)
  "Tab-bar description of FRAME."
  (if (and (featurep 'tab-bar) (fboundp 'tab-bar-tabs))
      (let ((tabs (tab-bar-tabs frame)))
        `((count . ,(max 1 (length tabs)))
          (current . ,(let ((cur (assq 'current-tab tabs)))
                        (substring-no-properties
                         (or (alist-get 'name cur)
                             (buffer-name (window-buffer (frame-selected-window frame)))))))))
    `((count . 1)
      (current . ,(buffer-name (window-buffer (frame-selected-window frame)))))))

(defun playground--displayed-text (beg end)
  "The text between BEG and END of the current buffer as it is displayed.
Invisible text (`invisible-p') is left out, and a stretch of text with
a string `display' property shows that string instead."
  (let ((pos beg) (out '()))
    (while (< pos end)
      (let ((display (get-char-property pos 'display))
            (next (min (next-single-char-property-change pos 'invisible nil end)
                       (next-single-char-property-change pos 'display nil end))))
        (cond ((invisible-p pos))
              ((stringp display) (push display out))
              (t (push (buffer-substring-no-properties pos next) out)))
        (setq pos next)))
    (substring-no-properties (apply #'concat (nreverse out)))))

(defun playground--prompt ()
  "The prompt of the active minibuffer, as displayed.
visual-replace's \"Replace in region (2L)from pointin buffer: \" shows
one of its three scopes and hides the others.  Read from the current
buffer when it is the minibuffer, else `minibuffer-prompt'."
  (if (minibufferp)
      (playground--displayed-text (point-min) (minibuffer-prompt-end))
    (substring-no-properties (or (minibuffer-prompt) ""))))

(defun playground--minibuffer ()
  "The open minibuffer or isearch as an object, or :null."
  (let ((mw (active-minibuffer-window)))
    (cond
     (mw
      (with-current-buffer (window-buffer mw)
        (let ((contents (minibuffer-contents-no-properties)))
          `((depth . ,(minibuffer-depth))
            (prompt . ,(playground--prompt))
            (contents . ,(if (> (length contents) 1000) (substring contents 0 1000) contents))
            (kind . "prompt")))))
     ((bound-and-true-p isearch-mode)
      `((depth . 0)
        (prompt . ,(playground--isearch-prompt))
        (contents . ,(substring-no-properties (or isearch-string "")))
        (kind . "isearch")))
     (t :null))))

(defun playground--isearch-prompt ()
  "The isearch prompt, e.g. \"I-search: \"."
  (or (ignore-errors
        (string-trim-left
         (substring-no-properties (isearch-message-prefix nil isearch-nonincremental))))
      "I-search: "))

(defun playground--active-minors ()
  "Vector of the interesting minor modes enabled in the current buffer."
  (let (out)
    (dolist (m playground-agent-interesting-minor-modes)
      (when (and (boundp m) (symbol-value m))
        (push (symbol-name m) out)))
    (vconcat (nreverse out))))

(defun playground--read-only-reason ()
  "Why the current buffer is read-only, or :null."
  (cond ((not buffer-read-only) :null)
        ((bound-and-true-p view-mode) "view-mode")
        ((bound-and-true-p magit-blame-read-only-mode) "magit-blame")
        ((derived-mode-p 'special-mode) "special-mode")
        ((derived-mode-p 'dired-mode) "dired-mode")
        ;; No TRAMP calls here: state runs from a timer, which may
        ;; interrupt another TRAMP operation on the same connection.
        ((and buffer-file-name (file-remote-p buffer-file-name))
         "file is write-protected")
        ((and buffer-file-name (file-exists-p buffer-file-name)
              (not (file-writable-p buffer-file-name)))
         "file is write-protected")
        (t "read-only-mode")))

(defun playground--diag-severity (type)
  "Map a Flymake diagnostic TYPE to error, warning or note."
  (let ((cat (or (ignore-errors (flymake--lookup-type-property type 'flymake-category)) type)))
    (cond ((memq cat '(:error flymake-error eglot-error)) "error")
          ((memq cat '(:warning flymake-warning eglot-warning)) "warning")
          (t "note"))))

(defun playground--diag-at-point (pt)
  "The Flymake diagnostic at PT in the current buffer, or :null."
  (or (and (bound-and-true-p flymake-mode)
           (fboundp 'flymake-diagnostics)
           (ignore-errors
             (when-let* ((d (car (flymake-diagnostics pt))))
               (let ((beg (flymake-diagnostic-beg d))
                     (backend (flymake-diagnostic-backend d)))
                 `((severity . ,(playground--diag-severity (flymake-diagnostic-type d)))
                   (message . ,(car (split-string (substring-no-properties
                                                   (flymake-diagnostic-text d))
                                                  "\n")))
                   (source . ,(cond ((null backend) "flymake")
                                    ((symbolp backend)
                                     (if (string-prefix-p "eglot" (symbol-name backend))
                                         "eglot"
                                       (symbol-name backend)))
                                    (t "flymake")))
                   (line . ,(line-number-at-pos beg t))
                   (col . ,(save-excursion (goto-char beg) (current-column))))))))
      :null))

(defun playground--state ()
  "The state object (an alist ready for `json-serialize')."
  (let* ((win (playground--user-window))
         (frame (window-frame win))
         (buf (window-buffer win)))
    (with-current-buffer buf
      (let ((pt (if (eq win (selected-window)) (point) (window-point win))))
        `((t . "state")
          (buf . ,(buffer-name buf))
          (file . ,(playground--str buffer-file-name))
          (mode . ,(symbol-name major-mode))
          (modeName . ,(substring-no-properties (format-mode-line mode-name)))
          (minors . ,(playground--active-minors))
          (modified . ,(playground--bool (buffer-modified-p)))
          (readonly . ,(playground--bool buffer-read-only))
          (narrowed . ,(playground--bool (buffer-narrowed-p)))
          (overwrite . ,(playground--bool overwrite-mode))
          (line . ,(line-number-at-pos pt t))
          (col . ,(save-excursion (goto-char pt) (current-column)))
          (point . ,pt)
          (markActive . ,(playground--bool (region-active-p)))
          (windows . ,(playground--windows frame win))
          (layout . ,(playground--layout frame win))
          (frame . ,(playground--frame-kind frame))
          (frames . ,(playground--frame-count))
          (tabs . ,(playground--tabs frame))
          (minibuffer . ,(playground--minibuffer))
          (isearch . ,(playground--bool (bound-and-true-p isearch-mode)))
          ;; Recursive edits proper: minibuffer levels are not counted,
          ;; like the [ ] brackets in the mode line.
          (recursive . ,(max 0 (- (recursion-depth) (minibuffer-depth))))
          (macro . ,(playground--bool defining-kbd-macro))
          ;; The prefix argument waiting for the next command (C-u ...).
          (prefixArg . ,(if prefix-arg (format "%S" prefix-arg) :null))
          (waiting . ,(playground--waiting-object))
          (readOnlyReason . ,(playground--read-only-reason))
          (diagAtPoint . ,(playground--diag-at-point pt)))))))

(defun playground--waiting-object ()
  "The state's `waiting': {kind, what, leave} for a running loop, or :null.
An open hydra is reported too (`playground--hydra-name')."
  (cond
   (playground--waiting
    `((kind . ,(nth 1 playground--waiting))
      (what . ,(nth 2 playground--waiting))
      (leave . ,(nth 4 playground--waiting))))
   ((playground--hydra-name)
    `((kind . "hydra")
      (what . ,(playground--hydra-what (playground--hydra-name)))
      (leave . "C-g")))
   (t :null)))

(defun playground--emit-state (&optional force)
  "Send the state unless it equals the last one sent (FORCE sends anyway)."
  (when playground--state-timer
    (cancel-timer playground--state-timer)
    (setq playground--state-timer nil))
  ;; States are not queued while disconnected: a fresh one follows hello.
  (when playground--connected
    (let ((line (playground--json (playground--state))))
      (when (or force (not (equal line playground--last-state)))
        (setq playground--last-state line)
        (playground--send-line line 'event)))))

(defun playground--schedule-state (&rest _)
  "Emit the state soon, once, whatever triggered it."
  (when (and playground--connected (not playground--state-timer))
    (setq playground--state-timer
          (run-at-time 0 nil (lambda ()
                               (setq playground--state-timer nil)
                               (playground--safely "state" (playground--emit-state)))))))

;;;; Command reporting

(defvar playground--pre-keys []
  "Keys of the command about to run, saved by `pre-command-hook'.
A command that signals (C-g, C-f at the end of the buffer) gets its
`post-command-hook' run only after the error, when Emacs no longer
knows its keys.")

(defvar playground--pre-command nil
  "The command about to run, saved by `pre-command-hook'.")

(defvar playground--pre-frame nil
  "Kind of the frame the command about to run was typed in.
C-x C-c deletes its frame before `post-command-hook' runs.")

(defconst playground--prefix-pager-commands '(which-key-C-h-dispatch)
  "Commands that page through the bindings of a pending prefix.
which-key runs one for C-h after a prefix; it ends with the prefix
read again, still pending, so it completed no key sequence and no key
event is sent for it.")

(defconst playground--prefix-arg-commands
  '(universal-argument universal-argument-more universal-argument-minus
    digit-argument negative-argument)
  "Commands that build a prefix argument.
They call `prefix-command-preserve-state', which sets `this-command'
and `real-this-command' back to the previous command, so they are
recognised before they run.")

(defun playground--command ()
  "The command the learner's keys ran, as reported in key events.
That is `real-this-command', not `this-command': commands rewrite the
latter to steer the next one (`kill-line' runs `kill-region', which
sets it to `kill-region' so that kills append; `yank-pop' sets it to
`yank'; `yank' with an empty kill ring leaves t; TAB in the minibuffer
runs `minibuffer-complete', which sets it to `completion-at-point'),
so it can name a command the learner never ran.  M-x sets both to the
command chosen by name.

One exception: an amaranth hydra ignores keys it does not list by
rewriting `this-command' to `hydra-amaranth-warn' before the command
runs, so that warning is what the key did."
  (if (eq this-command 'hydra-amaranth-warn) this-command real-this-command))

(defun playground--pre-command ()
  "Remember the keys of the command about to run, and the command.
A command starting at this depth means every command at this depth or
deeper has finished, even one whose last part ran from a timer and so
got no `post-command-hook' run (Embark's action after C-.)."
  (let ((depth (minibuffer-depth)))
    (setq playground--early (seq-remove (lambda (e) (>= (car e) depth)) playground--early)))
  (setq playground--pre-keys (this-command-keys-vector)
        playground--pre-command (playground--command)
        playground--pre-frame (playground--frame-kind)))

(defun playground--this-keys ()
  "Keys of the current command, in `key-description' form."
  (key-description (this-command-keys-vector)))

(defun playground--saved-keys ()
  "Keys of the running command: those `pre-command-hook' saved, if any.
Commands that read keys themselves change `this-command-keys': after
Embark's C-. d it is d."
  (if (> (length playground--pre-keys) 0)
      (key-description playground--pre-keys)
    (playground--this-keys)))

(defun playground--key-event (cmd keys mx &optional buffer frame)
  "Build a key event for CMD invoked with KEYS; MX when run through M-x.
BUFFER is where the command ran: by default the selected window's
buffer (the minibuffer while a prompt has the focus).  FRAME is the
kind of frame it was typed in, by default the selected window's."
  (let* ((win (if buffer (playground--user-window) (selected-window)))
         (buf (or buffer (window-buffer win))))
    `((t . "key")
      (keys . ,keys)
      (cmd . ,(playground--symbol-name cmd))
      (buf . ,(buffer-name buf))
      (mode . ,(symbol-name (buffer-local-value 'major-mode buf)))
      (frame . ,(or frame (playground--frame-kind (window-frame win))))
      ,@(when current-prefix-arg `((prefixArg . ,(format "%S" current-prefix-arg))))
      ,@(when mx '((mx . t))))))

(defun playground--mx-keys (depth)
  "Keys of the extended-command reader used at DEPTH, or \"M-x\"."
  (or (and playground--mx-reader (= (car playground--mx-reader) depth)
           (cdr playground--mx-reader))
      "M-x"))

(defun playground--post-command ()
  "Report the command that just finished, then the state."
  (unless playground--in-hook
    (let ((playground--in-hook t))
      (playground--safely "post-command"
        (let* ((depth (minibuffer-depth))
               (cmd (if (memq playground--pre-command playground--prefix-arg-commands)
                        playground--pre-command
                      (playground--command)))
               ;; Already reported when its prompt opened.  This also
               ;; covers the extra run of this hook that starts the
               ;; minibuffer's own command loop (depth one deeper, the
               ;; same `this-command', no keys).
               (early (seq-find (lambda (e) (eq (cdr e) cmd)) playground--early))
               (mx (and playground--mx-armed (>= playground--mx-armed depth)))
               ;; The keys saved before the command ran: commands that
               ;; read keys themselves (C-h k) change `this-command-keys'.
               (keys (if mx (playground--mx-keys playground--mx-armed) (playground--saved-keys)))
               ;; The frame the keys were typed in (C-x C-c deletes it).
               (frame (and (> (length playground--pre-keys) 0) playground--pre-frame))
               ;; Focus and mouse-movement events are no keys of the
               ;; learner's (a terminal frame hidden behind the GUI frame
               ;; still gets focus reports).
               (internal (or (memq cmd playground-agent-ignored-commands)
                             (string-match-p playground-agent-ignored-keys keys))))
          (unless internal (playground--note-learner-frame))
          (setq playground--pre-keys [] playground--pre-command nil playground--pre-frame nil)
          ;; Every command started at this depth or deeper has finished.
          (setq playground--early (seq-remove (lambda (e) (>= (car e) depth)) playground--early))
          (when mx (setq playground--mx-armed nil))
          (when (and cmd (eq cmd playground--exit-reported)) (setq early t))
          (setq playground--exit-reported nil)
          (when (and playground--mx-reader (>= (car playground--mx-reader) depth))
            (setq playground--mx-reader nil))
          (unless (or early internal
                      (memq cmd '(execute-extended-command execute-extended-command-for-buffer))
                      (memq cmd playground--prefix-pager-commands)
                      (and (null cmd) (string-empty-p keys)))
            (playground--emit (playground--key-event cmd keys mx nil frame)))
          (playground--emit-state))))))

(defun playground--mx-advice (orig &rest args)
  "Around advice for the M-x commands: remember the command runs by name."
  (setq playground--mx-armed (minibuffer-depth))
  (apply orig args))

;;;; Minibuffer and isearch

(defun playground--minibuffer-event (open prompt depth kind)
  "Emit a minibuffer event."
  (playground--emit `((t . "minibuffer")
                      (open . ,(playground--bool open))
                      (prompt . ,prompt)
                      (depth . ,depth)
                      (kind . ,kind))))

(defun playground--on-minibuffer-setup ()
  "A minibuffer opened."
  (unless playground--in-hook
    (let ((playground--in-hook t))
      (playground--safely "minibuffer-setup"
        (let* ((depth (minibuffer-depth))
               (outer (1- depth))
               (cmd (playground--command)))
          (cond
           ((memq cmd '(execute-extended-command execute-extended-command-for-buffer))
            ;; The M-x prompt itself: remember its keys, report the
            ;; command it runs instead.
            (setq playground--mx-reader (cons outer (playground--this-keys))))
           ((and cmd (not (assoc outer playground--early)))
            ;; Report the command now so the ticker shows C-x C-f while
            ;; its prompt is open, not after it has finished.
            (let ((mx (and playground--mx-armed (= playground--mx-armed outer))))
              ;; A prompt opened with no keys left to name (Embark's
              ;; action asking after C-. quit the prompt it was typed
              ;; in, which reported the keys) is the same command.
              (let ((keys (if mx (playground--mx-keys outer) (playground--saved-keys))))
                (unless (string-empty-p keys)
                  (playground--emit (playground--key-event
                                     cmd keys mx
                                     (window-buffer (playground--user-window))))))
              (push (cons outer cmd) playground--early))))
          (playground--minibuffer-event t (playground--prompt) depth "prompt")
          (playground--schedule-state))))))

(defun playground--on-minibuffer-exit ()
  "A minibuffer closed."
  (unless playground--in-hook
    (let ((playground--in-hook t))
      (playground--safely "minibuffer-exit"
        ;; The command that leaves the prompt (RET runs exit-minibuffer,
        ;; C-g minibuffer-keyboard-quit) exits by a throw, so its own
        ;; `post-command-hook' run never happens; report it here, where
        ;; its keys are still known.  A prompt closed by the command
        ;; that opened it (Embark's action filling in and accepting
        ;; Delete file:) was reported when it opened.
        (when-let* ((cmd (playground--command)))
          (let ((keys (playground--saved-keys)))
            (unless (or (string-empty-p keys) (rassq cmd playground--early))
              (playground--emit (playground--key-event cmd keys nil (current-buffer)))
              (setq playground--exit-reported cmd
                    playground--pre-keys []))))
        (playground--minibuffer-event nil (playground--prompt) (minibuffer-depth) "prompt")
        (playground--schedule-state)))))

(defun playground--on-isearch-start ()
  "Isearch started."
  (playground--safely "isearch-start"
    (playground--minibuffer-event t (playground--isearch-prompt) 0 "isearch")
    (playground--schedule-state)))

(defun playground--on-isearch-end ()
  "Isearch ended."
  (playground--safely "isearch-end"
    (playground--minibuffer-event nil (playground--isearch-prompt) 0 "isearch")
    (playground--schedule-state)))

;;;; Echo area

(defun playground--noise-p (text)
  "Non-nil when TEXT should not be reported."
  (seq-some (lambda (re) (string-match-p re text)) playground-agent-echo-noise))

(defvar playground--last-echo nil
  "(TEXT . KEYS) of the last echo sent: its text and `num-input-keys' then.")

(defun playground--echo (text)
  "Report echo-area TEXT unless it is noise or the last message shown again.
A message equal to the last one reported, with no key sequence read
since, is that message displayed again: in another frame's echo area,
or put back by `map-y-or-n-p' after `auto-save-visited-mode' saved
from its idle timer.  Echo messages are never key events: an unbound
key runs `undefined' between the command hooks, which report it."
  (when (and (stringp text) (not playground--suppress-echo) playground-agent-mode)
    (let ((text (string-trim (substring-no-properties text))))
      (unless (or (playground--noise-p text)
                  (equal (cons text num-input-keys) playground--last-echo))
        (setq playground--last-echo (cons text num-input-keys))
        (playground--emit `((t . "echo")
                            (text . ,(if (> (length text) 500) (substring text 0 500) text))))))))

(defun playground--on-message (message &rest _)
  "Watch MESSAGE on its way to the echo area (`set-message-function')."
  (playground--safely "message" (playground--echo message))
  nil)

(defun playground--on-command-error (data context _caller)
  "Report the error DATA the command loop is about to display, with CONTEXT."
  (playground--safely "command-error"
    (playground--echo (concat (or context "") (error-message-string data)))))


;;;; Ops (api.md 7)

(defun playground--jsonify (value &optional depth)
  "Convert Lisp VALUE to something `json-serialize' accepts.
nil is null, t is true, symbols are their names, proper lists of
conses with symbol or string keys become objects, other lists and
vectors arrays, and anything else its printed representation."
  (let ((depth (or depth 0)))
    (cond
     ((> depth 20) (format "%S" value))
     ((eq value nil) :null)
     ((eq value t) t)
     ((memq value '(:null :false)) value)
     ((integerp value) (if (fixnump value) value (number-to-string value)))
     ((floatp value) (if (or (isnan value) (= value 1.0e+INF) (= value -1.0e+INF))
                         (format "%S" value) value))
     ((stringp value) (substring-no-properties value))
     ((symbolp value) (symbol-name value))
     ((and (consp value) (proper-list-p value)
           (seq-every-p (lambda (e) (and (consp e) (or (symbolp (car e)) (stringp (car e)))
                                         (car e) (not (eq (car e) t))))
                        value))
      (let ((h (make-hash-table :test 'equal)))
        (dolist (e value)
          (let ((k (if (symbolp (car e)) (symbol-name (car e)) (car e))))
            (unless (gethash k h)
              (puthash k (playground--jsonify (cdr e) (1+ depth)) h))))
        h))
     ((and (consp value) (proper-list-p value))
      (vconcat (mapcar (lambda (e) (playground--jsonify e (1+ depth))) value)))
     ((vectorp value) (vconcat (mapcar (lambda (e) (playground--jsonify e (1+ depth))) value)))
     ((hash-table-p value)
      (let ((h (make-hash-table :test 'equal)))
        (maphash (lambda (k v) (puthash (format "%s" k) (playground--jsonify v (1+ depth)) h)) value)
        h))
     (t (format "%S" value)))))

(defun playground--op-eval (args &rest _)
  "Evaluate the form in ARGS and return {value}."
  (let ((form (car (read-from-string (alist-get 'form args)))))
    `((value . ,(playground--in-user-window (playground--jsonify (eval form t)))))))

(defun playground--check-form (string)
  "Evaluate the form in STRING: t, :false or \"unknown\"."
  (condition-case nil
      (let ((form (car (read-from-string string))))
        (with-timeout (playground-agent-check-timeout "unknown")
          (if (playground--in-user-window (eval form t)) t :false)))
    (error "unknown")))

(defun playground--op-check (args &rest _)
  "Evaluate every form of ARGS: {results:[true|false|\"unknown\"]}.
Each form runs with the learner's window selected in the learner's
frame (`playground--in-user-window')."
  `((results . ,(vconcat (mapcar #'playground--check-form (alist-get 'forms args))))))

(defun playground--kmacro-changed (&rest _)
  "A keyboard macro started or stopped."
  (playground--schedule-state))

(provide 'playground-observe)
;;; playground-observe.el ends here
