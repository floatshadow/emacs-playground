# syntax=docker/dockerfile:1.28
#
# Remote box image (deployment.md §6): a minimal Debian container with sshd,
# a `deploy` user and the session image's toolchains, without Emacs. It is
# the SSH target for the TRAMP lessons (Part 12) and the deploy tasks of
# projects P5 and P6. Build context: the repository root.
#
#   docker build -f images/remote-box/Dockerfile -t ghcr.io/floatshadow/emacs-playground/remote-box:dev .
#
# Stages:
#   | Stage  | Produces                                                        |
#   |--------|-----------------------------------------------------------------|
#   | pins   | versions.env split per stage (shared with the session image)    |
#   | rust   | the pinned Rust toolchain in /opt/rust (identical to the        |
#   |        | session image's stage, so a shared builder reuses its cache)    |
#   | python | uv, basedpyright, ruff in /opt/uv (same)                        |
#   | final  | openssh-server, user deploy (uid 1000), host keys generated at  |
#   |        | start, keys from AUTHORIZED_KEYS and/or AUTHORIZED_KEYS_FILE    |
#
# Run:
#   docker run -d -e AUTHORIZED_KEYS="$(cat ~/.ssh/id_ed25519.pub)" IMAGE
#   docker run -d -e AUTHORIZED_KEYS_FILE=/keys/authorized_keys -v "$PWD/keys:/keys:ro" IMAGE
# then `ssh deploy@<container>`. AUTHORIZED_KEYS_FILE is read at every
# login (sshd AuthorizedKeysCommand), so a key written after the box
# started works without a restart, whatever the file's owner and mode.

ARG DEBIAN=trixie

# ---- pins (same as images/session/Dockerfile) --------------------------------
FROM debian:${DEBIAN}-slim AS pins
COPY images/session/versions.env /pins/versions.env
RUN cd /pins \
 && grep -E '^EMACS_' versions.env > emacs.env \
 && grep -E '^(RUSTUP|RUST)_' versions.env > rust.env \
 && grep -E '^(UV|BASEDPYRIGHT|RUFF)_' versions.env > python.env \
 && grep -E '^NERD_FONTS_' versions.env > fonts.env \
 && grep -E '^TMUX_' versions.env > tmux.env

# ---- rust (same as images/session/Dockerfile) --------------------------------
FROM debian:${DEBIAN}-slim AS rust
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl gcc libc6-dev
COPY --from=pins /pins/rust.env /pins/rust.env
ENV RUSTUP_HOME=/opt/rust/rustup CARGO_HOME=/opt/rust/cargo
RUN . /pins/rust.env \
 && curl -fsSL -o /tmp/rustup-init \
      "https://static.rust-lang.org/rustup/archive/${RUSTUP_VERSION}/x86_64-unknown-linux-gnu/rustup-init" \
 && echo "${RUSTUP_INIT_SHA256}  /tmp/rustup-init" | sha256sum -c - \
 && chmod +x /tmp/rustup-init \
 && /tmp/rustup-init -y --no-modify-path --profile minimal \
      --default-toolchain "${RUST_TOOLCHAIN}" --component "${RUST_COMPONENTS}" \
 && rm /tmp/rustup-init \
 && rm -rf /opt/rust/rustup/downloads /opt/rust/rustup/tmp /opt/rust/cargo/registry \
 && /opt/rust/cargo/bin/rustc --version | grep -q "^rustc ${RUST_TOOLCHAIN} " \
 && /opt/rust/cargo/bin/rust-analyzer --version \
 && /opt/rust/cargo/bin/cargo clippy --version \
 && /opt/rust/cargo/bin/rustfmt --version

# ---- python (same as images/session/Dockerfile) ------------------------------
FROM debian:${DEBIAN}-slim AS python
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl python3
COPY --from=pins /pins/python.env /pins/python.env
ENV UV_TOOL_DIR=/opt/uv/tools UV_TOOL_BIN_DIR=/opt/uv/bin UV_PYTHON_DOWNLOADS=never \
    UV_PYTHON=/usr/bin/python3 UV_NO_CACHE=1
RUN . /pins/python.env \
 && curl -fsSL -o /tmp/uv.tar.gz \
      "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-x86_64-unknown-linux-gnu.tar.gz" \
 && echo "${UV_SHA256}  /tmp/uv.tar.gz" | sha256sum -c - \
 && tar -xzf /tmp/uv.tar.gz -C /tmp \
 && mkdir -p /opt/uv/bin \
 && install -m 0755 /tmp/uv-x86_64-unknown-linux-gnu/uv /tmp/uv-x86_64-unknown-linux-gnu/uvx /opt/uv/bin/ \
 && rm -rf /tmp/uv.tar.gz /tmp/uv-x86_64-unknown-linux-gnu \
 && /opt/uv/bin/uv tool install "basedpyright==${BASEDPYRIGHT_VERSION}" \
 && /opt/uv/bin/uv tool install "ruff==${RUFF_VERSION}" \
 && /opt/uv/bin/basedpyright --version \
 && /opt/uv/bin/ruff --version | grep -q "${RUFF_VERSION}"

# ---- final ------------------------------------------------------------------
FROM debian:${DEBIAN}-slim AS final
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openssh-server tini ca-certificates curl git rsync less procps \
      gcc libc6-dev make pkg-config python3 python3-venv ripgrep fd-find \
 && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
 && rm -f /etc/ssh/ssh_host_*
COPY --from=rust /opt/rust /opt/rust
COPY --from=python /opt/uv /opt/uv
COPY images/session/versions.env /opt/remote-box/versions.env
COPY --chmod=0755 images/remote-box/entrypoint.sh /usr/local/sbin/remote-box-entrypoint
COPY --chmod=0755 images/remote-box/authorized-keys.sh /usr/local/sbin/remote-box-authorized-keys
COPY images/remote-box/sshd_config /etc/ssh/sshd_config.d/remote-box.conf
# The deploy user logs in with keys only; '*' (not '!') keeps the account
# usable for public-key logins while no password can ever match.
RUN useradd --uid 1000 --user-group --create-home --shell /bin/bash deploy \
 && usermod -p '*' deploy \
 && install -d -m 0700 -o deploy -g deploy /home/deploy/.ssh \
 && install -d -m 0755 /run/sshd /etc/remote-box \
 && printf '%s\n' \
      'PATH=/opt/rust/cargo/bin:/opt/uv/bin:/usr/local/bin:/usr/bin:/bin' \
      'RUSTUP_HOME=/opt/rust/rustup' \
      'LANG=C.UTF-8' > /etc/environment \
 && printf '%s\n' \
      'export PATH=/opt/rust/cargo/bin:/opt/uv/bin:$PATH' \
      'export RUSTUP_HOME=/opt/rust/rustup' > /etc/profile.d/remote-box.sh \
 && /opt/rust/cargo/bin/rustc --version && /opt/uv/bin/ruff --version \
 && sshd -t -f /etc/ssh/sshd_config -h /dev/null 2>&1 | grep -v -i "no hostkeys" || true
ENV PATH=/opt/rust/cargo/bin:/opt/uv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    RUSTUP_HOME=/opt/rust/rustup \
    LANG=C.UTF-8
EXPOSE 22
LABEL org.opencontainers.image.title="emacs-playground remote box" \
      org.opencontainers.image.source="https://github.com/floatshadow/emacs-playground"
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/sbin/remote-box-entrypoint"]
