package sessiond

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/floatshadow/emacs-playground/internal/protocol"
	"github.com/floatshadow/emacs-playground/internal/sessiond/checkpoint"
)

// Bounds on the buffer state a checkpoint keeps; the restore form travels
// in one agent line (protocol.MaxAgentLine).
const (
	maxBufferText  = 256 << 10
	maxBufferUndo  = 128 << 10
	maxBuffersSize = 512 << 10
)

// buffersForm returns, for the file buffers under the workspace, what a
// checkpoint of the files on disk does not have: the text of a buffer with
// unsaved changes, and the undo history (`buffer-undo-list` reduced to
// entries that print readably: boundaries, positions, insertions,
// deletions without text properties, and unmodified-flag entries). The
// value is an alist from workspace-relative path to [modified, text or
// nil, printed history or nil], a JSON object through the agent's eval.
// Buffers over the bounds keep less (no history, then nothing).
func buffersForm(workspace string) string {
	ws := strings.TrimSuffix(workspace, "/") + "/"
	return fmt.Sprintf(`(let ((ws %s) (total 0) out)
  (dolist (b (buffer-list))
    (let ((f (buffer-file-name b)))
      (when (and f (not (buffer-base-buffer b)) (string-prefix-p ws (expand-file-name f)))
        (with-current-buffer b
          (let* ((modified (buffer-modified-p))
                 (text (and modified (<= (buffer-size) %d)
                            (save-restriction (widen) (buffer-substring-no-properties (point-min) (point-max)))))
                 (undo (and (consp buffer-undo-list)
                            (let ((print-length nil) (print-level nil) (print-escape-newlines nil))
                              (prin1-to-string
                               (delq 'drop
                                     (mapcar (lambda (e)
                                               (cond
                                                ((null e) nil)
                                                ((integerp e) e)
                                                ((and (consp e) (integerp (car e)) (integerp (cdr e))) e)
                                                ((and (consp e) (stringp (car e)) (integerp (cdr e)))
                                                 (cons (substring-no-properties (car e)) (cdr e)))
                                                ((and (consp e) (eq (car e) t)
                                                      (or (integerp (cdr e))
                                                          (and (proper-list-p (cdr e)) (seq-every-p #'integerp (cdr e)))))
                                                 e)
                                                (t 'drop)))
                                             buffer-undo-list))))))
                 (undo (and undo (<= (length undo) %d) undo))
                 (size (+ (length text) (length undo))))
            (when (and (or text undo) (<= (setq total (+ total size)) %d))
              (push (cons (file-relative-name (expand-file-name f) ws)
                          (vector (if modified t :false) (or text :null) (or undo :null)))
                    out)))))))
  out)`, elispString(ws), maxBufferText, maxBufferUndo, maxBuffersSize)
}

// bufferStates asks the agent for the buffer state to record with a
// checkpoint. Errors (no agent, a busy Emacs) record nothing.
func (s *Server) bufferStates(ctx context.Context) map[string]checkpoint.BufferState {
	if s.agent == nil || !s.agent.Connected() {
		return nil
	}
	v, err := s.evalRaw(ctx, buffersForm(s.cfg.Workspace), 2*time.Second)
	if err != nil {
		s.log.Debug("buffer state for the checkpoint", "err", err)
		return nil
	}
	var raw map[string][3]json.RawMessage
	if err := json.Unmarshal(v, &raw); err != nil || len(raw) == 0 {
		return nil
	}
	m := make(map[string]checkpoint.BufferState, len(raw))
	for rel, v := range raw {
		if clean := path.Clean(rel); clean != rel || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
			continue
		}
		var st checkpoint.BufferState
		_ = json.Unmarshal(v[0], &st.Modified)
		var text, undo *string
		_ = json.Unmarshal(v[1], &text)
		_ = json.Unmarshal(v[2], &undo)
		if text != nil {
			st.Text = *text
			st.HasText = true
		}
		if undo != nil {
			st.Undo = *undo
		}
		m[rel] = st
	}
	return m
}

// restoreBuffersForm puts recorded buffer state back after reset-step put
// the buffers on the restored files: the unsaved text of buffers that were
// modified (visiting the files when needed) and every recorded undo
// history, with undo recording off so the restore itself leaves no trace
// (lesson 1.3 undoes the earlier deletion within a region afterwards).
// Point goes to the snapshot's position, also in the windows showing the
// buffer. Each buffer is restored on its own; one that fails is skipped.
// The value is the number of buffers restored.
func restoreBuffersForm(workspace string, bufs map[string]checkpoint.BufferState, points map[string]int) string {
	ws := strings.TrimSuffix(workspace, "/") + "/"
	paths := make([]string, 0, len(bufs))
	for p := range bufs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var entries strings.Builder
	for _, p := range paths {
		b := bufs[p]
		text, undo := "nil", "nil"
		if b.Modified && b.HasText {
			text = elispString(b.Text)
		}
		if b.Undo != "" {
			undo = elispString(b.Undo)
		}
		fmt.Fprintf(&entries, "\n    (%s %s %s %d)", elispString(p), text, undo, points[p])
	}
	return fmt.Sprintf(`(let ((ws %s) (n 0))
  (dolist (e '(%s))
    (condition-case nil
        (let* ((f (expand-file-name (nth 0 e) ws))
               (b (or (find-buffer-visiting f) (and (nth 1 e) (find-file-noselect f t)))))
          (when b
            (with-current-buffer b
              (save-restriction
                (widen)
                (when (and (nth 1 e)
                           (not (string= (buffer-substring-no-properties (point-min) (point-max)) (nth 1 e))))
                  (let ((inhibit-read-only t) (buffer-undo-list t) (src (generate-new-buffer " *pg-unsaved*" t)))
                    (unwind-protect
                        (progn (with-current-buffer src (insert (nth 1 e)))
                               (replace-buffer-contents src 2))
                      (kill-buffer src)))
                  (set-buffer-modified-p t)))
              (when (nth 2 e)
                (setq buffer-undo-list (car (read-from-string (nth 2 e))))
                (setq pending-undo-list nil))
              (when (> (nth 3 e) 0)
                (let ((pt (min (nth 3 e) (point-max))))
                  (goto-char pt)
                  (dolist (w (get-buffer-window-list b nil t)) (set-window-point w pt))))
              (setq n (1+ n)))))
      (error nil)))
  n)`, elispString(ws), entries.String())
}

// GlobalVars are the Emacs session variables a checkpoint keeps: steps
// rely on the kill ring (M-y after C-y), the minibuffer and search
// histories (M-p in M-x, C-s C-s), and the keyboard macro and its counter.
// A demonstration followed by "Do it yourself" would otherwise leave the
// demo's kills, commands and macro counts in them.
var GlobalVars = []string{
	"kill-ring", "extended-command-history", "minibuffer-history", "file-name-history",
	"buffer-name-history", "search-ring", "regexp-search-ring", "query-replace-history",
	"query-replace-defaults", "read-expression-history", "shell-command-history",
	"kmacro-counter", "kmacro-initial-counter-value", "kmacro-default-counter-format", "last-kbd-macro",
}

// maxGlobals bounds the printed globals; larger state is not recorded.
const maxGlobals = 256 << 10

// globalsForm prints GlobalVars readably: strings without text properties
// (entries over 32 KiB dropped), lists cut to 100 entries, the macro
// counters as integers or nil (F3 starts from kmacro-initial-counter-value,
// which C-x C-k C-c sets and recording consumes), a macro only when it is
// keys (integers and symbols). The value is the printed alist, or nil when
// it would exceed maxGlobals.
func globalsForm() string {
	return fmt.Sprintf(`(let* ((print-length nil) (print-level nil)
       (str (lambda (x) (and (stringp x) (<= (length x) 32768) (substring-no-properties x))))
       (item (lambda (x)
               (cond ((stringp x) (funcall str x))
                     ((and (consp x) (stringp (car x)) (stringp (cdr x)))
                      (and (funcall str (car x)) (funcall str (cdr x))
                           (cons (funcall str (car x)) (funcall str (cdr x)))))
                     (t nil))))
       (out (delq nil
                  (mapcar (lambda (v)
                            (when (boundp v)
                              (let ((val (symbol-value v)))
                                (cond
                                 ((memq v '(kmacro-counter kmacro-initial-counter-value))
                                  (cons v (and (integerp val) val)))
                                 ((stringp val) (and (funcall str val) (cons v (funcall str val))))
                                 ((eq v 'last-kbd-macro)
                                  (and (or (stringp val)
                                           (and (vectorp val) (seq-every-p (lambda (e) (or (integerp e) (symbolp e))) val)))
                                       (cons v (if (stringp val) (substring-no-properties val) val))))
                                 ((listp val)
                                  (cons v (delq nil (mapcar item (seq-take val 100)))))))))
                          '(%s))))
       (printed (prin1-to-string out)))
  (and (<= (length printed) %d) printed))`, strings.Join(GlobalVars, " "), maxGlobals)
}

// restoreGlobalsForm sets the recorded variables back.
func restoreGlobalsForm(printed string) string {
	return fmt.Sprintf(`(condition-case nil
    (let ((g (car (read-from-string %s))))
      (dolist (e g) (when (boundp (car e)) (set (car e) (cdr e))))
      (when (assq 'kill-ring g) (setq kill-ring-yank-pointer kill-ring))
      (length g))
  (error nil))`, elispString(printed))
}

// globals asks the agent for the session variables to record with a
// checkpoint ("" when unavailable).
func (s *Server) globals(ctx context.Context) string {
	if s.agent == nil || !s.agent.Connected() {
		return ""
	}
	v, err := s.evalRaw(ctx, globalsForm(), 2*time.Second)
	if err != nil {
		s.log.Debug("session variables for the checkpoint", "err", err)
		return ""
	}
	var printed string
	if err := json.Unmarshal(v, &printed); err != nil {
		return ""
	}
	return printed
}

// restoreGlobals sets a checkpoint's session variables back.
func (s *Server) restoreGlobals(ctx context.Context, printed string) {
	if printed == "" || s.agent == nil || !s.agent.Connected() {
		return
	}
	if err := s.agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: restoreGlobalsForm(printed)}, 3*time.Second, nil); err != nil {
		s.log.Warn("restoring the session variables of the checkpoint", "err", err)
	}
}

// restoreBuffers puts a checkpoint's buffer state back.
func (s *Server) restoreBuffers(ctx context.Context, bufs map[string]checkpoint.BufferState, spec *protocol.WindowSpec) {
	if len(bufs) == 0 || s.agent == nil || !s.agent.Connected() {
		return
	}
	points := map[string]int{}
	if spec != nil {
		for _, f := range spec.Files {
			points[f.Path] = f.Point
		}
	}
	if err := s.agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: restoreBuffersForm(s.cfg.Workspace, bufs, points)}, 5*time.Second, nil); err != nil {
		s.log.Warn("restoring the buffers of the checkpoint", "err", err)
	}
}
