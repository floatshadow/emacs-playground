# Third-party software and APIs

Roles, licenses and caveats of external dependencies. Versions live in `go.mod`, `web/package.json`, `images/session/versions.env`, the Dockerfile `ARG`s and `emacs.d/packages.lock`; one appears here only where it is a decision.

## 1. Platform and infrastructure

| Software | Role | License | Caveats |
|---|---|---|---|
| Docker Engine 27+, Compose v2 | containers | Apache-2.0 | server mode reaches the Engine only through the socket proxy |
| Debian 13; distroless `static-debian13:nonroot` | session and remote-box base; gateway base | various; Apache-2.0 | no shell in distroless: health check `/gateway healthcheck` |
| Caddy 2 | TLS, reverse proxy (server) | Apache-2.0 | ports 80 and 443 |
| tecnativa/docker-socket-proxy | narrows the Engine API | Apache-2.0 | containers, images, volumes, networks, exec, info, POST |
| tini | PID 1 in session and remote box | MIT | |
| gVisor (optional) | stronger isolation | Apache-2.0 | slower file IO |

## 2. Emacs and the terminal

| Software | Role | License | Caveats |
|---|---|---|---|
| GNU Emacs 31.1 | from source: native compilation, tree-sitter, Lucid with Cairo | GPL-3.0+ | grammars at Emacs's pinned revisions |
| tmux | terminal frame persistence; from source (Debian 13 ships 3.5a) | ISC | settings in emacs.md §5; the build checks the version |
| TigerVNC Xvnc, xdotool | GUI frame display; demo typing in it | GPL-2.0, BSD-3 | localhost, no auth; only sessiond reaches it |
| kkp | decodes CSI u keys in Emacs | GPL-3.0 | until Emacs does |
| Iosevka Nerd Font, Noto CJK, Noto Color Emoji, Source Serif 4 | terminal and GUI font; CJK and emoji fallback; chrome typeface | OFL-1.1 | web fonts self-hosted as WOFF2 under `/fonts/` |

## 3. Emacs packages

GPL-3.0+ unless noted; `emacs.d/packages.el` maps modules to packages, pinned and vendored as emacs.md §8 describes. Packages whose last stable tag predates Emacs 30 use MELPA snapshots (`:prefer-snapshot`).

Native parts are built from the vendored tarballs: vterm's module against libvterm, jinx's against enchant, pdf-tools' `epdfinfo` against poppler. Chinese input is pyim with pyim-basedict, not rime. Org's Rust blocks are the org module's own `org-babel-execute:rust` (rustc); ob-rust needs rust-script. dape 0.28 lacks a codelldb configuration; the lsp module adds it. Typst's grammar is uben0/tree-sitter-typst at a pinned commit (no recent tag).

## 4. Language servers and tools

Rust through rustup (rustc, cargo, rust-analyzer, clippy, rustfmt, rust-src), cargo-nextest, codelldb; Python through uv with ruff and debugpy; OCaml through opam (dune, utop, ocaml-lsp-server, merlin, ocamlformat, odoc, earlybird); typst, tinymist; TeX Live 2025 (frozen tlnet-final, the newest year that no longer changes) with latexmk, biber and texlab; Node for bash-language-server and basedpyright (`images/session/node/package-lock.json`); marksman, pandoc, shellcheck, delta; ripgrep, fd, git, jq, gcc, make, ImageMagick, poppler-utils, enchant with hunspell en_US and en_GB. OCaml is 5.4: on 5.5, ocamlfind (needed by utop) is only a preview.

## 5. Go modules

`internal/deps/deps.go` imports every direct module so `go mod tidy` keeps them.

| Module | Role | Caveats |
|---|---|---|
| coder/websocket | pty, events, vnc | no compression for binary streams |
| creack/pty | tmux attach | v1: `creack/pty/v2` is a stray tag older than v1.1.24 |
| moby/moby/client, moby/moby/api | Engine API | not `docker/docker/client`; `containerd/errdefs` classifies errors |
| fsnotify | workspace watching | not recursive: watch new directories |
| modernc.org/sqlite | server store without cgo | single writer |
| coreos/go-oidc/v3, x/oauth2 | OIDC (*planned*: 501) | |
| invopop/jsonschema | `deploy/config.schema.json` | TypeScript types: api.md §8 |
| goccy/go-yaml; klauspost/compress | curriculum YAML; zstd fixtures | |
| prometheus/client_golang; testcontainers-go | metrics; sessiond integration test | |

The standard library covers routing, the reverse proxy, `os.Root`, logging and `embed`.

## 6. Frontend packages

Node 24 or newer (`engines`; @types/node follows that floor), pnpm through corepack (`packageManager`); the gateway image builds with Node 26 plus corepack, which Node 25+ no longer bundles. Where `$XDG_RUNTIME_DIR` is read-only the Makefile points pnpm's store locks at `artifacts/xdg-run`. MIT unless noted: noVNC MPL-2.0; pdfjs-dist, playwright-core and TypeScript Apache-2.0.

| Package | Role | Caveats |
|---|---|---|
| react 19, react-router 8, zustand, @tanstack/react-query; vite | UI, routing, live and server state; build | |
| typescript 6.0 | types | not 7: typescript-eslint 8.72 supports TypeScript below 6.1 |
| @xterm/xterm 6.1 beta, addons fit, webgl, unicode11, clipboard, web-links | terminal | betas from one upstream commit: stable 6.0 lacks `vtExtensions.kittyKeyboard`; re-pin all six together |
| @novnc/novnc | GUI frame | `core/rfb.js`, lazy loaded |
| pdfjs-dist | PDF preview | worker is a separate asset; legacy build |
| react-markdown, remark-gfm, remark-directive; react-i18next | lesson rendering; UI strings | |
| vitest, jsdom, Testing Library, msw, vitest-websocket-mock | unit tests | |
| playwright-core | `web/e2e/` over CDP | external `chromedp/headless-shell` pinned to Chrome's stable major (its `stable` tag runs ahead) |
| eslint 9, typescript-eslint, jsx-a11y, prettier | lint, format | not 10: jsx-a11y 6.10 does not declare ESLint 10 |

## 7. External services

None required. Optional: an OIDC provider, a registry (GHCR), Let's Encrypt via Caddy. Nothing leaves a deployment unless `features.telemetryUpload` is on.
