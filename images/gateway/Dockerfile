# syntax=docker/dockerfile:1.7
#
# Gateway image (deployment.md §1): a static Go binary with the embedded web
# bundle (frontend.md §15), the built curriculum bundle (backend.md §2.8) and
# the web fonts, on distroless. Build context: the repository root.
#
#   docker build -f images/gateway/Dockerfile -t ghcr.io/zsy/emacs-playground/gateway:dev .
#
# Stages: web (pnpm build) → go (gateway and playctl, -tags embedweb) →
# curriculum (playctl curriculum build) → final.

ARG GO_VERSION=1.26.10
ARG NODE_VERSION=24

# ---- web: build web/dist -------------------------------------------------
FROM node:${NODE_VERSION}-trixie-slim AS web
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0 CI=1
RUN corepack enable
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
# protocol.gen.ts is checked in; CI checks it against internal/protocol.
RUN pnpm build

# ---- go: gateway and playctl binaries ------------------------------------
FROM golang:${GO_VERSION}-trixie AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go web/embed_stub.go ./web/
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -tags embedweb \
      -ldflags "-s -w -X github.com/zsy/emacs-playground/internal/buildinfo.Version=${VERSION}" \
      -o /out/gateway ./cmd/gateway && \
    go build -trimpath \
      -ldflags "-s -w -X github.com/zsy/emacs-playground/internal/buildinfo.Version=${VERSION}" \
      -o /out/playctl ./cmd/playctl && \
    mkdir -p /out/data

# ---- curriculum: curriculum.json, lesson bundles, fixture tarballs --------
FROM go AS curriculum
COPY curriculum ./curriculum
# `playctl curriculum build` validates first and fails on errors.
# CURRICULUM_FLAGS=-lenient builds anyway, leaving broken lessons out
# (they show as planned); never use it for a release.
ARG CURRICULUM_FLAGS=
ARG VERSION=dev
RUN PG_CURRICULUM_VERSION="${VERSION}" /out/playctl curriculum build curriculum -o /out/curriculum ${CURRICULUM_FLAGS}

# ---- fonts: web fonts served at /fonts/ (frontend.md §5, §11) ------------
# Iosevka Nerd Font (OFL-1.1; Nerd Fonts release asset, TTF converted to
# WOFF2 with fontTools): IosevkaNerdFont-{Regular,Bold}.woff2 and the Mono
# variant (icons one cell wide) IosevkaNerdFontMono-{Regular,Bold}.woff2.
# Source Serif 4 variable WOFF2 (OFL-1.1, Adobe):
# SourceSerif4-Variable.woff2, SourceSerif4-Italic-Variable.woff2.
# The gateway serves /srv/fonts at /fonts/.
FROM debian:trixie-slim AS fonts
ARG NERD_FONTS_VERSION=v3.5.1
ARG NERD_FONTS_SHA256=3b94ea1dc3955756762f977b7677bca671947dd56bc755a6f8465a8e83b5f257
ARG SOURCE_SERIF_TAG=4.005R
ARG SOURCE_SERIF_ROMAN_SHA256=940a76eda1388de39d38c8e7a79bf6ea058a387faee0a9f33c8d25c6ba05e1be
ARG SOURCE_SERIF_ITALIC_SHA256=9d28b5749a1ad096a295cb607c521bd1af4cd9979b6f37332daf70143149fb44
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl xz-utils python3-fonttools python3-brotli \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /tmp/fonts
RUN curl -fsSLo Iosevka.tar.xz "https://github.com/ryanoasis/nerd-fonts/releases/download/${NERD_FONTS_VERSION}/Iosevka.tar.xz" \
 && echo "${NERD_FONTS_SHA256}  Iosevka.tar.xz" | sha256sum -c - \
 && mkdir -p /out/fonts \
 && for face in IosevkaNerdFont-Regular IosevkaNerdFont-Bold IosevkaNerdFontMono-Regular IosevkaNerdFontMono-Bold; do \
      tar -xJf Iosevka.tar.xz "$face.ttf" \
      && python3 -m fontTools.ttLib.woff2 compress -o "/out/fonts/$face.woff2" "$face.ttf" \
      && rm "$face.ttf" || exit 1; \
    done \
 && tar -xJf Iosevka.tar.xz LICENSE.md \
 && mv LICENSE.md /out/fonts/LICENSE-IosevkaNerdFont.md \
 && rm Iosevka.tar.xz
RUN base="https://raw.githubusercontent.com/adobe-fonts/source-serif/${SOURCE_SERIF_TAG}" \
 && curl -fsSLo /out/fonts/SourceSerif4-Variable.woff2 "$base/WOFF2/VAR/SourceSerif4Variable-Roman.ttf.woff2" \
 && curl -fsSLo /out/fonts/SourceSerif4-Italic-Variable.woff2 "$base/WOFF2/VAR/SourceSerif4Variable-Italic.ttf.woff2" \
 && echo "${SOURCE_SERIF_ROMAN_SHA256}  /out/fonts/SourceSerif4-Variable.woff2" | sha256sum -c - \
 && echo "${SOURCE_SERIF_ITALIC_SHA256}  /out/fonts/SourceSerif4-Italic-Variable.woff2" | sha256sum -c - \
 && curl -fsSLo /out/fonts/LICENSE-SourceSerif4.md "$base/LICENSE.md" \
 && chmod 644 /out/fonts/*

# ---- final ----------------------------------------------------------------
FROM gcr.io/distroless/static-debian13:nonroot AS final
ARG VERSION=dev
LABEL org.opencontainers.image.title="emacs-playground gateway" \
      org.opencontainers.image.source="https://github.com/zsy/emacs-playground" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="GPL-3.0-or-later"
COPY --from=go /out/gateway /gateway
COPY --from=go --chown=65532:65532 /out/data /data
COPY --from=curriculum /out/curriculum /srv/curriculum
COPY --from=fonts /out/fonts /srv/fonts
USER nonroot
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/gateway"]
