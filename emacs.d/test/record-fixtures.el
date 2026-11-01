;;; record-fixtures.el --- record the agent's contract fixtures  -*- lexical-binding: t; -*-

;; Plays scripted cases against the agent through the fake sessiond of
;; agent-test.el and writes what the agent answered and sent to
;; test/fixtures/agent/<case>.json (`make agent-fixtures' runs it in the
;; session image):
;;
;;   emacs --batch -L lisp -l playground-agent -l test/agent-test.el \
;;     -l test/record-fixtures.el -f pg-fixtures-record-batch [DIR]
;;
;; A case is its setup files and steps: keys pressed, text typed, ops
;; called (with "$NAME" standing for the reply saved under NAME,
;; "$NAME.FIELD" for a field of it, and "$WS" for the workspace), pauses.
;; `exact' names the reply fields that must be equal on replay; everything
;; else is compared by shape.  A fixture keeps the calls, the key and
;; minibuffer events and the final state, which is what replays assert;
;; `allStates' also keeps every state message, for the one case that
;; stands for the state shapes.  `make
;; emacs-test' replays the committed files against the agent
;; (pg-agent/contract-fixtures), and so does sessiond's fake editor
;; (internal/sessiond/agent/fakeeditor), so the fake is checked against
;; Emacs rather than against beliefs about it.

;;; Code:

(require 'json)

(defconst pg-fixtures--buffer-a
  "(let ((pg-b (or (get-buffer \"a.txt\") (seq-find (lambda (b) (let ((n (buffer-name b))) (and (string-prefix-p (concat \"a.txt\" \"<\") n) (string-suffix-p \">\" n)))) (buffer-list))))) (and pg-b (with-current-buffer pg-b (and t))))"
  "steps.BufferForm for {name: a.txt}.")

(defconst pg-fixtures--buffer-nope
  "(let ((pg-b (or (get-buffer \"nope.txt\") (seq-find (lambda (b) (let ((n (buffer-name b))) (and (string-prefix-p (concat \"nope.txt\" \"<\") n) (string-suffix-p \">\" n)))) (buffer-list))))) (and pg-b (with-current-buffer pg-b (and t))))"
  "steps.BufferForm for {name: nope.txt}.")

(defconst pg-fixtures--buffer-a-contains
  "(let ((pg-b (or (get-buffer \"a.txt\") (seq-find (lambda (b) (let ((n (buffer-name b))) (and (string-prefix-p (concat \"a.txt\" \"<\") n) (string-suffix-p \">\" n)))) (buffer-list))))) (and pg-b (with-current-buffer pg-b (and (save-excursion (save-restriction (widen) (goto-char (point-min)) (search-forward \"line two\" nil t))) (not (buffer-modified-p)) t))))"
  "steps.BufferForm for {name: a.txt, contains: line two, modified: false}.")

(defconst pg-fixtures--buffer-a-dired
  "(let ((pg-b (or (get-buffer \"a.txt\") (seq-find (lambda (b) (let ((n (buffer-name b))) (and (string-prefix-p (concat \"a.txt\" \"<\") n) (string-suffix-p \">\" n)))) (buffer-list))))) (and pg-b (with-current-buffer pg-b (and (derived-mode-p (intern \"dired-mode\")) t))))"
  "steps.BufferForm for {name: a.txt, mode: dired-mode}.")

(defconst pg-fixtures--buffer-a-edited
  "(let ((pg-b (or (get-buffer \"a.txt\") (seq-find (lambda (b) (let ((n (buffer-name b))) (and (string-prefix-p (concat \"a.txt\" \"<\") n) (string-suffix-p \">\" n)))) (buffer-list))))) (and pg-b (with-current-buffer pg-b (and (save-excursion (save-restriction (widen) (goto-char (point-min)) (search-forward \"edited line one\" nil t))) (buffer-modified-p) t))))"
  "steps.BufferForm for {name: a.txt, contains: edited line one, modified: true}.")

(defconst pg-fixtures--capture-args
  '((workspace . "$WS") (vars . ["kill-ring"])
    (maxText . 262144) (maxUndo . 131072) (maxTotal . 524288) (maxGlobals . 262144))
  "capture-editor arguments: sessiond's bounds, the kill ring as the variable.")

(defconst pg-fixtures--capture-exact
  ["spec.files[].path" "spec.files[].point" "spec.layout" "spec.selected"]
  "capture-editor fields compared exactly (buffers and globals by shape:
the fake prints undo and variables in a format of its own).")

(defconst pg-fixtures--spec-args
  '((workspace . "$WS") (vars . []) (maxText . 0) (maxUndo . 0) (maxTotal . 0) (maxGlobals . 0))
  "capture-editor arguments for the window spec alone (snapshot.SpecOnly).")

(defconst pg-fixtures--spec-exact
  ["spec.files[].path" "spec.files[].point" "spec.files[].visible" "spec.layout" "spec.selected"
   "spec.tab" "spec.tabs" "spec.tabIndex"]
  "Window spec fields compared exactly.")

(defconst pg-fixtures--restore-exact ["reverted" "killed" "buffers" "globals" "degraded"]
  "restore-editor fields compared exactly.")

(defconst pg-fixtures--events-exact
  '((key . ["keys" "cmd" "buf" "mode"])
    (minibuffer . ["open" "prompt" "depth" "kind"])
    (state . ["buf" "mode" "modified" "narrowed" "overwrite" "readonly" "line" "col" "point"
              "markActive" "frame" "isearch" "recursive"]))
  "Event fields compared exactly, by event type.")

(defun pg-fixtures--find (path)
  "Steps visiting PATH with C-x C-f."
  `(((press . "C-x C-f")) ((type . ,path)) ((press . "RET"))))

(defun pg-fixtures--spec (&optional as)
  "A capture-editor step for the window spec alone, saved as AS when given."
  `((call . "capture-editor") (args . ,pg-fixtures--spec-args) ,@(and as `((as . ,as)))
    (exact . ,pg-fixtures--spec-exact)))

(defun pg-fixtures--restore-spec (as)
  "A restore-editor step back to the spec saved as AS, without buffers or globals."
  `((call . "restore-editor") (args (workspace . "$WS") (spec . ,(concat "$" as ".spec")))
    (exact . ,pg-fixtures--restore-exact)))

(defconst pg-fixtures-cases
  `(((name . "hello-shape")
     (steps . [((call . "hello") (exact . ["protocol" "agent"]))]))

    ((name . "state-after-find-file")
     (allStates . t)
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-n"))]))

    ((name . "split-and-layout")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-x 2")) ((press . "C-x 3")) ((press . "C-x o"))
               ,(pg-fixtures--spec)]))

    ((name . "spec-single")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ,(pg-fixtures--spec)]))

    ((name . "spec-two-windows-with-point")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))
               ((file . "sub/b.txt") (contents . "This is b.\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-n")) ((press . "C-x 2")) ((press . "C-x o"))
               ,@(pg-fixtures--find "sub/b.txt") ((press . "C-x o")) ,(pg-fixtures--spec)]))

    ((name . "spec-with-dired")
     (setup . [((file . "docs/x.md") (contents . "# x\n"))])
     ;; The listing's text is the system's; point in it is not compared.
     (eventsExact . ((key . ["keys" "cmd" "buf" "mode"])
                     (minibuffer . ["open" "prompt" "depth" "kind"])
                     (state . ["buf" "mode" "modified" "readonly" "frame" "isearch" "recursive"])))
     (steps . [((press . "C-x d")) ((type . "docs")) ((press . "RET")) ,(pg-fixtures--spec)]))

    ((name . "spec-with-help")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-h f")) ((type . "find-file")) ((press . "RET"))
               ,(pg-fixtures--spec)]))

    ((name . "spec-with-tabs")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-x t 2")) ((press . "C-x t r")) ((type . "proj"))
               ((press . "RET")) ,(pg-fixtures--spec)]))

    ((name . "open-reapplies-layout")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))
               ((file . "b.txt") (contents . "This is b.\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-x 3")) ((press . "C-x o"))
               ,@(pg-fixtures--find "b.txt") ,(pg-fixtures--spec "s1") ((press . "C-x 1"))
               ((call . "open") (args . "$s1.spec") (exact . ["opened"]))
               ,(pg-fixtures--spec)]))

    ((name . "restore-spec-reverts-modified")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ,(pg-fixtures--spec "s1") ((type . "edited "))
               ,(pg-fixtures--restore-spec "s1")
               ,(pg-fixtures--spec)
               ((call . "check") (args (forms . [,pg-fixtures--buffer-a-contains])) (exact . ["results"]))]))

    ((name . "restore-spec-kills-unlisted")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))
               ((file . "c.txt") (contents . "This is c.\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ,(pg-fixtures--spec "s1") ,@(pg-fixtures--find "c.txt")
               ,(pg-fixtures--restore-spec "s1")
               ,(pg-fixtures--spec)]))

    ((name . "restore-spec-kills-later-compile")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ,(pg-fixtures--spec "s1") ((sleep . 20))
               ((call . "setup") (args (compile . "true") (workspace . "$WS")) (exact . ["ok"]))
               ((sleep . 300))
               ,(pg-fixtures--restore-spec "s1")
               ,(pg-fixtures--spec)]))

    ((name . "setup-reset-clears-desk")
     (setup . [((file . "p/a.rs") (contents . "line 1\nline 2\nline 3\n"))
               ((file . "p/b.rs") (contents . "b\n"))])
     (steps . [,@(pg-fixtures--find "p/b.rs") ,@(pg-fixtures--find "a.rs") ((press . "C-n"))
               ((call . "setup") (args (open . ["p/a.rs"]) (reset . t) (root . "p") (workspace . "$WS"))
                (exact . ["ok"]))
               ,(pg-fixtures--spec)]))

    ((name . "setup-without-reset-keeps-places")
     (setup . [((file . "p/a.rs") (contents . "line 1\nline 2\nline 3\n"))
               ((file . "p/b.rs") (contents . "b\n"))])
     (steps . [,@(pg-fixtures--find "p/b.rs") ,@(pg-fixtures--find "a.rs") ((press . "C-n")) ((press . "C-n"))
               ((call . "setup") (args (open . ["p/a.rs"]) (root . "p") (workspace . "$WS")) (exact . ["ok"]))
               ,(pg-fixtures--spec)]))

    ((name . "check-truth-values")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt")
               ((call . "check")
                (args (forms . ["t" "nil" "(error \"x\")" "(+ 1" ,pg-fixtures--buffer-a ,pg-fixtures--buffer-nope
                                ,pg-fixtures--buffer-a-contains ,pg-fixtures--buffer-a-dired]))
                (exact . ["results"]))]))

    ((name . "rescue-narrowing-and-windows")
     (setup . [((file . "n.txt") (contents . "one\ntwo\nthree\n"))])
     (steps . [,@(pg-fixtures--find "n.txt") ((press . "C-SPC")) ((press . "C-n")) ((press . "C-x n n")) ((press . "<insert>"))
               ((press . "C-x 2")) ((call . "rescue") (exact . ["did"])) ,(pg-fixtures--spec)]))

    ((name . "describe-prefix-and-command")
     (steps . [((call . "describe") (args (keys . "C-x C-f")) (exact . ["cmd"]))
               ((call . "describe") (args (keys . "C-x")) (exact . ["cmd" "doc"]))
               ((call . "describe") (args (keys . "C-c C-z C-z")) (exact . ["cmd" "doc"]))
               ((call . "describe") (args (cmd . "save-buffer")) (exact . ["cmd"]))]))

    ((name . "set-modules-writes-file")
     (steps . [((call . "set-modules") (args (enabled . ["base" "agent" "editing"])) (exact . ["written"]))
               ((call . "set-modules") (args (enabled . ["../evil"])))]))

    ((name . "kill-yank-undo")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-k")) ((press . "C-n")) ((press . "C-y"))
               ((press . "C-/")) ,(pg-fixtures--spec)]))

    ((name . "capture-editor-modified-buffer")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))
               ((file . "b.txt") (contents . "This is b.\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((type . "edited ")) ((press . "C-n")) ((press . "C-k"))
               ,@(pg-fixtures--find "b.txt")
               ((call . "capture-editor") (args . ,pg-fixtures--capture-args)
                (exact . ,pg-fixtures--capture-exact))]))

    ((name . "restore-editor-roundtrip")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((press . "C-n")) ((press . "C-k")) ((press . "C-p"))
               ((type . "edited "))
               ((call . "capture-editor") (args . ,pg-fixtures--capture-args) (as . "s1")
                (exact . ,pg-fixtures--capture-exact))
               ;; A demonstration: another kill, a save, point elsewhere.
               ((press . "C-k")) ((press . "C-x C-s")) ((press . "M->"))
               ((call . "restore-editor")
                (args (workspace . "$WS") (spec . "$s1.spec") (buffers . "$s1.buffers") (globals . "$s1.globals"))
                (exact . ,pg-fixtures--restore-exact))
               ,(pg-fixtures--spec)
               ((call . "check") (args (forms . [,pg-fixtures--buffer-a-edited])) (exact . ["results"]))
               ;; The kill ring is the step's: C-y yanks line two, not the demo's kill.
               ((press . "C-y"))
               ;; The history is the step's: undo takes back the yank, then the typing.
               ((press . "C-/")) ((press . "C-/"))]))

    ((name . "modified-files")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))
               ((file . "sub/b.txt") (contents . "This is b.\n"))])
     (steps . [,@(pg-fixtures--find "sub/b.txt") ((type . "x")) ,@(pg-fixtures--find "a.txt")
               ((call . "modified-files") (args (workspace . "$WS")) (exact . ["paths"]))]))

    ((name . "flush-buffers")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((type . "done "))
               ((call . "flush-buffers") (exact . ["saved"]))
               ((call . "check") (args (forms . [,pg-fixtures--buffer-a-contains])) (exact . ["results"]))
               ((call . "flush-buffers") (exact . ["saved"]))]))

    ((name . "visit")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))
               ((file . "sub/b.txt") (contents . "This is b.\n"))])
     (steps . [,@(pg-fixtures--find "a.txt")
               ((call . "visit") (args (path . "sub/b.txt")) (exact . ["buffer"]))
               ,(pg-fixtures--spec)]))

    ((name . "display-and-debug-ops")
     (steps . [((call . "leave-display") (exact . ["closed" "detail"]))
               ((call . "reset-debug"))]))

    ((name . "save-writes-file")
     (setup . [((file . "a.txt") (contents . "line one\nline two\n"))])
     (steps . [,@(pg-fixtures--find "a.txt") ((type . "done ")) ((press . "C-x C-s"))
               ((call . "check") (args (forms . [,pg-fixtures--buffer-a-contains])) (exact . ["results"]))])))
  "The contract cases, recorded in this order.")

(defun pg-fixtures--case-json (case)
  "Record CASE: the case with its calls and events."
  (let ((case (append case
                      (unless (assq 'setup case) '((setup . [])))
                      (unless (assq 'steps case) '((steps . []))))))
    (let ((run (pg-fixture-run case)))
      `((name . ,(alist-get 'name case))
        (setup . ,(alist-get 'setup case))
        (steps . ,(alist-get 'steps case))
        (calls . ,(nth 1 run))
        (events . ,(nth 2 run))
        (state . ,(nth 3 run))
        (eventsExact . ,(or (alist-get 'eventsExact case) pg-fixtures--events-exact))
        ,@(and (alist-get 'allStates case) '((allStates . t)))))))

(defun pg-fixtures-record (dir)
  "Record every case of `pg-fixtures-cases' into DIR."
  (make-directory dir t)
  (dolist (case pg-fixtures-cases)
    (message "recording %s" (alist-get 'name case))
    (let ((json (pg-fixtures--case-json case))
          (file (expand-file-name (concat (alist-get 'name case) ".json") dir)))
      (with-temp-file file
        (insert (json-serialize json))
        (json-pretty-print-buffer)
        (goto-char (point-max))
        (insert "\n"))
      (message "recorded %s" file))))

(defun pg-fixtures-record-batch ()
  "Batch entry: record into the directory on the command line (default `pg-fixture-dir')."
  (pg-fixtures-record (or (car command-line-args-left) pg-fixture-dir))
  (setq command-line-args-left nil))

(provide 'record-fixtures)
;;; record-fixtures.el ends here
