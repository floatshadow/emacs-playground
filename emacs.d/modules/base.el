;;; base.el --- defaults every lesson relies on  -*- lexical-binding: t; -*-

;; Module: base.  On from Part 0, never turned off.
;;
;; Sane defaults, the theme, the terminal (keys, mouse, clipboard), and
;; the small built-in modes that remember things for you: minibuffer
;; history, recent files, the place you were in each file.  Nothing
;; here needs a package except kkp (terminal keys) and clipetty (an
;; old-Emacs clipboard fallback).
;;
;; Sections follow the lessons that first rely on them.

;;;; Where state files go

;; Emacs remembers history, recent files and so on in small files.
;; They go to var/ (ignored by git, see .gitignore) so the workspace
;; repository only tracks the configuration you write.
(defvar playground-var-dir (expand-file-name "var/" user-emacs-directory)
  "Directory for state files.")

(defun playground-var (name)
  "Absolute name of state file NAME under `playground-var-dir'."
  (let ((file (expand-file-name name playground-var-dir)))
    (make-directory (file-name-directory file) t)
    file))

(setq savehist-file (playground-var "history")
      recentf-save-file (playground-var "recentf")
      save-place-file (playground-var "places")
      bookmark-default-file (playground-var "bookmarks")
      project-list-file (playground-var "projects")
      tramp-persistency-file-name (playground-var "tramp")
      url-configuration-directory (playground-var "url/")
      nsm-settings-file (playground-var "network-security.data")
      auto-save-list-file-prefix (playground-var "auto-save-list/.saves-")
      eshell-directory-name (playground-var "eshell/")
      multisession-directory (playground-var "multisession/")
      transient-history-file (playground-var "transient/history.el")
      transient-levels-file (playground-var "transient/levels.el")
      transient-values-file (playground-var "transient/values.el"))

;;;; Lesson 0.1  What the screen is telling you

;; The mode line shows line and column ("L12 C4"), and the theme is
;; modus-vivendi-tinted, the palette the browser uses around the
;; terminal.  Modus themes are designed for legibility (WCAG AAA).
(line-number-mode 1)
(column-number-mode 1)

;; The selected window's mode line must stand out: lessons say "the
;; bright mode line marks the selected window".  The theme's own two
;; mode lines (#484d67 and #292d48) are hard to tell apart in a browser
;; terminal, so the active one takes the playground's violet and the
;; inactive ones are dimmed (a background contrast of about 2.8 between
;; them; both keep their text above 4.5:1).  Theme-specific overrides,
;; so another Modus theme chosen later keeps its own colours.
(defvar modus-vivendi-tinted-palette-overrides)
(setq modus-vivendi-tinted-palette-overrides
      '((bg-mode-line-active "#7a58c4")
        (fg-mode-line-active "#ffffff")
        (border-mode-line-active "#a990e6")
        (bg-mode-line-inactive "#232639")
        (fg-mode-line-inactive "#969696")
        (border-mode-line-inactive "#3c3f55")))
(load-theme 'modus-vivendi-tinted t)

;; The mode line reads the way the course describes it:
;;   -:**-  main.rs   Top L12 C4   (Rust Eglot)
;; Out of the box a terminal frame would also show the keyboard and
;; terminal encodings (UUU), an @ for frames opened by emacsclient and
;; the frame's name (F2), and minor modes such as which-key's WK and
;; ElDoc.  The playground has one encoding and one frame per kind, so
;; those are left out; the encoding still shows when a file is not UTF-8.
(setq mode-line-position-column-line-format '(" L%l C%c")
      which-key-lighter ""
      eldoc-minor-mode-string nil
      server-client-instructions nil)

(defun playground--mode-line-coding ()
  "The buffer's encoding mnemonic, or nothing when it is UTF-8 or ASCII."
  (let ((base (coding-system-base buffer-file-coding-system)))
    (if (memq base '(utf-8 utf-8-emacs prefer-utf-8 undecided us-ascii raw-text no-conversion))
        ""
      (char-to-string (coding-system-mnemonic buffer-file-coding-system)))))

(setq-default mode-line-mule-info
              '("" (current-input-method (:propertize ("" current-input-method-title)))
                (:eval (playground--mode-line-coding))
                (:eval (mode-line-eol-desc)))
              mode-line-client nil
              mode-line-frame-identification "  ")

;; Prefix keys show up in the echo area quickly ("C-x-").
(setq echo-keystrokes 0.25)

;; No beeps; the echo area already says what went wrong.
(setq ring-bell-function #'ignore)

;;;; Lesson 0.2  C-g and the three ways to get lost

;; C-z (suspend) and C-x C-z are the classic VS Code trap: they would
;; freeze the terminal.  Here they do nothing ("C-z is undefined").
(keymap-global-unset "C-z")
(keymap-global-unset "C-x C-z")

;; Suspending by name is turned off too in the playground.
(defun playground--no-suspend (orig &rest args)
  "Around advice for `suspend-frame' (ORIG with ARGS): not in the playground."
  (if (and (playground-p) (daemonp))
      (message "Suspending Emacs is turned off in the playground (there is no shell to return to).")
    (apply orig args)))
(advice-add 'suspend-frame :around #'playground--no-suspend)

;; Show the minibuffer depth when prompts nest ("[2]").
(minibuffer-depth-indicate-mode 1)

;;;; Lesson 0.3  Asking Emacs

;; Help windows take the focus, so q closes them right away.
(setq help-window-select t)

;;;; Lesson 0.4  Which-key and the key ticker

;; After a prefix key, a popup lists what can follow it.
(setq which-key-idle-delay 0.8
      which-key-idle-secondary-delay 0.05
      which-key-sort-order #'which-key-key-order-alpha)
(which-key-mode 1)

;; Repeat a command's last key: C-x o o o, C-x { { {.
(repeat-mode 1)

;; M-x tells you the keys a command is bound to ("You can run the
;; command `save-buffer' with C-x C-s"), which teaches chords.  It does
;; not also suggest abbreviations ("... with M-x em-ve"): that message
;; replaced the answer of M-x emacs-version two seconds after it
;; appeared, and abbreviations are no help to someone learning names.
(setq extended-command-suggest-shorter nil)

;;;; Lesson 1.2  Kill, yank and the kill ring

;; Typing replaces the selection, as in every other editor.
(delete-selection-mode 1)
(setq kill-do-not-save-duplicates t
      save-interprogram-paste-before-kill t)

;;;; Lesson 2.3  Opening files, saving

;; Files save themselves 5 s after you stop typing
;; (auto-save-visited-mode), so nothing is lost when a frame or the
;; daemon goes away.  No backup~ files, #auto-save# files or .#lock
;; files appear next to your files: the workspace is a git repository
;; and the playground takes checkpoints.
(setq auto-save-visited-interval 5
      auto-save-default nil
      make-backup-files nil
      create-lockfiles nil
      require-final-newline t)
(auto-save-visited-mode 1)
;; Never auto-save over a file that changed on disk since it was visited
;; (a lesson reset, git, a formatter): `basic-save-buffer' would ask
;; "changed since visited or saved.  Save anyway?" from a timer.
;; Unmodified buffers follow the file (global-auto-revert-mode below); a
;; modified one keeps its edits until you save or revert it yourself.
(setq auto-save-visited-predicate
      (lambda () (verify-visited-file-modtime (current-buffer))))

(defun playground--quiet-auto-save (orig &optional arg pred)
  "Around advice for `save-some-buffers' (ORIG with ARG and PRED).
`auto-save-visited-mode' calls it with ARG `:no-prompt'; those saves
are silent so the echo area does not say \"Wrote ...\" every few
seconds."
  (if (eq arg :no-prompt)
      (let ((save-silently t) (inhibit-message t))
        (funcall orig arg pred))
    (funcall orig arg pred)))
(advice-add 'save-some-buffers :around #'playground--quiet-auto-save)

;; Buffers follow files changed on disk (by git, a formatter, a reset).
(setq global-auto-revert-non-file-buffers t
      auto-revert-verbose nil)
(global-auto-revert-mode 1)

;; Two files called main.rs show up as main.rs<hello> and main.rs<poly>.
(setq uniquify-buffer-name-style 'post-forward-angle-brackets)

;; Remember the minibuffer history, recent files, and where you were in
;; each file.
(setq history-length 500
      recentf-max-saved-items 200
      recentf-exclude '("/elpa/" "/var/" "\\`/tmp/" "/\\.git/"))
(savehist-mode 1)
(recentf-mode 1)
(save-place-mode 1)

;;;; Lesson 2.2  The minibuffer

(setq use-short-answers t
      enable-recursive-minibuffers nil
      completions-detailed t)

;;;; Editing defaults

(setq-default indent-tabs-mode nil
              tab-width 4
              fill-column 80)
(setq sentence-end-double-space nil
      scroll-conservatively 101
      scroll-preserve-screen-position t)
(global-so-long-mode 1)

;;;; Quitting (emacs.md section 7)

;; A frame that opens again (after C-x C-c, or the GUI frame) shows the
;; file you were working on, not *scratch*.
(defun playground--last-buffer ()
  "The most recently used file buffer, else *scratch*."
  (or (seq-find #'buffer-file-name (buffer-list))
      (get-buffer-create "*scratch*")))
(setq initial-buffer-choice #'playground--last-buffer)

;; In the playground, C-x C-c closes this frame only; the daemon keeps
;; every buffer and the playground reopens the frame.  It does not ask
;; about unsaved files (they are saved, as auto-save-visited would do a
;; few seconds later) and says what happened.
(defun playground--quit-frame (orig &optional arg)
  "Around advice for `save-buffers-kill-terminal' (ORIG with ARG).
In the playground daemon, a frame that belongs to an emacsclient
closes with its client; any other frame (one made with
`playground-gui-frame', say) is deleted on its own, so C-x C-c never
stops the daemon itself."
  (cond
   ((not (and (playground-p) (daemonp)))
    (funcall orig arg))
   ((frame-parameter nil 'client)
    (message "C-x C-c closes this frame; the playground reopens it in a moment. Your buffers stay open.")
    (funcall orig t))
   (t
    (save-some-buffers t)
    (message "C-x C-c closed this frame. Your buffers stay open.")
    (delete-frame nil t))))
(advice-add 'save-buffers-kill-terminal :around #'playground--quit-frame)

(setq confirm-kill-processes nil)

;;;; The terminal frame (emacs.md section 5)

;; Mouse: clicks, dragging window dividers and the wheel work in the
;; terminal.  Emacs 31 turns this on by itself only for terminals it
;; recognises; behind tmux it must be asked.
(xterm-mouse-mode 1)

;; Clipboard: killed text goes to the browser's clipboard through the
;; OSC 52 escape sequence (tmux forwards it with set-clipboard on).
;; Under TERM=tmux-256color Emacs reads the tmux variant of the list.
(setq xterm-extra-capabilities '(modifyOtherKeys setSelection)
      xterm-tmux-extra-capabilities '(modifyOtherKeys setSelection))

;; clipetty did the same before Emacs knew how; only for older Emacs.
(when (and (< emacs-major-version 29) (playground-package-p 'clipetty))
  (global-clipetty-mode 1))

;; Keys: the browser terminal speaks the kitty keyboard protocol and
;; tmux forwards modified keys as CSI u sequences (extended-keys always,
;; extended-keys-format csi-u).  kkp decodes them, which is what makes
;; C-;, C-RET, C-S-s and C-M-% distinguishable from plain keys.
(playground-with-package kkp
  (global-kkp-mode 1)

  ;; Home, End and Delete.  tmux sends them to the pane the way its own
  ;; terminal type (tmux-256color) spells them: ESC [ 1 ~, ESC [ 4 ~ and
  ;; ESC [ 3 ~.  The decoders below take every ESC [ <digit> sequence,
  ;; and kkp's table of ~ keys knows 7 and 8 for Home and End (rxvt) but
  ;; not 1 and 4, so it fell back to the code point: End arrived as C-d
  ;; (delete-char, which deleted text) and Home as C-a.  kkp also names
  ;; Delete <delete>, which a terminal's `local-function-key-map' turns
  ;; into DEL (delete-backward-char): Delete deleted backwards.  Here
  ;; Home and End are <home> and <end>, and Delete is <deletechar>
  ;; (delete-forward-char) as in a graphical frame; with a modifier it
  ;; keeps its name, so C-<delete> stays kill-word.
  (defvar kkp--non-printable-keys-with-tilde-terminator)
  (with-eval-after-load 'kkp
    (dolist (key '((1 . "<home>") (4 . "<end>")))
      (unless (assq (car key) kkp--non-printable-keys-with-tilde-terminator)
        (push key kkp--non-printable-keys-with-tilde-terminator))))

  (defun playground--kkp-decode (prefix)
    "Read the rest of a CSI sequence that began with PREFIX; return its key."
    (let ((keys (kkp--process-keys prefix)))
      (if (equal keys [delete]) [deletechar] keys)))

  ;; kkp switches itself on only after the terminal answers the kitty
  ;; protocol query.  tmux 3.5 forwards the keys but never answers, so
  ;; in tmux frames the decoders are installed directly.
  (defun playground--kkp-tmux (&optional _terminal)
    "Decode CSI u keys in a tmux terminal frame."
    (require 'kkp)
    (when (and (not (display-graphic-p))
               (string-prefix-p "tmux" (or (getenv "TERM" (selected-frame)) ""))
               (boundp 'kkp--key-prefixes))
      (dolist (prefix kkp--key-prefixes)
        (define-key input-decode-map (kkp--csi-escape (string prefix))
                    (lambda (_prompt) (playground--kkp-decode prefix))))))
  (add-hook 'tty-setup-hook #'playground--kkp-tmux 90))

;; Truecolor is announced with COLORTERM=truecolor (set in the image).

;;;; The GUI frame (emacs.md section 6)

;; Iosevka Nerd Font is the default (early-init.el); Chinese, Japanese
;; and Korean text falls back to Noto CJK.  Fontsets exist only once a
;; graphical frame does, so this runs when the first one opens.
(defvar playground--gui-fonts-done nil)
(defun playground--gui-fonts (&optional frame)
  "Set CJK fallback fonts the first time a graphical FRAME appears."
  (let ((frame (or frame (selected-frame))))
    (when (and (display-graphic-p frame) (not playground--gui-fonts-done))
      (setq playground--gui-fonts-done t)
      (dolist (script '(han kana hangul cjk-misc bopomofo))
        (set-fontset-font t script (font-spec :family "Noto Sans CJK SC") nil 'append)))))
(add-hook 'after-make-frame-functions #'playground--gui-fonts)
(add-hook 'server-after-make-frame-hook #'playground--gui-fonts)
(when (display-graphic-p) (playground--gui-fonts))

;; No window manager runs under Xvnc: never try to raise or move frames.
(setq pop-up-frames nil)

;;; base.el ends here
