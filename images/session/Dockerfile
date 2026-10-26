# syntax=docker/dockerfile:1.7
#
# Session image (emacs.md §1): Debian trixie, Emacs 31.1 built from source,
# toolchains, the Emacs packages pinned in emacs.d/packages.lock, the
# playground config and sessiond. Build context: the repository root.
#
#   docker build -f images/session/Dockerfile -t ghcr.io/zsy/emacs-playground/session:dev .
#
# Stage plan (emacs.md §1), each cached independently:
#
#   | Stage       | Produces                                                       |
#   |-------------|----------------------------------------------------------------|
#   | sessiond    | static sessiond binary (Go, CGO off)                          |
#   | emacs-build | Emacs 31.1 from the release tarball, installed to /usr/local  |
#   |             | (fallback 30.2 with the same flags; record which in           |
#   |             | versions.env)                                                  |
#   | toolchains  | rustup stable + rust-analyzer, clippy, rustfmt; uv +          |
#   |             | basedpyright, ruff; ripgrep, fd, git, tmux 3.5, TigerVNC      |
#   |             | Xvnc, xdotool, Iosevka Nerd Font, Noto CJK.                   |
#   |             | Slice 1 stubs (TODO): opam/OCaml, TeX Live, typst/tinymist,   |
#   |             | node LSP servers, delta, pandoc, enchant, poppler.            |
#   | packages    | Emacs packages from packages.lock, byte- and native-compiled  |
#   |             | (slice 1 modules: base agent editing completion files windows |
#   |             | ui terminals lsp lang-rust lang-python git)                   |
#   | config      | /opt/playground/emacs.d (read-only pristine config), tmux     |
#   |             | config, Xvnc helpers, prefetched project dependencies         |
#   | final       | sessiond, tini, user learner (uid 1000), entrypoint           |
#
# Emacs configure flags (emacs.md §1):
#   --with-native-compilation=aot --with-tree-sitter --with-json --with-modules
#   --with-x --with-x-toolkit=lucid --with-cairo --with-harfbuzz --with-xinput2
#   --with-rsvg --with-webp --with-imagemagick --with-gnutls --with-sqlite3
#   --without-pop
#
# Contract with sessiond (deployment.md §3, emacs.md §1): user learner uid
# 1000, home /home/learner, workspace /work, pristine config
# /opt/playground/emacs.d, live config /work/emacs.d symlinked from
# ~/.config/emacs, Emacs server socket dir /run/playground/emacs, agent
# socket /run/playground/agent.sock, versions at
# /opt/playground/versions.env, sessiond at /usr/local/bin/sessiond. The
# root filesystem may be read-only; /tmp, /run and /home/learner/.cache are
# tmpfs. Size budget: under 4 GB for slice 1.

ARG GO_VERSION=1.26.10

# ---- sessiond -------------------------------------------------------------
FROM golang:${GO_VERSION}-trixie AS sessiond
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed_stub.go ./web/
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath \
      -ldflags "-s -w -X github.com/zsy/emacs-playground/internal/buildinfo.Version=${VERSION}" \
      -o /out/sessiond ./cmd/sessiond

# ---- emacs-build ----------------------------------------------------------
# TODO(emacs, emacs.md §1): build dependencies, fetch and verify the 31.1
# release tarball, configure with the flags above, make, install to /usr/local.
FROM debian:trixie AS emacs-build

# ---- toolchains -----------------------------------------------------------
# TODO(emacs, emacs.md §2): runtime libraries for Emacs, the slice 1
# toolchains listed above, fonts; versions from images/session/versions.env.
FROM debian:trixie-slim AS toolchains

# ---- packages -------------------------------------------------------------
# TODO(emacs, emacs.md §8): install packages.lock with checksum verification,
# native-compile.
FROM toolchains AS packages

# ---- config ---------------------------------------------------------------
# TODO(emacs, emacs.md §3, §5, §6): emacs.d, tmux.conf, Xvnc helpers,
# prefetched dependencies (emacs.md §9).
FROM packages AS config

# ---- final ----------------------------------------------------------------
# TODO(emacs, emacs.md §1): tini, learner user, directories, entrypoint
# (tini -- sessiond), labels with versions.
FROM config AS final
ARG VERSION=dev
LABEL org.opencontainers.image.title="emacs-playground session" \
      org.opencontainers.image.source="https://github.com/zsy/emacs-playground" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=sessiond /out/sessiond /usr/local/bin/sessiond
