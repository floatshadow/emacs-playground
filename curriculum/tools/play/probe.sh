#!/bin/sh
# Evaluate one Lisp form in Emacs 31.1 with the harness configuration
# (modules base, editing, files, windows) and the fixture trees under
# /work, and print the value. For checking facts while writing demos.
#
#   curriculum/tools/play/probe.sh FORM.el
#
# Scratch files go to artifacts/probe/ (git-ignored).
set -eu
HERE=$(dirname "$(readlink -f "$0")")
CUR=$(readlink -f "$HERE/../..")
P=$(readlink -f "$CUR/..")/artifacts/probe
mkdir -p "$P"
chmod 777 "$P"
rm -rf "$P/work" "$P/out.txt"
mkdir -p "$P/work"
for t in projects editing hello; do cp -r "$CUR/tools/fixture-src/$t" "$P/work/"; done
chmod -R a+rwX "$P/work"
cp "$1" "$P/form.el"
chmod 666 "$P/form.el"
timeout 120 docker run --rm -t --user "$(id -u):$(id -g)" -e HOME=/tmp/home -e TERM=xterm-256color \
  -v "$P:/probe" -v "$P/work:/work" -v "$HERE:/play:ro" "${PG_PLAY_IMAGE:-pg-curriculum-play}" \
  script -qc "mkdir -p /tmp/home; cd /work; stty rows 36 cols 120; emacs -nw -Q -l /play/assumed-config.el -l /play/probe.el" /dev/null >/dev/null 2>&1 || true
cat "$P/out.txt"
