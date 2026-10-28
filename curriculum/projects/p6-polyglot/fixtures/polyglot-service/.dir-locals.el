;;; Directory-local variables: one build command per root.
;; C-x p c offers the compile-command of the file you are in; these
;; entries set it per subdirectory.  Files read them when visited:
;; after a change, C-x x g (revert-buffer) re-reads them.
(("service/" . ((nil . ((compile-command . "cargo build")))))
 ("tools/"   . ((nil . ((compile-command . "python3 -m unittest -v")))))
 ("proof/"   . ((nil . ((compile-command . "dune test")))))
 ("report/"  . ((nil . ((compile-command . "")))))) ; fill in: typst compile report.typ
