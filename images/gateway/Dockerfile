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
# Requires `playctl curriculum build` (backend.md §4).
RUN /out/playctl curriculum build curriculum -o /out/curriculum

# ---- fonts: web fonts served at /fonts/ (frontend.md §5, §11) ------------
# TODO(gateway, frontend.md §5): fetch Iosevka Nerd Font and Source Serif 4
# woff2 files (OFL-1.1) with pinned versions and checksums into /out/fonts.
FROM debian:trixie-slim AS fonts
RUN mkdir -p /out/fonts

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
