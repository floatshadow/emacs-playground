#!/bin/sh
# Entry point of the remote box (deployment.md §6), run as root under tini.
#
# 1. Generate SSH host keys on first start (they are not in the image, so
#    every box has its own).
# 2. Record where the learner's public keys come from:
#      AUTHORIZED_KEYS       the keys themselves (newline-separated)
#      AUTHORIZED_KEYS_FILE  a file read at every login (may be a read-only
#                            mount that appears or changes later)
#    sshd asks /usr/local/sbin/remote-box-authorized-keys at each login.
# 3. Run sshd in the foreground, logging to stderr.
set -eu

ssh-keygen -A >/dev/null

umask 077
printf '%s\n' "${AUTHORIZED_KEYS:-}" > /etc/remote-box/keys
printf '%s\n' "${AUTHORIZED_KEYS_FILE:-}" > /etc/remote-box/keys-file
chmod 0600 /etc/remote-box/keys /etc/remote-box/keys-file

if [ -z "${AUTHORIZED_KEYS:-}" ] && [ -z "${AUTHORIZED_KEYS_FILE:-}" ]; then
  echo "remote-box: neither AUTHORIZED_KEYS nor AUTHORIZED_KEYS_FILE is set; nobody can log in" >&2
fi

if [ "$#" -gt 0 ]; then
  exec "$@"
fi
exec /usr/sbin/sshd -D -e
