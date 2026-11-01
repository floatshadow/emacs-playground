;;; playground-editor-state.el --- Editor state of the playground agent  -*- lexical-binding: t; -*-

;;; Commentary:

;; Capture and restore of the editor state a checkpoint keeps beside
;; the files (emacs.md sections 7 and 10.3, api.md section 7): the window
;; spec (buffers, points, windows, tabs, help buffers), later build
;; output, the unsaved text and undo history of file buffers, the session
;; variables, and the files with unsaved changes.  `capture-editor' and
;; `restore-editor' are the ops sessiond's snapshot service calls; `open'
;; reopens a spec after a daemon restart.

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)
(require 'playground-transport)
(require 'playground-frames)
(require 'playground-observe)
(require 'playground-compat)

(declare-function tab-bar-select-tab "tab-bar" (&optional tab-number))
(declare-function tab-bar-close-tab "tab-bar" (&optional tab-number to-number))
(declare-function tab-bar-rename-tab "tab-bar" (name &optional tab-number))
(declare-function tab-bar-new-tab "tab-bar" (&optional arg from-number))
(declare-function tab-bar-undo-close-tab "tab-bar" ())
(declare-function tab-bar--tab-index-by-name "tab-bar" (name &optional tabs frame))
(defvar tab-bar-closed-tabs)
(defvar tab-bar-tabs-function)
(defvar help-window-select)
(defvar help-xref-stack-item)
(defvar window-state-normalize-buffer-name)
(defvar playground-agent-workspace)

(defvar playground--degraded nil
  "What the running restore could not put back, newest first (strings).
`restore-editor' binds it; each part that fails adds itself here and
the restore goes on with the next.")

(defmacro playground--guarded (what &rest body)
  "Run BODY; on an error, record WHAT and the error in `playground--degraded'."
  (declare (indent 1) (debug t))
  `(condition-case err
       (progn ,@body)
     (error (push (format "%s: %s" ,what (error-message-string err)) playground--degraded)
            nil)))

(defvar playground--pending-layout nil
  "(TIME . SPEC): a layout `open' could not apply yet, and when it was asked.
Right after a daemon restart sessiond reopens the buffers before any
client frame exists; the layout then goes to the first client frame.")


(defvar playground-reset-step-hook nil
  "Normal hook run when a checkpoint restore has put back the step's start.
`restore-editor' (and `reset-step') runs it after the buffers, tabs and
windows, before the unsaved text and the session variables.  The
files, buffers and windows go back to the step's start, but what
evaluating a file did stays in memory.  `setup' empties this hook
before a scenario's eval, so a lesson whose steps evaluate a file adds
here what puts memory back in line with the restored file (lesson 13.5
loads its menus again).")

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

(defun playground--state-add-files (state)
  "Writable window STATE (from `window-state-get') with file names added.
Each window's buffer entry, (buffer NAME . PROPS), gets (file . FILE)
when its buffer visits a file, and each prev-buffers entry, (NAME
START POINT), the file name as a fourth element.  Buffer names change
after the snapshot (uniquify renames README.md to README.md<hello>
when a second README.md opens); the file names let
`playground--state-map-files' find the same buffers again.
`window-state-put' ignores the additions.  A window on a Dired
listing, ibuffer or the buffer list gets what it takes to make that
buffer again (`playground--list-buffer-props')."
  (let ((file-of (lambda (name)
                   (when-let* (((stringp name))
                               (b (get-buffer name)))
                     (buffer-file-name b)))))
    (cond
     ((not (proper-list-p state)) state)
     ((and (eq (car state) 'buffer) (stringp (cadr state)))
      (let* ((file (funcall file-of (cadr state)))
             (extra (and (not file) (not (assq 'dired (cddr state)))
                         (playground--list-buffer-props
                          (cadr state) (alist-get 'point (cddr state))))))
        (cond
         ((and file (not (assq 'file (cddr state))))
          (append state (list (cons 'file file))))
         (extra (append state extra))
         (t state))))
     ((eq (car state) 'prev-buffers)
      (cons 'prev-buffers
            (mapcar (lambda (entry)
                      (let ((file (and (proper-list-p entry) (= (length entry) 3)
                                       (funcall file-of (car entry)))))
                        (if file (append entry (list file)) entry)))
                    (cdr state))))
     (t (mapcar #'playground--state-add-files state)))))


(defun playground--buffer-for-file (file &optional visit)
  "The live buffer visiting FILE; with VISIT, visit FILE when it exists."
  (or (find-buffer-visiting file)
      (and visit (file-exists-p file) (find-file-noselect file))))

(defun playground--file-by-buffer-name (name files)
  "The one file among FILES (absolute names) a buffer called NAME visited.
For window states saved without file names: NAME is matched by its
base name, and a uniquify suffix such as <hello> or <hello/src> must
name directories of the file.  Nil unless exactly one file fits."
  (when (string-match "\\`\\(.*?\\)\\(?:<\\([^>]*\\)>\\)?\\'" name)
    (let* ((base (match-string 1 name))
           (dirs (match-string 2 name))
           (fits (seq-filter
                  (lambda (f)
                    (and (equal (file-name-nondirectory f) base)
                         (or (null dirs)
                             (string-search (concat "/" dirs "/") f))))
                  files)))
      (and fits (null (cdr fits)) (car fits)))))

(defun playground--state-map-files (state &optional files)
  "Window STATE with buffer names replaced by today's names of their files.
The inverse of `playground--state-add-files': a buffer entry with a
file names the buffer visiting that file now (visiting it again if
needed), whatever it was called when the state was saved.  An entry
without a file whose buffer no longer exists is matched against FILES,
the absolute names of the spec's files, by its name."
  (cond
   ((not (proper-list-p state)) state)
   ((and (eq (car state) 'buffer) (stringp (cadr state)))
    (let* ((name (cadr state))
           (props (cddr state))
           (list-b (and (or (assq 'dired props) (assq 'ibuffer props) (assq 'buffer-menu props))
                        (playground--rebuild-list-buffer name props)))
           (file (and (not list-b)
                      (or (alist-get 'file props)
                          (and files (not (get-buffer name))
                               (playground--file-by-buffer-name name files)))))
           (b (or list-b (and (stringp file) (playground--buffer-for-file file t)))))
      (cond
       (list-b (cons 'buffer (cons (buffer-name list-b) (playground--list-buffer-point list-b props))))
       (b (cons 'buffer (cons (buffer-name b) props)))
       (t state))))
   ((eq (car state) 'prev-buffers)
    (cons 'prev-buffers
          (mapcar (lambda (entry)
                    (let* ((file (and (proper-list-p entry) (stringp (nth 3 entry))
                                      (nth 3 entry)))
                           (b (and file (playground--buffer-for-file file))))
                      (if b (cons (buffer-name b) (cdr entry)) entry)))
                  (cdr state))))
   (t (mapcar (lambda (s) (playground--state-map-files s files)) state))))

(defun playground--window-spec ()
  "Buffer and window spec of the learner's frame (protocol.WindowSpec)."
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
                        ;; Real buffer names, which the file names are
                        ;; looked up by, without their text properties.
                        (let ((window-state-normalize-buffer-name nil)
                              (print-length nil) (print-level nil))
                          (prin1-to-string
                           (playground--plain
                            (playground--state-add-files
                             (window-state-get (frame-root-window frame) t)))))))
      (selected . ,(playground--leaf-name (window-buffer (playground--user-window))))
      (tab . ,(alist-get 'current (playground--tabs frame)))
      ,@(playground--tab-spec frame)
      (specialBuffers . ,(let ((print-length nil) (print-level nil))
                           (prin1-to-string (playground--special-buffers))))
      (takenAt . ,(playground--now-ms)))))

(defun playground--plain (x)
  "X with the text properties of its strings removed.
A buffer name can carry properties: uniquify keeps the buffer itself
in `uniquify-orig-buffer' on a name it gave back, and a printed window
state would then hold an unreadable #<buffer main.rs>."
  (cond ((stringp x) (substring-no-properties x))
        ((consp x) (cons (playground--plain (car x)) (playground--plain (cdr x))))
        ((vectorp x) (vconcat (mapcar #'playground--plain x)))
        (t x)))

(defun playground--read-state (string)
  "Read STRING, a printed window state, and return it.
A state printed without `playground--plain' may hold unreadable
objects (#<buffer main.rs> in a name's text properties): those read
as nil, and the strings lose their properties."
  (playground--plain
   (condition-case nil
       (car (read-from-string string))
     (invalid-read-syntax
      (car (read-from-string
            (replace-regexp-in-string "#<[^<>]*\\(?:<[^<>]*>[^<>]*\\)*>" "nil" string t t)))))))

(defun playground--help-buffer-p (b)
  "Non-nil when B is a help buffer the learner can see (not internal)."
  (and (buffer-live-p b)
       (eq (buffer-local-value 'major-mode b) 'help-mode)
       (not (string-prefix-p " " (buffer-name b)))))

(defun playground--readable (x)
  "X with buffers, windows, frames, markers and the like replaced by nil.
Nil when the result still does not read back."
  (let ((clean
         (named-let walk ((x x))
           (cond
            ((consp x) (cons (walk (car x)) (walk (cdr x))))
            ((and (vectorp x) (not (stringp x))) (vconcat (mapcar #'walk x)))
            ((or (bufferp x) (windowp x) (framep x) (markerp x) (processp x)
                 (overlayp x) (window-configuration-p x))
             nil)
            (t x)))))
    (condition-case nil
        (let ((print-length nil) (print-level nil))
          (and (equal (car (read-from-string (prin1-to-string clean))) clean) clean))
      (error nil))))

(defun playground--special-buffers ()
  "The help buffers of the session and the help command that made each.
A list of (NAME ITEM), ITEM being the buffer's `help-xref-stack-item'
made readable; `playground--sync-special-buffers' makes them again."
  (let (out)
    (dolist (b (buffer-list))
      (when (playground--help-buffer-p b)
        (when-let* ((item (playground--readable
                           (buffer-local-value 'help-xref-stack-item b)))
                    ((functionp (car-safe item))))
          (push (list (buffer-name b) item) out))))
    (nreverse out)))

(defun playground--sync-special-buffers (printed)
  "Make the session's help buffers those PRINTED lists.
PRINTED is a WindowSpec's specialBuffers.  Help buffers it does not
name, or names with another help command, are killed, and the ones it
names that are gone are made again by running their help command
\(outside the learner's windows) and giving the result its old name.
A demo that killed help buffers (lesson 2.5's * h D) or opened some
leaves the step as it started.  Returns the number of buffers killed
or made."
  (let ((want (and (stringp printed)
                   (ignore-errors (car (read-from-string printed)))))
        (n 0))
    (when (and (stringp printed) (listp want))
      ;; A help buffer is kept when the list has its name and the same
      ;; help command (*Help* may now describe something else).
      (dolist (b (buffer-list))
        (when (playground--help-buffer-p b)
          (let ((entry (assoc (buffer-name b) want)))
            (unless (and entry
                         (equal (car-safe (cdr-safe entry))
                                (playground--readable
                                 (buffer-local-value 'help-xref-stack-item b))))
              (playground--kill-quietly b)
              (cl-incf n)))))
      (dolist (entry want)
        (let ((name (car-safe entry))
              (item (car-safe (cdr-safe entry))))
          (when (and (stringp name) (not (get-buffer name)) (functionp (car-safe item)))
            (playground--safely "remake help buffer"
              (let* ((plain (get-buffer "*Help*"))
                     (aside (and plain (not (equal name "*Help*"))
                                 (with-current-buffer plain
                                   (rename-buffer "*Help* (aside)" t)))))
                (unwind-protect
                    (progn
                      (save-window-excursion
                        (let ((help-window-select nil))
                          (apply (car item) (cdr item))))
                      (when-let* ((made (get-buffer "*Help*")))
                        (unless (equal name "*Help*")
                          (with-current-buffer made (rename-buffer name t)))
                        (cl-incf n)))
                  (when (and aside (get-buffer aside) (not (get-buffer "*Help*")))
                    (with-current-buffer aside (rename-buffer "*Help*"))))))))))
    n))

;;;; Build output and a step's start

(defvar-local playground--run-started nil
  "When the run in this compilation buffer started (ms), or nil.
Set by `playground--on-compilation-start'; `compilation-mode' clears
it when the next run starts, which sets it again.")

(defun playground--on-compilation-start (proc)
  "Stamp the buffer of compilation PROC with its start time.
Runs from `compilation-start-hook' (compile, recompile, grep)."
  (when-let* ((b (process-buffer proc))
              ((buffer-live-p b)))
    (with-current-buffer b
      (setq playground--run-started (playground--now-ms)))))

(defun playground--spec-shown-buffers (spec)
  "Names of the buffers the windows of SPEC (a WindowSpec alist) show.
From its window state, else from its layout pattern."
  (let ((names '())
        (state (alist-get 'windowState spec))
        (layout (alist-get 'layout spec)))
    (cl-labels ((walk (x)
                  (when (proper-list-p x)
                    (if (and (eq (car x) 'buffer) (stringp (cadr x)))
                        (push (cadr x) names)
                      (mapc #'walk x))))
                (leaves (x)
                  (cond ((stringp x) (push x names))
                        ((and (symbolp x) x (not (memq x '(vsplit hsplit))))
                         (push (symbol-name x) names))
                        ((proper-list-p x) (mapc #'leaves x)))))
      (when (and (stringp state) (not (string-empty-p state)))
        (walk (ignore-errors (playground--read-state state))))
      (when (and (null names) (stringp layout) (not (member layout '("" "single"))))
        (if (string-prefix-p "(" layout)
            (leaves (ignore-errors (car (read-from-string layout))))
          (push layout names))))
    names))

(defun playground--kill-later-runs (spec)
  "Kill the build output made after SPEC (a WindowSpec alist) was taken.
A compilation buffer (compile, grep) whose run started after the
spec's takenAt holds output the step did not start with: the
demonstration's build, or the learner's.  A condition that reads it
\(lesson 2.7 asks whether the build finished) would hold before the
learner builds anything, so it goes, process and all, unless one of
the spec's windows shows a buffer of that name (the step's layout
needs it; lesson 3.5).  Buffers without a start time are kept.
Returns the number killed."
  (let ((taken (alist-get 'takenAt spec))
        (n 0))
    (when (and (integerp taken) (> taken 0))
      (let ((shown (playground--spec-shown-buffers spec)))
        (dolist (b (buffer-list))
          (when (buffer-live-p b)
            (let ((started (buffer-local-value 'playground--run-started b)))
              (when (and (integerp started) (> started taken)
                         (provided-mode-derived-p (buffer-local-value 'major-mode b)
                                                  'compilation-mode)
                         (not (member (buffer-name b) shown)))
                (when-let* ((proc (get-buffer-process b)))
                  (set-process-query-on-exit-flag proc nil))
                (playground--kill-quietly b)
                (cl-incf n)))))))
    n))

(defun playground--tab-spec (frame)
  "The tabs and tabIndex entries of a WindowSpec for FRAME.
Tabs are listed by name in order, \"\" for a tab without an explicit
name (its name follows its buffer, so it identifies nothing)."
  (when (and (featurep 'tab-bar) (boundp 'tab-bar-tabs-function))
    (let* ((tabs (funcall tab-bar-tabs-function frame))
           (i (seq-position tabs 'current-tab (lambda (tab sym) (eq (car-safe tab) sym)))))
      `((tabs . ,(vconcat (mapcar (lambda (tab)
                                    (if (alist-get 'explicit-name tab)
                                        (substring-no-properties (alist-get 'name tab))
                                      ""))
                                  tabs)))
        (tabIndex . ,(1+ (or i 0)))))))

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
      (playground--only-window)
      (playground--build-layout node (selected-window))
      (balance-windows))))

(defun playground--apply-window-state (state &optional files)
  "Apply STATE, a printed `window-state-get' value.
Buffers recorded with their file (`playground--state-add-files') are
found by that file, so a window shows the file it showed when the
state was saved even if its buffer has been renamed since.  FILES,
absolute file names, help with states saved without them."
  (let ((ignore-window-parameters t)
        (state (playground--state-map-files (playground--read-state state) files)))
    (playground--in-user-window
      (playground--only-window)
      (window-state-put state (frame-root-window) 'safe))))

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

(defun playground--spec-files (spec)
  "Absolute names of the files listed in SPEC (a WindowSpec alist)."
  (mapcar (lambda (f) (playground--absolute (alist-get 'path f)))
          (append (alist-get 'files spec) nil)))

(defun playground--select-spec-tab (spec)
  "Select the tab SPEC (a WindowSpec alist) was taken in.
By position when the frame has as many tabs as SPEC lists, else the
tab with SPEC's name.  Never makes a tab (`tab-bar-switch-to-tab'
makes one for a name it does not find, and an unnamed tab's name is
whatever buffer it showed)."
  (when (featurep 'tab-bar)
    (playground--safely "select tab"
      (playground--in-user-window
        (let* ((tabs (funcall tab-bar-tabs-function))
               (want (append (alist-get 'tabs spec) nil))
               (index (alist-get 'tabIndex spec))
               (name (alist-get 'tab spec))
               (by-name (and (stringp name) (tab-bar--tab-index-by-name name))))
          (cond
           ((and (integerp index) want (= (length want) (length tabs))
                 (<= 1 index (length tabs)))
            (tab-bar-select-tab index))
           (by-name (tab-bar-select-tab (1+ by-name)))))))))

(defun playground--last-unnamed-tab ()
  "1-based index of the last tab without an explicit name, preferring
one that is not current; nil when every tab has a name."
  (let ((tabs (funcall tab-bar-tabs-function))
        (i 0) other current)
    (dolist (tab tabs)
      (setq i (1+ i))
      (unless (alist-get 'explicit-name tab)
        (if (eq (car-safe tab) 'current-tab) (setq current i) (setq other i))))
    (or other current)))

(defun playground--reopen-tab (name)
  "Bring back the tab called NAME: the closed one if there is one, else a new one."
  (let ((closed (seq-find (lambda (c)
                            (and (eq (alist-get 'frame c) (selected-frame))
                                 (equal (alist-get 'name (alist-get 'tab c)) name)))
                          tab-bar-closed-tabs)))
    (if closed
        (progn
          (let ((tab-bar-closed-tabs (list closed)))
            (tab-bar-undo-close-tab))
          (setq tab-bar-closed-tabs (delq closed tab-bar-closed-tabs)))
      (tab-bar-new-tab)
      (tab-bar-rename-tab name))))

(defun playground--sync-tabs (spec)
  "Make the learner's tabs those SPEC (a WindowSpec alist) lists.
Named tabs SPEC does not list are closed (a demo's C-x p p opened
one; the last tab loses its name instead), tabs without a name are
closed down to the number SPEC has, and named tabs SPEC lists that
are gone are made again: an unnamed tab beyond SPEC's number takes
the name (a frame made after the spec was taken, such as the GUI
frame, starts with one unnamed tab), else the tab is reopened (from
the closed tabs when possible).  Nothing happens when SPEC has no tab
list or tab-bar is not loaded.  Returns the number of tabs closed,
renamed or opened."
  (let ((want (append (alist-get 'tabs spec) nil))
        (n 0))
    (when (and want (featurep 'tab-bar))
      (playground--safely "sync tabs"
        (playground--in-user-window
          (let* ((want-named (seq-remove #'string-empty-p want))
                 (want-unnamed (- (length want) (length want-named))))
            (dolist (tab (funcall tab-bar-tabs-function))
              (let ((name (alist-get 'name tab)))
                (when (and (alist-get 'explicit-name tab) (not (member name want-named)))
                  (when-let* ((idx (tab-bar--tab-index-by-name name)))
                    (if (> (length (funcall tab-bar-tabs-function)) 1)
                        (tab-bar-close-tab (1+ idx))
                      (tab-bar-rename-tab "" (1+ idx)))
                    (cl-incf n)))))
            (let (extra)
              (while (and (> (length (funcall tab-bar-tabs-function)) 1)
                          (> (seq-count (lambda (tab) (not (alist-get 'explicit-name tab)))
                                        (funcall tab-bar-tabs-function))
                             want-unnamed)
                          (setq extra (playground--last-unnamed-tab)))
                (tab-bar-close-tab extra)
                (cl-incf n)))
            (dolist (name want-named)
              (unless (tab-bar--tab-index-by-name name)
                (let ((spare (and (> (seq-count (lambda (tab) (not (alist-get 'explicit-name tab)))
                                                 (funcall tab-bar-tabs-function))
                                      want-unnamed)
                                  (playground--last-unnamed-tab))))
                  (if spare
                      (tab-bar-rename-tab name spare)
                    (playground--reopen-tab name)))
                (cl-incf n)))))))
    n))

(defun playground--open-layout (spec)
  "Apply the window part of SPEC (a WindowSpec alist) to the learner's frame.
The tab comes first: switching tabs afterwards would replace the
windows just put back with the ones that tab remembers."
  (playground--select-spec-tab spec)
  (let ((state (alist-get 'windowState spec))
        (layout (alist-get 'layout spec)))
    (cond
     ((and state (not (string-empty-p state))
           (condition-case err
               (progn (playground--apply-window-state state (playground--spec-files spec)) t)
             (error (playground--log "window-state-put failed: %S" err) nil))))
     ((and layout (not (string-empty-p layout)))
      (playground--apply-layout layout))))
  (when-let* ((sel (alist-get 'selected spec)))
    (playground--select-leaf sel)))

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

(defun playground--kill-unlisted (files)
  "Kill unmodified workspace file buffers whose file is not among FILES.
FILES are the absolute names of the buffers a step started with.  A
buffer opened during the step keeps a step condition true (\"hello's
main.rs is open\") and renames the step's own buffers (README.md
becomes README.md<hello> next to README.md<polyglot>), so a reset
closes it.  Returns the number killed."
  (let ((root (file-name-as-directory (expand-file-name playground-agent-workspace)))
        (keep (mapcar #'expand-file-name files))
        (n 0))
    (dolist (b (buffer-list))
      (when (buffer-live-p b)
        (let ((file (buffer-file-name b)))
          (when (and file (not (buffer-modified-p b))
                     (string-prefix-p root (expand-file-name file))
                     (not (member (expand-file-name file) keep)))
            (playground--kill-quietly b)
            (cl-incf n)))))
    n))

(defun playground--reset-step-now (args)
  "Put the files and windows back to the spec in ARGS (the step's start).
Edited workspace buffers are reverted to the restored files, buffers of
files that are gone are killed, unmodified file buffers the step did not
start with are closed, the help buffers and the tabs are made those
of the spec (`playground--sync-special-buffers', `playground--sync-tabs'),
build output made since is killed (`playground--kill-later-runs'),
then the spec is opened: its tab, files,
points and windows, each window on the file it showed (whatever the
buffer is called now) or on its Dired listing, ibuffer or buffer list
made again, the selected window last.  Last, `playground-reset-step-hook'
runs.  Each buffer, and each of the later parts, is restored on its
own: one that fails (a remote buffer whose revert would ask a
question) is named in `playground--degraded' and the rest goes on."
  (let ((root (file-name-as-directory (expand-file-name playground-agent-workspace)))
        (reverted 0) (killed 0))
    (dolist (b (buffer-list))
      (when (buffer-live-p b)
        (let ((file (buffer-file-name b)))
          (playground--guarded (buffer-name b)
            (cond
             ((null file)
              (with-current-buffer b
                (when (and (derived-mode-p 'dired-mode) (stringp default-directory)
                           (string-prefix-p root (expand-file-name default-directory))
                           (not (file-directory-p default-directory)))
                  (kill-buffer b)
                  (cl-incf killed))))
             ((not (string-prefix-p root (expand-file-name file))))
             ((file-exists-p file)
              (with-current-buffer b
                (when (or (buffer-modified-p) (not (verify-visited-file-modtime b)))
                  (revert-buffer t t t)
                  (cl-incf reverted))))
             (t
              (with-current-buffer b (set-buffer-modified-p nil))
              (playground--kill-quietly b)
              (cl-incf killed)))))))
    (let ((spec (alist-get 'spec args)))
      ;; A spec without files is not a snapshot (sessiond sends an empty
      ;; one when the checkpoint has none): close nothing then.
      (when (> (length (alist-get 'files spec)) 0)
        (cl-incf killed (or (playground--guarded "unlisted buffers"
                              (playground--kill-unlisted (playground--spec-files spec)))
                            0)))
      ;; Help buffers before the windows (ibuffer lists them), and tabs
      ;; before the windows, which go into the spec's tab.
      (when spec
        (playground--guarded "help buffers"
          (playground--sync-special-buffers (alist-get 'specialBuffers spec)))
        (cl-incf killed (or (playground--guarded "build output" (playground--kill-later-runs spec)) 0))
        (playground--sync-tabs spec)
        (playground--guarded "windows" (playground--op-open spec))))
    (playground--safely "reset-step hook"
      (playground--in-user-window (run-hooks 'playground-reset-step-hook)))
    `((reverted . ,reverted) (killed . ,killed))))


;;;; Unsaved text, undo and session variables (api.md 7)

(defun playground--workspace-root (args)
  "The workspace directory ARGS names, else `playground-agent-workspace'."
  (file-name-as-directory
   (expand-file-name (or (alist-get 'workspace args) playground-agent-workspace))))

(defun playground--workspace-buffers (root)
  "The file buffers under ROOT, as (BUFFER . RELATIVE-PATH), in buffer order.
Indirect buffers are left out: their base buffer stands for the file."
  (let (out)
    (dolist (b (buffer-list))
      (let ((f (buffer-file-name b)))
        (when (and f (not (buffer-base-buffer b))
                   (string-prefix-p root (expand-file-name f)))
          (push (cons b (file-relative-name (expand-file-name f) root)) out))))
    (nreverse out)))

(defun playground--readable-undo-entry (entry)
  "ENTRY of `buffer-undo-list' if it prints readably, else `drop'.
Boundaries, positions, insertions, deletions (without their text
properties) and unmodified-flag entries are kept."
  (cond ((null entry) nil)
        ((integerp entry) entry)
        ((and (consp entry) (integerp (car entry)) (integerp (cdr entry))) entry)
        ((and (consp entry) (stringp (car entry)) (integerp (cdr entry)))
         (cons (substring-no-properties (car entry)) (cdr entry)))
        ((and (consp entry) (eq (car entry) t)
              (or (integerp (cdr entry))
                  (and (proper-list-p (cdr entry)) (seq-every-p #'integerp (cdr entry)))))
         entry)
        (t 'drop)))

(defun playground--printed-undo (limit)
  "The current buffer's undo history, readable entries only, printed.
Nil when there is none or it prints longer than LIMIT characters."
  (when (and (> limit 0) (consp buffer-undo-list))
    (let* ((print-length nil) (print-level nil) (print-escape-newlines nil)
           (printed (prin1-to-string
                     (delq 'drop (mapcar #'playground--readable-undo-entry buffer-undo-list)))))
      (and (<= (length printed) limit) printed))))

(defun playground--capture-buffers (root max-text max-undo max-total)
  "What the files on disk do not keep of the file buffers under ROOT.
A hash table from workspace-relative path to [MODIFIED TEXT UNDO]:
TEXT is the whole text of a buffer with unsaved changes when it is at
most MAX-TEXT characters, UNDO the printed history
\(`playground--printed-undo', at most MAX-UNDO), each null when not
kept.  A buffer with neither is left out, and so is every buffer once
the sizes kept so far pass MAX-TOTAL."
  (let ((out (make-hash-table :test 'equal))
        (total 0))
    (when (> max-total 0)
      (dolist (entry (playground--workspace-buffers root))
        (with-current-buffer (car entry)
          (let* ((modified (buffer-modified-p))
                 (text (and modified (<= (buffer-size) max-text)
                            (save-restriction
                              (widen)
                              (buffer-substring-no-properties (point-min) (point-max)))))
                 (undo (playground--printed-undo max-undo)))
            (when (and (or text undo)
                       (<= (setq total (+ total (length text) (length undo))) max-total))
              (puthash (cdr entry)
                       (vector (playground--bool modified) (or text :null) (or undo :null))
                       out))))))
    out))

(defun playground--global-string (x)
  "X without text properties when it is a string of at most 32 KiB, else nil."
  (and (stringp x) (<= (length x) 32768) (substring-no-properties x)))

(defun playground--global-item (x)
  "An entry X of a history or ring, made printable, or nil to drop it."
  (cond ((stringp x) (playground--global-string x))
        ((and (consp x) (stringp (car x)) (stringp (cdr x)))
         (let ((a (playground--global-string (car x)))
               (b (playground--global-string (cdr x))))
           (and a b (cons a b))))))

(defun playground--global-entry (var)
  "(VAR . VALUE) for the session variable VAR, printable, or nil.
Lists are cut to 100 entries of strings or string pairs, the keyboard
macro counters are integers or nil (F3 starts from
`kmacro-initial-counter-value', which C-x C-k C-c sets and recording
consumes), and a macro is kept only when it is keys."
  (when (boundp var)
    (let ((val (symbol-value var)))
      (cond
       ((memq var '(kmacro-counter kmacro-initial-counter-value))
        (cons var (and (integerp val) val)))
       ((stringp val)
        (and (playground--global-string val) (cons var (playground--global-string val))))
       ((eq var 'last-kbd-macro)
        (and (or (stringp val)
                 (and (vectorp val) (seq-every-p (lambda (e) (or (integerp e) (symbolp e))) val)))
             (cons var (if (stringp val) (substring-no-properties val) val))))
       ((listp val)
        (cons var (delq nil (mapcar #'playground--global-item (seq-take val 100)))))))))

(defun playground--capture-globals (vars limit)
  "The session variables VARS (symbols) as a printed alist, or nil.
Nil when VARS is empty or the alist prints longer than LIMIT."
  (when vars
    (let* ((print-length nil) (print-level nil)
           (printed (prin1-to-string (delq nil (mapcar #'playground--global-entry vars)))))
      (and (<= (length printed) limit) printed))))

(defun playground--arg-count (args key)
  "The natural number ARGS has under KEY, else 0."
  (let ((v (alist-get key args)))
    (if (natnump v) v 0)))

(defun playground--op-capture-editor (args &rest _)
  "The editor state a checkpoint keeps: {spec, buffers, globals}.
spec is the window spec of the learner's frame (`playground--window-spec'),
buffers the unsaved text and undo of the file buffers under ARGS's
workspace (`playground--capture-buffers', bounded by maxText, maxUndo
and maxTotal), globals the variables ARGS lists under vars printed
\(`playground--capture-globals', at most maxGlobals), or null.  Zero
bounds and no vars capture the spec alone."
  (let* ((playground-agent-workspace (playground--workspace-root args))
         (vars (mapcar (lambda (v)
                         (unless (and (stringp v) (string-match-p "\\`[a-z0-9-]+\\'" v))
                           (error "Bad variable name %S" v))
                         (intern v))
                       (alist-get 'vars args))))
    `((spec . ,(playground--window-spec))
      (buffers . ,(playground--capture-buffers
                   playground-agent-workspace (playground--arg-count args 'maxText)
                   (playground--arg-count args 'maxUndo) (playground--arg-count args 'maxTotal)))
      (globals . ,(or (playground--capture-globals vars (playground--arg-count args 'maxGlobals))
                      :null)))))

(defun playground--spec-points (spec)
  "Alist of workspace-relative path to point, from the files of SPEC."
  (mapcar (lambda (f) (cons (alist-get 'path f) (alist-get 'point f)))
          (append (alist-get 'files spec) nil)))

(defun playground--restore-buffer (path text undo point)
  "Put back the state of the buffer visiting PATH (workspace-relative).
TEXT, when non-nil, becomes its unsaved text (the file is visited if
needed), with undo recording off so the restore itself leaves no
trace: lesson 1.3 undoes the earlier deletion within a region
afterwards.  UNDO, a printed history, becomes its undo list.  POINT,
when positive, is where point goes, also in the windows showing it.
Returns non-nil when the buffer exists."
  (let* ((f (playground--absolute path))
         (b (or (find-buffer-visiting f) (and text (find-file-noselect f t)))))
    (when b
      (with-current-buffer b
        (save-restriction
          (widen)
          (when (and text (not (string= (buffer-substring-no-properties (point-min) (point-max)) text)))
            (let ((inhibit-read-only t)
                  (buffer-undo-list t)
                  (src (generate-new-buffer " *pg-unsaved*" t)))
              (unwind-protect
                  (progn (with-current-buffer src (insert text))
                         (with-suppressed-warnings ((obsolete replace-buffer-contents))
                           (replace-buffer-contents src 2)))
                (kill-buffer src)))
            (set-buffer-modified-p t)))
        (when undo
          (setq buffer-undo-list (car (read-from-string undo)))
          (setq pending-undo-list nil))
        (when (and (integerp point) (> point 0))
          (let ((pt (min point (point-max))))
            (goto-char pt)
            (dolist (w (get-buffer-window-list b nil t))
              (set-window-point w pt)))))
      t)))

(defun playground--restore-buffers (buffers points)
  "Put back BUFFERS, an alist of path to [MODIFIED TEXT UNDO].
TEXT counts only for a buffer that was MODIFIED.  POINTS maps paths
to points.  Each buffer is restored on its own; one that fails is
recorded in `playground--degraded'.  Returns the number restored."
  (let ((n 0))
    (dolist (e buffers)
      (let* ((path (if (symbolp (car e)) (symbol-name (car e)) (car e)))
             (v (and (vectorp (cdr e)) (= (length (cdr e)) 3) (cdr e)))
             (modified (and v (aref v 0)))
             (text (and modified (stringp (aref v 1)) (aref v 1)))
             (undo (and v (stringp (aref v 2)) (aref v 2))))
        (when (playground--guarded path
                (playground--restore-buffer path text undo (cdr (assoc path points))))
          (cl-incf n))))
    n))

(defun playground--restore-globals (printed)
  "Set the session variables PRINTED lists back; return how many it lists.
PRINTED comes from `playground--capture-globals'; the kill ring's yank
pointer goes back to its head."
  (if (not (and (stringp printed) (not (string-empty-p printed))))
      0
    (or (playground--guarded "session variables"
          (let ((g (car (read-from-string printed))))
            (dolist (e g)
              (when (boundp (car e)) (set (car e) (cdr e))))
            (when (assq 'kill-ring g)
              (setq kill-ring-yank-pointer kill-ring))
            (length g)))
        0)))

(defun playground--op-restore-editor (args &optional id)
  "Put the editor back to the state in ARGS {workspace, spec, buffers, globals}.
At top level: a prefix key typed before Reset (C-c waiting for its
second key) would otherwise swallow the next keys, so it is cancelled,
and so are prompts (see `playground--at-top-level'); the reply to call
ID may come later.  An open key menu is closed: its keys would take the
demo's or the learner's next keys, and the layout would delete its
window while they stay active."
  (playground--close-menu)
  (playground--at-top-level id (lambda () (playground--restore-editor-now args))))

(defun playground--restore-editor-now (args)
  "The work of `playground--op-restore-editor' (ARGS).
The step's start first (`playground--reset-step-now': buffers reverted
to the restored files, the spec's buffers, tabs and windows), then
the unsaved text, undo history and point of each buffer, then the
session variables.  Each part that fails is skipped and named in
degraded.  Replies {reverted, killed, buffers, globals, degraded}."
  (let* ((playground-agent-workspace (playground--workspace-root args))
         (playground--degraded nil)
         (reset (playground--reset-step-now args))
         (buffers (playground--restore-buffers
                   (alist-get 'buffers args) (playground--spec-points (alist-get 'spec args))))
         (globals (playground--restore-globals (alist-get 'globals args))))
    (playground--schedule-state)
    `((reverted . ,(alist-get 'reverted reset))
      (killed . ,(alist-get 'killed reset))
      (buffers . ,buffers)
      (globals . ,globals)
      (degraded . ,(vconcat (reverse playground--degraded))))))

(defun playground--op-modified-files (args &rest _)
  "The files under ARGS's workspace whose buffers have unsaved changes: {paths}."
  (let ((root (playground--workspace-root args)))
    `((paths . ,(vconcat (delq nil (mapcar (lambda (e) (and (buffer-modified-p (car e)) (cdr e)))
                                           (playground--workspace-buffers root))))))))

(defun playground--op-flush-buffers (&rest _)
  "Save every modified file buffer before Emacs stops: {saved}.
Whether or not `auto-save-visited-mode' is on: lessons about the
modified flag turn it off, and stopping the daemon would otherwise
lose the learner's edits.  Like base's `auto-save-visited-predicate'
it never saves over a file that changed on disk (a reset just
restored it), skips remote and read-only files, and leaves alone a
buffer that cannot be saved without a question."
  (let ((n 0))
    (dolist (b (buffer-list))
      (with-current-buffer b
        (when (and buffer-file-name (buffer-modified-p) (not buffer-read-only)
                   (not (file-remote-p buffer-file-name))
                   (verify-visited-file-modtime b))
          (condition-case nil
              (let ((save-silently t) (inhibit-message t))
                (basic-save-buffer)
                (cl-incf n))
            (error nil)))))
    `((saved . ,n))))

(provide 'playground-editor-state)
;;; playground-editor-state.el ends here
