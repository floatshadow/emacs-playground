#!/bin/sh
# CI smoke test for a session image: Emacs starts and both ERT suites pass.
# Usage: ci-smoke.sh IMAGE_REF
set -eu
run() { docker run --rm --entrypoint sh "$1" -c "$2"; }
run "$1" 'emacs --version | head -1'
run "$1" 'cd /opt/playground/emacs.d && emacs --batch -L lisp -l playground-agent -l test/agent-test.el -f ert-run-tests-batch-and-exit'
run "$1" 'cd /opt/playground/emacs.d && emacs --batch -l early-init.el -l test/config-test.el -f ert-run-tests-batch-and-exit'
