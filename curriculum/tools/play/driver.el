;;; driver.el --- replay a scenario's demos in a real Emacs  -*- lexical-binding: t; -*-

;; Loaded by curriculum/tools/play/play.py inside the pg-curriculum-play
;; container, after assumed-config.el. Reads /job/job.json, applies the
;; scenario's setup, then for every step feeds the demo to the command
;; loop as if typed (chunk by chunk through `unread-command-events', so
;; C-g and prompts behave as they do interactively) and records a
;; snapshot after every command: command counts since the step started,
;; a state object shaped like the agent's (api.md §5), the results of the
;; step's elisp conditions and the contents of the files its file
;; conditions read. play.py evaluates the conditions on those snapshots.

(require 'json)
(require 'cl-lib)
(require 'subr-x)

(defvar pg-job nil)
(defvar pg-results nil)
(defvar pg-step nil "Plist of the step being replayed.")
(defvar pg-counts nil "Hash table command name -> count since the step started.")
(defvar pg-snapshots nil)
(defvar pg-chunks nil)
(defvar pg-step-index 0)
(defvar pg-step-started 0)
(defvar pg-last-activity 0)
(defvar pg-next-window-id 0)
(defvar pg-timer nil)
(defvar pg-errors nil)
(defvar pg-log nil "Progress messages, not errors.")
(defvar pg-recording nil)

(defun pg-get (obj key) (gethash key obj))

(defun pg-window-id (w)
  (or (window-parameter w 'pg-id)
      (set-window-parameter w 'pg-id (cl-incf pg-next-window-id))))

(defun pg-main-window ()
  (let ((w (selected-window)))
    (if (minibuffer-window-active-p w)
        (or (minibuffer-selected-window) w)
      w)))

(defun pg-layout (tree)
  (if (windowp tree)
      (pg-window-id tree)
    (vconcat (list (if (car tree) "vsplit" "hsplit"))
             (mapcar #'pg-layout (cddr tree)))))

(defun pg-or-null (v) (if v v :null))

(defun pg-state ()
  (let* ((sel (selected-window))
         (main (pg-main-window))
         (buf (window-buffer sel))
         (mini (cond ((active-minibuffer-window)
                      (with-current-buffer (window-buffer (active-minibuffer-window))
                        `((depth . ,(minibuffer-depth))
                          (prompt . ,(substring-no-properties (or (minibuffer-prompt) "")))
                          (contents . ,(minibuffer-contents-no-properties))
                          (kind . "prompt"))))
                     ((bound-and-true-p isearch-mode)
                      `((depth . 0) (prompt . "I-search: ")
                        (contents . ,(substring-no-properties (or isearch-string ""))) (kind . "isearch")))
                     (t :null))))
    (with-current-buffer buf
      `((buf . ,(buffer-name))
        (file . ,(pg-or-null (buffer-file-name)))
        (mode . ,(symbol-name major-mode))
        (modeName . ,(format-mode-line mode-name))
        (minors . ,(vconcat (cl-loop for m in '(overwrite-mode iedit-mode multiple-cursors-mode follow-mode
                                                 olivetti-mode visual-line-mode view-mode rectangle-mark-mode
                                                 auto-revert-mode text-scale-mode display-line-numbers-mode
                                                 wdired-mode)
                                     when (and (boundp m) (symbol-value m)) collect (symbol-name m))))
        (modified . ,(if (buffer-modified-p) t :false))
        (readonly . ,(if buffer-read-only t :false))
        (narrowed . ,(if (buffer-narrowed-p) t :false))
        (overwrite . ,(if overwrite-mode t :false))
        (line . ,(line-number-at-pos))
        (col . ,(current-column))
        (point . ,(point))
        (markActive . ,(if (region-active-p) t :false))
        (windows . ,(vconcat
                     (mapcar (lambda (w)
                               `((id . ,(pg-window-id w))
                                 (buf . ,(buffer-name (window-buffer w)))
                                 (mode . ,(symbol-name (buffer-local-value 'major-mode (window-buffer w))))
                                 (selected . ,(if (eq w main) t :false))
                                 (dedicated . ,(if (window-dedicated-p w) t :false))
                                 (side . ,(let ((s (window-parameter w 'window-side))) (if s (symbol-name s) :null)))
                                 (height . ,(window-body-height w))
                                 (totalHeight . ,(window-total-height w))
                                 (width . ,(window-body-width w))))
                             (window-list nil 'no-minibuf (frame-first-window)))))
        (layout . ,(pg-layout (car (window-tree))))
        (frame . "terminal")
        (frames . ,(length (frame-list)))
        (tabs . ((count . ,(if (fboundp 'tab-bar-tabs) (length (funcall tab-bar-tabs-function)) 1))
                 (current . ,(or (and (fboundp 'tab-bar--current-tab) (alist-get 'name (tab-bar--current-tab))) ""))))
        (minibuffer . ,mini)
        (isearch . ,(if (bound-and-true-p isearch-mode) t :false))
        (recursive . ,(recursion-depth))
        (macro . ,(if defining-kbd-macro t :false))
        (prefixArg . ,(let ((a (or prefix-arg current-prefix-arg))) (if a (format "%S" a) :null)))))))

(defun pg-eval-condition (form)
  (condition-case nil
      (with-temp-buffer
        (if (eval (car (read-from-string form)) t) t :false))
    (error "unknown")))

(defun pg-read-file (path)
  (let ((f (expand-file-name path "/work/")))
    (if (file-regular-p f)
        (with-temp-buffer
          (insert-file-contents f)
          (buffer-string))
      :null)))

(defun pg-counts-alist ()
  (let (out)
    (maphash (lambda (k v) (push (cons k v) out)) pg-counts)
    out))

(defun pg-snapshot (why)
  (when pg-step
    (push `((why . ,why)
            (counts . ,(copy-hash-table pg-counts))
            (state . ,(condition-case err (pg-state) (error `((error . ,(format "%S" err))))))
            (elisp . ,(vconcat (mapcar #'pg-eval-condition (pg-get pg-step "elisp"))))
            (files . ,(let ((h (make-hash-table :test 'equal))) (mapc (lambda (p) (puthash p (pg-read-file p) h)) (pg-get pg-step "files")) h))
            (echo . ,(substring-no-properties (or (current-message) ""))))
          pg-snapshots)))

(defun pg-progress (fmt &rest args)
  "Append a line to /job/progress.log, so a hang can be located."
  (let ((inhibit-message t) (coding-system-for-write 'utf-8-unix))
    (write-region (concat (apply #'format fmt args) "\n") nil "/job/progress.log" t 'silent)))

(defun pg-post-command ()
  (when pg-recording
    (pg-progress "%s %s" (and pg-step (pg-get pg-step "id")) real-this-command)
    (setq pg-last-activity (float-time))
    ;; Count both `real-this-command' (what the key ran) and
    ;; `this-command' (which commands such as kill-line, yank-pop and
    ;; undo-redo rewrite to kill-region, yank and undo), so a condition on
    ;; either name holds whichever the agent reports.
    (let* ((name-of (lambda (c) (cond ((null c) nil) ((symbolp c) (symbol-name c)) (t "<lambda>"))))
           (real (funcall name-of real-this-command))
           (this (funcall name-of this-command))
           (names (delete-dups (delq nil (list real this)))))
      (dolist (n names)
        (puthash n (1+ (gethash n pg-counts 0)) pg-counts))
      (when names
        (pg-snapshot (string-join names "|"))))))

;;; Setup

(defun pg-resolve-buffer (name)
  (let ((f (expand-file-name name "/work/")))
    (cond ((and (string-match-p "/" name) (file-exists-p f)) (find-file-noselect f))
          ((get-buffer name))
          ((file-exists-p f) (find-file-noselect f))
          (t (get-buffer-create name)))))

(defun pg-apply-layout (spec window)
  (cond
   ((and (consp spec) (memq (car spec) '(vsplit hsplit)))
    (let* ((children (cdr spec))
           (n (length children))
           (wins (list window)))
      (dotimes (i (1- n))
        (let* ((last (car (last wins)))
               (size (if (eq (car spec) 'vsplit) (window-total-height last) (window-total-width last)))
               (new (split-window last (- size (/ size (- n i))) (if (eq (car spec) 'vsplit) 'below 'right))))
          (setq wins (append wins (list new)))))
      (cl-mapc (lambda (c w) (pg-apply-layout c w)) children wins)))
   (t (set-window-buffer window (pg-resolve-buffer (format "%s" spec))))))

(defun pg-setup (setup)
  (let* ((open (append (pg-get setup "open") nil))
         (layout (pg-get setup "layout"))
         (ev (pg-get setup "eval"))
         (compile-cmd (pg-get setup "compile"))
         (first (and open (find-file-noselect (expand-file-name (car open) "/work/")))))
    (dolist (f (cdr open)) (find-file-noselect (expand-file-name f "/work/")))
    (delete-other-windows)
    (when first (switch-to-buffer first))
    (when (and layout (not (equal layout "single")))
      (pg-apply-layout (car (read-from-string layout)) (selected-window))
      (let ((w (and first (get-buffer-window first))))
        (select-window (or w (frame-first-window)))))
    (when (and compile-cmd (not (equal compile-cmd "")))
      (let ((default-directory (file-name-as-directory (expand-file-name (or (pg-get setup "root") "") "/work/"))))
        (save-window-excursion (compile compile-cmd))
        (let ((p (get-buffer-process "*compilation*")))
          (while (and p (process-live-p p)) (accept-process-output p 0.1)))))
    (when (and ev (not (equal ev "")))
      (eval (car (read-from-string ev)) t))))

;;; Demo replay

(defun pg-chunk-events (chunk)
  (pcase (pg-get chunk "kind")
    ("key" (listify-key-sequence (kbd (pg-get chunk "key"))))
    ("text" (mapcar (lambda (c) (if (eq c ?\n) ?\r c)) (string-to-list (pg-get chunk "text"))))
    (_ nil)))

(defun pg-finish ()
  (pg-progress "finish: serialising")
  (when pg-timer (cancel-timer pg-timer))
  (setq pg-recording nil)
  (let ((doc `((steps . ,(vconcat (reverse pg-results)))
               (errors . ,(vconcat (reverse pg-errors))))))
    (condition-case err
        ;; json-serialize returns UTF-8 bytes: decode them, and never let
        ;; write-region ask for a coding system (that hangs the replay).
        (let ((coding-system-for-write 'utf-8-unix))
          (with-temp-file "/job/result.json"
            (insert (decode-coding-string (json-serialize doc) 'utf-8))))
      (error
       (pg-progress "finish: json error %S" err)
       (with-temp-file "/job/result.json"
         (insert (decode-coding-string (json-serialize
                  `((steps . [])
                    (errors . ,(vector (format "result not serialisable: %S" err)))))
                 'utf-8))))))
  (pg-progress "finish: written, killing")
  (kill-emacs 0))

(defun pg-start-step ()
  (let ((steps (pg-get pg-job "steps")))
    (if (>= pg-step-index (length steps))
        (pg-finish)
      (setq pg-step (aref steps pg-step-index)
            pg-counts (make-hash-table :test 'equal)
            pg-snapshots nil
            pg-chunks (append (pg-get pg-step "chunks") nil)
            pg-step-started (float-time)
            pg-last-activity (float-time)
            pg-recording t)
      (pg-snapshot "start"))))

(defun pg-end-step (timed-out)
  (pg-progress "end-step %s" (pg-get pg-step "id"))
  (push (format "end step %s after %.1fs%s" (pg-get pg-step "id") (- (float-time) pg-step-started) (if timed-out " (timed out)" "")) pg-log)
  (pg-snapshot "final")
  (push `((id . ,(pg-get pg-step "id"))
          (timedOut . ,(if timed-out t :false))
          (pendingChunks . ,(length pg-chunks))
          (snapshots . ,(vconcat (nreverse pg-snapshots))))
        pg-results)
  (setq pg-step nil pg-recording nil)
  (cl-incf pg-step-index)
  (pg-start-step))

(defvar pg-wait-until 0)

(defun pg-tick ()
  (condition-case err
      (when pg-step
        (let ((now (float-time)))
          (cond
           ((> (- now pg-step-started) 40) (pg-end-step t))
           (unread-command-events nil)
           ((< now pg-wait-until) nil)
           (pg-chunks
            (let ((c (pop pg-chunks)))
              (setq pg-last-activity now)
              (if (equal (pg-get c "kind") "pause")
                  (setq pg-wait-until (+ now (/ (pg-get c "ms") 1000.0)))
                (setq unread-command-events (append unread-command-events (pg-chunk-events c)))
                (setq pg-wait-until (+ now 0.12)))))
           ((> (- now pg-last-activity) 1.2) (pg-end-step nil)))))
    (error (push (format "tick: %S" err) pg-errors))))

(defun pg-run ()
  (condition-case err
      (progn
        (setq pg-job (json-parse-string
                      (with-temp-buffer (insert-file-contents "/job/job.json") (buffer-string))))
        (mapc #'pg-play-enable-module (pg-get pg-job "modules"))
        (pg-setup (pg-get pg-job "setup"))
        (add-hook 'post-command-hook #'pg-post-command)
        (setq pg-timer (run-with-timer 0.3 0.05 #'pg-tick))
        (pg-start-step))
    (error
     (push (format "setup: %S" err) pg-errors)
     (pg-finish))))

(run-with-timer 1.0 nil #'pg-run)
;; Never let a stray error or a prompt keep the container alive forever.
(run-with-timer 600 nil (lambda () (push "global timeout" pg-errors) (pg-finish)))

;;; driver.el ends here
