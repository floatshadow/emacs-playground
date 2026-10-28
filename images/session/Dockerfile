# syntax=docker/dockerfile:1.28
#
# Session image (emacs.md §1): Debian trixie, Emacs 31.1 built from source,
# toolchains, the Emacs packages pinned in emacs.d/packages.lock, the
# playground config and sessiond. Build context: the repository root.
#
#   docker build -f images/session/Dockerfile -t ghcr.io/floatshadow/emacs-playground/session:dev .
#
# Stages (emacs.md §1, §2, §9). Each heavy one is cached on its own and
# rebuilds only when its inputs change: its pins in versions.env (through
# the per-stage files of "pins") or its list under images/session/prefetch.
#
#   | Stage          | Produces                                                |
#   |----------------|---------------------------------------------------------|
#   | pins           | versions.env split per stage                            |
#   | sessiond       | static sessiond binary (Go, CGO off)                    |
#   | emacs-build    | Emacs 31.1 in /usr/local with native code for every     |
#   |                | Lisp file (native-gaps.el), tree-sitter grammars, the   |
#   |                | Debian packages Emacs needs at run time                 |
#   | natives        | vterm-module.so, jinx-mod.so and pdf-tools' epdfinfo,   |
#   |                | compiled from the vendored package tarballs             |
#   | emacs-runtime  | Debian slim with Emacs, its libraries, libgccjit and    |
#   |                | the libraries of the native parts (also the remote box) |
#   | packages       | packages.lock, sha256-checked, byte- and native-        |
#   |                | compiled into /opt/playground/{elpa,eln-cache}, with    |
#   |                | the native parts copied into their package directories  |
#   | rust           | rustup, the pinned toolchain with rust-analyzer, clippy,|
#   |                | rustfmt, rust-src in /opt/rust                          |
#   | python         | uv, ruff in /opt/uv; debugpy in /opt/debugpy            |
#   | ocaml          | opam and one switch (/opt/opam/default): the compiler,  |
#   |                | dune, ocaml-lsp-server, ocamlformat, merlin, utop,      |
#   |                | odoc, earlybird and prefetch/opam.txt's libraries       |
#   | typst          | typst, tinymist, prefetch/typst.txt's packages          |
#   | texlive        | TeX Live scheme-basic plus prefetch/texlive.txt in      |
#   |                | /opt/texlive; texlab                                    |
#   | node           | Node (one binary), bash-language-server and basedpyright|
#   |                | in /opt/node                                            |
#   | tools          | marksman, pandoc, delta, shellcheck, cargo-nextest      |
#   | debug          | codelldb in /opt/codelldb                               |
#   | prefetch-cargo | prefetch/cargo's crates as a cargo registry cache       |
#   | prefetch-python| prefetch/python.txt's wheels                            |
#   | fonts          | Iosevka Nerd Font (4 styles)                            |
#   | tmux           | tmux from the release tarball (trixie has 3.5a)         |
#   | toolchains     | emacs-runtime plus the runtime Debian packages (Xvnc,   |
#   |                | xdotool, ripgrep, fd, git, fonts, ImageMagick, poppler, |
#   |                | hunspell) and everything above                          |
#   | config         | /opt/playground/emacs.d (pristine config, agent         |
#   |                | compiled), /etc/tmux.conf, helper scripts, prefetched   |
#   |                | dependencies, versions.env; prebuild.el loads every     |
#   |                | module and ships the native trampolines                 |
#   | final          | sessiond, user learner (uid 1000), environment          |
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
# Offline by default (emacs.md §9): sessions have no egress, so cargo and
# uv run offline (CARGO_NET_OFFLINE, UV_OFFLINE) against what the image
# prefetched; set both to false/0 for a session that has network.
#
# Size and time on a 96-core builder: see emacs.md §1.

ARG GO_VERSION=1.27.3
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

# ---- ocaml ------------------------------------------------------------------
# opam's release binary and one switch (/opt/opam/default) at the pinned
# compiler with dune, utop and prefetch/opam.txt's libraries, resolved
# against opam-repository at a pinned commit (a local copy of its
# archive, so the build never sees a newer repository). The editor tools
# (ocaml-lsp-server, ocamlformat, merlin, odoc, earlybird) are built in a
# second switch and only their executables are kept: their libraries
# would triple the size. No package may be a prerelease (a ~ in its
# version). Sources, build directories, logs, the .cmt files of
# libraries a learner never opens, bytecode copies of native tools and
# helper executables nothing calls (utop-full, ppx-base) are removed;
# native executables are stripped (bytecode ones, which end in a
# Caml1999X trailer, cannot be).
FROM debian:${DEBIAN} AS ocaml
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      build-essential ca-certificates curl git unzip bzip2 m4 patch rsync pkg-config
COPY --from=pins /pins/ocaml.env /pins/ocaml.env
ENV OPAMROOT=/opt/opam OPAMYES=1 OPAMCONFIRMLEVEL=unsafe-yes OPAMCOLOR=never \
    OPAMNODEPEXTS=1 OPAMDOWNLOADJOBS=16 OPAMREPOSITORYTARRING=1
RUN . /pins/ocaml.env \
 && curl -fsSL -o /usr/local/bin/opam \
      "https://github.com/ocaml/opam/releases/download/${OPAM_VERSION}/opam-${OPAM_VERSION}-x86_64-linux" \
 && echo "${OPAM_SHA256}  /usr/local/bin/opam" | sha256sum -c - \
 && chmod 0755 /usr/local/bin/opam \
 && curl -fsSL -o /tmp/opam-repository.tar.gz \
      "https://github.com/ocaml/opam-repository/archive/${OPAM_REPOSITORY_COMMIT}.tar.gz" \
 && echo "${OPAM_REPOSITORY_SHA256}  /tmp/opam-repository.tar.gz" | sha256sum -c - \
 && mkdir -p /src/opam-repository \
 && tar -xzf /tmp/opam-repository.tar.gz -C /src/opam-repository --strip-components=1 \
 && rm /tmp/opam-repository.tar.gz \
 && opam init -q --bare --disable-sandboxing --no-setup default /src/opam-repository \
 && OPAMJOBS="$(nproc)" opam switch create -q default "ocaml-base-compiler.${OCAML_VERSION}" \
 && OPAMJOBS="$(nproc)" opam switch create -q tools "ocaml-base-compiler.${OCAML_VERSION}" \
 && OPAMJOBS="$(nproc)" opam install -q --switch tools \
      ocaml-lsp-server ocamlformat merlin odoc earlybird
COPY images/session/prefetch/opam.txt /src/opam.txt
RUN . /pins/ocaml.env \
 && OPAMJOBS="$(nproc)" opam install -q --switch default dune utop $(grep -Ev '^\s*(#|$)' /src/opam.txt) \
 && for t in default tools; do opam list -s --switch $t --columns=name,version --installed; done > /tmp/all.txt \
 && if grep '~' /tmp/all.txt; then echo "prerelease packages above"; exit 1; fi \
 && tools=/opt/opam/tools/bin default=/opt/opam/default/bin \
 && for b in ocamllsp ocamlformat ocamlformat-rpc ocamlmerlin ocamlmerlin-server \
             dot-merlin-reader ocaml-index odoc ocamlearlybird; do \
      cp "$tools/$b" "$default/$b"; \
    done \
 && opam list -s --switch tools --installed --columns=name,version \
      ocaml-lsp-server ocamlformat merlin odoc earlybird > /opt/opam/editor-tools.txt \
 && opam switch remove -q tools \
 && opam list -s --switch default --installed --columns=name,version > /opt/opam/installed.txt \
 && opam clean -q --all-switches --download-cache --switch-cleanup --logs \
 && rm -rf /opt/opam/default/.opam-switch/sources /opt/opam/default/.opam-switch/build \
      /opt/opam/download-cache /opt/opam/log /opt/opam/default/doc /src/opam-repository \
 && find /opt/opam/default/lib -name '*.cmt' \
      -not -path '*/lib/ocaml/*' -not -path '*/lib/base/*' -not -path '*/lib/stdio/*' -delete \
 && for f in /opt/opam/default/bin/*; do \
      if [ -f "$f" ] && [ ! -L "$f" ] && head -c 4 "$f" | grep -q ELF \
         && ! tail -c 12 "$f" | grep -q Caml1999X; then strip "$f"; fi; \
    done \
 && cd /opt/opam/default/bin \
 && rm -f utop-full ppx-base lambda-term-actions \
 && for f in *.byte; do [ -e "${f%.byte}.opt" ] && rm -f "$f"; done \
 && find /opt/opam/default/lib/ocaml/compiler-libs -name '*.cmt' -delete \
 && cd / \
 && eval "$(opam env --switch default)" \
 && ocaml -version | grep -q "version ${OCAML_VERSION}" \
 && dune --version && ocamllsp --version && ocamlformat --version && odoc --version \
 && ocamlmerlin -version && ocamlearlybird --help >/dev/null && utop -version \
 && cat /opt/opam/editor-tools.txt && du -sh /opt/opam

# ---- typst ------------------------------------------------------------------
# typst and tinymist (release binaries) and the Typst packages of
# prefetch/typst.txt, unpacked where TYPST_PACKAGE_PATH points. A document
# importing every listed package must compile without downloading.
FROM debian:${DEBIAN}-slim AS typst
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl xz-utils
COPY --from=pins /pins/typst.env /pins/typst.env
COPY images/session/prefetch/typst.txt /src/typst.txt
RUN . /pins/typst.env && mkdir -p /out/bin /out/packages /tmp/t && cd /tmp/t \
 && curl -fsSL -o typst.tar.xz \
      "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" \
 && echo "${TYPST_SHA256}  typst.tar.xz" | sha256sum -c - \
 && tar -xJf typst.tar.xz --strip-components=1 && install -m 0755 typst /out/bin/typst \
 && curl -fsSL -o tinymist \
      "https://github.com/Myriad-Dreamin/tinymist/releases/download/v${TINYMIST_VERSION}/tinymist-linux-x64" \
 && echo "${TINYMIST_SHA256}  tinymist" | sha256sum -c - \
 && install -m 0755 tinymist /out/bin/tinymist \
 && grep -Ev '^\s*(#|$)' /src/typst.txt | while read -r spec version sha; do \
      ns="${spec%%/*}"; name="${spec#*/}"; \
      curl -fsSL -o pkg.tar.gz "https://packages.typst.org/${ns}/${name}-${version}.tar.gz"; \
      echo "${sha}  pkg.tar.gz" | sha256sum -c - >/dev/null || exit 1; \
      mkdir -p "/out/packages/${ns}/${name}/${version}"; \
      tar -xzf pkg.tar.gz -C "/out/packages/${ns}/${name}/${version}"; \
      echo "#import \"@${ns}/${name}:${version}\"" >> check.typ; \
    done \
 && echo 'Prefetched packages compile offline.' >> check.typ \
 && TYPST_PACKAGE_PATH=/out/packages TYPST_PACKAGE_CACHE_PATH=/tmp/t/cache /out/bin/typst compile check.typ \
 && if [ -e /tmp/t/cache ]; then echo "typst downloaded packages not in typst.txt:"; find /tmp/t/cache -maxdepth 3 -mindepth 3; exit 1; fi \
 && /out/bin/typst --version | grep -q "${TYPST_VERSION}" && /out/bin/tinymist --version \
 && cd / && rm -rf /tmp/t

# ---- texlive ----------------------------------------------------------------
# TeX Live scheme-basic plus prefetch/texlive.txt from the frozen
# repository of TEXLIVE_YEAR, without documentation or sources. Per-user
# files (generated fonts, formats) go under ~/.cache, the only writable
# part of a learner's home on a server. Checked by building
# tex-check.tex (biblatex and biber, cleveref, siunitx, TikZ) and
# beamer-check.tex with latexmk. texlab is the LaTeX language server.
FROM debian:${DEBIAN}-slim AS texlive
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl xz-utils perl gnupg
COPY --from=pins /pins/texlive.env /pins/texlive.env
COPY images/session/prefetch/texlive.txt images/session/prefetch/tex-check.tex \
     images/session/prefetch/tex-check.bib images/session/prefetch/beamer-check.tex /src/tex/
RUN . /pins/texlive.env && mkdir -p /tmp/tl && cd /tmp/tl \
 && curl -fsSL -o install-tl-unx.tar.gz "${TEXLIVE_REPOSITORY}/install-tl-unx.tar.gz" \
 && echo "${TEXLIVE_INSTALLER_SHA256}  install-tl-unx.tar.gz" | sha256sum -c - \
 && curl -fsSL -o texlive.tlpdb "${TEXLIVE_REPOSITORY}/tlpkg/texlive.tlpdb" \
 && echo "${TEXLIVE_TLPDB_SHA512}  texlive.tlpdb" | sha512sum -c - \
 && tar -xzf install-tl-unx.tar.gz --strip-components=1 \
 && printf '%s\n' \
      'selected_scheme scheme-basic' \
      'TEXDIR /opt/texlive' \
      'TEXMFLOCAL /opt/texlive/texmf-local' \
      'TEXMFSYSVAR /opt/texlive/texmf-var' \
      'TEXMFSYSCONFIG /opt/texlive/texmf-config' \
      'TEXMFHOME ~/texmf' \
      'TEXMFVAR ~/.cache/texlive/texmf-var' \
      'TEXMFCONFIG ~/.cache/texlive/texmf-config' \
      'binary_x86_64-linux 1' \
      'instopt_adjustpath 0' \
      'instopt_adjustrepo 0' \
      'instopt_letter 0' \
      'instopt_write18_restricted 1' \
      'tlpdbopt_autobackup 0' \
      'tlpdbopt_install_docfiles 0' \
      'tlpdbopt_install_srcfiles 0' > texlive.profile \
 && ./install-tl -profile texlive.profile -repository "${TEXLIVE_REPOSITORY}" > install.log 2>&1 \
      || { tail -40 install.log; exit 1; } \
 && export PATH=/opt/texlive/bin/x86_64-linux:$PATH \
 && tlmgr install $(grep -Ev '^\s*(#|$)' /src/tex/texlive.txt) > tlmgr.log 2>&1 \
      || { tail -40 tlmgr.log; exit 1; } \
 && rm -rf /opt/texlive/tlpkg/backups /opt/texlive/install-tl.log /opt/texlive/texmf-var/web2c/*.log \
 && mkdir -p /tmp/check && cp /src/tex/*.tex /src/tex/*.bib /tmp/check && cd /tmp/check \
 && HOME=/tmp/check latexmk -pdf -interaction=nonstopmode -halt-on-error tex-check.tex > tex.log 2>&1 \
      || { tail -40 tex.log; exit 1; } \
 && HOME=/tmp/check latexmk -pdf -interaction=nonstopmode -halt-on-error beamer-check.tex > beamer.log 2>&1 \
      || { tail -40 beamer.log; exit 1; } \
 && test -s tex-check.pdf && test -s beamer-check.pdf \
 && curl -fsSL -o /tmp/texlab.tar.gz \
      "https://github.com/latex-lsp/texlab/releases/download/v${TEXLAB_VERSION}/texlab-x86_64-linux.tar.gz" \
 && echo "${TEXLAB_SHA256}  /tmp/texlab.tar.gz" | sha256sum -c - \
 && mkdir -p /out/bin && tar -xzf /tmp/texlab.tar.gz -C /out/bin texlab \
 && /out/bin/texlab --version \
 && cd / && rm -rf /tmp/tl /tmp/check /tmp/texlab.tar.gz \
 && du -sh /opt/texlive

# ---- node -------------------------------------------------------------------
# One Node binary (no npm at run time) and the Node language servers of
# images/session/node/package-lock.json, installed with `npm ci`:
# bash-language-server and basedpyright (from npm, not PyPI, whose wheel
# bundles a second Node).
FROM debian:${DEBIAN}-slim AS node
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl xz-utils
COPY --from=pins /pins/node.env /pins/node.env
COPY images/session/node/package.json images/session/node/package-lock.json /opt/node/lsp/
RUN . /pins/node.env \
 && curl -fsSL -o /tmp/node.tar.xz \
      "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
 && echo "${NODE_SHA256}  /tmp/node.tar.xz" | sha256sum -c - \
 && mkdir -p /tmp/node && tar -xJf /tmp/node.tar.xz -C /tmp/node --strip-components=1 \
 && mkdir -p /opt/node/bin && install -m 0755 /tmp/node/bin/node /opt/node/bin/node \
 && cd /opt/node/lsp \
 && PATH=/tmp/node/bin:$PATH npm ci --ignore-scripts --no-audit --no-fund --omit=dev --loglevel=error \
 && for b in bash-language-server basedpyright basedpyright-langserver; do \
      ln -s "../lsp/node_modules/.bin/$b" "/opt/node/bin/$b"; \
    done \
 && rm -rf /tmp/node /tmp/node.tar.xz /root/.npm \
 && PATH=/opt/node/bin:$PATH bash-language-server --version \
 && PATH=/opt/node/bin:$PATH basedpyright --version

# ---- tools ------------------------------------------------------------------
# Single-binary tools from their releases: marksman (Markdown language
# server), pandoc, delta, shellcheck, cargo-nextest.
FROM debian:${DEBIAN}-slim AS tools
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl xz-utils
COPY --from=pins /pins/tools.env /pins/tools.env
RUN . /pins/tools.env && mkdir -p /out/bin /tmp/t && cd /tmp/t \
 && curl -fsSL -o marksman "https://github.com/artempyanykh/marksman/releases/download/${MARKSMAN_VERSION}/marksman-linux-x64" \
 && echo "${MARKSMAN_SHA256}  marksman" | sha256sum -c - && install -m 0755 marksman /out/bin/ \
 && curl -fsSL -o pandoc.tar.gz "https://github.com/jgm/pandoc/releases/download/${PANDOC_VERSION}/pandoc-${PANDOC_VERSION}-linux-amd64.tar.gz" \
 && echo "${PANDOC_SHA256}  pandoc.tar.gz" | sha256sum -c - \
 && tar -xzf pandoc.tar.gz --strip-components=2 "pandoc-${PANDOC_VERSION}/bin/pandoc" && install -m 0755 pandoc /out/bin/ \
 && curl -fsSL -o delta.tar.gz "https://github.com/dandavison/delta/releases/download/${DELTA_VERSION}/delta-${DELTA_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
 && echo "${DELTA_SHA256}  delta.tar.gz" | sha256sum -c - \
 && tar -xzf delta.tar.gz --strip-components=1 && install -m 0755 delta /out/bin/ \
 && curl -fsSL -o shellcheck.tar.xz "https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK_VERSION}/shellcheck-v${SHELLCHECK_VERSION}.linux.x86_64.tar.xz" \
 && echo "${SHELLCHECK_SHA256}  shellcheck.tar.xz" | sha256sum -c - \
 && tar -xJf shellcheck.tar.xz --strip-components=1 && install -m 0755 shellcheck /out/bin/ \
 && curl -fsSL -o nextest.tar.gz "https://github.com/nextest-rs/nextest/releases/download/cargo-nextest-${NEXTEST_VERSION}/cargo-nextest-${NEXTEST_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
 && echo "${NEXTEST_SHA256}  nextest.tar.gz" | sha256sum -c - \
 && tar -xzf nextest.tar.gz && install -m 0755 cargo-nextest /out/bin/ \
 && cd / && rm -rf /tmp/t \
 && for t in marksman pandoc delta shellcheck cargo-nextest; do "/out/bin/$t" --version | head -1; done

# ---- debug ------------------------------------------------------------------
# codelldb, the debug adapter dape uses for Rust (lesson 7.7), from its
# VS Code extension: the adapter and its bundled LLDB.
FROM debian:${DEBIAN}-slim AS debug
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl unzip
COPY --from=pins /pins/debug.env /pins/debug.env
RUN . /pins/debug.env \
 && curl -fsSL -o /tmp/codelldb.vsix \
      "https://github.com/vadimcn/codelldb/releases/download/v${CODELLDB_VERSION}/codelldb-linux-x64.vsix" \
 && echo "${CODELLDB_SHA256}  /tmp/codelldb.vsix" | sha256sum -c - \
 && mkdir -p /tmp/vsix && unzip -q /tmp/codelldb.vsix 'extension/adapter/*' 'extension/lldb/*' 'extension/lang_support/*' 'extension/bin/*' 'extension/package.json' -d /tmp/vsix \
 && mv /tmp/vsix/extension /opt/codelldb && chmod 0755 /opt/codelldb/adapter/codelldb \
 && rm -rf /tmp/vsix /tmp/codelldb.vsix \
 && du -sh /opt/codelldb

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

# ---- tmux -------------------------------------------------------------------
# The terminal frame's tmux (tmux.conf), built because trixie ships 3.5a.
# runtime-packages.txt names the Debian packages of its shared libraries.
FROM debian:${DEBIAN}-slim AS tmux
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    rm -f /etc/apt/apt.conf.d/docker-clean \
 && apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl build-essential pkg-config bison libevent-dev libncurses-dev
COPY --from=pins /pins/tmux.env /pins/tmux.env
RUN . /pins/tmux.env \
 && curl -fsSL -o /tmp/tmux.tar.gz \
      "https://github.com/tmux/tmux/releases/download/${TMUX_VERSION}/tmux-${TMUX_VERSION}.tar.gz" \
 && echo "${TMUX_SHA256}  /tmp/tmux.tar.gz" | sha256sum -c - \
 && tar -xzf /tmp/tmux.tar.gz -C /tmp && cd "/tmp/tmux-${TMUX_VERSION}" \
 && { ./configure --prefix=/usr/local > configure.log 2>&1 || { tail -30 configure.log; exit 1; }; } \
 && make -s -j"$(nproc)" && make -s install-strip DESTDIR=/out \
 && /out/usr/local/bin/tmux -V | grep -qx "tmux ${TMUX_VERSION}" \
 && for lib in $(ldd /out/usr/local/bin/tmux | awk '/=> \//{print $3}'); do \
      dpkg -S "$(readlink -f "$lib")" 2>/dev/null || dpkg -S "$lib"; \
    done | cut -d: -f1 | sort -u > /out/runtime-packages.txt \
 && cat /out/runtime-packages.txt

# ---- toolchains -------------------------------------------------------------
FROM emacs-runtime AS toolchains
COPY --from=tmux /out/runtime-packages.txt /tmp/tmux-runtime-packages.txt
# imagemagick: lesson 6.4 converts images from Dired, image-dired makes
# thumbnails; poppler-utils: doc-view shows a PDF as text in the
# terminal frame; perl: latexmk and tlmgr; zip and unzip: archive-mode.
# marksman is a .NET program: DOTNET_SYSTEM_GLOBALIZATION_INVARIANT below
# spares it libicu.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      xz-utils zstd openssh-client rsync psmisc jq zip unzip \
      tini fontconfig fonts-noto-cjk fonts-noto-color-emoji \
      tigervnc-standalone-server xauth xdotool \
      ripgrep fd-find \
      python3 python3-venv \
      imagemagick poppler-utils perl \
      $(cat /tmp/tmux-runtime-packages.txt) \
 && rm /tmp/tmux-runtime-packages.txt
COPY --from=tmux /out/usr/local/ /usr/local/
COPY --from=fonts /out/iosevka-nerd /usr/local/share/fonts/iosevka-nerd
COPY --from=rust /opt/rust /opt/rust
COPY --from=python /opt/uv /opt/uv
COPY --from=python /opt/debugpy /opt/debugpy
COPY --from=ocaml /opt/opam /opt/opam
COPY --from=ocaml /usr/local/bin/opam /usr/local/bin/opam
COPY --from=texlive /opt/texlive /opt/texlive
COPY --from=node /opt/node /opt/node
COPY --from=debug /opt/codelldb /opt/codelldb
COPY --from=typst /out/bin/ /usr/local/bin/
COPY --from=texlive /out/bin/ /usr/local/bin/
COPY --from=tools /out/bin/ /usr/local/bin/
COPY --from=typst /out/packages /opt/playground/typst/packages
COPY --from=prefetch-cargo /out/cargo /opt/playground/prefetch/cargo
COPY --from=prefetch-python /out/wheels /opt/playground/prefetch/wheels
COPY --from=pins /pins/tmux.env /pins/tmux.env
ENV COLORTERM=truecolor \
    RUSTUP_HOME=/opt/rust/rustup \
    OPAMROOT=/opt/opam \
    OPAM_SWITCH_PREFIX=/opt/opam/default \
    CAML_LD_LIBRARY_PATH=/opt/opam/default/lib/stublibs:/opt/opam/default/lib/ocaml/stublibs:/opt/opam/default/lib/ocaml \
    OCAML_TOPLEVEL_PATH=/opt/opam/default/lib/toplevel \
    TYPST_PACKAGE_PATH=/opt/playground/typst/packages \
    UV_FIND_LINKS=/opt/playground/prefetch/wheels \
    UV_PYTHON_DOWNLOADS=never \
    UV_OFFLINE=1 \
    CARGO_NET_OFFLINE=true \
    DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 \
    PATH=/opt/playground/bin:/opt/rust/cargo/bin:/opt/uv/bin:/opt/opam/default/bin:/opt/texlive/bin/x86_64-linux:/opt/node/bin:/opt/codelldb/adapter:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
RUN ln -sf /usr/bin/fdfind /usr/local/bin/fd \
 && fc-cache -f >/dev/null \
 && fc-list | grep -q "Iosevka Nerd Font" \
 && fc-list | grep -q "Noto Sans CJK" \
 && fc-list | grep -q "Noto Color Emoji" \
 && . /pins/tmux.env && tmux -V | grep -qx "tmux ${TMUX_VERSION}" \
 && Xvnc -version 2>&1 | head -2 \
 && rg --version | head -1 && fd --version && git --version \
 && rustc --version && basedpyright --version && ruff --version \
 && ocaml -version && dune --version && ocamllsp --version \
 && typst --version && tinymist --version && texlab --version \
 && pdflatex --version | head -1 && latexmk --version | sed -n 2p && biber --version \
 && node --version && bash-language-server --version \
 && marksman --version && pandoc --version | head -1 && delta --version \
 && shellcheck --version | sed -n 2p && cargo nextest --version \
 && codelldb --help >/dev/null && /opt/debugpy/bin/python -m debugpy --version \
 && magick -version | head -1 && pdftotext -v 2>&1 | head -1 \
 && rm -rf /tmp/* /root/.cache /root/.cargo

# ---- config -----------------------------------------------------------------
FROM toolchains AS config
COPY --from=packages /opt/playground/elpa /opt/playground/elpa
COPY --from=packages /opt/playground/eln-cache /opt/playground/eln-cache
COPY images/session/versions.env /opt/playground/versions.env
COPY images/session/tmux.conf /etc/tmux.conf
COPY images/session/tmux.conf /opt/playground/tmux.conf
COPY --chmod=0755 images/session/bin/ /opt/playground/bin/
COPY --exclude=vendor emacs.d/ /opt/playground/emacs.d/
COPY images/session/prebuild.el /src/prebuild.el
# The pristine config does not need the vendored tarballs (not copied;
# the rm covers a builder that ignores --exclude). The agent is
# compiled in place (byte and native code), since agent.el loads this
# copy. prebuild.el loads every module (failing the build on any error)
# and ships the native trampolines they need.
RUN rm -rf /opt/playground/emacs.d/vendor \
 && cd /opt/playground/emacs.d/lisp \
 && emacs --batch \
      --eval '(setq native-comp-eln-load-path (cons "/opt/playground/eln-cache/" (last native-comp-eln-load-path)))' \
      --eval '(progn (byte-compile-file "playground-agent.el") (native-compile "playground-agent.el"))' \
 && { PG_MODULES="base agent editing files windows completion git terminals lsp lang-rust lang-ocaml lang-python writing input org power remote ui" \
      HOME=/tmp/pg-check PG_WORKSPACE=/tmp/pg-check/work emacs --batch -l /src/prebuild.el > /src/prebuild.log 2>&1 \
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
 && ln -s .cache/ssh /home/learner/.ssh \
 && chown learner:learner /work /run/playground /home/learner/.config /home/learner/.cache \
 && chmod 0700 /run/playground
COPY --from=sessiond /out/sessiond /usr/local/bin/sessiond
# ~/.ssh (sessiond writes the box key and alias there) and CARGO_HOME are
# under ~/.cache, which stays writable on a read-only root; pg-entrypoint
# makes the first and seeds the second with the prefetched registry.
ENV HOME=/home/learner \
    USER=learner \
    XDG_RUNTIME_DIR=/run/playground \
    CARGO_HOME=/home/learner/.cache/cargo \
    EDITOR="emacsclient -s playground" \
    VISUAL="emacsclient -s playground"
USER learner
WORKDIR /work
ENTRYPOINT ["/usr/bin/tini", "-s", "--", "/opt/playground/bin/pg-entrypoint"]
CMD ["sessiond"]
