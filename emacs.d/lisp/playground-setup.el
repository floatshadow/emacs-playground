;;; playground-setup.el --- Lesson setup for the playground agent  -*- lexical-binding: t; -*-

;;; Commentary:

;; A lesson's setup in Emacs (emacs.md section 10.3, CURRICULUM.md Part
;; B.3): clearing the desk, forgetting saved places, opening files and
;; layouts, the setup's eval and compile; and the small ops beside it:
;; writing the module list, visiting a file, describing a key.

;;; Code:

(require 'cl-lib)
(require 'subr-x)
(require 'seq)
(require 'playground-transport)
(require 'playground-frames)
(require 'playground-observe)
(require 'playground-compat)
(require 'playground-editor-state)

(declare-function isearch-done "isearch" (&optional nopush edit))
(declare-function project-root "project" (project))
(declare-function tab-bar-rename-tab "tab-bar" (name &optional tab-number))
(declare-function tab-bar-close-other-tabs "tab-bar" (&optional tab-number))
(defvar tab-bar-tabs-function)
(defvar playground-config-dir)
(defvar playground-agent-workspace)
(defvar save-place-alist)
(defvar save-place-loaded)

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
      (when (buffer-live-p b)
        (let ((file (buffer-file-name b)))
          (when (and file (string-prefix-p root (expand-file-name file)))
            (cond
             ((not (file-exists-p file))
              (with-current-buffer b (set-buffer-modified-p nil))
              (playground--kill-quietly b)
              (cl-incf n))
             ((not (verify-visited-file-modtime b))
              (with-current-buffer b
                (let ((revert-without-query '(".")))
                  (revert-buffer t t t)))
              (cl-incf n)))))))
    n))

(defun playground--op-setup (args &optional id)
  "Run a scenario's setup: open, layout, eval, compile (ARGS).
A prompt, recursive edit, prefix key or hydra still open (the learner
switched lessons in the middle of a command) would swallow the lesson's
first keys, so it is left first.  For the first three that needs a throw
to the command loop, so, as in rescue, the setup then runs from a timer
at top level and the reply to call ID is sent from there."
  (when (bound-and-true-p isearch-mode)
    (playground--safely "setup isearch" (isearch-done)))
  (playground--close-hydra)
  (playground--at-top-level id (lambda () (playground--setup-now args))))

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
            (playground--kill-quietly b)
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

(defun playground--start-at-top (buffers &optional windows-too)
  "Put point at the start of each of BUFFERS.
With WINDOWS-TOO, also every window of the learner's frame that shows
one of them (window start included), and forget the positions those
windows remember for them, which `switch-to-buffer' would bring back.
A buffer kept from an earlier visit otherwise opens where that visit
left it, and a lesson's \"the cursor is on line 5\" can already be
true when it starts."
  (dolist (b buffers)
    (when (buffer-live-p b)
      (with-current-buffer b (goto-char (point-min)))))
  (when windows-too
    (dolist (w (window-list (playground--learner-frame) 'nomini))
      (when (memq (window-buffer w) buffers)
        (with-current-buffer (window-buffer w)
          (set-window-start w (point-min))
          (set-window-point w (point-min))))
      (set-window-prev-buffers
       w (seq-remove (lambda (entry) (memq (car-safe entry) buffers))
                     (window-prev-buffers w))))))

(defun playground--setup-now (args)
  "The work of `playground--op-setup' (ARGS), at top level.
With reset, the files of `open' start at their first line, in their
buffers and in the windows that show them, before the setup's eval
\(which may move them on purpose)."
  (let* ((playground-agent-workspace
          (file-name-as-directory (or (alist-get 'workspace args) playground-agent-workspace)))
         (open (append (alist-get 'open args) nil))
         (layout (alist-get 'layout args))
         (form (alist-get 'eval args))
         (compile-cmd (alist-get 'compile args))
         (reset (eq (alist-get 'reset args) t))
         (opened nil)
         (first nil))
    ;; What the previous lesson redid after a restore is not this one's.
    (setq playground-reset-step-hook nil)
    (playground--sync-changed-buffers playground-agent-workspace)
    (when reset
      (playground--restore-auto-save)
      (playground--in-user-window
        ;; Tabs from an earlier lesson (3.7, project tabs) are not part of
        ;; this lesson's layout, nor is the name 3.7 gave the tab kept:
        ;; the lesson starts with one tab named after its buffer.
        (when (featurep 'tab-bar)
          (playground--safely "setup tabs"
            (when (and (bound-and-true-p tab-bar-mode)
                       (> (length (funcall tab-bar-tabs-function)) 1))
              (tab-bar-close-other-tabs))
            (when (alist-get 'explicit-name (assq 'current-tab (funcall tab-bar-tabs-function)))
              (tab-bar-rename-tab ""))))
        (playground--clear-desk playground-agent-workspace
                                (mapcar #'playground--absolute open)))
      (let ((root (alist-get 'root args)))
        (when (and (stringp root) (not (string-empty-p root)))
          (playground--forget-places (playground--absolute root)))))
    (dolist (path open)
      (let ((b (find-file-noselect (playground--absolute path))))
        (push b opened)
        (unless first (setq first b))))
    (when reset
      (playground--start-at-top opened))
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
          (playground--only-window)
          (switch-to-buffer first)))))
    ;; A layout saved as a window state (.eld) brings its own positions.
    (when (and reset (not (and layout (string-suffix-p ".eld" layout))))
      (playground--start-at-top opened t))
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

(defun playground--command-doc (cmd)
  "The first line of CMD's documentation, or a gloss for a hydra's command."
  (let ((line (playground--first-doc-line cmd)))
    (or (playground--hydra-doc line) line)))

(defun playground--op-describe (args &rest _)
  "Describe the key sequence (or command name) in ARGS: {cmd, doc}."
  (playground--in-user-window
    (let ((keys (alist-get 'keys args))
          (cmd-name (alist-get 'cmd args)))
      (if (and cmd-name (not keys))
          (let ((sym (intern-soft cmd-name)))
            `((cmd . ,cmd-name)
              (doc . ,(if (and sym (fboundp sym)) (playground--command-doc sym) ""))))
        (let ((binding (key-binding (key-parse keys) t)))
          (cond
           ((null binding) '((cmd . "") (doc . "")))
           ((keymapp binding) `((cmd . "") (doc . ,(format "%s is a prefix key" keys))))
           ((symbolp binding)
            `((cmd . ,(symbol-name binding)) (doc . ,(playground--command-doc binding))))
           (t `((cmd . ,(playground--symbol-name binding)) (doc . "")))))))))


(defun playground--op-visit (args &rest _)
  "Visit the file named by ARGS's path in the learner's window: {buffer}.
The path is absolute or relative to the workspace; sessiond checked
that it is a file inside the workspace."
  (let ((path (alist-get 'path args)))
    (unless (and (stringp path) (not (string-empty-p path)))
      (error "No path to visit"))
    (playground--in-user-window
      (find-file (playground--absolute path))
      `((buffer . ,(buffer-name))))))

(provide 'playground-setup)
;;; playground-setup.el ends here
