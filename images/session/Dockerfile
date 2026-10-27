# syntax=docker/dockerfile:1.7
#
# Session image (emacs.md §1): Debian trixie, Emacs 31.1 built from source,
# toolchains, the Emacs packages pinned in emacs.d/packages.lock, the
# playground config and sessiond. Build context: the repository root.
#
#   docker build -f images/session/Dockerfile -t ghcr.io/floatshadow/emacs-playground/session:dev .
#
# Stages (emacs.md §1); the heavy ones are cached independently and only
# rebuild when their own pins in versions.env change (see "pins"):
#
#   | Stage       | Produces                                                     |
#   |-------------|--------------------------------------------------------------|
#   | pins        | versions.env split per stage                                 |
#   | sessiond    | static sessiond binary (Go, CGO off)                         |
#   | emacs-build | Emacs 31.1 in /usr/local with native code for every Lisp     |
#   |             | file (native-gaps.el fills what the AOT build skips),        |
#   |             | tree-sitter grammars, the Debian packages Emacs needs        |
#   | rust        | rustup + the pinned stable toolchain with rust-analyzer,     |
#   |             | clippy, rustfmt, rust-src in /opt/rust                       |
#   | python      | uv, basedpyright, ruff in /opt/uv                            |
#   | fonts       | Iosevka Nerd Font (4 styles)                                 |
#   | toolchains  | runtime base: Debian packages (tmux 3.5a, TigerVNC Xvnc,     |
#   |             | xdotool, ripgrep, fd, git, Noto CJK, gcc for native code and |
#   |             | cargo) plus everything above                                 |
#   | packages    | the Emacs packages of packages.lock, sha256-checked, byte-   |
#   |             | and native-compiled into /opt/playground/{elpa,eln-cache}    |
#   | config      | /opt/playground/emacs.d (pristine config, agent compiled),   |
#   |             | /etc/tmux.conf, helper scripts, versions.env; prebuild.el    |
#   |             | loads the slice-1 modules and ships native trampolines       |
#   | final       | sessiond, user learner (uid 1000), environment, entrypoint   |
#
# TODO(emacs, emacs.md §2): OCaml (opam), TeX Live, Typst and tinymist,
# node LSP servers, delta, pandoc, enchant, poppler, Libertinus, Noto Color
# Emoji, codelldb and debugpy are not installed in slice 1.
# TODO(emacs, emacs.md §9): prefetched project dependencies (cargo fetch,
# uv sync, opam, typst packages) once the projects exist.
#
# Contract with sessiond (deployment.md §3, emacs.md §1): user learner uid
# 1000, home /home/learner, workspace /work, pristine config
# /opt/playground/emacs.d, live config /work/emacs.d symlinked from
# ~/.config/emacs, XDG_RUNTIME_DIR=/run/playground so the Emacs server
# socket is /run/playground/emacs/playground for both `emacs
# --fg-daemon=playground` and `emacsclient -s playground`, agent socket
# /run/playground/agent.sock, versions in /opt/playground/versions.env,
# sessiond at /usr/local/bin/sessiond, helpers in /opt/playground/bin
# (pg-terminal-frame, pg-xvnc, pg-gui-frame). The root filesystem may be
# read-only; /tmp, /run and /home/learner/.cache are tmpfs. Nothing needs
# compiling at run time except the learner's own Lisp: it goes to
# ~/.cache/emacs/eln-cache, or /work/.playground/eln-cache when ~/.cache
# is mounted noexec (Docker's tmpfs default), or nowhere (early-init.el).
#
# Size and time (2026-10-26, 96-core builder): 3.35 GB unpacked, 855 MB
# compressed; a cold build takes about 9 minutes (525 s), a config-only change
# about 15 seconds.

ARG GO_VERSION=1.26.10
ARG DEBIAN=trixie

# ---- pins -------------------------------------------------------------------
# One small file per stage, so that bumping (say) ruff does not rebuild
# Emacs: COPY --from=pins is cached by the content of the file it copies.
FROM debian:${DEBIAN}-slim AS pins
COPY images/session/versions.env /pins/versions.env
RUN cd /pins \
 && grep -E '^EMACS_' versions.env > emacs.env \
 && grep -E '^(RUSTUP|RUST)_' versions.env > rust.env \
 && grep -E '^(UV|BASEDPYRIGHT|RUFF)_' versions.env > python.env \
 && grep -E '^NERD_FONTS_' versions.env > fonts.env \
 && grep -E '^TMUX_' versions.env > tmux.env

# ---- sessiond ---------------------------------------------------------------
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
      -ldflags "-s -w -X github.com/floatshadow/emacs-playground/internal/buildinfo.Version=${VERSION}" \
      -o /out/sessiond ./cmd/sessiond

# ---- emacs-build ------------------------------------------------------------
FROM debian:${DEBIAN} AS emacs-build
RUN rm -f /etc/apt/apt.conf.d/docker-clean \
 && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
# Build dependencies for the configure flags below, including the image
# libraries (PNG, JPEG, GIF, TIFF, XPM, SVG, WebP, ImageMagick).
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      build-essential pkg-config ca-certificates curl xz-utils git \
      libgccjit-14-dev libtree-sitter-dev libgnutls28-dev libxml2-dev \
      libncurses-dev zlib1g-dev libgmp-dev libacl1-dev liblcms2-dev libdbus-1-dev \
      libjpeg-dev libpng-dev libgif-dev libtiff-dev libxpm-dev \
      librsvg2-dev libwebp-dev libmagickwand-dev libsqlite3-dev \
      libharfbuzz-dev libcairo2-dev libfreetype-dev libfontconfig-dev \
      libm17n-dev libotf-dev \
      libx11-dev libxt-dev libxaw7-dev libxext-dev libxi-dev libxrandr-dev \
      libxinerama-dev libxfixes-dev libxmu-dev
COPY --from=pins /pins/emacs.env /pins/emacs.env
RUN . /pins/emacs.env \
 && mkdir -p /src && cd /src \
 && curl -fsSL -o emacs.tar.xz "https://ftp.gnu.org/gnu/emacs/emacs-${EMACS_VERSION}.tar.xz" \
 && echo "${EMACS_SHA256}  emacs.tar.xz" | sha256sum -c - \
 && tar -xJf emacs.tar.xz && rm emacs.tar.xz
# Configure flags from emacs.md §1, except --with-json: Emacs 30 made JSON
# support built in and 31's configure no longer knows the option.
RUN . /pins/emacs.env && cd /src/emacs-${EMACS_VERSION} \
 && ./configure --prefix=/usr/local \
      --with-native-compilation=aot --with-tree-sitter --with-modules \
      --with-x --with-x-toolkit=lucid --with-cairo --with-harfbuzz --with-xinput2 \
      --with-rsvg --with-webp --with-imagemagick --with-gnutls --with-sqlite3 \
      --without-pop \
      CFLAGS="-O2 -g0" > /src/configure.log 2>&1 || { tail -50 /src/configure.log; exit 1; } \
 && make -j"$(nproc)" \
 && make install-strip \
 && /usr/local/bin/emacs --version | head -1 | grep -qx "GNU Emacs ${EMACS_VERSION}"
# The AOT build from the release tarball leaves about 200 of Emacs's own
# Lisp files without native code; compile them now so nothing is
# compiled into ~/.cache at run time (see native-gaps.el).
COPY images/session/native-gaps.el /src/native-gaps.el
RUN /usr/local/bin/emacs --batch -l /src/native-gaps.el
# Tree-sitter grammars at the revisions Emacs 31 itself pins
# (treesit-language-source-alist), compiled for the image's libtree-sitter.
RUN /usr/local/bin/emacs --batch --eval '(progn \
      (require (quote treesit)) \
      (dolist (f (quote (rust-ts-mode python toml-ts-mode json-ts-mode sh-script yaml-ts-mode))) (require f)) \
      (dolist (lang (quote (rust python toml json bash yaml))) \
        (treesit-install-language-grammar lang "/opt/playground/tree-sitter")) \
      (setq treesit-extra-load-path (list "/opt/playground/tree-sitter")) \
      (dolist (lang (quote (rust python toml json bash yaml))) \
        (unless (treesit-ready-p lang t) (error "grammar %s does not load" lang))))'
# Debian packages that provide the shared libraries Emacs links against,
# for the runtime stage.
RUN for lib in $(ldd /usr/local/bin/emacs | awk '/=> \//{print $3}'); do \
      real="$(readlink -f "$lib")"; \
      dpkg -S "$real" 2>/dev/null || dpkg -S "${real#/usr}" 2>/dev/null || dpkg -S "$lib" 2>/dev/null || echo "missing: $lib" >&2; \
    done | cut -d: -f1 | sort -u > /usr/local/share/emacs/runtime-packages.txt \
 && cat /usr/local/share/emacs/runtime-packages.txt

# ---- rust -------------------------------------------------------------------
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

# ---- python -----------------------------------------------------------------
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

# ---- fonts ------------------------------------------------------------------
FROM debian:${DEBIAN}-slim AS fonts
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl xz-utils
COPY --from=pins /pins/fonts.env /pins/fonts.env
RUN . /pins/fonts.env \
 && curl -fsSL -o /tmp/Iosevka.tar.xz \
      "https://github.com/ryanoasis/nerd-fonts/releases/download/v${NERD_FONTS_VERSION}/Iosevka.tar.xz" \
 && echo "${NERD_FONTS_IOSEVKA_SHA256}  /tmp/Iosevka.tar.xz" | sha256sum -c - \
 && mkdir -p /out/iosevka-nerd \
 && tar -xJf /tmp/Iosevka.tar.xz -C /out/iosevka-nerd \
      IosevkaNerdFont-Regular.ttf IosevkaNerdFont-Bold.ttf \
      IosevkaNerdFont-Italic.ttf IosevkaNerdFont-BoldItalic.ttf LICENSE.md \
 && rm /tmp/Iosevka.tar.xz

# ---- toolchains -------------------------------------------------------------
FROM debian:${DEBIAN}-slim AS toolchains
RUN rm -f /etc/apt/apt.conf.d/docker-clean \
 && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
COPY --from=emacs-build /usr/local/share/emacs/runtime-packages.txt /tmp/emacs-runtime-packages.txt
# gcc, binutils and libc6-dev serve both cargo (linking) and Emacs's
# native compiler (libgccjit) for packages the learner installs later.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl xz-utils zstd git openssh-client less procps psmisc file jq \
      tmux tini fontconfig fonts-noto-cjk \
      tigervnc-standalone-server xauth xdotool \
      ripgrep fd-find \
      python3 python3-venv \
      gcc libc6-dev binutils make pkg-config libgccjit0 \
      $(cat /tmp/emacs-runtime-packages.txt) \
 && rm /tmp/emacs-runtime-packages.txt
COPY --from=emacs-build /usr/local/ /usr/local/
COPY --from=emacs-build /opt/playground/tree-sitter /opt/playground/tree-sitter
COPY --from=fonts /out/iosevka-nerd /usr/local/share/fonts/iosevka-nerd
COPY --from=rust /opt/rust /opt/rust
COPY --from=python /opt/uv /opt/uv
COPY --from=pins /pins/tmux.env /pins/tmux.env
ENV LANG=C.UTF-8 \
    COLORTERM=truecolor \
    RUSTUP_HOME=/opt/rust/rustup \
    PATH=/opt/playground/bin:/opt/rust/cargo/bin:/opt/uv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
RUN ln -sf /usr/bin/fdfind /usr/local/bin/fd \
 && fc-cache -f >/dev/null \
 && fc-list | grep -q "Iosevka Nerd Font" \
 && fc-list | grep -q "Noto Sans CJK" \
 && if ldd /usr/local/bin/emacs | grep "not found"; then exit 1; fi \
 && emacs --batch --eval '(princ (format "%s native=%s treesit=%s\n" emacs-version (native-comp-available-p) (treesit-available-p)))' \
 && . /pins/tmux.env && tmux -V | grep -qx "tmux ${TMUX_VERSION}" \
 && Xvnc -version 2>&1 | head -2 \
 && rg --version | head -1 && fd --version && git --version \
 && rustc --version && basedpyright --version && ruff --version

# ---- packages ---------------------------------------------------------------
# Exactly packages.lock: vendored or downloaded files whose sha256 must
# match, installed in order with no package archive configured, then
# native-compiled. Any mismatch fails the build.
FROM toolchains AS packages
COPY emacs.d/packages.lock /src/emacs.d/packages.lock
COPY emacs.d/vendor /src/emacs.d/vendor
COPY emacs.d/tools/install.el /src/emacs.d/tools/install.el
RUN emacs --batch -l /src/emacs.d/tools/install.el -f playground-install-batch \
      /src/emacs.d /opt/playground/elpa /opt/playground/eln-cache > /src/install.log 2>&1 \
      || { tail -60 /src/install.log; exit 1; } \
 && grep -E '^(installing|native-compiled|installed) ' /src/install.log \
 && rm -rf /src

# ---- config -----------------------------------------------------------------
FROM packages AS config
COPY images/session/versions.env /opt/playground/versions.env
COPY images/session/tmux.conf /etc/tmux.conf
COPY images/session/tmux.conf /opt/playground/tmux.conf
COPY --chmod=0755 images/session/bin/ /opt/playground/bin/
COPY emacs.d/ /opt/playground/emacs.d/
COPY images/session/prebuild.el /src/prebuild.el
# The pristine config does not need the vendored tarballs. The agent is
# compiled in place (byte and native code), since agent.el loads this
# copy. prebuild.el loads the slice-1 modules (failing the build on any
# error) and ships the native trampolines they need.
RUN rm -rf /opt/playground/emacs.d/vendor \
 && cd /opt/playground/emacs.d/lisp \
 && emacs --batch \
      --eval '(setq native-comp-eln-load-path (cons "/opt/playground/eln-cache/" (last native-comp-eln-load-path)))' \
      --eval '(progn (byte-compile-file "playground-agent.el") (native-compile "playground-agent.el"))' \
 && { PG_MODULES="base agent editing completion files windows git terminals lsp lang-rust lang-python ui" \
      HOME=/tmp/pg-check emacs --batch -l /src/prebuild.el > /src/prebuild.log 2>&1 \
      || { tail -40 /src/prebuild.log; exit 1; }; } \
 && grep -E '^prebuild:' /src/prebuild.log \
 && test -z "$(find /opt/playground/emacs.d -name '*.eln')" \
 && chmod -R a+rX /opt/playground \
 && rm -rf /tmp/pg-check /src

# ---- final ------------------------------------------------------------------
FROM config AS final
ARG VERSION=dev
LABEL org.opencontainers.image.title="emacs-playground session" \
      org.opencontainers.image.description="Emacs 31.1 with the Emacs Playground configuration, toolchains and sessiond" \
      org.opencontainers.image.source="https://github.com/floatshadow/emacs-playground" \
      org.opencontainers.image.version="${VERSION}"
# safe.directory: the workspace is often owned by another uid (a bind
# mount from the host, a volume created by root), and git then refuses
# to work in it ("dubious ownership"), which breaks project.el, Magit
# and diff-hl.  The container has one user, so the check protects
# nothing here.
RUN useradd --uid 1000 --user-group --create-home --shell /bin/bash learner \
 && git config --system safe.directory '*' \
 && git config --system init.defaultBranch main \
 && mkdir -p /work /run/playground /home/learner/.config /home/learner/.cache \
 && ln -s /work/emacs.d /home/learner/.config/emacs \
 && chown learner:learner /work /run/playground /home/learner/.config /home/learner/.cache \
 && chmod 0700 /run/playground
COPY --from=sessiond /out/sessiond /usr/local/bin/sessiond
ENV HOME=/home/learner \
    USER=learner \
    XDG_RUNTIME_DIR=/run/playground \
    EDITOR="emacsclient -s playground" \
    VISUAL="emacsclient -s playground" \
    UV_PYTHON_DOWNLOADS=never
USER learner
WORKDIR /work
ENTRYPOINT ["/usr/bin/tini", "-s", "--", "/opt/playground/bin/pg-entrypoint"]
CMD ["sessiond"]
