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
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	if err := s.agent.Call(ctx, protocol.AgentOpEval, protocol.AgentEvalArgs{Form: buffersForm(s.cfg.Workspace)}, 2*time.Second, &out); err != nil {
		s.log.Debug("buffer state for the checkpoint", "err", err)
		return nil
	}
	var raw map[string][3]json.RawMessage
	if err := json.Unmarshal(out.Value, &raw); err != nil || len(raw) == 0 {
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
