;;; remote.el --- TRAMP and remote work  -*- lexical-binding: t; -*-

;; Module: remote.  Unlocked in Part 12 (Remote work and containers).
;;
;; Planned: TRAMP defaults for the remote box (ssh with connection
;; sharing through the ~/.ssh/config the playground writes),
;; tramp-container for docker and podman, remote compilation and shells,
;; and "connecting to ..." progress reported by the agent.
;;
;; TODO(emacs, emacs.md sections 3 and 10.1): not in slice 1; the agent
;; does not report TRAMP connection progress yet.

(setq tramp-default-method "ssh"
      tramp-use-connection-share t
      remote-file-name-inhibit-auto-save-visited t)

;;; remote.el ends here
