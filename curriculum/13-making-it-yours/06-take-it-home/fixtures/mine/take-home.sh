#!/bin/sh
# Lesson 13.6. Makes your configuration, /work/emacs.d, a git repository
# if it is not one yet, without a remote called box; gives the box an
# empty repository to push it to (standing in for GitHub) and a home
# without an Emacs configuration (a fresh machine). Every load starts
# over, so the lesson can be done again.
set -eu
cd ../emacs.d
if [ ! -d .git ]; then
  git init -q -b main
  git config user.name Learner
  git config user.email learner@playground.invalid
  git add -A
  git commit -q -m "My Emacs configuration"
fi
git remote remove box 2>/dev/null || true
git config --unset remote.pushDefault 2>/dev/null || true
ssh box 'pkill -u deploy -x emacs || true
rm -rf .emacs.d emacs.d.git
git init -q --bare -b main emacs.d.git'
