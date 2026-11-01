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
;;     rescue the learner from confusing states, capture and restore
;;     the editor state a checkpoint keeps, set up a lesson, write the
;;     module list, open or let go of a GUI frame, describe a key.
;;
;; It reports; it does not change editing behaviour.  Everything it
;; runs from hooks is wrapped so that a bug here can never break a
;; command, and the socket is written asynchronously: when sessiond is
;; away, events are queued (at most `playground-agent-queue-limit') and
;; the agent reconnects with a backoff of 1 to 16 seconds.
;;
;; Wire format: one JSON object per line, UTF-8, produced with
;; `json-serialize'.  Lisp nil is ambiguous in JSON, so the builders
;; always say what they mean: `:false' for false, `:null' for null and
;; vectors for arrays.
;;
;; This file holds the options, the op table and the minor mode; the
;; features live in their own files, each loaded from here:
;;
;;   playground-transport     socket, queue, pings, calls and replies
;;   playground-frames        the learner's frame and window, frame ops
;;   playground-observe       key, state, echo and prompt reports; eval, check
;;   playground-rescue        Rescue
;;   playground-editor-state  window specs, capture-editor, restore-editor
;;   playground-setup         lesson setup, set-modules, visit, describe
;;   playground-compat        what the agent knows about packages
;;
;; Only built-in libraries are used, so the agent loads on its own:
;;   emacs --batch -L lisp -l playground-agent -l test/agent-test.el

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)

;;;; Customisation

(defgroup playground-agent nil
  "Report Emacs state to the Emacs Playground."
  :group 'tools
  :prefix "playground-agent-")

(defconst playground-agent-version "0.1.0"
  "Version of playground-agent.el, reported in the hello message.")

(defconst playground-agent-protocol 2
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
    diff-hl-mode apheleia-mode subword-mode superword-mode
    outline-minor-mode hs-minor-mode display-line-numbers-mode hl-line-mode
    whitespace-mode text-scale-mode buffer-face-mode tab-line-mode
    smerge-mode git-timemachine-mode magit-blame-mode wdired-mode
    dired-hide-details-mode compilation-shell-minor-mode
    compilation-minor-mode electric-pair-mode abbrev-mode
    eldoc-box-hover-mode eldoc-box-hover-at-point-mode
    breadcrumb-local-mode pulsar-mode org-capture-mode org-src-mode
    with-editor-mode edebug-mode)
  "Minor modes reported in the state's `minors' list when enabled.
Housekeeping modes that never ask anything of the learner, such as
ws-butler (which trims the spaces you added), are left out."
  :type '(repeat symbol))

(defcustom playground-agent-echo-noise
  '("\\`\\'" "\\`Auto-saving" "\\`Saving file " "\\`Loading " "\\`Garbage collecting"
    "\\`Starting new Ispell" "\\`Reverting buffer" "\\`Indentation setup for shell type"
    "\\`Waiting for git" "\\`Running git" "\\`Refreshing magit"
    "\\`\\[eglot\\] Waiting" "\\`Fontifying" "\\`Wrote .*/\\.?#"
    ;; helpful and elisp-refs while they look up a definition.
    "\\`uncompressing .*\\.gz\\.\\.\\." "\\`Searched [0-9]+/[0-9]+ files"
    ;; Isearch redraws its prompt on every key; the minibuffer event and
    ;; the state's minibuffer field carry it.  The prompt starts with
    ;; the match count when `isearch-lazy-count' is on ("1/4 ").
    "\\`\\(?:[0-9?]+/[0-9?+]+ \\)?\\(?:Failing \\|Wrapped \\|Overwrapped \\|Pending \\)*\\(?:[[:alpha:] ]+ \\)?I-search")
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

(defcustom playground-agent-waiting-loops
  '((popup-menu "menu" "a menu" "closed the menu" "C-g")
    (avy--read-candidates "avy" "avy, for the characters to jump to" "left avy" "C-g")
    (avy-read "avy" "avy, for the letters of the target" "left avy" "C-g")
    ;; C-g there only goes back to the prefix; a aborts.
    (which-key-C-h-dispatch "which-key" "which-key, for a key that turns its pages"
                            "left which-key's pages" "a"))
  "Functions that read keys in a loop of their own.
Each entry is (FUNCTION KIND WHAT DID LEAVE).  A menu-bar menu in a
terminal frame (F10, or a click on the menu bar) runs in `popup-menu'.
Such a loop is neither a prompt nor a recursive edit, and every key
goes to it.  While one runs, the state's `waiting' names it (KIND, and
WHAT is waiting) and the keys that LEAVE it; Rescue leaves it with
those keys, saying DID."
  :type '(repeat (list function string string string string)))

(defcustom playground-agent-debug nil
  "When non-nil, log agent traffic to the buffer *playground-agent*."
  :type 'boolean)

(defvar playground-agent-ops nil
  "Alist of (OP-NAME . FUNCTION) for calls from sessiond.
Each FUNCTION takes the call's argument object (an alist) and returns
a JSON-ready value, or the symbol `playground--deferred' when it
sends the reply itself later.")

;;;; Features

(require 'playground-transport)
(require 'playground-frames)
(require 'playground-compat)
(require 'playground-observe)
(require 'playground-rescue)
(require 'playground-editor-state)
(require 'playground-setup)

;;;; Ops (api.md 7, emacs.md 10.3)

(setq playground-agent-ops
      `(("eval" . playground--op-eval)
        ("check" . playground--op-check)
        ("rescue" . playground--op-rescue)
        ("capture-editor" . playground--op-capture-editor)
        ("restore-editor" . playground--op-restore-editor)
        ("modified-files" . playground--op-modified-files)
        ("flush-buffers" . playground--op-flush-buffers)
        ("open" . playground--op-open)
        ("setup" . playground--op-setup)
        ("set-modules" . playground--op-set-modules)
        ("visit" . playground--op-visit)
        ("describe" . playground--op-describe)
        ("frame" . playground--op-frame)
        ("leave-display" . playground--op-leave-display)
        ("resume-loop" . playground--op-resume-loop)
        ("reset-debug" . playground--op-reset-debug)))

;;;; Interactive commands (emacs.md 10.3)

(defun playground-rescue ()
  "Leave prompts, searches, macros, narrowing and extra windows.
Never saves, kills file buffers or reverts."
  (interactive)
  (playground--op-rescue nil nil))

(defun playground-check (forms)
  "Evaluate FORMS (strings) like a lesson check: t, nil or \"unknown\"."
  (mapcar (lambda (f) (let ((r (playground--check-form f))) (if (eq r :false) nil r))) forms))

(defun playground-open (spec)
  "Visit the files of SPEC and apply its layout."
  (playground--op-open spec))

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
  "Open a GUI frame on DISPLAY (default $PG_DISPLAY or :1)."
  (interactive)
  (playground--op-frame `((display . ,display))))

;;;; The minor mode

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
    ;; A transient menu closed from outside the command loop gets no
    ;; post-command run, and the state would keep it.
    (funcall hook-fn 'transient-post-exit-hook #'playground--schedule-state)
    (funcall hook-fn 'kbd-macro-termination-hook #'playground--kmacro-changed)
    (funcall hook-fn 'compilation-start-hook #'playground--on-compilation-start)
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
        (dolist (entry playground-agent-waiting-loops)
          (advice-add (car entry) :around (playground--waiting-advice entry)
                      '((name . playground-waiting))))
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
    (dolist (entry playground-agent-waiting-loops)
      (advice-remove (car entry) 'playground-waiting))
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
