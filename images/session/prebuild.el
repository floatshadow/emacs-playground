;;; prebuild.el --- check the configuration and ship its trampolines  -*- lexical-binding: t; -*-

;; Run at image build time (config stage), as root:
;;
;;   PG_MODULES="base agent ..." HOME=/tmp/x emacs --batch -l prebuild.el
;;
;; 1. Loads /opt/playground/emacs.d/init.el with the modules named in
;;    PG_MODULES; a module that fails to load fails the build.
;; 2. Loads the packages those modules configure lazily, so that the
;;    advice they add at load time is in place.
;; 3. Compiles a native-code trampoline into /opt/playground/eln-cache
;;    for every primitive that is now advised or redefined, and for the
;;    primitives packages are known to redefine while running (with
;;    `cl-letf').  Emacs needs one before native code calling the
;;    primitive sees the advice; it would otherwise compile it at run
;;    time into ~/.cache, which on a server may be a noexec tmpfs
;;    (early-init.el, emacs.md section 1).

;;; Code:

(require 'comp)
(require 'comp-run)

(defconst prebuild-eln-dir "/opt/playground/eln-cache/"
  "Where the image's own native code lives.")

;; Compile into the shipped directory, not ~/.emacs.d.
(setq native-comp-eln-load-path (cons prebuild-eln-dir (last native-comp-eln-load-path)))

(load "/opt/playground/emacs.d/init.el" nil t)
(defvar playground-module-errors)
(defvar playground-modules-loaded)
(when playground-module-errors
  (message "prebuild: module errors: %S" playground-module-errors)
  (kill-emacs 1))
(message "prebuild: modules ok: %S" playground-modules-loaded)

(defconst prebuild-features
  '(magit diff-hl git-timemachine eat eglot flymake eldoc-box apheleia dape consult-eglot
    corfu cape tempel vertico orderless marginalia consult embark embark-consult
    avy expreg multiple-cursors iedit vundo visual-replace ws-butler
    dirvish consult-dir ace-window popper burly olivetti helpful
    nerd-icons nerd-icons-dired nerd-icons-completion nerd-icons-corfu nerd-icons-ibuffer
    breadcrumb pulsar ligature kkp markdown-mode pet
    xt-mouse ibuffer dired project tab-bar winner windmove compile eshell
    rust-ts-mode python which-key server tramp)
  "Features the slice-1 modules configure but load lazily.")

(defconst prebuild-runtime-primitives
  '(message run-hooks write-region delete-frame all-completions execute-kbd-macro
    make-process read-char read-event read-key-sequence read-from-minibuffer
    completing-read yes-or-no-p insert-file-contents kill-buffer select-window
    set-window-buffer current-message sleep-for call-process process-send-string
    redisplay ding file-exists-p file-directory-p expand-file-name)
  "Primitives packages redefine or advise while running (seen in sessions).")

(dolist (f prebuild-features)
  (condition-case err (require f)
    (error (message "prebuild: require %s: %s" f (error-message-string err)))))

(let ((prims (copy-sequence prebuild-runtime-primitives)))
  (mapatoms (lambda (s)
              (when (and (fboundp s) (advice--p (symbol-function s))
                         (subr-primitive-p (advice--cd*r (symbol-function s))))
                (push s prims))))
  (setq prims (delete-dups prims))
  (dolist (p prims)
    (condition-case err
        (unless (comp--trampoline-search p)
          (comp-trampoline-compile p))
      (error (message "prebuild: trampoline %s: %s" p (error-message-string err)))))
  (let ((missing (seq-remove #'comp--trampoline-search prims)))
    (message "prebuild: %d trampolines in %s%s" (length prims) prebuild-eln-dir
             (if missing (format ", missing %S" missing) ""))
    (when missing (kill-emacs 1))))

;;; prebuild.el ends here
