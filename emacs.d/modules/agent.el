;;; agent.el --- the playground agent  -*- lexical-binding: t; -*-

;; Module: agent.  On from Part 0 in the playground.
;;
;; Turns on `playground-agent-mode' (lisp/playground-agent.el), which
;; tells the browser what each key did and what state Emacs is in, and
;; lets the lesson check your work and the Rescue button get you out of
;; trouble.  It reports; it never changes how editing works.
;;
;; The agent is part of the playground, not of your configuration: the
;; copy that ships with the image (/opt/playground/emacs.d/lisp, already
;; compiled) is preferred over the one in your emacs.d, so it always
;; speaks the same protocol as the sessiond it talks to.
;;
;; The mode starts after init.el has loaded every module, so the first
;; message it sends lists them all (and any that failed).  It stays off
;; in batch runs, with PLAYGROUND=0, and in a plain `emacs' (not the
;; daemon) when no playground socket exists.

(let ((image-lisp (or (getenv "PG_AGENT_LISP") "/opt/playground/emacs.d/lisp")))
  (when (file-exists-p (expand-file-name "playground-agent.el" image-lisp))
    (add-to-list 'load-path image-lisp)))

(require 'playground-agent)

(when (and (not noninteractive)
           (playground-p)
           (or (daemonp) (file-exists-p playground-agent-socket)))
  (if after-init-time
      (playground-agent-mode 1)
    (add-hook 'after-init-hook (lambda () (playground-agent-mode 1)) 90)))

;;; agent.el ends here
