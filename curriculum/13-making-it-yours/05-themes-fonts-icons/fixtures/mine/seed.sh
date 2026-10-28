#!/bin/sh
# Copies starter files from seed/ into your configuration's user-lisp/,
# but only those that are not there yet: once a file is in user-lisp/
# it is yours, and no lesson overwrites it.
set -eu
dest=../emacs.d/user-lisp
mkdir -p "$dest"
for f in "$@"; do
  [ -e "$dest/$f" ] || cp "seed/$f" "$dest/$f"
done
