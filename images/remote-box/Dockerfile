# syntax=docker/dockerfile:1.7
#
# Remote box image (deployment.md §6): a minimal Debian container with sshd,
# a `deploy` user and the session toolchains, no Emacs. SSH target for TRAMP
# lessons (Part 12) and the deploy tasks of projects P5 and P6.
#
# Stage plan:
#   | Stage      | Produces                                                 |
#   |------------|----------------------------------------------------------|
#   | toolchains | the same toolchains as the session image (shared stage   |
#   |            | or copied from it), no Emacs                             |
#   | final      | openssh-server, user deploy, host keys generated at start,|
#   |            | AUTHORIZED_KEYS / AUTHORIZED_KEYS_FILE honoured           |
#
# TODO(M6, deployment.md §6): implement. Not part of slice 1; compose.local.yaml
# keeps the service behind the "remote" profile until then.
FROM debian:trixie-slim AS final
