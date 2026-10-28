#!/bin/sh
# CI smoke test for a remote-box image: sshd, sudo, nginx, Emacs and the
# toolchains are present. Usage: ci-smoke.sh IMAGE_REF
set -eu
docker run --rm --entrypoint sh "$1" -c \
  'test -x /usr/sbin/sshd && ls /opt/rust /opt/uv >/dev/null && emacs --version | head -1 && test -x /usr/sbin/nginx && test -u /usr/bin/sudo && echo ok'
