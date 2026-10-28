# Third-party software and APIs

Roles, licenses and caveats of external dependencies. Versions live in `go.mod`, `web/package.json` (with `web/pnpm-lock.yaml`), `images/session/versions.env`, the Dockerfile `ARG`s and `emacs.d/packages.lock`; a version appears here only where it is a decision. *Planned* items are not in the images yet.

## 1. Platform and infrastructure

| Software | Role | License | Caveats |
|---|---|---|---|
| Docker Engine 27+, Compose v2 | containers | Apache-2.0 | server mode reaches the Engine API only through the socket proxy |
| Debian 13; distroless `static-debian13:nonroot` | session and remote-box base; gateway base | various; Apache-2.0 | distroless has no shell: health check `/gateway healthcheck` |
| Caddy 2 | TLS, reverse proxy (server) | Apache-2.0 | needs ports 80 and 443 |
| tecnativa/docker-socket-proxy | narrows the Engine API | Apache-2.0 | containers, images, volumes, networks, exec, info, POST |
| tini | PID 1 in session and remote box | MIT | |
| gVisor (optional) | stronger isolation | Apache-2.0 | slower file IO; check lesson timings |

## 2. Emacs and the terminal

| Software | Role | License | Caveats |
|---|---|---|---|
| GNU Emacs 31.1 | from source: native compilation, tree-sitter, Lucid with Cairo | GPL-3.0+ | grammars at Emacs's pinned revisions |
| tmux 3.5a | terminal frame persistence | ISC | `extended-keys always`, `csi-u`; the build checks the version |
| TigerVNC Xvnc, xdotool | GUI frame display; demo typing in it | GPL-2.0, BSD-3 | localhost, no auth; only sessiond reaches it |
| kkp | decodes CSI u keys in Emacs | GPL-3.0 | until Emacs decodes CSI u itself |
| Iosevka Nerd Font, Noto CJK, Source Serif 4 | terminal and GUI font; CJK fallback; chrome typeface | OFL-1.1 | self-hosted WOFF2 under `/fonts/` |

## 3. Emacs packages

GPL-3.0+ unless noted, from GNU ELPA, NonGNU ELPA, MELPA and MELPA Stable. `emacs.d/packages.el` maps modules to packages (modules: emacs.md); `packages.lock` pins version, archive and SHA-256, every locked tarball is vendored in `emacs.d/vendor/`, and the image installs exactly the lock. Packages whose last stable tag predates Emacs 30 come from MELPA snapshots (`:prefer-snapshot`).

*Planned:* forge, vterm (needs cmake, libvterm), wgrep, rime (librime); writing: typst-ts-mode, auctex, cdlatex, pdf-tools (builds `epdfinfo` against poppler and cairo), citar, jinx (compiles against enchant); OCaml: tuareg, dune, utop, merlin-eldoc; org-roam, org-present, ob-rust. dape needs debug adapters on PATH.

## 4. Language servers and tools

Rust through rustup (rustc, cargo, rust-analyzer, clippy, rustfmt, rust-src); Python through uv with basedpyright and ruff; ripgrep, fd, git, jq, gcc, make. *Planned:* ocaml-lsp-server, merlin, ocamlformat, utop, dune; tinymist, typst; texlab, latexmk, TeX Live; marksman, bash-language-server, shellcheck, debugpy, codelldb, ocamlearlybird, delta, pandoc.

## 5. Go modules

`internal/deps/deps.go` imports every direct module so `go mod tidy` keeps it.

| Module | Role | Caveats |
|---|---|---|
| coder/websocket | pty, events, vnc | compression off for binary streams |
| creack/pty | tmux attach | |
| moby/moby/client, moby/moby/api | Engine API | not `docker/docker/client`; `containerd/errdefs` classifies errors |
| fsnotify | workspace watching | not recursive: add directories as they appear |
| modernc.org/sqlite | server store without cgo | single writer |
| coreos/go-oidc/v3, x/oauth2 | OIDC (*planned*: 501) | |
| invopop/jsonschema | `deploy/config.schema.json` | TypeScript types come from `internal/protocol/tsgen` |
| goccy/go-yaml; klauspost/compress | curriculum YAML; zstd fixtures | |
| prometheus/client_golang; testcontainers-go | metrics; sessiond integration test | |

The standard library covers routing, the reverse proxy, `os.Root`, `log/slog` and `embed`.

## 6. Frontend packages

Node 24, pnpm through corepack (`packageManager`). MIT unless noted: noVNC MPL-2.0; pdfjs-dist, playwright-core and TypeScript Apache-2.0.

| Package | Role | Caveats |
|---|---|---|
| react 19, react-router 7, zustand, @tanstack/react-query | UI, routing, live and server state | |
| vite, @vitejs/plugin-react | build, dev server | |
| typescript 5.9 | types | typescript-eslint 8.72 supports TypeScript below 6.1 |
| @xterm/xterm 6.1 beta, addons fit, webgl, unicode11, clipboard, web-links | terminal | betas from one upstream commit (`vtExtensions.kittyKeyboard`); re-pin all six together |
| @novnc/novnc | GUI frame | `core/rfb.js`, lazy loaded |
| pdfjs-dist | PDF preview | worker is a separate asset |
| react-markdown, remark-gfm, remark-directive | lesson rendering | |
| react-i18next | UI strings | |
| vitest, jsdom, Testing Library, msw, vitest-websocket-mock | unit tests | |
| playwright-core | `web/e2e/` over CDP | drives an external Chrome |
| eslint 9, typescript-eslint, jsx-a11y, prettier | lint, format | jsx-a11y 6.10 does not declare ESLint 10 |
| @playwright/test, json-schema-to-typescript | unused | to be removed through integration |

## 7. External services

None required. Optional: an OIDC provider, a registry (GHCR), Let's Encrypt via Caddy. Nothing leaves a deployment unless the operator enables `features.telemetryUpload`.
