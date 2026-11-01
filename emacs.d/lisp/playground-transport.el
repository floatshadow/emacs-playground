;;; playground-transport.el --- Socket, queue and calls of the playground agent  -*- lexical-binding: t; -*-

;;; Commentary:

;; The transport of the playground agent (emacs.md section 10.1, api.md
;; section 7): the Unix socket to sessiond, the queue kept while it is
;; away, pings and stall handling, and the dispatch of calls to the ops
;; in `playground-agent-ops', whose replies it sends.  It also holds the
;; small helpers every other part of the agent uses.

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)

(defvar playground-agent-socket)
(defvar playground-agent-workspace)
(defvar playground-agent-queue-limit)
(defvar playground-agent-ping-interval)
(defvar playground-agent-peer-timeout)
(defvar playground-agent-stall-seconds)
(defvar playground-agent-backoff-max)
(defvar playground-agent-debug)
(defvar playground-agent-version)
(defvar playground-agent-protocol)
(defvar playground-agent-ops)
(defvar playground-agent-mode)
(defvar playground-modules-loaded)
(defvar playground-module-errors)
(defvar playground--last-state)
(defvar playground--waiting)
(declare-function playground--emit-state "playground-observe" (&optional force))

(defvar playground--process nil "The socket process, or nil.")
(defvar playground--connected nil "Non-nil once the current socket is open.")
(defvar playground--queue nil "Serialized lines waiting for a connection, newest first.")
(defvar playground--queue-length 0)
(defvar playground--backoff 1 "Next reconnection delay in seconds.")
(defvar playground--reconnect-timer nil)
(defvar playground--ping-timer nil)
(defvar playground--last-pong nil "Time of the last pong, or nil if none on this connection.")
(defvar playground--partial "" "Bytes received after the last newline.")

(defvar playground-agent-rescue-unwind-function #'top-level
  "Function that leaves every minibuffer and recursive edit.
It must not return (it throws to the command loop).  Tests replace it.")

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

(defun playground--relative (file)
  "FILE relative to the workspace when inside it, else absolute."
  (let ((abs (expand-file-name file)))
    (if (file-in-directory-p abs playground-agent-workspace)
        (file-relative-name abs playground-agent-workspace)
      abs)))

(defun playground--absolute (path)
  "Absolute name of workspace-relative or absolute PATH."
  (expand-file-name path playground-agent-workspace))

(defun playground--kill-quietly (buffer)
  "Kill BUFFER without running `kill-buffer-query-functions'."
  (let ((kill-buffer-query-functions nil))
    (kill-buffer buffer)))

;;;; Transport

(defun playground--peer-silent-for ()
  "Seconds since the last pong, or nil when none arrived on this connection."
  (and playground--last-pong (- (float-time) playground--last-pong)))

(defun playground--drain ()
  "Process input already waiting on the socket (pongs, calls) without blocking.
An integer JUST-THIS-ONE runs no timers: a timer that throws (setup's
`top-level') would otherwise unwind the caller, the ping timer included."
  (when (process-live-p playground--process)
    (accept-process-output playground--process 0 nil 0)))

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
  "Send a ping; drop a connection whose peer stopped answering.
A quit or throw out of a repeating timer leaves it marked as
triggered, and Emacs never runs it again: no pings, no pongs, and
every reply would wait in the queue.  The unwind form clears the mark."
  (unwind-protect
      (when playground--connected
        ;; A pong may be waiting unread if Emacs was busy; read it first.
        (when (and playground--last-pong
                   (> (playground--peer-silent-for) playground-agent-ping-interval))
          (playground--drain))
        (if (and playground--last-pong
                 (> (playground--peer-silent-for) playground-agent-peer-timeout))
            (progn (playground--log "peer silent, reconnecting")
                   (playground--disconnected))
          (playground--send-line "{\"t\":\"ping\"}" 'ping)))
    (when (timerp playground--ping-timer)
      (setf (timer--triggered playground--ping-timer) nil))))

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

(defun playground--prefix-pending-p ()
  "Non-nil when the learner has typed a prefix key (C-c) and Emacs waits for more.
Ops run from the process filter while the command loop reads keys, so
the keys read so far are the pending prefix.  A loop reading keys of
its own (`playground-agent-waiting-loops') is not a prefix."
  (and (not playground--waiting) (> (length (this-single-command-keys)) 0)))

(defun playground--at-top-level (id fn)
  "Call FN at top level and reply to call ID with its value.
Run directly when no prompt, recursive edit or pending prefix key would
swallow the keys that follow; otherwise those need a throw to the
command loop, so FN runs from a timer there and the reply is sent from
it (`playground--deferred')."
  (if (and id (or (> (recursion-depth) 0) (playground--prefix-pending-p)))
      (progn
        (run-at-time 0 nil
                     (lambda ()
                       (condition-case err
                           (let ((inhibit-interaction t))
                             (playground--reply id t (funcall fn)))
                         (error (playground--reply id nil nil (error-message-string err))))))
        (funcall playground-agent-rescue-unwind-function)
        'playground--deferred)
    (funcall fn)))

(provide 'playground-transport)
;;; playground-transport.el ends here
