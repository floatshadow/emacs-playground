;;; playground-agent.el --- Report Emacs state to the playground  -*- lexical-binding: t; -*-

;; Version: 0.1.0
;; Package-Requires: ((emacs "30.1"))
;; Keywords: tools

;;; Commentary:

;; The playground agent is the Emacs half of the Emacs Playground
;; (docs/emacs.md section 10, docs/api.md section 7).  It is a global
;; minor mode that connects to sessiond over a Unix socket and
;;
;;   - reports what happened: one `key' event per command (the keys
;;     and the command they ran), a `state' snapshot of the selected
;;     window's buffer and the frame layout, echo-area messages,
;;     minibuffer and isearch prompts, frames opening and closing;
;;   - answers calls from sessiond: evaluate forms for lesson checks,
;;     rescue the learner from confusing states, reset a step, take and
;;     restore buffer snapshots, write the module list, open a GUI
;;     frame, describe a key.
;;
;; It reports; it does not change editing behaviour.  Everything it
;; runs from hooks is wrapped so that a bug here can never break a
;; command, and the socket is written asynchronously: when sessiond is
;; away, events are queued (at most `playground-agent-queue-limit') and
;; the agent reconnects with a backoff of 1 to 16 seconds.
;;
;; Wire format: one JSON object per line, UTF-8, produced with
;; `json-serialize'.  Lisp nil is ambiguous in JSON, so the builders
;; below always say what they mean: `:false' for false, `:null' for
;; null and vectors for arrays.
;;
;; Only built-in libraries are used, so the file can be loaded on its
;; own:  emacs --batch -l lisp/playground-agent.el -l test/agent-test.el

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)

(declare-function flymake-diagnostics "flymake" (&optional beg end))
(declare-function flymake-diagnostic-type "flymake" (diag))
(declare-function flymake-diagnostic-text "flymake" (diag))
(declare-function flymake-diagnostic-beg "flymake" (diag))
(declare-function flymake-diagnostic-backend "flymake" (diag))
(declare-function flymake--lookup-type-property "flymake" (type prop &optional default))
(declare-function tab-bar-tabs "tab-bar" (&optional frame))
(declare-function tab-bar--current-tab "tab-bar" (&optional tab frame))
(declare-function tab-bar-switch-to-tab "tab-bar" (name))
(declare-function isearch-done "isearch" (&optional nopush edit))
(declare-function isearch-message-prefix "isearch" (&optional ellipsis nonincremental))
(declare-function server-running-p "server" (&optional name))
(defvar isearch-string)
(defvar isearch-nonincremental)
(defvar isearch--current-buffer)
(defvar flymake-mode)
(defvar playground-modules)
(defvar playground-modules-loaded)
(defvar playground-module-errors)
(defvar playground-config-dir)
(defvar playground-agent-mode)
(declare-function project-root "project" (project))
(declare-function iedit-mode "iedit" (&optional arg))
(declare-function multiple-cursors-mode "multiple-cursors-core" (&optional arg))

;;;; Customisation

(defgroup playground-agent nil
  "Report Emacs state to the Emacs Playground."
  :group 'tools
  :prefix "playground-agent-")

(defconst playground-agent-version "0.1.0"
  "Version of playground-agent.el, reported in the hello message.")

(defconst playground-agent-protocol 1
  "Agent protocol version (api.md section 7).")

(defcustom playground-agent-socket
  (or (getenv "PG_AGENT_SOCKET") "/run/playground/agent.sock")
  "Unix socket sessiond listens on."
  :type 'file)

(defcustom playground-agent-workspace
  (file-name-as-directory (or (getenv "PG_WORKSPACE") "/work"))
  "Workspace root; paths in snapshots are relative to it."
  :type 'directory)

(defcustom playground-agent-queue-limit 1000
  "Events kept while disconnected; older ones are dropped."
  :type 'natnum)

(defcustom playground-agent-ping-interval 2
  "Seconds between pings."
  :type 'number)

(defcustom playground-agent-peer-timeout 10
  "Seconds without a pong after which sessiond is considered hung.
Only enforced once sessiond has answered at least one ping on the
current connection, so a peer that never sends pongs is tolerated."
  :type 'number)

(defcustom playground-agent-stall-seconds 5
  "Seconds without a pong after which events are queued, not sent.
`process-send-string' waits when the socket buffer is full, which
would freeze editing if sessiond stopped reading; queueing while the
peer is silent keeps the buffer from filling.  The queue is flushed
on the next pong, or the connection is dropped after
`playground-agent-peer-timeout'."
  :type 'number)

(defcustom playground-agent-backoff-max 16
  "Longest wait between reconnection attempts, in seconds."
  :type 'number)

(defcustom playground-agent-check-timeout 1.0
  "Seconds each form of a `check' call may run."
  :type 'number)

(defcustom playground-agent-interesting-minor-modes
  '(eglot--managed-mode flymake-mode overwrite-mode read-only-mode view-mode
    auto-revert-mode visual-line-mode auto-fill-mode follow-mode olivetti-mode
    iedit-mode multiple-cursors-mode rectangle-mark-mode corfu-mode
    diff-hl-mode apheleia-mode ws-butler-mode subword-mode superword-mode
    outline-minor-mode hs-minor-mode display-line-numbers-mode hl-line-mode
    whitespace-mode text-scale-mode buffer-face-mode tab-line-mode
    smerge-mode git-timemachine-mode magit-blame-mode wdired-mode
    dired-hide-details-mode compilation-shell-minor-mode
    compilation-minor-mode electric-pair-mode abbrev-mode
    eldoc-box-hover-mode eldoc-box-hover-at-point-mode
    breadcrumb-local-mode pulsar-mode)
  "Minor modes reported in the state's `minors' list when enabled."
  :type '(repeat symbol))

(defcustom playground-agent-echo-noise
  '("\\`\\'" "\\`Auto-saving" "\\`Saving file " "\\`Loading " "\\`Garbage collecting"
    "\\`Starting new Ispell" "\\`Reverting buffer" "\\`Indentation setup for shell type"
    "\\`Waiting for git" "\\`Running git" "\\`Refreshing magit"
    "\\`\\[eglot\\] Waiting" "\\`Fontifying" "\\`Wrote .*/\\.?#"
    ;; helpful and elisp-refs while they look up a definition.
    "\\`uncompressing .*\\.gz\\.\\.\\." "\\`Searched [0-9]+/[0-9]+ files"
    ;; Isearch redraws its prompt on every key; the minibuffer event and
    ;; the state's minibuffer field carry it.
    "\\`\\(?:Failing \\|Wrapped \\|Overwrapped \\|Pending \\)*\\(?:[[:alpha:] ]+ \\)?I-search")
  "Regexps for echo-area messages that are not reported.
\"Mark set\", \"Quit\" and \"Wrote ...\" are kept on purpose."
  :type '(repeat regexp))

(defcustom playground-agent-ignored-commands
  '(handle-switch-frame handle-focus-in handle-focus-out handle-select-window
    handle-delete-frame handle-move-frame)
  "Commands Emacs runs for internal events; no key event is sent for them."
  :type '(repeat symbol))

(defcustom playground-agent-ignored-keys
  "\\`<\\(?:mouse-movement\\|focus-in\\|focus-out\\|help-echo\\|select-window\\)>\\'"
  "Key descriptions that are input events, not keys the learner pressed.
A mouse drag runs a command for every movement; no key event is sent
for those (the state still follows the region)."
  :type 'regexp)

(defcustom playground-agent-debug nil
  "When non-nil, log agent traffic to the buffer *playground-agent*."
  :type 'boolean)

;;;; Internal state

(defvar playground--process nil "The socket process, or nil.")
(defvar playground--connected nil "Non-nil once the current socket is open.")
(defvar playground--queue nil "Serialized lines waiting for a connection, newest first.")
(defvar playground--queue-length 0)
(defvar playground--backoff 1 "Next reconnection delay in seconds.")
(defvar playground--reconnect-timer nil)
(defvar playground--ping-timer nil)
(defvar playground--last-pong nil "Time of the last pong, or nil if none on this connection.")
(defvar playground--partial "" "Bytes received after the last newline.")
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
(defvar playground--undefined-reported nil
  "Keys of an unbound sequence just reported from its echo message.")
(defvar playground--window-counter 0 "Last window id handed out.")
(defvar playground--suppress-echo nil "Non-nil while messages must not be reported.")
(defvar playground--in-hook nil "Guards against re-entering the agent from its own hooks.")
(defvar playground--pending-layout nil
  "(TIME . SPEC): a layout `open' could not apply yet, and when it was asked.
Right after a daemon restart sessiond reopens the buffers before any
client frame exists; the layout then goes to the first client frame.")

(defvar playground-agent-rescue-unwind-function #'top-level
  "Function that leaves every minibuffer and recursive edit.
It must not return (it throws to the command loop).  Tests replace it.")

(defvar playground-agent-ops nil
  "Alist of (OP-NAME . FUNCTION) for calls from sessiond.
Each FUNCTION takes the call's argument object (an alist) and returns
a JSON-ready value, or the symbol `playground--deferred' when it
sends the reply itself later.")

;;;; Logging and small helpers

(defun playground--log (fmt &rest args)
  "Log FMT with ARGS to *playground-agent* when debugging."
  (when playground-agent-debug
    (with-current-buffer (get-buffer-create "*playground-agent*")
      (let ((inhibit-read-only t))
        (goto-char (point-max))
        (insert (format-time-string "%T.%3N ") (apply #'format fmt args) "\n")))))

(defmacro playground--safely (where &rest body)
  "Run BODY; log and swallow any error, tagging it with WHERE."
  (declare (indent 1) (debug t))
  `(condition-case-unless-debug err
       (progn ,@body)
     (error (playground--log "error in %s: %S" ,where err) nil)))

(defsubst playground--bool (x) "JSON boolean for X." (if x t :false))
(defsubst playground--str (x) "String X, or JSON null." (if x (substring-no-properties x) :null))

(defun playground--now-ms ()
  "Milliseconds since the epoch."
  (truncate (* 1000 (float-time))))

(defun playground--json (object)
  "Serialize OBJECT to a JSON string."
  (json-serialize object))

(defun playground--symbol-name (cmd)
  "Name of command CMD as reported in key events."
  (cond ((null cmd) "undefined")
        ((symbolp cmd) (symbol-name cmd))
        ((or (stringp cmd) (vectorp cmd)) "<kbd-macro>")
        (t "<lambda>")))

(defun playground--frame-kind (&optional frame)
  "\"gui\" or \"terminal\" for FRAME."
  (if (display-graphic-p frame) "gui" "terminal"))

(defun playground--user-window ()
  "The window the learner is working in.
That is the selected window, or the window that was selected when
the minibuffer was entered."
  (let ((w (selected-window)))
    (if (window-minibuffer-p w)
        (let ((m (minibuffer-selected-window)))
          (if (and m (window-live-p m)) m (frame-first-window)))
      w)))

(defun playground--relative (file)
  "FILE relative to the workspace when inside it, else absolute."
  (let ((abs (expand-file-name file)))
    (if (file-in-directory-p abs playground-agent-workspace)
        (file-relative-name abs playground-agent-workspace)
      abs)))

(defun playground--absolute (path)
  "Absolute name of workspace-relative or absolute PATH."
  (expand-file-name path playground-agent-workspace))

;;;; Transport

(defun playground--peer-silent-for ()
  "Seconds since the last pong, or nil when none arrived on this connection."
  (and playground--last-pong (- (float-time) playground--last-pong)))

(defun playground--drain ()
  "Process input already waiting on the socket (pongs, calls) without blocking."
  (when (process-live-p playground--process)
    (accept-process-output playground--process 0 nil t)))

(defun playground--stalled-p ()
  "Non-nil when sessiond has stopped answering pings for a while.
Pongs that arrived while Emacs was busy are processed first, so a
long command in Emacs does not count as a silent peer."
  (let ((silent (playground--peer-silent-for)))
    (and silent (> silent playground-agent-stall-seconds)
         (progn (playground--drain)
                (let ((again (playground--peer-silent-for)))
                  (and again (> again playground-agent-stall-seconds)))))))

(defun playground--send-line (line &optional kind)
  "Send LINE (without newline) to sessiond.
KIND is `event' (queued while disconnected), `reply' (queued, never
dropped) or `ping' (never queued).  While the peer is stalled (see
`playground-agent-stall-seconds') events and replies are queued too."
  (if (and playground--connected (process-live-p playground--process)
           (or (eq kind 'ping) (not (playground--stalled-p))))
      (condition-case err
          (process-send-string playground--process (concat line "\n"))
        (error
         (playground--log "send failed: %S" err)
         (playground--disconnected)
         (playground--enqueue line kind)))
    (playground--enqueue line kind)))

(defun playground--enqueue (line kind)
  "Keep LINE of KIND for the next connection."
  (unless (eq kind 'ping)
    (push (cons kind line) playground--queue)
    (cl-incf playground--queue-length)
    (when (> playground--queue-length playground-agent-queue-limit)
      ;; Drop the oldest event; replies are kept.
      (let* ((rev (nreverse playground--queue))
             (victim (seq-find (lambda (e) (eq (car e) 'event)) rev)))
        (setq playground--queue (nreverse (if victim (delq victim rev) rev)))
        (when victim (cl-decf playground--queue-length))))))

(defun playground--flush-queue ()
  "Send everything queued while disconnected, oldest first."
  (let ((items (reverse playground--queue)))
    (setq playground--queue nil playground--queue-length 0)
    (dolist (item items)
      (playground--send-line (cdr item) (car item)))))

(defun playground--emit (object &optional kind)
  "Serialize OBJECT and send it as KIND (default `event')."
  (playground--send-line (playground--json object) (or kind 'event)))

(defun playground--connect ()
  "Open the socket to sessiond, or schedule another attempt."
  (setq playground--reconnect-timer nil)
  (when (and playground-agent-mode (not (process-live-p playground--process)))
    (condition-case err
        (let ((proc (make-network-process
                     :name "playground-agent"
                     :family 'local
                     :service playground-agent-socket
                     :coding 'utf-8-unix
                     :nowait t
                     :noquery t
                     :filter #'playground--filter
                     :sentinel #'playground--sentinel)))
          (setq playground--process proc
                playground--partial "")
          (pcase (process-status proc)
            ('open (playground--on-open))
            ;; A local connect that fails at once (no socket yet) leaves
            ;; the process `failed' without ever calling the sentinel.
            ((or 'failed 'closed)
             (delete-process proc)
             (setq playground--process nil)
             (playground--schedule-reconnect))))
      (error
       (playground--log "connect failed: %S" err)
       (setq playground--process nil)
       (playground--schedule-reconnect)))))

(defun playground--schedule-reconnect ()
  "Try to connect again after the current backoff delay."
  (when (and playground-agent-mode (not playground--reconnect-timer))
    (let ((delay playground--backoff))
      (setq playground--backoff (min playground-agent-backoff-max (* 2 playground--backoff)))
      (playground--log "reconnect in %ss" delay)
      (setq playground--reconnect-timer
            (run-with-timer delay nil #'playground--connect)))))

(defun playground--on-open ()
  "The socket is open: say hello, flush the queue, send the state."
  (unless playground--connected
    (setq playground--connected t
          playground--backoff 1
          playground--last-pong nil
          playground--last-state nil)
    (playground--log "connected to %s" playground-agent-socket)
    (playground--send-hello)
    (playground--flush-queue)
    (playground--safely "initial state" (playground--emit-state t))
    (unless playground--ping-timer
      (setq playground--ping-timer
            (run-with-timer playground-agent-ping-interval
                            playground-agent-ping-interval
                            #'playground--ping)))))

(defun playground--disconnected ()
  "Forget the current socket and plan a reconnection."
  (setq playground--connected nil)
  (when (process-live-p playground--process)
    (delete-process playground--process))
  (setq playground--process nil)
  (playground--schedule-reconnect))

(defun playground--sentinel (proc event)
  "Sentinel for PROC: react to EVENT."
  (when (eq proc playground--process)
    (playground--log "sentinel: %s" (string-trim event))
    (cond ((string-prefix-p "open" event) (playground--on-open))
          ((memq (process-status proc) '(closed failed exit signal))
           (setq playground--connected nil playground--process nil)
           (playground--schedule-reconnect)))))

(defun playground--ping ()
  "Send a ping; drop a connection whose peer stopped answering."
  (when playground--connected
    ;; A pong may be waiting unread if Emacs was busy; read it first.
    (when (and playground--last-pong
               (> (playground--peer-silent-for) playground-agent-ping-interval))
      (playground--drain))
    (if (and playground--last-pong
             (> (playground--peer-silent-for) playground-agent-peer-timeout))
        (progn (playground--log "peer silent, reconnecting")
               (playground--disconnected))
      (playground--send-line "{\"t\":\"ping\"}" 'ping))))

(defun playground--send-hello ()
  "Send the hello message."
  (let ((errors (bound-and-true-p playground-module-errors))
        (msg `((t . "hello")
               (protocol . ,playground-agent-protocol)
               (agent . ,playground-agent-version)
               (emacs . ,emacs-version)
               (pid . ,(emacs-pid)))))
    (when (boundp 'playground-modules-loaded)
      (setq msg (append msg `((modules . ,(vconcat (mapcar #'symbol-name playground-modules-loaded)))))))
    (when errors
      (setq msg (append msg `((moduleErrors . ,(mapcar (lambda (e) (cons (car e) (format "%s" (cdr e))))
                                                       errors))))))
    (playground--emit msg 'reply)))

(defun playground--filter (proc string)
  "Accumulate STRING from PROC into lines and dispatch them."
  (when (eq proc playground--process)
    (setq playground--partial (concat playground--partial string))
    (let (pos)
      (while (setq pos (string-search "\n" playground--partial))
        (let ((line (substring playground--partial 0 pos)))
          (setq playground--partial (substring playground--partial (1+ pos)))
          (unless (string-blank-p line)
            (playground--dispatch line)))))
    (when (> (length playground--partial) (* 1024 1024))
      (playground--log "line longer than 1 MB dropped")
      (setq playground--partial ""))))

(defun playground--dispatch (line)
  "Handle one LINE from sessiond."
  (let ((msg (condition-case nil
                 (json-parse-string line :object-type 'alist :array-type 'array
                                    :null-object nil :false-object nil)
               (error nil))))
    (pcase (alist-get 't msg)
      ("pong"
       (setq playground--last-pong (float-time))
       ;; Events queued while the peer was stalled go out now.
       (when (and playground--queue playground--connected)
         (playground--flush-queue)))
      ("call"
       ;; Run outside the process filter, at top level from a timer: ops
       ;; may throw to the command loop (setup, rescue).  Timers, like
       ;; filters, run with quitting inhibited: three C-g in a row (or
       ;; sessiond's SIGUSR2) are what stop a runaway eval.
       (run-at-time 0 nil #'playground--run-call msg))
      (_ (playground--log "ignored: %s" line)))))

(defun playground--reply (id ok &optional value error)
  "Reply to call ID."
  (playground--emit
   `((t . "reply") (id . ,id) (ok . ,(playground--bool ok))
     ,@(when ok `((value . ,(if (eq value nil) :null value))))
     ,@(unless ok `((error . ,(format "%s" (or error "error"))))))
   'reply))

(defun playground--run-call (msg)
  "Run the call MSG from sessiond and reply."
  (let* ((id (alist-get 'id msg))
         (op (alist-get 'op msg))
         (args (alist-get 'args msg))
         (timeout (/ (or (alist-get 'timeoutMs msg) 2000) 1000.0))
         (fn (cdr (assoc op playground-agent-ops))))
    (playground--log "call %s %s" id op)
    (if (not fn)
        (playground--reply id nil nil (format "unknown op %s" op))
      (condition-case err
          ;; Ops never ask the learner anything: a prompt would block the
          ;; agent (and every later call) until someone answers it, so
          ;; `inhibit-interaction' turns it into an error reply instead.
          (let ((value (with-timeout (timeout 'playground--timeout)
                         (let ((inhibit-interaction t))
                           (funcall fn args id)))))
            (pcase value
              ('playground--deferred nil)
              ('playground--timeout (playground--reply id nil nil "timeout"))
              (_ (playground--reply id t value))))
        (quit (playground--reply id nil nil "quit"))
        (error (playground--reply id nil nil (error-message-string err)))))))

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

(defun playground--minibuffer ()
  "The open minibuffer or isearch as an object, or :null."
  (let ((mw (active-minibuffer-window)))
    (cond
     (mw
      (with-current-buffer (window-buffer mw)
        (let ((contents (minibuffer-contents-no-properties)))
          `((depth . ,(minibuffer-depth))
            (prompt . ,(substring-no-properties (or (minibuffer-prompt) "")))
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
        ((derived-mode-p 'special-mode) "special-mode")
        ((derived-mode-p 'dired-mode) "dired-mode")
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
          (markActive . ,(playground--bool (and mark-active (eq win (playground--user-window))
                                                (region-active-p))))
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
          (readOnlyReason . ,(playground--read-only-reason))
          (diagAtPoint . ,(playground--diag-at-point pt)))))))

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

(defconst playground--prefix-arg-commands
  '(universal-argument universal-argument-more universal-argument-minus
    digit-argument negative-argument)
  "Commands that build a prefix argument.
They call `prefix-command-preserve-state', which sets `this-command'
back to the previous command, so they are recognised before they run.")

(defun playground--pre-command ()
  "Remember the keys of the command about to run, and the command."
  (setq playground--pre-keys (this-command-keys-vector)
        playground--pre-command this-command
        playground--pre-frame (playground--frame-kind)))

(defun playground--this-keys ()
  "Keys of the current command, in `key-description' form."
  (key-description (this-command-keys-vector)))

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
                      this-command))
               ;; Already reported when its prompt opened.  This also
               ;; covers the extra run of this hook that starts the
               ;; minibuffer's own command loop (depth one deeper, the
               ;; same `this-command', no keys).
               (early (seq-find (lambda (e) (eq (cdr e) cmd)) playground--early))
               (mx (and playground--mx-armed (>= playground--mx-armed depth)))
               ;; The keys saved before the command ran: commands that
               ;; read keys themselves (C-h k) change `this-command-keys'.
               (keys (cond (mx (playground--mx-keys playground--mx-armed))
                           ((> (length playground--pre-keys) 0)
                            (key-description playground--pre-keys))
                           (t (playground--this-keys))))
               ;; The frame the keys were typed in (C-x C-c deletes it).
               (frame (and (> (length playground--pre-keys) 0) playground--pre-frame)))
          (setq playground--pre-keys [] playground--pre-command nil playground--pre-frame nil)
          ;; Every command started at this depth or deeper has finished.
          (setq playground--early (seq-remove (lambda (e) (>= (car e) depth)) playground--early))
          (when mx (setq playground--mx-armed nil))
          (when (and cmd (eq cmd playground--exit-reported)) (setq early t))
          (when (and (memq cmd '(nil undefined)) (equal keys playground--undefined-reported))
            (setq early t))
          (setq playground--exit-reported nil
                playground--undefined-reported nil)
          (when (and playground--mx-reader (>= (car playground--mx-reader) depth))
            (setq playground--mx-reader nil))
          (unless (or early
                      (memq cmd '(execute-extended-command execute-extended-command-for-buffer))
                      (memq cmd playground-agent-ignored-commands)
                      (string-match-p playground-agent-ignored-keys keys)
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
               (cmd this-command))
          (cond
           ((memq cmd '(execute-extended-command execute-extended-command-for-buffer))
            ;; The M-x prompt itself: remember its keys, report the
            ;; command it runs instead.
            (setq playground--mx-reader (cons outer (playground--this-keys))))
           ((and cmd (not (assoc outer playground--early)))
            ;; Report the command now so the ticker shows C-x C-f while
            ;; its prompt is open, not after it has finished.
            (let ((mx (and playground--mx-armed (= playground--mx-armed outer))))
              (playground--emit (playground--key-event
                                 cmd
                                 (if mx (playground--mx-keys outer) (playground--this-keys))
                                 mx
                                 (window-buffer (playground--user-window))))
              (push (cons outer cmd) playground--early))))
          (playground--minibuffer-event t (substring-no-properties (or (minibuffer-prompt) ""))
                                        depth "prompt")
          (playground--schedule-state))))))

(defun playground--on-minibuffer-exit ()
  "A minibuffer closed."
  (unless playground--in-hook
    (let ((playground--in-hook t))
      (playground--safely "minibuffer-exit"
        ;; The command that leaves the prompt (RET runs exit-minibuffer,
        ;; C-g minibuffer-keyboard-quit) exits by a throw, so its own
        ;; `post-command-hook' run never happens; report it here, where
        ;; its keys are still known.
        (when this-command
          (let ((keys (playground--this-keys)))
            (unless (string-empty-p keys)
              (playground--emit (playground--key-event this-command keys nil (current-buffer)))
              (setq playground--exit-reported this-command))))
        (playground--minibuffer-event nil (substring-no-properties (or (minibuffer-prompt) ""))
                                      (minibuffer-depth) "prompt")
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

(defun playground--on-make-frame ()
  "An emacsclient frame opened (it is selected)."
  (playground--safely "make-frame"
    (playground--frame-event "opened" (selected-frame))
    (playground--emit-state t)
    ;; After the server has finished setting the frame up (its initial
    ;; buffer), restore a layout kept by `open' during a restart.
    (when playground--pending-layout
      (run-at-time 0 nil #'playground--apply-pending-layout (selected-frame)))))

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
  "FRAME is about to be deleted."
  (playground--safely "delete-frame"
    (when (playground--last-frame-on-terminal-p frame)
      (playground--frame-event "closed" frame))
    (playground--schedule-state)))

;;;; Echo area

(defun playground--noise-p (text)
  "Non-nil when TEXT should not be reported."
  (seq-some (lambda (re) (string-match-p re text)) playground-agent-echo-noise))

(defvar playground--last-echo nil "(TEXT . TIME) of the last echo sent.")

(defun playground--echo (text)
  "Report echo-area TEXT unless it is noise."
  (when (and (stringp text) (not playground--suppress-echo) playground-agent-mode)
    (let ((text (string-trim (substring-no-properties text))))
      (unless (or (playground--noise-p text)
                  ;; The same message redisplayed (in another frame's
                  ;; echo area, say) within a second is sent once.
                  (and (equal text (car playground--last-echo))
                       (< (- (float-time) (cdr playground--last-echo)) 1.0)))
        (setq playground--last-echo (cons text (float-time)))
        (playground--emit `((t . "echo")
                            (text . ,(if (> (length text) 500) (substring text 0 500) text))))
        ;; Emacs runs no command, hence no hooks, for an unbound key
        ;; sequence; its only trace is this message.
        (when (string-match "\\`\\(.+\\) is undefined\\'" text)
          (let ((keys (match-string 1 text)))
            (setq playground--undefined-reported keys)
            (playground--emit (playground--key-event nil keys nil))))))))

(defun playground--on-message (message &rest _)
  "Watch MESSAGE on its way to the echo area (`set-message-function')."
  (playground--safely "message" (playground--echo message))
  nil)

(defun playground--on-command-error (data context _caller)
  "Report the error DATA the command loop is about to display, with CONTEXT."
  (playground--safely "command-error"
    (playground--echo (concat (or context "") (error-message-string data)))))

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

(defmacro playground--in-user-window (&rest body)
  "Run BODY with the learner's window selected (not the minibuffer)."
  (declare (indent 0) (debug t))
  `(let ((w (playground--user-window)))
     (if (eq w (selected-window))
         (progn ,@body)
       (with-selected-window w ,@body))))

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
  "Evaluate every form of ARGS: {results:[true|false|\"unknown\"]}."
  `((results . ,(vconcat (mapcar #'playground--check-form (alist-get 'forms args))))))

;;;;; Rescue (emacs.md 10.3)

(defun playground--rescue-plan ()
  "Actions the first, unwinding phase of rescue will take."
  (let (did)
    (when (> (minibuffer-depth) 0) (push "cancelled the prompt" did))
    (when (> (- (recursion-depth) (minibuffer-depth)) 0) (push "left the recursive edit" did))
    did))

(defun playground--op-rescue (_args &optional id)
  "Get the learner out of any confusing state; reply {did:[...]}.
Leaving minibuffers and recursive edits needs a throw to the command
loop, so in that case the rest of the work runs from a timer at top
level and the reply is sent from there."
  (let ((did (playground--rescue-plan)))
    (if (null did)
        `((did . ,(vconcat (playground--rescue-rest nil))))
      ;; Phase 2 after unwinding.
      (run-at-time 0 nil (lambda ()
                           (let ((all (playground--rescue-rest (reverse did))))
                             (when id (playground--reply id t `((did . ,(vconcat all))))))))
      (funcall playground-agent-rescue-unwind-function)
      'playground--deferred)))

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
    (let ((playground--suppress-echo nil))
      (message "%s" (if did
                        (concat "Rescue: " (string-join did ", ") ".")
                      "Rescue: nothing to undo; you were already in a normal state.")))
    did))

;;;;; Snapshot, open, reset (emacs.md 7, 10.3)

(defun playground--leaf-name (buffer)
  "Name used for BUFFER in layout descriptions."
  (let ((name (if (buffer-file-name buffer)
                  (playground--relative (buffer-file-name buffer))
                (buffer-name buffer))))
    (if (string-match-p "[ ()\"]" name) (format "%S" name) name)))

(defun playground--layout-string (node)
  "Layout pattern for NODE of a pruned window tree (`playground--tree')."
  (if (windowp node)
      (playground--leaf-name (window-buffer node))
    (format "(%s %s)" (if (car node) "vsplit" "hsplit")
            (mapconcat #'playground--layout-string (cdr node) " "))))

(defun playground--op-snapshot (&rest _)
  "Buffer and window spec of the selected frame (protocol.WindowSpec)."
  (let* ((frame (window-frame (playground--user-window)))
         (wins (window-list frame 'nomini (frame-first-window frame)))
         (files '()))
    (dolist (b (buffer-list frame))
      (when-let* ((f (buffer-file-name b)))
        (let ((w (seq-find (lambda (w) (eq (window-buffer w) b)) wins)))
          (push `((path . ,(playground--relative f))
                  (point . ,(if w (window-point w) (with-current-buffer b (point))))
                  (visible . ,(playground--bool w)))
                files))))
    `((files . ,(vconcat (nreverse files)))
      (layout . ,(playground--layout-string (playground--tree frame)))
      (windowState . ,(with-selected-frame frame
                        (prin1-to-string (window-state-get (frame-root-window frame) t))))
      (selected . ,(playground--leaf-name (window-buffer (playground--user-window))))
      (tab . ,(alist-get 'current (playground--tabs frame)))
      (takenAt . ,(playground--now-ms)))))

(defun playground--find-buffer (name &optional create)
  "Buffer for layout leaf NAME: a buffer name, a workspace path or a file name.
With CREATE, visit the file (or create the special buffer) when needed."
  (or (get-buffer name)
      (let ((path (playground--absolute name)))
        (or (find-buffer-visiting path)
            (and create (file-exists-p path) (find-file-noselect path))))
      (seq-find (lambda (b)
                  (or (equal (file-name-nondirectory (or (buffer-file-name b) "")) name)
                      (equal (replace-regexp-in-string "<[^>]*>\\'" "" (buffer-name b)) name)))
                (buffer-list))
      (and create
           (if (string-prefix-p "*" name)
               (get-buffer-create name)
             (let ((path (playground--absolute name)))
               (when (file-directory-p (file-name-directory path))
                 (find-file-noselect path)))))))

(defun playground--build-layout (node window)
  "Arrange WINDOW according to layout NODE (read from a pattern)."
  (cond
   ((memq node '(* single nil)) nil)
   ((or (symbolp node) (stringp node))
    (when-let* ((b (playground--find-buffer (format "%s" node) t)))
      (set-window-buffer window b)))
   ((and (consp node) (memq (car node) '(vsplit hsplit)))
    (let ((children (cdr node))
          (wins (list window))
          (w window))
      (dotimes (_ (1- (length children)))
        (setq w (split-window w nil (if (eq (car node) 'vsplit) 'below 'right)))
        (push w wins))
      (cl-mapc #'playground--build-layout children (nreverse wins))))
   (t (error "Bad layout node %S" node))))

(defun playground--apply-layout (layout)
  "Apply LAYOUT, a pattern string, to the selected frame."
  (let ((node (car (read-from-string layout)))
        (ignore-window-parameters t))
    (playground--in-user-window
      (playground--leave-side-window)
      (delete-other-windows)
      (playground--unside-sole-window)
      (playground--build-layout node (selected-window))
      (balance-windows))))

(defun playground--apply-window-state (state)
  "Apply STATE, a printed `window-state-get' value."
  (let ((ignore-window-parameters t))
    (playground--in-user-window
      (playground--leave-side-window)
      (delete-other-windows)
      (playground--unside-sole-window)
      (window-state-put (car (read-from-string state)) (frame-root-window) 'safe))))

(defun playground--select-leaf (name)
  "Select the window showing NAME, if any."
  (when-let* ((b (playground--find-buffer name))
              (w (get-buffer-window b)))
    (select-window w)))

(defconst playground--pending-layout-ttl 120
  "Seconds a pending layout waits for a frame before it is forgotten.")

(defun playground--frame-ready-p ()
  "Non-nil when the selected frame is one the learner can see.
A daemon's initial frame is not, and right after a restart it is the
only frame there is."
  (not (and (daemonp) (frame-live-p terminal-frame)
            (eq (selected-frame) terminal-frame))))

(defun playground--open-layout (spec)
  "Apply the window part of SPEC (a WindowSpec alist) to the selected frame."
  (let ((state (alist-get 'windowState spec))
        (layout (alist-get 'layout spec)))
    (cond
     ((and state (not (string-empty-p state))
           (condition-case err (progn (playground--apply-window-state state) t)
             (error (playground--log "window-state-put failed: %S" err) nil))))
     ((and layout (not (string-empty-p layout)))
      (playground--apply-layout layout))))
  (when-let* ((sel (alist-get 'selected spec)))
    (playground--select-leaf sel))
  (when-let* ((tab (alist-get 'tab spec)))
    (when (and (featurep 'tab-bar) (bound-and-true-p tab-bar-mode))
      (ignore-errors (tab-bar-switch-to-tab tab)))))

(defun playground--apply-pending-layout (frame)
  "Give FRAME the layout `open' kept for the first client frame, if any."
  (when-let* ((pending playground--pending-layout))
    (setq playground--pending-layout nil)
    (when (and (frame-live-p frame)
               (< (- (float-time) (car pending)) playground--pending-layout-ttl))
      (with-selected-frame frame
        (playground--safely "pending layout"
          (playground--open-layout (cdr pending))))
      (playground--schedule-state))))

(defun playground--op-open (args &rest _)
  "Visit the files of a WindowSpec ARGS and apply its layout.
Buffers and points are restored at once.  When no frame the learner
sees exists yet (a daemon that has just restarted), the layout is kept
and applied to the next client frame."
  (let ((opened 0))
    (seq-doseq (f (or (alist-get 'files args) []))
      (let ((path (playground--absolute (alist-get 'path f))))
        (when (file-exists-p path)
          (with-current-buffer (find-file-noselect path)
            (cl-incf opened)
            (when-let* ((p (alist-get 'point f)))
              (goto-char (max (point-min) (min p (point-max)))))))))
    (if (playground--frame-ready-p)
        (progn (setq playground--pending-layout nil)
               (playground--open-layout args)
               (playground--schedule-state)
               `((opened . ,opened)))
      (setq playground--pending-layout (cons (float-time) args))
      `((opened . ,opened) (layoutDeferred . t)))))

(defun playground--op-reset-step (args &rest _)
  "Revert or kill file buffers, then open the spec in ARGS."
  (let ((reverted 0) (killed 0))
    (dolist (b (buffer-list))
      (let ((file (buffer-file-name b)))
        (cond
         ((null file)
          (with-current-buffer b
            (when (and (derived-mode-p 'dired-mode) (stringp default-directory)
                       (not (file-directory-p default-directory)))
              (kill-buffer b)
              (cl-incf killed))))
         ((file-exists-p file)
          (with-current-buffer b
            (when (or (buffer-modified-p) (not (verify-visited-file-modtime b)))
              (revert-buffer t t t)
              (cl-incf reverted))))
         (t
          (with-current-buffer b (set-buffer-modified-p nil))
          (let ((kill-buffer-query-functions nil))
            (kill-buffer b))
          (cl-incf killed)))))
    (let ((spec (alist-get 'spec args)))
      (when spec (playground--op-open spec)))
    `((reverted . ,reverted) (killed . ,killed))))

(defun playground--sync-changed-buffers (root)
  "Revert buffers under ROOT whose file changed on disk; kill orphans.
sessiond extracts a scenario's fixtures before `setup', so a buffer
left over from an earlier lesson may visit a file that was just
rewritten or removed.  Visiting it again would make
`find-file-noselect' ask whether to reread it and block the
agent, and a modified buffer would later be saved over the fresh
fixture.  Returns the number of buffers reverted or killed."
  (let ((root (file-name-as-directory (expand-file-name root)))
        (n 0))
    (dolist (b (buffer-list))
      (let ((file (buffer-file-name b)))
        (when (and file (string-prefix-p root (expand-file-name file)))
          (cond
           ((not (file-exists-p file))
            (with-current-buffer b (set-buffer-modified-p nil))
            (let ((kill-buffer-query-functions nil))
              (kill-buffer b))
            (cl-incf n))
           ((not (verify-visited-file-modtime b))
            (with-current-buffer b
              (let ((revert-without-query '(".")))
                (revert-buffer t t t)))
            (cl-incf n))))))
    n))

(defun playground--op-setup (args &optional id)
  "Run a scenario's setup: open, layout, eval, compile (ARGS).
A prompt or recursive edit still open (the learner switched lessons
in the middle of a command) would swallow the lesson's first keys, so
it is left first.  That needs a throw to the command loop, so, as in
rescue, the setup then runs from a timer at top level and the reply
to call ID is sent from there."
  (when (bound-and-true-p isearch-mode)
    (playground--safely "setup isearch" (isearch-done)))
  (if (and id (> (recursion-depth) 0))
      (progn
        (run-at-time 0 nil
                     (lambda ()
                       (condition-case err
                           (let ((inhibit-interaction t))
                             (playground--reply id t (playground--setup-now args)))
                         (error (playground--reply id nil nil (error-message-string err))))))
        (funcall playground-agent-rescue-unwind-function)
        'playground--deferred)
    (playground--setup-now args)))

(defconst playground--list-modes
  '(ibuffer-mode Buffer-menu-mode bookmark-bmenu-mode dired-mode)
  "Modes of buffers that list things and keep marks; rebuilt on demand.")

(defun playground--clear-desk (root keep)
  "Kill unmodified buffers a fresh lesson under ROOT could trip over.
KEEP lists the absolute file names the lesson opens.  Killed: buffers
visiting files under ROOT that are not in KEEP (so names such as
main.rs are unique again), Dired and buffer lists (their marks would
carry over: ibuffer's * h D after % f in another lesson), and empty
buffers in Fundamental mode with ordinary names (made by C-x b with a
name that did not exist).  Modified buffers, *scratch*, *Messages*,
help and build output are kept.  Returns the number killed."
  (let ((root (file-name-as-directory (expand-file-name root)))
        (n 0))
    (dolist (b (buffer-list))
      (when (and (buffer-live-p b) (not (buffer-modified-p b)))
        (let ((file (buffer-file-name b))
              (name (buffer-name b))
              (mode (buffer-local-value 'major-mode b))
              (dir (buffer-local-value 'default-directory b)))
          (when (cond
                 (file (and (string-prefix-p root (expand-file-name file))
                            (not (member (expand-file-name file) keep))))
                 ((memq mode playground--list-modes)
                  (or (not (eq mode 'dired-mode))
                      (and dir (string-prefix-p root (expand-file-name dir)))))
                 ((eq mode 'fundamental-mode)
                  (and (not (string-match-p "\\`[ *]" name))
                       (zerop (buffer-size b)))))
            (let ((kill-buffer-query-functions nil))
              (kill-buffer b))
            (cl-incf n)))))
    n))

(defvar save-place-alist)
(defvar save-place-loaded)

(defun playground--forget-places (root)
  "Forget the cursor positions `save-place-mode' keeps for files under ROOT.
ROOT has just been restored to a lesson's fixtures, so a position
saved during an earlier attempt points into text that may no longer
be there, and replaying the lesson would open its files where the
last attempt left them, with a step's \"the cursor is on ...\"
already true.  Killing the leftover buffers (`playground--clear-desk')
saves their positions too, so this runs after it.  Returns the number
of entries removed."
  (if (not (and (boundp 'save-place-alist) (stringp root) (not (string-empty-p root))))
      0
    ;; The alist is read from `save-place-file' lazily, on the first visit,
    ;; and reading it replaces the variable: read it first.
    (when (and (boundp 'save-place-loaded) (not save-place-loaded))
      (cond ((fboundp 'save-place-load-alist-from-file) (funcall 'save-place-load-alist-from-file))
            ((fboundp 'load-save-place-alist-from-file) (funcall 'load-save-place-alist-from-file))))
    (let* ((root (file-name-as-directory (expand-file-name root)))
           (before (length save-place-alist)))
      (setq save-place-alist
            (seq-remove (lambda (entry)
                          (and (consp entry) (stringp (car entry))
                               (string-prefix-p root (expand-file-name (car entry)))))
                        save-place-alist))
      (- before (length save-place-alist)))))

(defun playground--restore-auto-save ()
  "Turn `auto-save-visited-mode' back on when the base module turns it on.
Lessons about the modified flag (0.1, 2.3, 2.4, 2.5) turn it off in
their setup's eval.  A setup with reset turns it on again first, so
that switch never outlives the lesson that made it: the next lesson,
or the same lesson's eval, decides again.  Returns non-nil when it
turned the mode on."
  (when (and (fboundp 'playground-module-p)
             (funcall 'playground-module-p 'base)
             (fboundp 'auto-save-visited-mode)
             (not (bound-and-true-p auto-save-visited-mode)))
    (auto-save-visited-mode 1)
    t))

(defun playground--setup-now (args)
  "The work of `playground--op-setup' (ARGS), at top level."
  (let* ((playground-agent-workspace
          (file-name-as-directory (or (alist-get 'workspace args) playground-agent-workspace)))
         (open (append (alist-get 'open args) nil))
         (layout (alist-get 'layout args))
         (form (alist-get 'eval args))
         (compile-cmd (alist-get 'compile args))
         (first nil))
    (playground--sync-changed-buffers playground-agent-workspace)
    (when (eq (alist-get 'reset args) t)
      (playground--restore-auto-save)
      (playground--in-user-window
        ;; Tabs from an earlier lesson (3.7, project tabs) are not part of
        ;; this lesson's layout.
        (when (and (bound-and-true-p tab-bar-mode)
                   (> (length (funcall tab-bar-tabs-function)) 1))
          (playground--safely "setup tabs" (tab-bar-close-other-tabs)))
        (playground--clear-desk playground-agent-workspace
                                (mapcar #'playground--absolute open)))
      (let ((root (alist-get 'root args)))
        (when (and (stringp root) (not (string-empty-p root)))
          (playground--forget-places (playground--absolute root)))))
    (dolist (path open)
      (let ((b (find-file-noselect (playground--absolute path))))
        (unless first (setq first b))))
    (playground--in-user-window
      (let ((ignore-window-parameters t))
        (cond
         ((and layout (string-suffix-p ".eld" layout))
          (let ((file (playground--absolute layout)))
            (if (file-exists-p file)
                (playground--apply-window-state
                 (with-temp-buffer (insert-file-contents file) (buffer-string)))
              (error "Layout file %s not found" layout))))
         ((and layout (not (string-empty-p layout)) (not (equal layout "single")))
          (when (and compile-cmd (string-match-p "\\*compilation\\*" layout))
            (with-current-buffer (get-buffer-create "*compilation*")
              (unless (derived-mode-p 'compilation-mode) (compilation-mode))))
          (playground--apply-layout layout))
         (first
          (playground--leave-side-window)
          (delete-other-windows)
          (playground--unside-sole-window)
          (switch-to-buffer first)))))
    (when (and form (not (string-empty-p form)))
      (playground--in-user-window (eval (car (read-from-string form)) t)))
    (when (and compile-cmd (not (string-empty-p compile-cmd)))
      (let ((default-directory
             (or (and first (buffer-local-value 'default-directory first))
                 playground-agent-workspace)))
        (when-let* ((proj (and (fboundp 'project-current) (project-current nil))))
          (setq default-directory (project-root proj)))
        (save-selected-window (compile compile-cmd))))
    (playground--schedule-state)
    '((ok . t))))

(defun playground--op-set-modules (args &rest _)
  "Write modules.el with the module names in ARGS."
  (let* ((names (mapcar (lambda (s)
                          (unless (and (stringp s) (string-match-p "\\`[a-z0-9-]+\\'" s))
                            (error "Bad module name %S" s))
                          (intern s))
                        (alist-get 'enabled args)))
         (file (expand-file-name "modules.el" (if (boundp 'playground-config-dir)
                                                   playground-config-dir
                                                 user-emacs-directory)))
         (tmp (concat file ".tmp")))
    (with-temp-file tmp
      (insert ";;; modules.el --- configuration modules to load  -*- lexical-binding: t; -*-\n"
              ";; Written by the playground when a lesson changes the module set\n"
              ";; (emacs.md section 4).  init.el loads the modules in this order.\n\n"
              (format "(setq playground-modules '%S)\n" names)))
    (rename-file tmp file t)
    `((written . ,(abbreviate-file-name file)))))

(defun playground--op-frame (args &rest _)
  "Open a GUI frame on the display in ARGS."
  (let* ((display (or (alist-get 'display args) (getenv "PG_DISPLAY") ":1"))
         (frame (make-frame-on-display display '((fullscreen . fullboth)))))
    (playground--frame-event "opened" frame)
    `((frame . ,(frame-parameter frame 'name)))))

(defun playground--first-doc-line (cmd)
  "First line of the documentation of CMD."
  (let ((doc (ignore-errors (documentation cmd t))))
    (if doc (car (split-string doc "\n")) "")))

(defun playground--op-describe (args &rest _)
  "Describe the key sequence (or command name) in ARGS: {cmd, doc}."
  (playground--in-user-window
    (let ((keys (alist-get 'keys args))
          (cmd-name (alist-get 'cmd args)))
      (if (and cmd-name (not keys))
          (let ((sym (intern-soft cmd-name)))
            `((cmd . ,cmd-name)
              (doc . ,(if (and sym (fboundp sym)) (playground--first-doc-line sym) ""))))
        (let ((binding (key-binding (key-parse keys) t)))
          (cond
           ((null binding) '((cmd . "") (doc . "")))
           ((keymapp binding) `((cmd . "") (doc . ,(format "%s is a prefix key" keys))))
           ((symbolp binding)
            `((cmd . ,(symbol-name binding)) (doc . ,(playground--first-doc-line binding))))
           (t `((cmd . ,(playground--symbol-name binding)) (doc . "")))))))))

(setq playground-agent-ops
      `(("eval" . playground--op-eval)
        ("check" . playground--op-check)
        ("rescue" . playground--op-rescue)
        ("reset-step" . playground--op-reset-step)
        ("snapshot" . playground--op-snapshot)
        ("set-modules" . playground--op-set-modules)
        ("open" . playground--op-open)
        ("frame" . playground--op-frame)
        ("describe" . playground--op-describe)
        ("setup" . playground--op-setup)))

;;;; Interactive commands (emacs.md 10.3)

(defun playground-rescue ()
  "Leave prompts, searches, macros, narrowing and extra windows.
Never saves, kills file buffers or reverts."
  (interactive)
  (playground--op-rescue nil nil))

(defun playground-check (forms)
  "Evaluate FORMS (strings) like a lesson check: t, nil or \"unknown\"."
  (mapcar (lambda (f) (let ((r (playground--check-form f))) (if (eq r :false) nil r))) forms))

(defun playground-snapshot ()
  "Return the buffer and window spec of the selected frame."
  (playground--op-snapshot))

(defun playground-open (spec)
  "Visit the files of SPEC and apply its layout."
  (playground--op-open spec))

(defun playground-reset-step (spec)
  "Revert file buffers to disk and apply SPEC."
  (playground--op-reset-step `((spec . ,spec))))

(defun playground-set-modules (list)
  "Write modules.el with LIST (strings); sessiond restarts the daemon."
  (playground--op-set-modules `((enabled . ,(vconcat list)))))

(defun playground-describe (keys)
  "Command bound to KEYS and the first line of its documentation."
  (interactive "sKeys: ")
  (let ((r (playground--op-describe `((keys . ,keys)))))
    (when (called-interactively-p 'any)
      (message "%s: %s" (alist-get 'cmd r) (alist-get 'doc r)))
    r))

(defun playground-gui-frame (&optional display)
  "Open a full-screen GUI frame on DISPLAY (default $PG_DISPLAY or :1)."
  (interactive)
  (playground--op-frame `((display . ,display))))

;;;; The minor mode

(defun playground--kmacro-changed (&rest _)
  "A keyboard macro started or stopped."
  (playground--schedule-state))

(defun playground--install (on)
  "Add the agent's hooks and advice when ON, else remove them."
  (let ((hook-fn (if on #'add-hook #'remove-hook)))
    (funcall hook-fn 'pre-command-hook #'playground--pre-command)
    (funcall hook-fn 'post-command-hook #'playground--post-command 90)
    ;; Early, so the command's key event goes out before other setup
    ;; functions can trigger redisplay, timers and a state.
    (funcall hook-fn 'minibuffer-setup-hook #'playground--on-minibuffer-setup -90)
    (funcall hook-fn 'minibuffer-exit-hook #'playground--on-minibuffer-exit)
    (funcall hook-fn 'isearch-mode-hook #'playground--on-isearch-start)
    (funcall hook-fn 'isearch-mode-end-hook #'playground--on-isearch-end)
    (funcall hook-fn 'window-configuration-change-hook #'playground--schedule-state)
    (funcall hook-fn 'window-selection-change-functions #'playground--schedule-state)
    (funcall hook-fn 'window-buffer-change-functions #'playground--schedule-state)
    (funcall hook-fn 'after-change-major-mode-hook #'playground--schedule-state)
    (funcall hook-fn 'kbd-macro-termination-hook #'playground--kmacro-changed)
    (funcall hook-fn 'server-after-make-frame-hook #'playground--on-make-frame)
    (funcall hook-fn 'delete-frame-functions #'playground--on-delete-frame))
  (if on
      (progn
        (advice-add 'execute-extended-command :around #'playground--mx-advice)
        (advice-add 'execute-extended-command-for-buffer :around #'playground--mx-advice)
        (advice-add 'kmacro-start-macro :after #'playground--kmacro-changed)
        (advice-add 'kmacro-end-macro :after #'playground--kmacro-changed)
        (advice-add 'eglot--connect :before #'playground--eglot-connecting)
        (advice-add 'eldoc-display-in-echo-area :around #'playground--eldoc-quietly)
        ;; Eldoc also re-shows its last message before each command.
        (advice-add 'eldoc-pre-command-refresh-echo-area :around #'playground--eldoc-quietly)
        (unless set-message-function (setq set-message-function #'ignore))
        (add-function :before set-message-function #'playground--on-message)
        (add-function :before command-error-function #'playground--on-command-error))
    (advice-remove 'execute-extended-command #'playground--mx-advice)
    (advice-remove 'execute-extended-command-for-buffer #'playground--mx-advice)
    (advice-remove 'kmacro-start-macro #'playground--kmacro-changed)
    (advice-remove 'kmacro-end-macro #'playground--kmacro-changed)
    (advice-remove 'eglot--connect #'playground--eglot-connecting)
    (advice-remove 'eldoc-display-in-echo-area #'playground--eldoc-quietly)
    (advice-remove 'eldoc-pre-command-refresh-echo-area #'playground--eldoc-quietly)
    (remove-function set-message-function #'playground--on-message)
    (remove-function command-error-function #'playground--on-command-error)))

(defun playground--stop ()
  "Close the socket and cancel timers."
  (dolist (tm (list playground--reconnect-timer playground--ping-timer playground--state-timer))
    (when (timerp tm) (cancel-timer tm)))
  (setq playground--reconnect-timer nil playground--ping-timer nil playground--state-timer nil
        playground--connected nil playground--backoff 1)
  (when (process-live-p playground--process)
    (delete-process playground--process))
  (setq playground--process nil))

;;;###autoload
(define-minor-mode playground-agent-mode
  "Report commands and state to the Emacs Playground over a Unix socket."
  :global t
  :group 'playground-agent
  (playground--install playground-agent-mode)
  (if playground-agent-mode
      (playground--connect)
    (playground--stop)))

(provide 'playground-agent)
;;; playground-agent.el ends here
