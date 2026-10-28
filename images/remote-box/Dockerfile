# syntax=docker/dockerfile:1.28
#
# Remote box image (deployment.md §6): Debian with sshd, a `deploy` user,
# Emacs with the pinned packages (lesson 13.6 clones the playground
# config here and starts it) and the session's Rust and Python
# toolchains with the same prefetched crates and wheels, so remote
# builds work offline (lessons 12.2, 9.5; projects P5, P6, P7). It is
# the SSH target for the TRAMP lessons (Part 12) and the deploy tasks of
# P5 (a static site served by nginx from /srv/www) and P6 (a release
# build in /srv/app). Build context: the repository root.
#
#   docker build -f images/remote-box/Dockerfile -t ghcr.io/floatshadow/emacs-playground/remote-box:dev .
#
# Every stage but `final` is a verbatim copy of the session image's
# stage of the same name (images/session/Dockerfile), so a shared
# builder (or a shared cache) builds each once; keep them identical.
#
#   | Stage           | Produces                                               |
#   |-----------------|--------------------------------------------------------|
#   | pins            | versions.env split per stage                           |
#   | emacs-build     | Emacs 31.1 with native code and tree-sitter grammars   |
#   | natives         | vterm, jinx and pdf-tools' compiled parts              |
#   | emacs-runtime   | Debian slim with Emacs and its libraries               |
#   | packages        | packages.lock in /opt/playground/{elpa,eln-cache}      |
#   | rust, python    | the pinned Rust toolchain; uv and ruff                 |
#   | prefetch-cargo, | the prefetched crates (copied into deploy's ~/.cargo)  |
#   | prefetch-python | and wheels (uv's find-links)                           |
#   | final           | openssh-server, sudo, nginx, user deploy (uid 1000),   |
#   |                 | host keys generated at start, keys from                |
#   |                 | AUTHORIZED_KEYS and/or AUTHORIZED_KEYS_FILE            |
#
# Run:
#   docker run -d -e AUTHORIZED_KEYS="$(cat ~/.ssh/id_ed25519.pub)" IMAGE
#   docker run -d -e AUTHORIZED_KEYS_FILE=/keys/authorized_keys -v "$PWD/keys:/keys:ro" IMAGE
# then `ssh deploy@<container>`. AUTHORIZED_KEYS_FILE is read at every
# login (sshd AuthorizedKeysCommand), so a key written after the box
# started works without a restart, whatever the file's owner and mode.

ARG DEBIAN=trixie

# ---- pins -------------------------------------------------------------------
# One small file per stage, so that bumping (say) ruff does not rebuild
# Emacs: COPY --from=pins is cached by the content of the file it copies.
FROM debian:${DEBIAN}-slim AS pins
COPY images/session/versions.env /pins/versions.env
RUN cd /pins \
 && grep -E '^EMACS_' versions.env > emacs.env \
 && grep -E '^(RUSTUP|RUST)_' versions.env > rust.env \
 && grep -E '^(UV|RUFF|DEBUGPY|PIP)_' versions.env > python.env \
 && grep -E '^NERD_FONTS_' versions.env > fonts.env \
 && grep -E '^TMUX_' versions.env > tmux.env \
 && grep -E '^(OPAM|OCAML)_' versions.env > ocaml.env \
 && grep -E '^(TYPST|TINYMIST)_' versions.env > typst.env \
 && grep -E '^(TEXLIVE|TEXLAB)_' versions.env > texlive.env \
 && grep -E '^NODE_' versions.env > node.env \
 && grep -E '^(MARKSMAN|PANDOC|DELTA|SHELLCHECK|NEXTEST)_' versions.env > tools.env \
 && grep -E '^CODELLDB_' versions.env > debug.env

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
# The AOT build from the release tarball leaves some of Emacs's own Lisp
# files without native code; compile them now so nothing is compiled
# into ~/.cache at run time (see native-gaps.el).
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
# Grammars of the later parts: HTML and CSS (P5's docs site) at Emacs's
# revisions, Typst (typst-ts-mode, lesson 8.2) at the newest commit of
# the grammar typst-ts-mode follows (it has no recent release tag).
RUN /usr/local/bin/emacs --batch --eval '(progn \
      (require (quote treesit)) \
      (require (quote css-mode)) (require (quote html-ts-mode)) \
      (add-to-list (quote treesit-language-source-alist) \
        (quote (typst "https://github.com/uben0/tree-sitter-typst" :commit "46cf4ded12ee974a70bf8457263b67ad7ee0379d"))) \
      (dolist (lang (quote (css html typst))) \
        (unless (assq lang treesit-language-source-alist) (error "no source for grammar %s" lang)) \
        (treesit-install-language-grammar lang "/opt/playground/tree-sitter")) \
      (setq treesit-extra-load-path (list "/opt/playground/tree-sitter")) \
      (dolist (lang (quote (css html typst))) \
        (unless (treesit-ready-p lang t) (error "grammar %s does not load" lang))))'
# Debian packages that provide the shared libraries Emacs links against,
# for the runtime stage.
RUN for lib in $(ldd /usr/local/bin/emacs | awk '/=> \//{print $3}'); do \
      real="$(readlink -f "$lib")"; \
      dpkg -S "$real" 2>/dev/null || dpkg -S "${real#/usr}" 2>/dev/null || dpkg -S "$lib" 2>/dev/null || echo "missing: $lib" >&2; \
    done | cut -d: -f1 | sort -u > /usr/local/share/emacs/runtime-packages.txt \
 && cat /usr/local/share/emacs/runtime-packages.txt

# ---- natives ----------------------------------------------------------------
# The compiled parts of three packages, built from the same tarballs
# packages.lock pins: vterm's module against Debian's libvterm, jinx's
# against enchant, pdf-tools' epdfinfo server against poppler. Their
# packages would otherwise compile them on first use, with a prompt.
FROM debian:${DEBIAN} AS natives
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      build-essential pkg-config cmake autoconf automake \
      libvterm-dev libenchant-2-dev libpoppler-glib-dev libpoppler-private-dev \
      libpng-dev zlib1g-dev
COPY --from=emacs-build /usr/local/include/emacs-module.h /usr/local/include/emacs-module.h
COPY emacs.d/vendor/vterm-*.tar emacs.d/vendor/jinx-*.tar emacs.d/vendor/pdf-tools-*.tar /src/
RUN cd /src && for t in *.tar; do tar -xf "$t"; done && mkdir -p /out \
 && cd /src/vterm-*/ && mkdir build && cd build \
 && cmake -DUSE_SYSTEM_LIBVTERM=yes -DCMAKE_BUILD_TYPE=Release .. > cmake.log \
 && make -s && cp ../vterm-module.so /out/ \
 && cd /src/jinx-*/ \
 && gcc -I. -O2 -Wall -Wextra -fPIC -shared -o jinx-mod.so jinx-mod.c $(pkg-config --cflags --libs enchant-2) \
 && cp jinx-mod.so /out/ \
 && cd /src/pdf-tools-*/server \
 && ./autogen.sh > autogen.log 2>&1 \
 && { ./configure -q > configure.log 2>&1 || { tail -30 configure.log; exit 1; }; } \
 && make -s && strip epdfinfo && cp epdfinfo /out/ \
 && ls -l /out

# ---- emacs-runtime ----------------------------------------------------------
# What running Emacs and its pinned packages needs; the base of the
# toolchains stage and of the remote box (Part 13.6 starts Emacs there).
# gcc, binutils and libc6-dev serve Emacs's native compiler (libgccjit)
# and, later, cargo's linker.
FROM debian:${DEBIAN}-slim AS emacs-runtime
RUN rm -f /etc/apt/apt.conf.d/docker-clean \
 && echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache
COPY --from=emacs-build /usr/local/share/emacs/runtime-packages.txt /tmp/emacs-runtime-packages.txt
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl git less procps file \
      gcc libc6-dev binutils make pkg-config libgccjit0 \
      libvterm0 libenchant-2-2 hunspell-en-us hunspell-en-gb libpoppler-glib8t64 \
      $(cat /tmp/emacs-runtime-packages.txt) \
 && rm /tmp/emacs-runtime-packages.txt
COPY --from=emacs-build /usr/local/ /usr/local/
COPY --from=emacs-build /opt/playground/tree-sitter /opt/playground/tree-sitter
ENV LANG=C.UTF-8
RUN if ldd /usr/local/bin/emacs | grep "not found"; then exit 1; fi \
 && emacs --batch --eval '(princ (format "%s native=%s treesit=%s\n" emacs-version (native-comp-available-p) (treesit-available-p)))'

# ---- packages ---------------------------------------------------------------
# Exactly packages.lock: vendored files whose sha256 must match,
# installed in order with no package archive configured, then
# native-compiled. Any mismatch fails the build. Then the native parts go
# into their package directories, where the packages look for them, and
# each is loaded once.
FROM emacs-runtime AS packages
COPY emacs.d/packages.lock /src/emacs.d/packages.lock
COPY emacs.d/vendor /src/emacs.d/vendor
COPY emacs.d/tools/install.el /src/emacs.d/tools/install.el
RUN emacs --batch -l /src/emacs.d/tools/install.el -f playground-install-batch \
      /src/emacs.d /opt/playground/elpa /opt/playground/eln-cache > /src/install.log 2>&1 \
      || { tail -60 /src/install.log; exit 1; } \
 && grep -E '^(installing|native-compiled|installed) ' /src/install.log \
 && rm -rf /src
COPY --from=natives /out/ /tmp/natives/
RUN set -e; elpa=/opt/playground/elpa \
 && install -m 0755 /tmp/natives/vterm-module.so "$(echo $elpa/vterm-[0-9]*)/" \
 && install -m 0755 /tmp/natives/jinx-mod.so "$(echo $elpa/jinx-[0-9]*)/" \
 && install -m 0755 /tmp/natives/epdfinfo "$(echo $elpa/pdf-tools-[0-9]*)/" \
 && rm -rf /tmp/natives \
 && if ldd $elpa/*/vterm-module.so $elpa/*/jinx-mod.so $elpa/*/epdfinfo | grep "not found"; then exit 1; fi \
 && HOME=/tmp/pg-natives emacs --batch \
      --eval '(setq package-user-dir "/opt/playground/elpa" native-comp-eln-load-path (cons "/opt/playground/eln-cache/" (last native-comp-eln-load-path)))' \
      --eval '(package-activate-all)' \
      --eval '(progn (require (quote vterm)) (require (quote jinx)) (jinx--load-module) (require (quote pdf-info)) (unless (file-executable-p pdf-info-epdfinfo-program) (error "no epdfinfo")) (princ (format "natives: vterm-module, jinx-mod (%s), %s\n" (jinx--mod-langs) pdf-info-epdfinfo-program)))' \
 && rm -rf /tmp/pg-natives

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
# debugpy lives in its own environment: dape runs its adapter with that
# Python, and the adapter starts the program with the project's.
RUN . /pins/python.env \
 && curl -fsSL -o /tmp/uv.tar.gz \
      "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-x86_64-unknown-linux-gnu.tar.gz" \
 && echo "${UV_SHA256}  /tmp/uv.tar.gz" | sha256sum -c - \
 && tar -xzf /tmp/uv.tar.gz -C /tmp \
 && mkdir -p /opt/uv/bin \
 && install -m 0755 /tmp/uv-x86_64-unknown-linux-gnu/uv /tmp/uv-x86_64-unknown-linux-gnu/uvx /opt/uv/bin/ \
 && rm -rf /tmp/uv.tar.gz /tmp/uv-x86_64-unknown-linux-gnu \
 && /opt/uv/bin/uv tool install "ruff==${RUFF_VERSION}" \
 && /opt/uv/bin/uv venv -q /opt/debugpy \
 && /opt/uv/bin/uv pip install -q --python /opt/debugpy/bin/python "debugpy==${DEBUGPY_VERSION}" \
 && /opt/uv/bin/ruff --version | grep -q "${RUFF_VERSION}" \
 && /opt/debugpy/bin/python -c 'import debugpy; print("debugpy", debugpy.__version__)'

# ---- prefetch-cargo ---------------------------------------------------------
# prefetch/cargo's crates (Cargo.lock, checksummed) as a cargo registry
# cache: the index entries and .crate files cargo needs to build offline.
# pg-entrypoint copies it to $CARGO_HOME when a container starts.
FROM rust AS prefetch-cargo
COPY images/session/prefetch/cargo /src/cargo
RUN cd /src/cargo && PATH=/opt/rust/cargo/bin:$PATH CARGO_HOME=/out/cargo cargo fetch --locked \
 && rm -rf /out/cargo/registry/src \
 && ls /out/cargo/registry/cache/*/ | wc -l

# ---- prefetch-python --------------------------------------------------------
# prefetch/python.txt's wheels, each checked against its hash, markers
# dropped so that wheels for other platforms' markers come too; uv reads
# the directory as find-links (UV_FIND_LINKS).
FROM python AS prefetch-python
COPY images/session/prefetch/python.txt /src/python.txt
RUN . /pins/python.env \
 && sed 's/ ; [^\\]*\\$/ \\/' /src/python.txt > /tmp/requirements.txt \
 && /opt/uv/bin/uvx --from "pip==${PIP_VERSION}" pip download -q --require-hashes --only-binary :all: \
      --no-deps -r /tmp/requirements.txt -d /out/wheels \
 && ls /out/wheels

# ---- final ------------------------------------------------------------------
# deploy may use sudo without a password (lesson 12.3 edits root-owned
# files through /ssh:box|sudo::), owns /srv/www, which nginx serves on
# port 80 (P5's site), and /srv/app (P6's release). Login shells get the
# toolchains and the offline settings through /etc/environment (PAM) and
# /etc/profile.d.
FROM emacs-runtime AS final
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      openssh-server tini sudo nginx rsync xz-utils zstd \
      python3 python3-venv ripgrep fd-find \
 && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
 && rm -f /etc/ssh/ssh_host_*
COPY --from=rust /opt/rust /opt/rust
COPY --from=python /opt/uv /opt/uv
COPY --from=packages /opt/playground/elpa /opt/playground/elpa
COPY --from=packages /opt/playground/eln-cache /opt/playground/eln-cache
COPY --from=prefetch-python /out/wheels /opt/playground/prefetch/wheels
COPY --from=prefetch-cargo --chown=1000:1000 /out/cargo/registry /home/deploy/.cargo/registry
COPY images/session/versions.env /opt/remote-box/versions.env
COPY --chmod=0755 images/remote-box/entrypoint.sh /usr/local/sbin/remote-box-entrypoint
COPY --chmod=0755 images/remote-box/authorized-keys.sh /usr/local/sbin/remote-box-authorized-keys
COPY images/remote-box/sshd_config /etc/ssh/sshd_config.d/remote-box.conf
COPY images/remote-box/nginx-site.conf /etc/nginx/sites-available/default
# The deploy user logs in with keys only; '*' (not '!') keeps the account
# usable for public-key logins while no password can ever match.
RUN useradd --uid 1000 --user-group --create-home --shell /bin/bash deploy \
 && usermod -p '*' deploy \
 && install -d -m 0700 -o deploy -g deploy /home/deploy/.ssh \
 && chown deploy:deploy /home/deploy/.cargo \
 && install -d -m 0755 /run/sshd /etc/remote-box \
 && install -d -m 0755 -o deploy -g deploy /srv/www /srv/app \
 && printf '%s\n' '<!doctype html><title>box</title><p>Nothing deployed yet.' > /srv/www/index.html \
 && chown deploy:deploy /srv/www/index.html \
 && printf 'deploy ALL=(ALL) NOPASSWD:ALL\n' > /etc/sudoers.d/deploy \
 && chmod 0440 /etc/sudoers.d/deploy && visudo -cq \
 && printf '%s\n' \
      'PATH=/opt/rust/cargo/bin:/opt/uv/bin:/usr/local/bin:/usr/bin:/bin' \
      'RUSTUP_HOME=/opt/rust/rustup' \
      'CARGO_NET_OFFLINE=true' \
      'UV_OFFLINE=1' \
      'UV_FIND_LINKS=/opt/playground/prefetch/wheels' \
      'UV_PYTHON_DOWNLOADS=never' \
      'LANG=C.UTF-8' > /etc/environment \
 && printf '%s\n' \
      'export PATH=/opt/rust/cargo/bin:/opt/uv/bin:$PATH' \
      'export RUSTUP_HOME=/opt/rust/rustup' \
      'export CARGO_NET_OFFLINE=true UV_OFFLINE=1' \
      'export UV_FIND_LINKS=/opt/playground/prefetch/wheels UV_PYTHON_DOWNLOADS=never' \
      > /etc/profile.d/remote-box.sh \
 && git config --system init.defaultBranch main \
 && chmod -R a+rX /opt/playground \
 && /opt/rust/cargo/bin/rustc --version && /opt/uv/bin/ruff --version && emacs --version | head -1 \
 && nginx -t \
 && sshd -t -f /etc/ssh/sshd_config -h /dev/null 2>&1 | grep -v -i "no hostkeys" || true
ENV PATH=/opt/rust/cargo/bin:/opt/uv/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    RUSTUP_HOME=/opt/rust/rustup \
    CARGO_NET_OFFLINE=true \
    UV_OFFLINE=1 \
    UV_FIND_LINKS=/opt/playground/prefetch/wheels \
    UV_PYTHON_DOWNLOADS=never \
    LANG=C.UTF-8
EXPOSE 22 80
LABEL org.opencontainers.image.title="emacs-playground remote box" \
      org.opencontainers.image.source="https://github.com/floatshadow/emacs-playground"
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/sbin/remote-box-entrypoint"]
