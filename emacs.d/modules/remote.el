;;; remote.el --- TRAMP and remote work  -*- lexical-binding: t; -*-

;; Module: remote.  Unlocked in Part 12 (Remote editing with TRAMP).
;;
;; TRAMP is built in: /ssh:box:/path opens a file on the remote box,
;; and Dired, compile, Eshell, Magit and Eglot work on remote
;; directories.  This module makes it quick and quiet: one shared SSH
;; connection per host (ControlMaster), the remote login PATH (the box
;; puts Rust and uv on it), no auto-saving over the network, caches
;; that keep directory listings fast, and sudo through ssh
;; (/ssh:box|sudo::/etc/...) without a password prompt, which the box
;; allows for its deploy user.  The SSH key and the `box' host alias
;; come from ~/.ssh/config, which the playground writes.
;;
;; TRAMP's progress ("Tramp: Opening connection ...") reaches the
;; browser like any other echo-area message.
;;
;; Keys, by lesson (all built in):
;;   12.1  C-x C-f /ssh:box:/home/deploy/ RET; C-x C-s saves over SSH;
;;         M-x tramp-cleanup-all-connections forgets every connection
;;   12.2  M-x compile and M-x eshell in a remote directory;
;;         Eglot starts the box's rust-analyzer for remote Rust files
;;   12.3  C-x g in a remote directory; /ssh:box|sudo::/etc/nginx/nginx.conf
;;   12.4  /ssh:box|sudo:www-data@box:/srv/sidecar/ (a hop to another user);
;;         M-x tramp-cleanup-this-connection; /docker:NAME:/ and
;;         /podman:NAME:/ (tramp-container) need a container runtime

;;;; Lesson 12.1  TRAMP basics

(setq tramp-default-method "ssh"
      tramp-use-connection-share t
      tramp-verbose 3
      tramp-completion-reread-directory-timeout 60
      remote-file-name-inhibit-cache 60
      remote-file-name-inhibit-locks t
      remote-file-name-inhibit-auto-save-visited t
      tramp-auto-save-directory (playground-var "tramp-auto-save/"))

;; Faster: files under version control on the remote side are not
;; checked by every VC backend Emacs knows, only git.
(setq vc-handled-backends '(Git))

;;;; Lesson 12.2  Remote compile, Eshell and LSP

;; The remote PATH is the one a login shell on the box gets, so cargo,
;; rust-analyzer and uv are found there.
(with-eval-after-load 'tramp
  (add-to-list 'tramp-remote-path 'tramp-own-remote-path))

;; Processes that talk through a pipe, such as Eglot's language
;; servers, start as their own `ssh box COMMAND' (TRAMP's direct async
;; processes) with no tty on the box, so bytes pass unchanged.  Started
;; the classic way, from a remote shell, rust-analyzer often lost the
;; first kilobytes of Eglot's `initialize' request and Eglot timed out.
;; Terminals and builds want a tty on the box and keep the classic way.
(with-eval-after-load 'tramp
  (connection-local-set-profile-variables
   'playground-remote-direct-async '((tramp-direct-async-process . t)))
  (connection-local-set-profiles
   '(:application tramp :protocol "ssh") 'playground-remote-direct-async)
  (add-to-list 'tramp-connection-properties
               (list (regexp-quote "/ssh:") "direct-async" t))
  (define-advice tramp-direct-async-process-p
      (:around (orig &rest args) playground-pipes-only)
    "Use a direct async process only for a pipe connection."
    (and (eq (plist-get (if (keywordp (car args)) args (car args))
                        :connection-type)
             'pipe)
         (apply orig args))))

;;;; Lesson 12.3  Magit over TRAMP and sudo

;; Nothing to set: /ssh:box|sudo::/etc/... chains ssh to the box and
;; then sudo on it, and Magit runs git on the box through TRAMP.

;;;; Lesson 12.4  Containers and other hops

;; tramp-container (built in, autoloaded) adds /docker:, /podman: and
;; /kubernetes:; each needs its runtime's command on this side, which
;; the session image does not ship.

;;; remote.el ends here
