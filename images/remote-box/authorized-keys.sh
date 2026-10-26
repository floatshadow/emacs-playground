#!/bin/sh
# sshd AuthorizedKeysCommand: print the public keys allowed to log in as
# the user in $1 (only `deploy`). Reads what the entrypoint recorded:
# the AUTHORIZED_KEYS value and the AUTHORIZED_KEYS_FILE path, the latter
# at every login so keys added after start are honoured.
[ "${1:-}" = "deploy" ] || exit 0
cat /etc/remote-box/keys 2>/dev/null
file="$(cat /etc/remote-box/keys-file 2>/dev/null)"
if [ -n "$file" ] && [ -r "$file" ]; then
  cat "$file"
fi
exit 0
