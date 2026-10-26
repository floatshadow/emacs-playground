;;; probe.el --- evaluate /probe/form.el for probe.sh  -*- lexical-binding: t; -*-
(mapc #'pg-play-enable-module '("base" "agent" "editing" "files" "windows"))
(run-with-timer
 0.5 nil
 (lambda ()
   (let ((out (condition-case err
                  (format "%S" (eval (car (read-from-string
                                           (with-temp-buffer
                                             (insert-file-contents "/probe/form.el")
                                             (buffer-string))))
                                     t))
                (error (format "ERR %S" err)))))
     (with-temp-file "/probe/out.txt" (insert out "\n")))
   (kill-emacs 0)))
;;; probe.el ends here
