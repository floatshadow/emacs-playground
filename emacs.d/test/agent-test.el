;;; agent-test.el --- ERT tests for playground-agent.el  -*- lexical-binding: t; -*-

;; Run from emacs.d/ (emacs.md section 10.6):
;;
;;   emacs --batch -l lisp/playground-agent.el -l test/agent-test.el \
;;     -f ert-run-tests-batch-and-exit
;;
;; In the session image the compiled agent can be tested the same way:
;;
;;   emacs --batch -L /opt/playground/emacs.d/lisp -l playground-agent \
;;     -l /opt/playground/emacs.d/test/agent-test.el -f ert-run-tests-batch-and-exit
;;
;; The tests talk to the agent through a fake sessiond written in Elisp:
;; a Unix socket server (make-network-process :server t) that records
;; every line the agent sends and can send calls.  They cover the event
;; shapes, the state object, rescue from each trap state, reset with a
;; spec, snapshot and open, check timeouts, the other ops, reconnection
;; with backoff, the queue, and the post-command cost.

;;; Code:

(require 'ert)
(require 'cl-lib)
(require 'subr-x)
(require 'playground-agent)

;;;; The fake sessiond

(defvar pg-test--dir nil "Temporary directory of the current test.")
(defvar pg-test--server nil "The listening process.")
(defvar pg-test--client nil "The connection the agent opened, once accepted.")
(defvar pg-test--messages nil "Decoded messages from the agent, newest first.")
(defvar pg-test--partial "" "Bytes after the last newline.")
(defvar pg-test--next-id 0)
(defvar pg-test--answer-pings t "When non-nil, the fake answers pings with pong.")
(defvar playground-config-dir)
(defvar playground-modules)

(defun pg-test--filter (proc string)
  "Collect lines from PROC (STRING)."
  (setq pg-test--client proc)
  (setq pg-test--partial (concat pg-test--partial string))
  (let (pos)
    (while (setq pos (string-search "\n" pg-test--partial))
      (let ((line (substring pg-test--partial 0 pos)))
        (setq pg-test--partial (substring pg-test--partial (1+ pos)))
        (let ((msg (json-parse-string line :object-type 'alist :array-type 'list
                                      :null-object :null :false-object :false)))
          (push msg pg-test--messages)
          (when (and pg-test--answer-pings (equal (alist-get 't msg) "ping"))
            (process-send-string proc "{\"t\":\"pong\"}\n")))))))

(defun pg-test-start-server ()
  "Listen on the test socket."
  (let ((path (expand-file-name "agent.sock" pg-test--dir)))
    (when (file-exists-p path) (delete-file path))
    (setq pg-test--messages nil pg-test--partial "" pg-test--client nil)
    (setq pg-test--server
          (make-network-process
           :name "pg-fake-sessiond" :family 'local :service path :server t
           :noquery t :coding 'utf-8-unix :filter #'pg-test--filter
           :log (lambda (_server client _message) (setq pg-test--client client))))))

(defun pg-test-stop-server ()
  "Close the server and its connection."
  (when (process-live-p pg-test--client) (delete-process pg-test--client))
  (when (process-live-p pg-test--server) (delete-process pg-test--server))
  (setq pg-test--client nil pg-test--server nil))

(defun pg-test-wait (pred &optional timeout)
  "Process events until PRED returns non-nil (its value) or TIMEOUT seconds."
  (let ((deadline (+ (float-time) (or timeout 5)))
        value)
    (while (and (not (setq value (funcall pred)))
                (< (float-time) deadline))
      (accept-process-output nil 0.02))
    value))

(defun pg-test-messages (&optional type)
  "Messages received so far, oldest first, only of TYPE if given.
Pings are left out unless TYPE is \"ping\"."
  (let ((all (reverse pg-test--messages)))
    (seq-filter (lambda (m) (if type
                                (equal (alist-get 't m) type)
                              (not (equal (alist-get 't m) "ping"))))
                all)))

(defun pg-test-last (type)
  "The newest message of TYPE."
  (seq-find (lambda (m) (equal (alist-get 't m) type)) pg-test--messages))

(defun pg-test-settle ()
  "Let in-flight lines arrive: wait until nothing new comes for 50 ms."
  (let ((n -1))
    (while (/= n (length pg-test--messages))
      (setq n (length pg-test--messages))
      (accept-process-output nil 0.05))))

(defun pg-test-clear ()
  "Forget received messages, after letting in-flight ones arrive."
  (pg-test-settle)
  (setq pg-test--messages nil))

(defun pg-test-call (op &optional args timeout-ms)
  "Send call OP with ARGS (an alist) to the agent and return its reply."
  (let* ((id (cl-incf pg-test--next-id))
         (msg `((t . "call") (id . ,id) (op . ,op)
                (args . ,(or args (make-hash-table)))
                (timeoutMs . ,(or timeout-ms 2000)))))
    (process-send-string pg-test--client (concat (json-serialize msg) "\n"))
    (pg-test-wait (lambda ()
                    (seq-find (lambda (m) (and (equal (alist-get 't m) "reply")
                                               (equal (alist-get 'id m) id)))
                              pg-test--messages))
                  (+ 3 (/ (or timeout-ms 2000) 1000.0)))))

(defun pg-test-value (reply)
  "The value of a successful REPLY; fail the test otherwise."
  (should reply)
  (unless (eq (alist-get 'ok reply) t)
    (ert-fail (list "call failed" reply)))
  (alist-get 'value reply))

(defun pg-test--reset-agent-state ()
  "Return the agent to a pristine state."
  (setq playground--queue nil playground--queue-length 0
        playground--backoff 1 playground--last-state nil
        playground--early nil playground--mx-reader nil playground--mx-armed nil))

(defmacro pg-test-with-agent (&rest body)
  "Run BODY with a fake sessiond and a connected agent."
  (declare (indent 0) (debug t))
  `(let* ((pg-test--dir (make-temp-file "pg-agent-test" t))
          (playground-agent-socket (expand-file-name "agent.sock" pg-test--dir))
          (playground-agent-workspace (file-name-as-directory pg-test--dir))
          (pg-test--answer-pings t))
     (unwind-protect
         (progn
           (pg-test--reset-agent-state)
           (pg-test-start-server)
           (playground-agent-mode 1)
           (should (pg-test-wait (lambda () (pg-test-last "hello"))))
           (should (pg-test-wait (lambda () (pg-test-last "state"))))
           ,@body)
       (playground-agent-mode -1)
       (pg-test-stop-server)
       (delete-other-windows)
       (switch-to-buffer "*scratch*")
       (dolist (b (buffer-list))
         (when (and (buffer-file-name b)
                    (string-prefix-p pg-test--dir (buffer-file-name b)))
           (with-current-buffer b (set-buffer-modified-p nil))
           (kill-buffer b)))
       (delete-directory pg-test--dir t))))

(defun pg-test-file (name &optional contents)
  "Create file NAME in the test directory with CONTENTS; return its path."
  (let ((path (expand-file-name name pg-test--dir)))
    (make-directory (file-name-directory path) t)
    (with-temp-file path (insert (or contents (format "This is %s.\n" name))))
    path))

(defmacro pg-test-command (cmd keys &rest body)
  "Simulate running command CMD typed as KEYS: BODY, then the hook."
  (declare (indent 2) (debug t))
  `(let ((this-command ,cmd))
     (cl-letf (((symbol-function 'playground--this-keys) (lambda () ,keys)))
       ,@body
       (playground--post-command))))

;;;; Hello and the state object

(ert-deftest pg-agent/hello ()
  "The first line is hello, followed by a full state."
  (pg-test-with-agent
    (let ((hello (car (pg-test-messages))))
      (should (equal (alist-get 't hello) "hello"))
      (should (equal (alist-get 'protocol hello) 1))
      (should (equal (alist-get 'agent hello) playground-agent-version))
      (should (equal (alist-get 'emacs hello) emacs-version))
      (should (equal (alist-get 'pid hello) (emacs-pid))))
    (should (equal (alist-get 't (cadr (pg-test-messages))) "state"))))

(ert-deftest pg-agent/state-shape ()
  "Every field of api.md section 5, with JSON types the browser expects."
  (pg-test-with-agent
    (find-file (pg-test-file "hello/src/main.rs" "fn main() {\n    println!(\"hi\");\n}\n"))
    (forward-line 1)
    (forward-char 4)
    (let ((s (json-parse-string (json-serialize (playground--state))
                                :object-type 'alist :array-type 'list
                                :null-object :null :false-object :false)))
      (dolist (key '(t buf file mode modeName minors modified readonly narrowed overwrite
                       line col point markActive windows layout frame frames tabs
                       minibuffer isearch recursive macro prefixArg readOnlyReason
                       diagAtPoint))
        (should (assq key s)))
      (should (equal (alist-get 't s) "state"))
      (should (equal (alist-get 'buf s) "main.rs"))
      (should (string-suffix-p "hello/src/main.rs" (alist-get 'file s)))
      (should (stringp (alist-get 'mode s)))
      (should (equal (alist-get 'line s) 2))
      (should (equal (alist-get 'col s) 4))
      (should (equal (alist-get 'point s) (point)))
      (should (eq (alist-get 'modified s) :false))
      (should (eq (alist-get 'readonly s) :false))
      (should (eq (alist-get 'minibuffer s) :null))
      (should (eq (alist-get 'isearch s) :false))
      (should (equal (alist-get 'recursive s) 0))
      (should (eq (alist-get 'prefixArg s) :null))
      (should (eq (alist-get 'readOnlyReason s) :null))
      (should (eq (alist-get 'diagAtPoint s) :null))
      (should (equal (alist-get 'frame s) "terminal"))
      (should (integerp (alist-get 'frames s)))
      (should (equal (alist-get 'count (alist-get 'tabs s)) 1))
      (should (listp (alist-get 'minors s)))
      (let ((w (car (alist-get 'windows s))))
        (should (= (length (alist-get 'windows s)) 1))
        (should (integerp (alist-get 'id w)))
        (should (equal (alist-get 'buf w) "main.rs"))
        (should (eq (alist-get 'selected w) t))
        (should (eq (alist-get 'dedicated w) :false))
        (should (eq (alist-get 'side w) :null))
        ;; One window: the layout is that window's id.
        (should (equal (alist-get 'layout s) (alist-get 'id w)))))
    (insert "x")
    (let ((s (playground--state)))
      (should (eq (alist-get 'modified s) t)))))

(ert-deftest pg-agent/state-flags ()
  "Overwrite, narrowing, read-only reasons, prefix argument, minors."
  (pg-test-with-agent
    (with-current-buffer (get-buffer-create "pg-flags")
      (switch-to-buffer (current-buffer))
      (insert "one\ntwo\nthree\n")
      (overwrite-mode 1)
      (narrow-to-region 1 5)
      (let ((s (playground--state)))
        (should (eq (alist-get 'overwrite s) t))
        (should (eq (alist-get 'narrowed s) t))
        (should (member "overwrite-mode" (append (alist-get 'minors s) nil))))
      (overwrite-mode -1)
      (widen)
      (read-only-mode 1)
      (should (equal (alist-get 'readOnlyReason (playground--state)) "read-only-mode"))
      (read-only-mode -1)
      (let ((prefix-arg '(4)))
        (should (equal (alist-get 'prefixArg (playground--state)) "(4)")))
      (kill-buffer))
    (with-current-buffer (get-buffer-create "*pg-help*")
      (help-mode)
      (switch-to-buffer (current-buffer))
      (should (equal (alist-get 'readOnlyReason (playground--state)) "special-mode"))
      (kill-buffer))))

(ert-deftest pg-agent/layout ()
  "Splits: vsplit stacks top to bottom (C-x 2), hsplit side by side (C-x 3)."
  (pg-test-with-agent
    (delete-other-windows)
    (let* ((top (selected-window))
           (bottom (split-window-below))
           (right (with-selected-window bottom (split-window-right)))
           (id (lambda (w) (playground--window-id w))))
      (should (equal (playground--layout (selected-frame))
                     (vector "vsplit" (funcall id top)
                             (vector "hsplit" (funcall id bottom) (funcall id right)))))
      (should (= (length (alist-get 'windows (playground--state))) 3))
      ;; Ids are stable for a window's lifetime.
      (should (= (funcall id top) (playground--window-id top)))
      (delete-window right)
      (should (equal (playground--layout (selected-frame))
                     (vector "vsplit" (funcall id top) (funcall id bottom)))))))

(ert-deftest pg-agent/internal-windows-hidden ()
  "A popup showing an internal buffer (which-key's) is not a window of the layout."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer "*scratch*")
    (let* ((main (selected-window))
           (right (split-window-right))
           (popup (with-selected-window right (split-window-below))))
      (set-window-buffer right (get-buffer-create "pg-other"))
      (set-window-buffer popup (get-buffer-create " *pg-popup*"))
      (let ((s (playground--state)))
        (should (equal (mapcar (lambda (w) (alist-get 'buf w)) (alist-get 'windows s))
                       '("*scratch*" "pg-other")))
        ;; (hsplit main (vsplit right popup)) collapses to (hsplit main right).
        (should (equal (alist-get 'layout s)
                       (vector "hsplit" (playground--window-id main) (playground--window-id right)))))
      (should (equal (alist-get 'layout (playground--op-snapshot)) "(hsplit *scratch* pg-other)"))
      ;; Same-direction splits merge when the popup between them goes.
      (delete-other-windows main)
      (let* ((mid (split-window-below))
             (bottom (with-selected-window mid (split-window-below))))
        (set-window-buffer mid (get-buffer-create " *pg-popup*"))
        (set-window-buffer bottom (get-buffer-create "pg-other"))
        (should (equal (playground--layout (selected-frame))
                       (vector "vsplit" (playground--window-id main)
                               (playground--window-id bottom)))))
      (kill-buffer "pg-other")
      (kill-buffer " *pg-popup*"))))

(ert-deftest pg-agent/non-ascii ()
  "Buffer names and text survive the trip through the socket."
  (pg-test-with-agent
    (switch-to-buffer (get-buffer-create "héllo-世界"))
    (pg-test-clear)
    (playground--emit-state t)
    (let ((s (pg-test-wait (lambda () (pg-test-last "state")))))
      (should (equal (alist-get 'buf s) "héllo-世界")))
    (kill-buffer "héllo-世界")))

;;;; Key events

(ert-deftest pg-agent/key-then-state ()
  "A command produces a key event, then a state."
  (pg-test-with-agent
    (switch-to-buffer (get-buffer-create "pg-keys"))
    (insert "abc")
    (pg-test-clear)
    (pg-test-command 'backward-char "C-b" (backward-char))
    (let ((msgs (pg-test-wait (lambda () (and (pg-test-last "state") (pg-test-messages))))))
      (should (equal (mapcar (lambda (m) (alist-get 't m)) msgs) '("key" "state")))
      (let ((k (car msgs)))
        (should (equal (alist-get 'keys k) "C-b"))
        (should (equal (alist-get 'cmd k) "backward-char"))
        (should (equal (alist-get 'buf k) "pg-keys"))
        (should (equal (alist-get 'frame k) "terminal"))
        (should-not (assq 'mx k))))
    (kill-buffer "pg-keys")))

(ert-deftest pg-agent/key-kinds ()
  "Unbound keys, lambdas, keyboard macros, prefix arguments."
  (pg-test-with-agent
    (pg-test-clear)
    (pg-test-command nil "C-c z" nil)
    (pg-test-command (lambda () (interactive)) "C-c y" nil)
    (pg-test-command "abc" "C-c x" nil)
    (let ((current-prefix-arg '(4)))
      (pg-test-command 'forward-char "C-u C-f" nil))
    (let ((keys (pg-test-wait (lambda () (let ((k (pg-test-messages "key"))) (and (= (length k) 4) k))))))
      (should (equal (mapcar (lambda (k) (alist-get 'cmd k)) keys)
                     '("undefined" "<lambda>" "<kbd-macro>" "forward-char")))
      (should (equal (alist-get 'prefixArg (nth 3 keys)) "(4)")))))

(ert-deftest pg-agent/state-deduplicated ()
  "Identical consecutive states are sent once; key events always."
  (pg-test-with-agent
    (switch-to-buffer (get-buffer-create "pg-dedup"))
    (playground--emit-state)
    (pg-test-clear)
    (pg-test-command 'ignore "C-c i" nil)
    (pg-test-command 'ignore "C-c i" nil)
    (pg-test-wait (lambda () (= (length (pg-test-messages "key")) 2)))
    (accept-process-output nil 0.1)
    (should (= (length (pg-test-messages "key")) 2))
    (should (= (length (pg-test-messages "state")) 0))
    (insert "x")
    (pg-test-command 'self-insert-command "x" nil)
    (should (pg-test-wait (lambda () (= (length (pg-test-messages "state")) 1))))
    (kill-buffer "pg-dedup")))

(ert-deftest pg-agent/mx ()
  "A command run through M-x is reported with the reader's keys and mx."
  (pg-test-with-agent
    (pg-test-clear)
    ;; The M-x prompt opens: remembered, not reported.
    (let ((this-command 'execute-extended-command))
      (cl-letf (((symbol-function 'playground--this-keys) (lambda () "M-x"))
                ((symbol-function 'minibuffer-depth) (lambda () 1))
                ((symbol-function 'minibuffer-prompt) (lambda () "M-x ")))
        (playground--on-minibuffer-setup)))
    ;; execute-extended-command runs the chosen command.
    (playground--mx-advice #'ignore)
    (pg-test-command 'forward-char "RET" nil)
    (let ((k (pg-test-wait (lambda () (car (pg-test-messages "key"))))))
      (should (equal (alist-get 'keys k) "M-x"))
      (should (equal (alist-get 'cmd k) "forward-char"))
      (should (eq (alist-get 'mx k) t)))
    (should (= (length (pg-test-messages "key")) 1))
    (should-not playground--mx-armed)))

(ert-deftest pg-agent/minibuffer-early-key ()
  "A command that opens a prompt is reported when the prompt opens, once."
  (pg-test-with-agent
    (pg-test-clear)
    (cl-letf (((symbol-function 'minibuffer-depth) (lambda () 1))
              ((symbol-function 'minibuffer-prompt) (lambda () "Find file: ")))
      (let ((this-command 'find-file))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "C-x C-f")))
          (playground--on-minibuffer-setup)
          ;; The minibuffer's own command loop starts with one more run
          ;; of post-command-hook for the same command: not reported.
          (playground--post-command)))
      ;; RET accepts: exit-minibuffer leaves by a throw, so the exit
      ;; hook reports it.
      (let ((this-command 'exit-minibuffer))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "RET")))
          (playground--on-minibuffer-exit))))
    (pg-test-command 'find-file "C-x C-f" nil)
    (pg-test-settle)
    (let ((types (mapcar (lambda (m) (alist-get 't m)) (pg-test-messages))))
      (should (equal (seq-remove (lambda (x) (equal x "state")) types)
                     '("key" "minibuffer" "key" "minibuffer"))))
    (let ((k (car (pg-test-messages "key")))
          (ret (cadr (pg-test-messages "key")))
          (open (car (pg-test-messages "minibuffer")))
          (close (cadr (pg-test-messages "minibuffer"))))
      (should (equal (alist-get 'keys k) "C-x C-f"))
      (should (equal (alist-get 'cmd k) "find-file"))
      (should (equal (alist-get 'keys ret) "RET"))
      (should (equal (alist-get 'cmd ret) "exit-minibuffer"))
      (should (eq (alist-get 'open open) t))
      (should (equal (alist-get 'prompt open) "Find file: "))
      (should (equal (alist-get 'depth open) 1))
      (should (equal (alist-get 'kind open) "prompt"))
      (should (eq (alist-get 'open close) :false)))
    (should-not playground--early)))

(ert-deftest pg-agent/isearch ()
  "Isearch is reported as a prompt-like minibuffer of kind isearch."
  (pg-test-with-agent
    (switch-to-buffer (get-buffer-create "pg-isearch"))
    (insert "greet greet greet")
    (goto-char (point-min))
    (pg-test-clear)
    (isearch-mode t)
    (let ((s (playground--state)))
      (should (eq (alist-get 'isearch s) t))
      (should (equal (alist-get 'kind (alist-get 'minibuffer s)) "isearch"))
      (should (string-match-p "I-search" (alist-get 'prompt (alist-get 'minibuffer s)))))
    (isearch-done)
    (should (eq (alist-get 'isearch (playground--state)) :false))
    (let ((events (pg-test-wait (lambda () (let ((m (pg-test-messages "minibuffer")))
                                             (and (= (length m) 2) m))))))
      (should (equal (alist-get 'kind (car events)) "isearch"))
      (should (eq (alist-get 'open (car events)) t))
      (should (eq (alist-get 'open (cadr events)) :false)))
    (kill-buffer "pg-isearch")))

(ert-deftest pg-agent/frames-and-echo ()
  "Frame events, echo messages with the noise filter, command errors."
  (pg-test-with-agent
    (pg-test-clear)
    (playground--on-make-frame)
    (let ((f (pg-test-wait (lambda () (pg-test-last "frame")))))
      (should (equal (alist-get 'event f) "opened"))
      (should (equal (alist-get 'kind f) "terminal")))
    (pg-test-clear)
    (playground--on-message "Mark set")
    (playground--on-message "Auto-saving...done")
    (playground--on-message "Wrote /work/hello/src/main.rs")
    (playground--on-command-error '(beginning-of-buffer) nil nil)
    (playground--on-command-error '(quit) nil nil)
    (let ((texts (pg-test-wait (lambda () (let ((e (pg-test-messages "echo")))
                                            (and (= (length e) 4)
                                                 (mapcar (lambda (m) (alist-get 'text m)) e)))))))
      (should (equal texts '("Mark set" "Wrote /work/hello/src/main.rs"
                             "Beginning of buffer" "Quit"))))))

(ert-deftest pg-agent/hooks-never-signal ()
  "A failure inside the agent never escapes into the command loop."
  (pg-test-with-agent
    (cl-letf (((symbol-function 'playground--state) (lambda () (error "Boom"))))
      (pg-test-command 'forward-char "C-f" nil)
      (playground--on-isearch-start)
      (should (memq #'playground--post-command post-command-hook)))))

(ert-deftest pg-agent/cancelled-prompt ()
  "C-g in a prompt is reported once, with its keys."
  (pg-test-with-agent
    (pg-test-clear)
    (cl-letf (((symbol-function 'minibuffer-depth) (lambda () 1))
              ((symbol-function 'minibuffer-prompt) (lambda () "Find file: ")))
      (let ((this-command 'find-file))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "C-x C-f")))
          (playground--on-minibuffer-setup)))
      (let ((this-command 'minibuffer-keyboard-quit))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "C-g")))
          (playground--on-minibuffer-exit))))
    ;; After the quit unwinds, the command loop runs the hook again for
    ;; the same command, without keys.
    (pg-test-command 'minibuffer-keyboard-quit "" nil)
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (list (alist-get 'keys k) (alist-get 'cmd k)))
                           (pg-test-messages "key"))
                   '(("C-x C-f" "find-file") ("C-g" "minibuffer-keyboard-quit"))))))

(ert-deftest pg-agent/unbound-keys ()
  "Unbound keys run no command; their echo message yields an undefined key event."
  (pg-test-with-agent
    (pg-test-clear)
    (playground--on-message "C-c z is undefined")
    ;; A key bound to the command `undefined' runs the hook as well: once.
    (pg-test-command 'undefined "C-c z" nil)
    (pg-test-settle)
    (let ((keys (pg-test-messages "key")))
      (should (= (length keys) 1))
      (should (equal (alist-get 'keys (car keys)) "C-c z"))
      (should (equal (alist-get 'cmd (car keys)) "undefined")))))

(ert-deftest pg-agent/keys-before-command ()
  "Keys come from pre-command-hook: C-h k reports C-h k, not the key it described."
  (pg-test-with-agent
    (pg-test-clear)
    (cl-letf (((symbol-function 'this-command-keys-vector) (lambda () (kbd "C-h k"))))
      (playground--pre-command))
    (pg-test-command 'describe-key "C-x C-f" nil)
    ;; Internal events are not keys.
    (pg-test-command 'handle-switch-frame "<switch-frame>" nil)
    (pg-test-command (lambda () (interactive)) "<mouse-movement>" nil)
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (alist-get 'keys k)) (pg-test-messages "key"))
                   '("C-h k")))))

(ert-deftest pg-agent/prefix-argument-commands ()
  "C-u and digit arguments are reported as themselves, not as the command before."
  (pg-test-with-agent
    (pg-test-clear)
    (dolist (case '((universal-argument "C-u") (digit-argument "C-M-5")))
      (let ((this-command (car case)))
        (cl-letf (((symbol-function 'this-command-keys-vector)
                   (lambda () (key-parse (cadr case)))))
          (playground--pre-command)))
      ;; `prefix-command-preserve-state' has set this-command back to
      ;; the previous command by the time post-command-hook runs.
      (pg-test-command 'forward-char (cadr case) nil))
    ;; The command after the prefix is itself again.
    (pg-test-command 'forward-char "C-f" nil)
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (list (alist-get 'keys k) (alist-get 'cmd k)))
                           (pg-test-messages "key"))
                   '(("C-u" "universal-argument") ("C-M-5" "digit-argument")
                     ("C-f" "forward-char"))))))

(ert-deftest pg-agent/key-frame-is-where-typed ()
  "C-x C-c in the GUI frame is reported as typed there, though the frame is gone."
  (pg-test-with-agent
    (pg-test-clear)
    (cl-letf (((symbol-function 'this-command-keys-vector) (lambda () (kbd "C-x C-c")))
              ((symbol-function 'playground--frame-kind) (lambda (&optional _) "gui")))
      (playground--pre-command))
    ;; By post-command-hook time the selected frame is the terminal one.
    (pg-test-command 'save-buffers-kill-terminal "C-x C-c" nil)
    (let ((k (pg-test-wait (lambda () (car (pg-test-messages "key"))))))
      (should (equal (alist-get 'keys k) "C-x C-c"))
      (should (equal (alist-get 'frame k) "gui")))))

(ert-deftest pg-agent/echo-noise ()
  "Progress chatter from helpful and friends is not reported."
  (pg-test-with-agent
    (pg-test-clear)
    (dolist (m '("uncompressing files.el.gz..." "uncompressing files.el.gz...done"
                 "Searched 1/1 files" "Auto-saving...done" "Mark set"))
      (playground--on-message m))
    ;; Eldoc's signatures and hints, however they reach the echo area.
    (playground--eldoc-quietly (lambda () (playground--on-message "fn greet(name: &str) -> String")))
    (should (advice-member-p #'playground--eldoc-quietly 'eldoc-pre-command-refresh-echo-area))
    (pg-test-settle)
    (should (equal (mapcar (lambda (e) (alist-get 'text e)) (pg-test-messages "echo"))
                   '("Mark set")))))

(ert-deftest pg-agent/echo-repeat ()
  "The same message shown twice within a second is sent once."
  (pg-test-with-agent
    (pg-test-clear)
    (playground--on-message "Hello")
    (playground--on-message "Hello")
    (playground--on-message "Bye")
    (pg-test-settle)
    (should (equal (mapcar (lambda (e) (alist-get 'text e)) (pg-test-messages "echo"))
                   '("Hello" "Bye")))))

;;;; Ops

(ert-deftest pg-agent/op-eval ()
  "eval returns JSON for any value; errors come back as failed replies."
  (pg-test-with-agent
    (should (equal (pg-test-value (pg-test-call "eval" '((form . "(+ 1 2)"))))
                   '((value . 3))))
    (should (equal (alist-get 'value (pg-test-value
                                      (pg-test-call "eval" '((form . "(list 1 \"a\" t nil 'sym)")))))
                   '(1 "a" t :null "sym")))
    (should (equal (alist-get 'value (pg-test-value
                                      (pg-test-call "eval" '((form . "'((a . 1) (b . \"x\"))")))))
                   '((a . 1) (b . "x"))))
    (should (string-prefix-p "#<buffer"
                             (alist-get 'value (pg-test-value
                                                (pg-test-call "eval" '((form . "(current-buffer)")))))))
    (let ((r (pg-test-call "eval" '((form . "(error \"Boom\")")))))
      (should (eq (alist-get 'ok r) :false))
      (should (equal (alist-get 'error r) "Boom")))
    (let ((r (pg-test-call "no-such-op")))
      (should (eq (alist-get 'ok r) :false))
      (should (string-match-p "unknown op" (alist-get 'error r))))))

(ert-deftest pg-agent/op-check ()
  "check: true, false or unknown per form, each bounded by a timeout."
  (pg-test-with-agent
    (let* ((start (float-time))
           (r (pg-test-value
               (pg-test-call "check"
                             `((forms . ,(vector "t" "nil" "(error \"x\")" "(sleep-for 5)" "(+ 1"
                                                 "(equal (buffer-name) (buffer-name))")))
                             8000))))
      (should (equal (alist-get 'results r) '(t :false "unknown" "unknown" "unknown" t)))
      (should (< (- (float-time) start) 3.5)))))

(ert-deftest pg-agent/op-describe ()
  "describe: the command bound to keys and its first doc line."
  (pg-test-with-agent
    (let ((r (pg-test-value (pg-test-call "describe" '((keys . "C-x C-f"))))))
      (should (equal (alist-get 'cmd r) "find-file"))
      (should (string-match-p "file" (alist-get 'doc r))))
    (let ((r (pg-test-value (pg-test-call "describe" '((keys . "C-x"))))))
      (should (equal (alist-get 'cmd r) ""))
      (should (string-match-p "prefix" (alist-get 'doc r))))
    (let ((r (pg-test-value (pg-test-call "describe" '((keys . "C-c C-z C-z"))))))
      (should (equal (alist-get 'cmd r) "")))
    (let ((r (pg-test-value (pg-test-call "describe" '((cmd . "save-buffer"))))))
      (should (equal (alist-get 'cmd r) "save-buffer"))
      (should (string-match-p "[Ss]ave" (alist-get 'doc r))))))

(ert-deftest pg-agent/op-set-modules ()
  "set-modules writes modules.el; bad names are refused."
  (pg-test-with-agent
    (let ((playground-config-dir (file-name-as-directory pg-test--dir)))
      (pg-test-value (pg-test-call "set-modules" `((enabled . ,(vector "base" "agent" "editing")))))
      (let (playground-modules)
        (load (expand-file-name "modules.el" pg-test--dir) nil t)
        (should (equal playground-modules '(base agent editing))))
      (should (eq (alist-get 'ok (pg-test-call "set-modules" `((enabled . ,(vector "../evil"))))) :false)))))

(ert-deftest pg-agent/snapshot-and-open ()
  "snapshot describes buffers and layout; open puts them back."
  (pg-test-with-agent
    (let ((a (pg-test-file "a.txt" "line one\nline two\n"))
          (b (pg-test-file "sub/b.txt")))
      (delete-other-windows)
      (find-file a)
      (goto-char 10)
      (split-window-below)
      (other-window 1)
      (find-file b)
      (other-window 1)
      (let ((snap (pg-test-value (pg-test-call "snapshot"))))
        (should (equal (alist-get 'layout snap) "(vsplit a.txt sub/b.txt)"))
        (should (equal (alist-get 'selected snap) "a.txt"))
        (should (> (length (alist-get 'windowState snap)) 0))
        (let ((fa (seq-find (lambda (f) (equal (alist-get 'path f) "a.txt")) (alist-get 'files snap))))
          (should (equal (alist-get 'point fa) 10))
          (should (eq (alist-get 'visible fa) t)))
        ;; Destroy the layout, then restore it from the simple layout
        ;; description only.
        (delete-other-windows)
        (switch-to-buffer "*scratch*")
        (setf (alist-get 'windowState snap) "")
        (setf (alist-get 'files snap) (vconcat (alist-get 'files snap)))
        (pg-test-value (pg-test-call "open" snap))
        (should (= (length (window-list)) 2))
        (should (equal (buffer-name (window-buffer (frame-first-window))) "a.txt"))
        (should (equal (buffer-name (window-buffer (next-window (frame-first-window)))) "b.txt"))
        (should (window-combined-p (frame-first-window)))))))

(ert-deftest pg-agent/open-before-any-frame ()
  "After a restart, open restores buffers at once and the layout on the first frame."
  (pg-test-with-agent
    (let ((a (pg-test-file "a.txt" "line one\nline two\n"))
          (spec `((files . ,(vector '((path . "a.txt") (point . 6) (visible . t))
                                    '((path . "b.txt") (point . 1) (visible . t))))
                  (layout . "(hsplit a.txt b.txt)")
                  (selected . "b.txt"))))
      (pg-test-file "b.txt")
      (delete-other-windows)
      (switch-to-buffer "*scratch*")
      ;; Only the daemon's own frame exists: the layout waits.
      (cl-letf (((symbol-function 'playground--frame-ready-p) #'ignore))
        (let ((r (pg-test-value (pg-test-call "open" spec))))
          (should (eq (alist-get 'layoutDeferred r) t))
          (should (equal (alist-get 'opened r) 2))))
      (should (= (length (window-list)) 1))
      (should (= (with-current-buffer (get-file-buffer a) (point)) 6))
      (should playground--pending-layout)
      ;; The first client frame gets it.
      (playground--on-make-frame)
      (should (pg-test-wait (lambda () (= (length (window-list)) 2)) 2))
      (should (equal (playground--layout-string (playground--tree (selected-frame)))
                     "(hsplit a.txt b.txt)"))
      (should (equal (buffer-name (window-buffer (selected-window))) "b.txt"))
      (should-not playground--pending-layout))))

(ert-deftest pg-agent/op-reset-step ()
  "reset-step reverts edited buffers, kills buffers of deleted files, applies the spec."
  (pg-test-with-agent
    (let ((a (pg-test-file "a.txt" "original a\n"))
          (b (pg-test-file "b.txt" "original b\n"))
          (c (pg-test-file "c.txt" "original c\n")))
      (with-current-buffer (find-file-noselect a) (goto-char (point-max)) (insert "edited\n"))
      (find-file-noselect b)
      (with-temp-file b (insert "restored b\n"))
      (set-file-times b (time-add nil 2))
      (find-file-noselect c)
      (delete-file c)
      (let ((r (pg-test-value
                (pg-test-call "reset-step"
                              `((spec . ((files . ,(vector '((path . "a.txt") (point . 1) (visible . t))
                                                           '((path . "b.txt") (point . 1) (visible . t))))
                                         (layout . "(hsplit a.txt b.txt)"))))))))
        (should (>= (alist-get 'reverted r) 2))
        (should (= (alist-get 'killed r) 1)))
      (should (equal (with-current-buffer (get-file-buffer a) (buffer-string)) "original a\n"))
      (should (equal (with-current-buffer (get-file-buffer b) (buffer-string)) "restored b\n"))
      (should-not (get-file-buffer c))
      (should (= (length (window-list)) 2))
      (should (equal (playground--layout-string (playground--tree (selected-frame))) "(hsplit a.txt b.txt)")))))

(ert-deftest pg-agent/op-setup ()
  "setup opens files, applies a layout pattern and evaluates a form."
  (pg-test-with-agent
    (pg-test-file "hello/README.md" "# hello\n")
    (pg-test-file "hello/src/main.rs" "fn main() {}\n")
    (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/src/main.rs" "hello/README.md"))
                                           (layout . "(vsplit main.rs README.md)")
                                           (eval . "(setq pg-test--setup-ran t)")
                                           (workspace . ,pg-test--dir))))
    (should (bound-and-true-p pg-test--setup-ran))
    (should (equal (playground--layout-string (playground--tree (selected-frame)))
                   "(vsplit hello/src/main.rs hello/README.md)"))))
(defvar pg-test--setup-ran nil)

;;;; Rescue from each trap state (emacs.md 10.3)

(defun pg-test-rescue ()
  "Run rescue through the socket and return its did list."
  (alist-get 'did (pg-test-value (pg-test-call "rescue"))))

(ert-deftest pg-agent/rescue-normal ()
  "Rescue in a normal state does nothing and says so."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer "*scratch*")
    (should (equal (pg-test-rescue) nil))))

(ert-deftest pg-agent/rescue-overwrite-and-narrowing ()
  "Overwrite mode off, narrowing widened, region dropped; text untouched."
  (pg-test-with-agent
    (delete-other-windows)
    (find-file (pg-test-file "n.txt" "one\ntwo\nthree\n"))
    (overwrite-mode 1)
    (narrow-to-region 1 4)
    (transient-mark-mode 1)
    (set-mark 1) (goto-char 3) (activate-mark)
    (let ((did (pg-test-rescue)))
      (should (member "turned off overwrite mode" did))
      (should (member "widened n.txt" did))
      (should (member "dropped the selection" did)))
    (should-not overwrite-mode)
    (should-not (buffer-narrowed-p))
    (should-not (region-active-p))
    (should (equal (buffer-string) "one\ntwo\nthree\n"))
    (should-not (buffer-modified-p))))

(ert-deftest pg-agent/rescue-windows-and-help ()
  "Extra windows close, keeping the file window; special buffers are put away."
  (pg-test-with-agent
    (delete-other-windows)
    (find-file (pg-test-file "w.txt"))
    (let ((help (get-buffer-create "*pg-help*")))
      (with-current-buffer help (special-mode))
      (split-window-right)
      (other-window 1)
      (switch-to-buffer help)          ; the learner is inside the help window
      (let ((did (pg-test-rescue)))
        (should (member "closed 1 other window" did)))
      (should (= (length (window-list)) 1))
      (should (equal (buffer-name (window-buffer (selected-window))) "w.txt"))
      (should (buffer-live-p help))
      (kill-buffer help))))

(ert-deftest pg-agent/rescue-isearch ()
  "A running isearch is stopped."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer (get-buffer-create "pg-rescue-isearch"))
    (insert "greet greet")
    (goto-char (point-min))
    (isearch-mode t)
    (should (pg-test-wait (lambda () isearch-mode) 1))
    (should (member "stopped the search" (pg-test-rescue)))
    (should-not isearch-mode)
    (kill-buffer "pg-rescue-isearch")))

(ert-deftest pg-agent/rescue-macro ()
  "Recording a keyboard macro is stopped."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer "*scratch*")
    (skip-unless (condition-case nil (progn (start-kbd-macro nil) t) (error nil)))
    (should defining-kbd-macro)
    (should (member "stopped recording the keyboard macro" (pg-test-rescue)))
    (should-not defining-kbd-macro)))

(ert-deftest pg-agent/rescue-prompt-and-recursive-edit ()
  "With prompts or recursive edits open, rescue unwinds first, then finishes and replies."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer "*scratch*")
    (let* ((unwound nil)
           (playground-agent-rescue-unwind-function (lambda () (setq unwound t)))
           (depth 1))
      (cl-letf (((symbol-function 'minibuffer-depth) (lambda () depth))
                ((symbol-function 'recursion-depth) (lambda () (+ depth 1))))
        (let ((did (pg-test-rescue)))
          (should unwound)
          (should (equal (seq-take did 2) '("cancelled the prompt" "left the recursive edit"))))))))

;;;; Connection

(ert-deftest pg-agent/reconnect-with-backoff ()
  "The agent connects when sessiond appears and again after it drops."
  (let* ((pg-test--dir (make-temp-file "pg-agent-test" t))
         (playground-agent-socket (expand-file-name "agent.sock" pg-test--dir)))
    (unwind-protect
        (progn
          (pg-test--reset-agent-state)
          (playground-agent-mode 1)          ; nobody listening yet
          (should-not playground--connected)
          ;; The failed connection is reported through the sentinel.
          (should (pg-test-wait (lambda () playground--reconnect-timer) 2))
          (playground--emit '((t . "echo") (text . "queued while away")))
          (should (= playground--queue-length 1))
          (pg-test-start-server)
          (should (pg-test-wait (lambda () (pg-test-last "hello")) 4))
          ;; Hello first, then what was queued, then a fresh state.
          (let ((types (mapcar (lambda (m) (alist-get 't m))
                               (pg-test-wait (lambda () (and (pg-test-last "state") (pg-test-messages)))))))
            (should (equal (seq-take types 3) '("hello" "echo" "state"))))
          (should (= playground--queue-length 0))
          ;; sessiond goes away: the agent notices and comes back.
          (pg-test-clear)
          (delete-process pg-test--client)
          (should (pg-test-wait (lambda () (not playground--connected)) 2))
          (should (pg-test-wait (lambda () (pg-test-last "hello")) 4))
          (should playground--connected))
      (playground-agent-mode -1)
      (pg-test-stop-server)
      (delete-directory pg-test--dir t))))

(ert-deftest pg-agent/queue-limit ()
  "While disconnected at most `playground-agent-queue-limit' events are kept."
  (let ((playground--connected nil)
        (playground--process nil)
        (playground--queue nil)
        (playground--queue-length 0)
        (playground-agent-queue-limit 1000))
    (dotimes (i 1100)
      (playground--emit `((t . "echo") (text . ,(format "%d" i)))))
    (should (= playground--queue-length 1000))
    (should (= (length playground--queue) 1000))
    ;; The oldest were dropped.
    (should (string-match-p "\"100\"" (cdr (car (last playground--queue)))))
    ;; Pings are never queued.
    (playground--send-line "{\"t\":\"ping\"}" 'ping)
    (should (= playground--queue-length 1000))))

(ert-deftest pg-agent/ping-and-silent-peer ()
  "Pings go out; a peer that stops answering is dropped and reconnected."
  (pg-test-with-agent
    (pg-test-clear)
    (playground--ping)
    (should (pg-test-wait (lambda () (pg-test-last "ping")) 2))
    (should (pg-test-wait (lambda () playground--last-pong) 2))
    (setq playground--last-pong (- (float-time) 60))
    (setq pg-test--answer-pings nil)
    (pg-test-clear)
    (playground--ping)
    (should-not playground--connected)
    (should (pg-test-wait (lambda () (pg-test-last "hello")) 4))))

(ert-deftest pg-agent/stalled-peer-queues ()
  "While sessiond is silent, events wait in the queue; the next pong flushes them."
  (pg-test-with-agent
    (playground--ping)
    (should (pg-test-wait (lambda () playground--last-pong) 2))
    (pg-test-clear)
    ;; sessiond stops answering (and so, presumably, reading).
    (setq pg-test--answer-pings nil
          playground--last-pong (- (float-time) (1+ playground-agent-stall-seconds)))
    (playground--emit '((t . "echo") (text . "while stalled")))
    (should (= playground--queue-length 1))
    (accept-process-output nil 0.1)
    (should-not (pg-test-messages "echo"))
    (should playground--connected)
    ;; It recovers: the next ping is answered and the queue goes out.
    (setq pg-test--answer-pings t)
    (playground--ping)
    (let ((e (pg-test-wait (lambda () (pg-test-last "echo")) 2)))
      (should (equal (alist-get 'text e) "while stalled")))
    (should (= playground--queue-length 0))
    (should playground--connected)))

(ert-deftest pg-agent/busy-emacs-is-not-a-silent-peer ()
  "A pong that arrived while Emacs was busy is read before judging the peer."
  (pg-test-with-agent
    (playground--ping)
    (should (pg-test-wait (lambda () playground--last-pong) 2))
    (pg-test-clear)
    ;; A pong is waiting in the socket, unread, and the last one we
    ;; processed is old: as after a long-running command.
    (process-send-string pg-test--client "{\"t\":\"pong\"}\n")
    (setq playground--last-pong (- (float-time) (1+ playground-agent-peer-timeout)))
    (playground--ping)
    (should playground--connected)
    (should (< (playground--peer-silent-for) 2))))

;;;; Cost (emacs.md 10.5)

(ert-deftest pg-agent/post-command-cost ()
  "Median cost of the post-command work (state, JSON, send) is small."
  (pg-test-with-agent
    (find-file (pg-test-file "cost.txt" (string-join (make-list 200 "fn main() { println!(\"hi\"); }") "\n")))
    (goto-char (/ (point-max) 2))
    (let ((times nil)
          (compiled (or (native-comp-function-p (symbol-function 'playground--state))
                        (byte-code-function-p (symbol-function 'playground--state)))))
      (dotimes (i 300)
        (let ((start (float-time)))
          (pg-test-command 'forward-char "C-f" (forward-char (if (cl-oddp i) 1 -1)))
          (push (- (float-time) start) times)))
      (let ((median (* 1000 (nth 150 (sort times #'<)))))
        (message "post-command median %.3f ms (%s)" median (if compiled "compiled" "interpreted"))
        (should (< median (if compiled 0.3 3.0)))))))

(provide 'agent-test)
;;; agent-test.el ends here
