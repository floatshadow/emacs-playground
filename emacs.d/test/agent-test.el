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
(require 'ibuffer)
(require 'ibuf-ext)
(require 'dired)

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
  `(let* ((this-command ,cmd) (real-this-command this-command))
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
    (let ((this-command 'execute-extended-command) (real-this-command 'execute-extended-command))
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
      (let ((this-command 'find-file) (real-this-command 'find-file))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "C-x C-f")))
          (playground--on-minibuffer-setup)
          ;; The minibuffer's own command loop starts with one more run
          ;; of post-command-hook for the same command: not reported.
          (playground--post-command)))
      ;; RET accepts: exit-minibuffer leaves by a throw, so the exit
      ;; hook reports it.
      (let ((this-command 'exit-minibuffer) (real-this-command 'exit-minibuffer))
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
      (let ((this-command 'find-file) (real-this-command 'find-file))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "C-x C-f")))
          (playground--on-minibuffer-setup)))
      (let ((this-command 'minibuffer-keyboard-quit) (real-this-command 'minibuffer-keyboard-quit))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "C-g")))
          (playground--on-minibuffer-exit))))
    ;; After the quit unwinds, the command loop runs the hook again for
    ;; the same command, without keys.
    (pg-test-command 'minibuffer-keyboard-quit "" nil)
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (list (alist-get 'keys k) (alist-get 'cmd k)))
                           (pg-test-messages "key"))
                   '(("C-x C-f" "find-file") ("C-g" "minibuffer-keyboard-quit"))))))

(ert-deftest pg-agent/action-key-read-by-the-command ()
  "Embark's C-. d in a prompt is C-., once, then the y that confirms it.
embark-act reads d itself (`this-command-keys' becomes d), quits the
prompt it was typed in, and its action asks a question and then opens
a prompt of its own, both while embark-act is still the command."
  (pg-test-with-agent
    (cl-letf (((symbol-function 'minibuffer-depth) (lambda () 1))
              ((symbol-function 'minibuffer-prompt) (lambda () "Find file: ")))
      (let ((this-command 'find-file) (real-this-command 'find-file))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "C-x C-f")))
          (playground--on-minibuffer-setup)
          (playground--post-command)))
      (pg-test-clear)
      (let ((this-command 'embark-act) (real-this-command 'embark-act))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "d")))
          (setq playground--pre-keys (kbd "C-."))
          ;; The action quits the file prompt (the command loop runs
          ;; the hook once more, at depth 0)...
          (playground--on-minibuffer-exit)
          (cl-letf (((symbol-function 'minibuffer-depth) (lambda () 0)))
            (playground--post-command))
          ;; ...asks y or n, when `this-command-keys' is empty...
          (cl-letf (((symbol-function 'playground--this-keys) (lambda () "")))
            (playground--on-minibuffer-setup))))
      (let ((this-command 'y-or-n-p-insert-y) (real-this-command 'y-or-n-p-insert-y))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "y")))
          (setq playground--pre-keys [?y])
          (playground--on-minibuffer-exit)))
      ;; ...and fills in a prompt of its own.
      (let ((this-command 'embark-act) (real-this-command 'embark-act))
        (cl-letf (((symbol-function 'playground--this-keys) (lambda () "y")))
          (playground--on-minibuffer-setup)
          (playground--on-minibuffer-exit))))
    (cl-letf (((symbol-function 'minibuffer-depth) (lambda () 0)))
      (pg-test-command 'embark-act "" nil))
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (list (alist-get 'keys k) (alist-get 'cmd k)))
                           (pg-test-messages "key"))
                   '(("C-." "embark-act") ("y" "y-or-n-p-insert-y"))))))

(ert-deftest pg-agent/unbound-keys ()
  "An unbound key is one key event, however often its message is shown."
  (pg-test-with-agent
    (pg-test-clear)
    ;; Emacs runs `undefined' for an unbound key between the command
    ;; hooks, with `this-command' nil.
    (let ((num-input-keys (1+ num-input-keys)))
      (pg-test-command nil "C-z"
        (playground--on-message "C-z is undefined"))
      ;; auto-save-visited saves from its idle timer and puts the message
      ;; back: no key was read since.
      (playground--on-message "C-z is undefined"))
    ;; C-h k C-z answers with the same text: the key event is describe-key's.
    (let ((num-input-keys (+ 2 num-input-keys)))
      (pg-test-command 'describe-key "C-h k"
        (playground--on-message "C-z is undefined")))
    ;; A key bound to the command `undefined' is reported once too.
    (let ((num-input-keys (+ 3 num-input-keys)))
      (pg-test-command 'undefined "C-c z"
        (playground--on-message "C-c z is undefined")))
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (list (alist-get 'keys k) (alist-get 'cmd k)))
                           (pg-test-messages "key"))
                   '(("C-z" "undefined") ("C-h k" "describe-key") ("C-c z" "undefined"))))
    (should (equal (mapcar (lambda (e) (alist-get 'text e)) (pg-test-messages "echo"))
                   '("C-z is undefined" "C-z is undefined" "C-c z is undefined")))))

(ert-deftest pg-agent/which-key-pages-are-no-command ()
  "C-h after a prefix pages through which-key; the prefix is still pending after it."
  (pg-test-with-agent
    (pg-test-clear)
    (pg-test-command 'which-key-C-h-dispatch "C-x r C-h" nil)
    (pg-test-command 'copy-to-register "C-x r s" nil)
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (alist-get 'keys k)) (pg-test-messages "key"))
                   '("C-x r s")))))

(ert-deftest pg-agent/internal-events-keep-the-learner-frame ()
  "Focus and mouse-movement events do not make their frame the learner's."
  (pg-test-with-agent
    (let ((playground--learner-frame nil))
      (pg-test-command 'handle-focus-in "<focus-in>" nil)
      (pg-test-command 'ignore "<mouse-movement>" nil)
      (should-not playground--learner-frame)
      (pg-test-command 'forward-char "C-f" nil)
      (should (eq playground--learner-frame (selected-frame))))))

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
      (let* ((this-command (car case)) (real-this-command this-command))
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

(defmacro pg-test-run (cmd keys)
  "Run command CMD as the command loop does, typed as KEYS, then the hook.
Errors (an empty kill ring) are ignored, as the command loop shows them."
  `(let* ((this-command ,cmd) (real-this-command this-command))
     (cl-letf (((symbol-function 'playground--this-keys) (lambda () ,keys)))
       (ignore-errors (call-interactively this-command))
       (playground--post-command))))

(ert-deftest pg-agent/reports-the-command-run ()
  "Commands that rewrite `this-command' are reported by the command the keys ran."
  (pg-test-with-agent
    (switch-to-buffer (get-buffer-create "pg-kills"))
    (erase-buffer)
    (insert "first line\nsecond line\n")
    (goto-char (point-min))
    (let ((kill-ring nil) (kill-ring-yank-pointer nil)
          (interprogram-paste-function nil) (interprogram-cut-function nil)
          (last-command nil))
      ;; yank with nothing to yank leaves this-command at t.
      (pg-test-run 'yank "C-y")
      ;; kill-line goes through kill-region, which renames this-command.
      (pg-test-run 'kill-line "C-k")
      (should (equal (car kill-ring) "first line"))
      (setq last-command 'forward-line)
      (forward-line 1)
      (pg-test-run 'kill-line "C-k")
      (setq last-command 'end-of-buffer)
      (goto-char (point-max))
      (pg-test-run 'yank "C-y")
      (should (string-suffix-p "second line" (buffer-string)))
      ;; yank-pop sets this-command to yank so that M-y can follow.
      (setq last-command 'yank)
      (pg-test-run 'yank-pop "M-y")
      (should (string-suffix-p "first line" (buffer-string))))
    (pg-test-settle)
    (should (equal (mapcar (lambda (k) (list (alist-get 'keys k) (alist-get 'cmd k)))
                           (pg-test-messages "key"))
                   '(("C-y" "yank") ("C-k" "kill-line") ("C-k" "kill-line")
                     ("C-y" "yank") ("M-y" "yank-pop"))))
    (kill-buffer "pg-kills")))

(ert-deftest pg-agent/minibuffer-complete-is-itself ()
  "TAB in a prompt is minibuffer-complete, though it sets this-command otherwise."
  (pg-test-with-agent
    (pg-test-clear)
    (let ((real-this-command 'minibuffer-complete)
          (this-command 'completion-at-point))
      (cl-letf (((symbol-function 'playground--this-keys) (lambda () "TAB")))
        (playground--post-command)))
    (let ((k (pg-test-wait (lambda () (car (pg-test-messages "key"))))))
      (should (equal (alist-get 'cmd k) "minibuffer-complete")))))

(ert-deftest pg-agent/learner-frame ()
  "The learner's frame is the frame of the last command, while it exists."
  (pg-test-with-agent
    (let ((playground--learner-frame nil))
      (pg-test-command 'forward-char "C-f" nil)
      (should (eq playground--learner-frame (selected-frame)))
      (should (eq (playground--learner-frame) (selected-frame)))
      ;; A frame that is gone (a closed client frame) is not used.
      (setq playground--learner-frame 'not-a-live-frame)
      (should (eq (playground--learner-frame) (selected-frame)))
      (should (eq (playground--user-window) (selected-window))))))

(ert-deftest pg-agent/check-runs-in-the-learners-window ()
  "check evaluates its forms in the learner's window, not in whatever is selected."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer (get-buffer-create "pg-upper"))
    (let ((lower (split-window-below)))
      (set-window-buffer lower (get-buffer-create "pg-lower"))
      ;; The learner works in the lower window; a timer runs with the
      ;; upper one selected.
      (cl-letf (((symbol-function 'playground--user-window) (lambda () lower)))
        (let ((r (pg-test-value
                  (pg-test-call "check"
                                `((forms . ,(vector "(not (eq (selected-window) (frame-first-window)))"
                                                    "(equal (buffer-name) \"pg-lower\")")))))))
          (should (equal (alist-get 'results r) '(t t))))))
    (delete-other-windows)
    (kill-buffer "pg-upper")
    (kill-buffer "pg-lower")))

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
  "The same message shown again before another key is sent once."
  (pg-test-with-agent
    (pg-test-clear)
    (playground--on-message "Hello")
    (playground--on-message "Hello")
    (playground--on-message "Bye")
    (playground--on-message "Bye")
    ;; After a key, the same text is news again.
    (let ((num-input-keys (1+ num-input-keys)))
      (playground--on-message "Bye"))
    (pg-test-settle)
    (should (equal (mapcar (lambda (e) (alist-get 'text e)) (pg-test-messages "echo"))
                   '("Hello" "Bye" "Bye")))))

(ert-deftest pg-agent/rescue-message-repeats ()
  "Each Rescue says what it did, also when it says the same as the last one."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer "*scratch*")
    (pg-test-clear)
    ;; (A batch Emacs prints messages without `set-message-function'.)
    (let ((said "Rescue: nothing to undo; you were already in a normal state."))
      (pg-test-rescue)
      (playground--on-message said)
      (pg-test-rescue)
      (playground--on-message said)
      (pg-test-settle)
      (should (equal (mapcar (lambda (e) (alist-get 'text e)) (pg-test-messages "echo"))
                     (list said said))))))

(ert-deftest pg-agent/isearch-count-is-noise ()
  "Isearch prompts are not echo messages, with or without a match count."
  (dolist (text '("I-search: greet" "1/4 I-search: greet" "?/? I-search: g"
                  "0/0 Failing I-search: zzz" "2/3 I-search backward: fn"
                  "1/2+1 Wrapped I-search: x"))
    (should (playground--noise-p text)))
  (should-not (playground--noise-p "1/4 files copied")))

(ert-deftest pg-agent/displayed-prompt ()
  "A prompt is reported as displayed: invisible text out, display strings in."
  (with-temp-buffer
    (insert "Replace "
            (propertize "in region (0L)" 'invisible 'region)
            (propertize "from point" 'invisible 'from-point)
            (propertize "in buffer" 'invisible 'full)
            ": colour"
            (propertize " " 'display " ->? ")
            "color")
    (setq buffer-invisibility-spec '(full region t))
    (should (equal (playground--displayed-text (point-min) (point-max))
                   "Replace from point: colour ->? color"))
    (setq buffer-invisibility-spec '(from-point region t))
    (should (equal (playground--displayed-text (point-min) 44)
                   "Replace in buffer: "))))

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
        (setq snap (pg-test--spec snap))
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

(defun pg-test--shown-files ()
  "Files of the selected frame's windows, top to bottom, left to right."
  (mapcar (lambda (w) (buffer-file-name (window-buffer w)))
          (window-list nil 'nomini (frame-first-window))))

(ert-deftest pg-agent/reset-step-by-file ()
  "Reset this step puts each window back on its file and closes files opened since."
  (pg-test-with-agent
    (let ((hello (pg-test-file "projects/hello/README.md" "# hello\n"))
          (hello-main (pg-test-file "projects/hello/src/main.rs" "fn main() {}\n"))
          (poly (pg-test-file "projects/polyglot/README.md" "# polyglot\n"))
          (poly-main (pg-test-file "projects/polyglot/src/main.rs" "fn main() {}\n"))
          (uniquify-buffer-name-style 'post-forward-angle-brackets))
      ;; The step starts on hello's README.md, the only README.md open.
      (delete-other-windows)
      (find-file hello)
      (should (equal (buffer-name) "README.md"))
      (let ((snap (pg-test-value (pg-test-call "snapshot"))))
        (should (string-match-p "(file \\. " (alist-get 'windowState snap)))
        ;; During the step: hello's main.rs, polyglot's main.rs and
        ;; README.md; the window ends on polyglot's README.md, and the
        ;; first README.md is now README.md<hello>.
        (find-file hello-main)
        (find-file poly-main)
        (find-file poly)
        (should (equal (buffer-name (get-file-buffer hello)) "README.md<hello>"))
        (setq snap (pg-test--spec snap))
        (pg-test-value (pg-test-call "reset-step" `((spec . ,snap)))))
      (should (equal (pg-test--shown-files) (list hello)))
      (should (equal (buffer-name (window-buffer (selected-window))) "README.md"))
      (should (equal (buffer-local-value 'default-directory (window-buffer (selected-window)))
                     (file-name-directory hello)))
      (dolist (f (list hello-main poly poly-main))
        (should-not (get-file-buffer f))))))

(ert-deftest pg-agent/open-finds-renamed-buffers ()
  "open shows the file a window showed, whatever its buffer is called now."
  (pg-test-with-agent
    (let ((hello (pg-test-file "projects/hello/README.md" "# hello\n"))
          (poly (pg-test-file "projects/polyglot/README.md" "# polyglot\n"))
          (main (pg-test-file "projects/hello/src/main.rs" "fn main() {}\n"))
          (uniquify-buffer-name-style 'post-forward-angle-brackets))
      (delete-other-windows)
      (find-file main)
      (split-window-below)
      (other-window 1)
      (find-file hello)
      (let ((snap (pg-test-value (pg-test-call "snapshot"))))
        ;; Another README.md opens and takes the lower window.
        (find-file poly)
        (other-window 1)
        (setq snap (pg-test--spec snap))
        (pg-test-value (pg-test-call "open" snap))
        (should (equal (pg-test--shown-files) (list main hello)))
        (should (equal (buffer-file-name (window-buffer (selected-window))) hello))
        ;; A state saved by an older agent, without file names, is
        ;; matched through the spec's files by the buffer's base name.
        (switch-to-buffer (get-file-buffer poly))
        (let ((old (replace-regexp-in-string " (file \\. \"[^\"]*\")" ""
                                             (alist-get 'windowState snap))))
          (should-not (string-match-p "(file \\. " old))
          (setf (alist-get 'windowState snap) old)
          (pg-test-value (pg-test-call "open" snap)))
        (should (equal (pg-test--shown-files) (list main hello)))))))

(defun pg-test--spec (snap)
  "SNAP, a parsed snapshot, ready to be sent back as a WindowSpec."
  (setf (alist-get 'files snap) (vconcat (alist-get 'files snap)))
  (when (alist-get 'tabs snap)
    (setf (alist-get 'tabs snap) (vconcat (alist-get 'tabs snap))))
  snap)

(ert-deftest pg-agent/reset-step-remakes-dired ()
  "A window on a Dired listing comes back on that listing, up to date."
  (pg-test-with-agent
    (let* ((brief (pg-test-file "inbox/brief.md" "# brief\n"))
           (notes (pg-test-file "inbox/notes.md" "# notes\n"))
           (dir (file-name-directory brief)))
      (delete-other-windows)
      (find-file brief)
      (dired dir)
      (dired-goto-file notes)
      (let ((snap (pg-test-value (pg-test-call "snapshot"))))
        (should (string-match-p "(dired \\. " (alist-get 'windowState snap)))
        ;; The demo moves notes.md away and leaves the listing; a reset
        ;; setup would have killed it.  The restore puts the file back.
        (make-directory (expand-file-name "docs" dir))
        (rename-file notes (expand-file-name "docs/notes.md" dir))
        (kill-buffer (current-buffer))
        (switch-to-buffer (get-file-buffer brief))
        (rename-file (expand-file-name "docs/notes.md" dir) notes)
        (pg-test-value (pg-test-call "reset-step" `((spec . ,(pg-test--spec snap))))))
      (let ((b (window-buffer (selected-window))))
        (should (eq (buffer-local-value 'major-mode b) 'dired-mode))
        (with-current-buffer b
          (should (equal (expand-file-name default-directory) dir))
          (should (save-excursion (dired-goto-file notes)))
          (should (equal (dired-get-filename nil t) notes)))))))

(ert-deftest pg-agent/reset-step-ibuffer-filters ()
  "ibuffer comes back with the filters and marks it had when the step started."
  (pg-test-with-agent
    (let ((a (pg-test-file "a.rs" "fn a() {}\n"))
          (b (pg-test-file "b.md" "# b\n")))
      (find-file-noselect a)
      (find-file-noselect b)
      (delete-other-windows)
      (ibuffer)
      (let ((snap (pg-test-value (pg-test-call "snapshot"))))
        (should (string-match-p "(ibuffer \\. t)" (alist-get 'windowState snap)))
        ;; The demo filters the list and marks a buffer.
        (setq ibuffer-filtering-qualifiers '((filename . "rs$")))
        (ibuffer-update nil t)
        (ibuffer-mark-on-buffer (lambda (x) (eq x (get-file-buffer a))) ?>)
        (pg-test-value (pg-test-call "reset-step" `((spec . ,(pg-test--spec snap))))))
      (let ((ib (window-buffer (selected-window))))
        (should (eq (buffer-local-value 'major-mode ib) 'ibuffer-mode))
        (with-current-buffer ib
          (should-not ibuffer-filtering-qualifiers)
          (should-not (seq-find (lambda (e) (not (eq (cdr e) ?\s)))
                                (ibuffer-current-state-list))))))))

(ert-deftest pg-agent/reset-step-tabs ()
  "reset-step closes the tabs a step opened and reopens the ones it closed."
  (pg-test-with-agent
    (require 'tab-bar)
    (let ((main (pg-test-file "hello/main.rs" "fn main() {}\n")))
      (unwind-protect
          (progn
            (delete-other-windows)
            (find-file main)
            (tab-bar-rename-tab "hello")
            (let ((one (pg-test-value (pg-test-call "snapshot"))))
              (should (equal (alist-get 'tabs one) '("hello")))
              ;; A demo opens a project tab (C-x p p).
              (tab-bar-new-tab)
              (tab-bar-rename-tab "polyglot")
              (let ((two (pg-test-value (pg-test-call "snapshot"))))
                (should (equal (alist-get 'tabs two) '("hello" "polyglot")))
                (should (equal (alist-get 'tabIndex two) 2))
                (pg-test-value (pg-test-call "reset-step" `((spec . ,(pg-test--spec one)))))
                (should (equal (mapcar (lambda (tab) (alist-get 'name tab)) (funcall tab-bar-tabs-function))
                               '("hello")))
                ;; The next step started with both tabs, polyglot current;
                ;; its demo closed polyglot.
                (pg-test-value (pg-test-call "reset-step" `((spec . ,(pg-test--spec two)))))
                (should (equal (mapcar (lambda (tab) (alist-get 'name tab)) (funcall tab-bar-tabs-function))
                               '("hello" "polyglot")))
                (should (equal (alist-get 'name (tab-bar--current-tab)) "polyglot")))))
        (tab-bar-close-other-tabs)
        (tab-bar-rename-tab "")))))

(ert-deftest pg-agent/reset-step-help-buffers ()
  "Help buffers killed in a step come back; help buffers opened in it go."
  (pg-test-with-agent
    (save-window-excursion
      (describe-function 'find-file)
      (with-current-buffer (help-buffer) (rename-uniquely))
      (describe-function 'save-buffer))
    (let* ((names (sort (mapcar #'buffer-name (seq-filter #'playground--help-buffer-p (buffer-list)))
                        #'string<))
           (snap (pg-test-value (pg-test-call "snapshot"))))
      (should (= (length names) 2))
      ;; The demo kills them (ibuffer's * h D) and the learner opens another.
      (dolist (b (seq-filter #'playground--help-buffer-p (buffer-list))) (kill-buffer b))
      (save-window-excursion (describe-function 'kill-buffer))
      (pg-test-value (pg-test-call "reset-step" `((spec . ,(pg-test--spec snap)))))
      (should (equal (sort (mapcar #'buffer-name (seq-filter #'playground--help-buffer-p (buffer-list)))
                           #'string<)
                     names))
      (should (string-match-p "find-file\\|save-buffer"
                              (with-current-buffer (car names) (buffer-substring 1 (min 200 (point-max))))))
      (dolist (b (seq-filter #'playground--help-buffer-p (buffer-list))) (kill-buffer b)))))

;; `compile' that never asks to save and waits for the run to end.
(defun pg-test--build (command &optional name)
  "Run COMMAND as a compilation in buffer NAME (default *compilation*).
Return the buffer once the run finished."
  (let* ((compilation-ask-about-save nil)
         (compilation-buffer-name-function (lambda (_) (or name "*compilation*")))
         (default-directory pg-test--dir)
         (b (save-window-excursion (compile command))))
    (should (pg-test-wait (lambda () (not (get-buffer-process b))) 10))
    b))

(ert-deftest pg-agent/reset-step-drops-later-builds ()
  "Build output made since the checkpoint goes, unless its window is part of the step.
Otherwise a demonstration's *compilation* would make \"the build
finished\" hold before the learner builds anything (lesson 2.7)."
  (pg-test-with-agent
    (find-file-noselect (pg-test-file "a.txt" "a\n"))
    (let* ((early (pg-test--build "echo early" "*early*"))
           (shown (pg-test--build "echo shown" "*shown*")))
      (switch-to-buffer (get-file-buffer (expand-file-name "a.txt" pg-test--dir)))
      (delete-other-windows)
      (set-window-buffer (split-window-below) shown)
      (should (buffer-local-value 'playground--run-started early))
      (let ((snap (pg-test-value (pg-test-call "snapshot"))))
        (sleep-for 0.01)
        ;; The demonstration builds, and so does the learner in the shown window.
        (let ((later (pg-test--build "echo Finished dev profile"))
              (again (pg-test--build "echo shown again" "*shown*")))
          (should (eq again shown))
          (should (> (buffer-local-value 'playground--run-started later)
                     (alist-get 'takenAt snap)))
          (let ((r (pg-test-value (pg-test-call "reset-step" `((spec . ,(pg-test--spec snap)))))))
            (should (>= (alist-get 'killed r) 1)))
          (should-not (buffer-live-p later))
          (should (buffer-live-p early))
          (should (buffer-live-p shown))
          (should (get-buffer-window shown))
          (dolist (b (list early shown)) (kill-buffer b)))))))

(ert-deftest pg-agent/snapshot-with-propertized-buffer-names ()
  "A buffer name with text properties still gives a readable window state.
uniquify can give a name back with the buffer in a text property; an
unreadable state would make reset-step fall back to the layout pattern
and lose the side window."
  (pg-test-with-agent
    (let* ((a (find-file-noselect (pg-test-file "a.txt" "a\n")))
           (b (find-file-noselect (pg-test-file "b.txt" "b\n")))
           (side (get-buffer-create "*side*")))
      (with-current-buffer a (rename-buffer (propertize "a.txt<x>" 'uniquify-orig-buffer a)))
      (should (text-properties-at 0 (buffer-name a)))
      (delete-other-windows)
      (switch-to-buffer a)
      (switch-to-buffer b)
      (display-buffer-in-side-window side '((side . right)))
      (let ((snap (pg-test-value (pg-test-call "snapshot"))))
        (should-not (string-search "#<" (alist-get 'windowState snap)))
        (should (car (read-from-string (alist-get 'windowState snap))))
        ;; The step's demo hides the side window (C-x w s); Reset brings it back.
        (window-toggle-side-windows)
        (should-not (get-buffer-window side))
        (pg-test-value (pg-test-call "reset-step" `((spec . ,(pg-test--spec snap)))))
        (should (eq (window-parameter (get-buffer-window side) 'window-side) 'right)))
      (kill-buffer side)))
  ;; A state printed with buffer objects reads them as nil.
  (should (equal (playground--read-state
                  "((buffer #(\"main.rs\" 0 1 (uniquify-orig-buffer #<buffer main.rs>)) (point . 1)) (prev-buffers (#(\"README.md<hello>\" 0 1 (x #<buffer README.md<hello>>)) 1 1)))")
                 '((buffer "main.rs" (point . 1)) (prev-buffers ("README.md<hello>" 1 1))))))

(ert-deftest pg-agent/op-setup-reset-starts-at-the-top ()
  "With reset, the files of a setup start on their first line, in their windows too."
  (pg-test-with-agent
    (let ((readme (pg-test-file "hello/README.md" "one\ntwo\nthree\nfour\nfive\nsix\n"))
          (main (pg-test-file "hello/src/main.rs" "fn main() {}\n")))
      (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/src/main.rs" "hello/README.md"))
                                             (layout . "(vsplit hello/src/main.rs hello/README.md)")
                                             (reset . t)
                                             (workspace . ,pg-test--dir))))
      ;; The learner moved to line 5 of README.md in the lower window.
      (let ((w (get-buffer-window (get-file-buffer readme))))
        (with-selected-window w (goto-char (point-min)) (forward-line 4))
        (should (= (with-current-buffer (get-file-buffer readme)
                     (line-number-at-pos (window-point w)))
                   5)))
      (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/src/main.rs" "hello/README.md"))
                                             (layout . "(vsplit hello/src/main.rs hello/README.md)")
                                             (reset . t)
                                             (workspace . ,pg-test--dir))))
      (let ((w (get-buffer-window (get-file-buffer readme))))
        (should (= (window-point w) 1))
        (should (= (window-start w) 1))
        (should (= (with-current-buffer (get-file-buffer readme) (point)) 1)))
      (should (get-file-buffer main))
      ;; Without reset (a step's own setup) the position stays.
      (let ((w (get-buffer-window (get-file-buffer readme))))
        (with-selected-window w (goto-char (point-min)) (forward-line 2))
        (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/src/main.rs" "hello/README.md"))
                                               (layout . "(vsplit hello/src/main.rs hello/README.md)")
                                               (workspace . ,pg-test--dir))))
        (should (= (with-current-buffer (get-file-buffer readme)
                     (line-number-at-pos (window-point (get-buffer-window (get-file-buffer readme)))))
                   3))))))

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

(ert-deftest pg-agent/op-setup-after-fixture-reset ()
  "setup visits files rewritten on disk without asking, even if their buffers were edited."
  (pg-test-with-agent
    (let ((main (pg-test-file "hello/src/main.rs" "fn main() {}\n"))
          (gone (pg-test-file "hello/old.rs" "// old\n")))
      (with-current-buffer (find-file-noselect main)
        (goto-char (point-max))
        (insert "// unsaved edit\n"))
      (find-file-noselect gone)
      ;; sessiond extracts the fixtures again (setup.reset).
      (with-temp-file main (insert "fn main() { println!(\"fresh\"); }\n"))
      (set-file-times main (time-add nil 2))
      (delete-file gone)
      (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/src/main.rs"))
                                             (layout . "single")
                                             (workspace . ,pg-test--dir))))
      (with-current-buffer (get-file-buffer main)
        (should-not (buffer-modified-p))
        (should (string-match-p "fresh" (buffer-string))))
      (should-not (get-file-buffer gone)))))

(ert-deftest pg-agent/op-setup-reset-clears-the-desk ()
  "With reset, setup kills unmodified leftovers and keeps modified buffers."
  (pg-test-with-agent
    (let ((a (pg-test-file "p/a.rs" "a\n"))
          (b (pg-test-file "p/b.rs" "b\n"))
          (c (pg-test-file "p/c.rs" "c\n")))
      (find-file-noselect a)
      (find-file-noselect b)
      (with-current-buffer (find-file-noselect c) (insert "edit"))
      (get-buffer-create "junk.rs")
      (dired-noselect (file-name-directory a))
      (with-current-buffer (get-buffer-create "*Ibuffer*") (ibuffer-mode))
      (pg-test-value (pg-test-call "setup" `((open . ,(vector "p/a.rs"))
                                             (layout . "single")
                                             (reset . t)
                                             (workspace . ,pg-test--dir))))
      (should (get-file-buffer a))
      (should-not (get-file-buffer b))
      (should (get-file-buffer c))
      (should-not (get-buffer "junk.rs"))
      (should-not (get-buffer "*Ibuffer*"))
      (should-not (seq-find (lambda (x) (eq (buffer-local-value 'major-mode x) 'dired-mode)) (buffer-list)))
      (should (get-buffer "*scratch*"))
      (with-current-buffer (get-file-buffer c) (set-buffer-modified-p nil)))))

(ert-deftest pg-agent/op-setup-reset-forgets-places ()
  "With reset and a root, setup forgets saved places under the root only."
  (require 'saveplace)
  (pg-test-with-agent
    (let* ((a (pg-test-file "p/a.rs" "line 1\nline 2\nline 3\n"))
           (b (pg-test-file "p/b.rs" "line 1\nline 2\nline 3\n"))
           (c (pg-test-file "q/c.rs" "line 1\nline 2\nline 3\n"))
           (save-place-file (expand-file-name "places" pg-test--dir))
           (save-place-loaded t)
           ;; An earlier attempt left a.rs on line 3; c.rs is another lesson's.
           (save-place-alist (list (cons a 15) (cons c 8)))
           (was-on save-place-mode))
      (unwind-protect
          (progn
            (save-place-mode 1)
            ;; A leftover buffer: killing it (the desk is cleared) saves its place.
            (with-current-buffer (find-file-noselect b) (goto-char 10))
            (pg-test-value (pg-test-call "setup" `((open . ,(vector "p/a.rs"))
                                                   (layout . "single")
                                                   (reset . t)
                                                   (root . "p")
                                                   (workspace . ,pg-test--dir))))
            (should-not (get-file-buffer b))
            (should-not (assoc a save-place-alist))
            (should-not (assoc b save-place-alist))
            (should (equal (cdr (assoc c save-place-alist)) 8))
            (should (= (with-current-buffer (get-file-buffer a) (point)) 1)))
        (unless was-on (save-place-mode -1))))))

(ert-deftest pg-agent/op-setup-without-reset-keeps-places ()
  "Without reset, saved places stay (save-place is taught in lesson 2.8)."
  (require 'saveplace)
  (pg-test-with-agent
    (let* ((a (pg-test-file "p/a.rs" "line 1\nline 2\nline 3\n"))
           (save-place-file (expand-file-name "places" pg-test--dir))
           (save-place-loaded t)
           (save-place-alist (list (cons a 15))))
      (pg-test-value (pg-test-call "setup" `((open . ,(vector "p/a.rs"))
                                             (layout . "single")
                                             (root . "p")
                                             (workspace . ,pg-test--dir))))
      (should (equal (cdr (assoc a save-place-alist)) 15)))))

(ert-deftest pg-agent/op-setup-leaves-open-prompt ()
  "With a prompt open, setup unwinds first, then runs at top level and replies."
  (pg-test-with-agent
    (pg-test-file "hello/README.md" "# hello\n")
    (let* ((unwound nil)
           (playground-agent-rescue-unwind-function (lambda () (setq unwound t)))
           (depth 1))
      (cl-letf (((symbol-function 'recursion-depth) (lambda () depth)))
        (let ((reply (pg-test-call "setup" `((open . ,(vector "hello/README.md"))
                                             (layout . "hello/README.md")
                                             (workspace . ,pg-test--dir)))))
          (should unwound)
          (pg-test-value reply)
          (should (equal (buffer-name (window-buffer (selected-window))) "README.md")))))))

(ert-deftest pg-agent/delete-frame-reports-the-last-frame ()
  "Deleting the only frame of a terminal emits frame closed."
  (pg-test-with-agent
    (cl-letf (((symbol-function 'playground--top-level-frame-p) (lambda (_f) t)))
      (should (eq (playground--last-frame-on-terminal-p (selected-frame)) t))
      (playground--on-delete-frame (selected-frame))
      (should (pg-test-wait
               (lambda () (seq-find (lambda (m) (equal (alist-get 'event m) "closed"))
                                    (pg-test-messages "frame"))))))))

(ert-deftest pg-agent/ops-never-prompt ()
  "An op that would prompt fails with an error reply instead of blocking."
  (pg-test-with-agent
    (let* ((reply (pg-test-call "eval" '((form . "(y-or-n-p \"Really? \")"))))
           (ok (alist-get 'ok reply)))
      (should (memq ok '(:false nil)))
      (should (= (minibuffer-depth) 0)))))

;;;; Side windows and automatic saving across setups

(defun pg-test--side-window (buffer-name)
  "Show a new special buffer BUFFER-NAME in a right side window and select it."
  (let* ((b (get-buffer-create buffer-name))
         (w (progn (with-current-buffer b (special-mode))
                   (display-buffer-in-side-window b '((side . right) (window-width . 0.3))))))
    (select-window w)
    w))

(defun pg-test--splits-p ()
  "Non-nil when the selected window splits both ways (C-x 2, C-x 3)."
  (let ((ok (and (window-live-p (split-window-below))
                 (window-live-p (split-window-right)))))
    (delete-other-windows)
    ok))

(ert-deftest pg-agent/op-setup-from-a-side-window ()
  "A setup started while a side window is selected leaves normal windows."
  (pg-test-with-agent
    (pg-test-file "hello/README.md" "# hello\n")
    (pg-test-file "hello/src/main.rs" "fn main() {}\n")
    (delete-other-windows)
    (dolist (layout '("single" "(vsplit main.rs README.md)" "hello/src/main.rs"))
      (pg-test--side-window "*pg-side-help*")
      (should (window-parameter (selected-window) 'window-side))
      (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/src/main.rs" "hello/README.md"))
                                             (layout . ,layout)
                                             (workspace . ,pg-test--dir))))
      (should-not (seq-find (lambda (w) (window-parameter w 'window-side)) (window-list nil 'nomini)))
      (should-not (window-dedicated-p (selected-window)))
      (should (buffer-file-name (window-buffer (selected-window))))
      (should (= (length (window-list nil 'nomini)) (if (string-prefix-p "(vsplit" layout) 2 1)))
      (should (pg-test--splits-p)))
    (kill-buffer "*pg-side-help*")))

(ert-deftest pg-agent/rescue-sole-side-window ()
  "Rescue repairs a side window that became the frame's only window."
  (pg-test-with-agent
    (delete-other-windows)
    (find-file (pg-test-file "s.txt"))
    (pg-test--side-window "*pg-side-help*")
    ;; Delete the other windows from the side window, ignoring its parameters.
    (let ((ignore-window-parameters t)) (delete-other-windows))
    (should (= (length (window-list nil 'nomini)) 1))
    (should (window-parameter (selected-window) 'window-side))
    (should (member "made the window splittable again" (pg-test-rescue)))
    (should-not (window-parameter (selected-window) 'window-side))
    (should (pg-test--splits-p))
    (switch-to-buffer "*scratch*")
    (kill-buffer "*pg-side-help*")))

(ert-deftest pg-agent/op-setup-reset-restores-auto-save ()
  "A reset setup turns base's auto-save-visited-mode back on before its eval."
  (pg-test-with-agent
    (pg-test-file "hello/README.md" "# hello\n")
    (let ((was auto-save-visited-mode))
      (unwind-protect
          (cl-letf (((symbol-function 'playground-module-p) (lambda (m) (eq m 'base))))
            (auto-save-visited-mode -1)
            ;; A lesson that teaches the modified flag turns it off again.
            (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/README.md")) (layout . "single")
                                                   (reset . t)
                                                   (eval . "(progn (setq pg-test--saw-auto-save auto-save-visited-mode) (auto-save-visited-mode -1))")
                                                   (workspace . ,pg-test--dir))))
            (should pg-test--saw-auto-save)
            (should-not auto-save-visited-mode)
            ;; The next lesson starts with it on.
            (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/README.md")) (layout . "single")
                                                   (reset . t) (workspace . ,pg-test--dir))))
            (should auto-save-visited-mode)
            ;; Without reset (a step's own setup) nothing changes.
            (auto-save-visited-mode -1)
            (pg-test-value (pg-test-call "setup" `((open . ,(vector "hello/README.md")) (layout . "single")
                                                   (workspace . ,pg-test--dir))))
            (should-not auto-save-visited-mode))
        (auto-save-visited-mode (if was 1 -1))))))
(defvar pg-test--saw-auto-save nil)

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

(ert-deftest pg-agent/waiting-loops ()
  "A loop reading keys of its own (a tty menu, avy, which-key) is in the state."
  (pg-test-with-agent
    (should (eq (alist-get 'waiting (playground--state)) :null))
    (should (advice-member-p 'playground-waiting 'popup-menu))
    (should (advice-member-p 'playground-waiting 'avy-read))
    (let (inside)
      (cl-letf (((symbol-function 'pg-test--loop)
                 (lambda () (setq inside (alist-get 'waiting (playground--state))) 'chosen)))
        (let ((playground-agent-waiting-loops
               '((pg-test--loop "menu" "a menu" "closed the menu" "C-g"))))
          (advice-add 'pg-test--loop :around
                      (playground--waiting-advice (car playground-agent-waiting-loops))
                      '((name . playground-waiting)))
          (should (eq (pg-test--loop) 'chosen))
          (advice-remove 'pg-test--loop 'playground-waiting)))
      (should (equal inside '((kind . "menu") (what . "a menu") (leave . "C-g")))))
    (should (eq (alist-get 'waiting (playground--state)) :null))))

(ert-deftest pg-agent/rescue-waiting-loop ()
  "Rescue leaves a waiting loop with a C-g it reads, then finishes and replies."
  (pg-test-with-agent
    (delete-other-windows)
    (switch-to-buffer "*scratch*")
    (let ((unread-command-events nil)
          (playground--waiting '(popup-menu "menu" "a menu" "closed the menu" "C-g"))
          (reply nil))
      (let ((id (cl-incf pg-test--next-id)))
        (process-send-string
         pg-test--client
         (concat (json-serialize `((t . "call") (id . ,id) (op . "rescue")
                                   (args . ,(make-hash-table)) (timeoutMs . 2000)))
                 "\n"))
        ;; The loop reads the C-g...
        (should (pg-test-wait (lambda () (equal unread-command-events '(?\C-g))) 2))
        (setq unread-command-events nil)
        ;; ...and ends; rescue goes on.
        (setq playground--waiting nil)
        (setq reply (pg-test-wait
                     (lambda ()
                       (seq-find (lambda (m) (and (equal (alist-get 't m) "reply")
                                                  (equal (alist-get 'id m) id)))
                                 pg-test--messages))
                     3)))
      (should (equal (alist-get 'did (pg-test-value reply)) '("closed the menu"))))))

;;;; Frames

(ert-deftest pg-agent/copy-windows ()
  "A frame given another frame's window state shows the same windows, same one selected."
  (pg-test-with-agent
    (delete-other-windows)
    (let ((a (find-file-noselect (pg-test-file "a.txt")))
          (b (find-file-noselect (pg-test-file "b.txt"))))
      (switch-to-buffer a)
      (split-window-below)
      (other-window 1)
      (switch-to-buffer b)
      (let ((state (playground--frame-windows (selected-frame))))
        ;; Sized by proportions: a GUI frame's pixels do not fit a terminal.
        (should-not (string-match-p "(pixel-\\(?:width\\|height\\) \\." (format "%S" state)))
        (should (string-match-p "normal-height" (format "%S" state)))
        (delete-other-windows)
        (switch-to-buffer "*scratch*")
        (playground--copy-windows state (selected-frame)))
      (should (equal (mapcar (lambda (w) (buffer-name (window-buffer w)))
                             (window-list nil 'nomini (frame-first-window)))
                     '("a.txt" "b.txt")))
      (should (eq (window-buffer (selected-window)) b)))))

(ert-deftest pg-agent/reset-step-tabs-in-a-new-frame ()
  "A frame with one unnamed tab gets the spec's named tab by renaming, not a second tab."
  (pg-test-with-agent
    (require 'tab-bar)
    (let ((main (pg-test-file "hello/main.rs" "fn main() {}\n")))
      (unwind-protect
          (progn
            (delete-other-windows)
            (find-file main)
            (tab-bar-rename-tab "hello")
            (let ((spec (pg-test--spec (pg-test-value (pg-test-call "snapshot")))))
              ;; The GUI frame made since starts with one unnamed tab.
              (tab-bar-rename-tab "")
              (pg-test-value (pg-test-call "reset-step" `((spec . ,spec))))
              (should (equal (mapcar (lambda (tab) (alist-get 'name tab))
                                     (funcall tab-bar-tabs-function))
                             '("hello")))))
        (tab-bar-close-other-tabs)
        (tab-bar-rename-tab "")))))

(ert-deftest pg-agent/op-setup-reset-forgets-tab-names ()
  "A reset setup leaves one tab without the name an earlier lesson gave it."
  (pg-test-with-agent
    (require 'tab-bar)
    (pg-test-file "p/a.rs" "a\n")
    (unwind-protect
        (progn
          (tab-bar-rename-tab "hello")
          (pg-test-value (pg-test-call "setup" `((open . ,(vector "p/a.rs"))
                                                 (layout . "single")
                                                 (reset . t)
                                                 (workspace . ,pg-test--dir))))
          (should (= (length (funcall tab-bar-tabs-function)) 1))
          (should-not (alist-get 'explicit-name (tab-bar--current-tab))))
      (tab-bar-rename-tab ""))))

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

;; A throw out of the repeating ping timer would leave it marked as
;; triggered, and Emacs would never run it again (no pings, every reply
;; queued behind a "stalled" peer).
(ert-deftest pg-agent/ping-timer-survives-a-throw ()
  (let ((playground--ping-timer (timer-create))
        (playground--connected t)
        (playground--last-pong nil))
    (setf (timer--triggered playground--ping-timer) t)
    (cl-letf (((symbol-function 'playground--send-line)
               (lambda (&rest _) (throw 'pg-test-out nil))))
      (catch 'pg-test-out (playground--ping)))
    (should-not (timer--triggered playground--ping-timer))))

(ert-deftest pg-agent/remote-read-only-without-tramp ()
  "The read-only reason of a remote file asks the file name handler nothing
but whether the name is remote (no I/O on the connection)."
  (let ((file-name-handler-alist
         `(("\\`/ssh:" . ,(lambda (op &rest args)
                              (if (eq op 'file-remote-p)
                                  "/ssh:box:"
                                (error "TRAMP call %s %S" op args)))))))
    (with-temp-buffer
      (setq buffer-file-name "/ssh:box:/etc/app/app.env"
            buffer-read-only t)
      (should (equal (playground--read-only-reason) "file is write-protected"))
      (setq buffer-file-name nil))))

(provide 'agent-test)
;;; agent-test.el ends here
