# Third-party software and APIs

Everything external the system depends on, with its role, pinned version, license and caveats. The sources of truth for versions are `go.mod`, `web/package.json` (with `web/pnpm-lock.yaml`), `images/session/versions.env`, the `ARG`s of `images/*/Dockerfile` and `emacs.d/packages.lock`; this document follows them. Rows marked *planned* are specified for later milestones and not in the images yet.

## 1. Platform and infrastructure

| Software | Role | Version | License | Caveats |
|---|---|---|---|---|
| [Docker Engine](https://docs.docker.com/engine/) and Compose v2 | containers, local and server runs | Engine 27+ | Apache-2.0 | server mode reaches the Engine API only through the socket proxy |
| [Docker Engine API](https://docs.docker.com/engine/api/) | containers, volumes, networks | negotiated by the client | | used only through `github.com/moby/moby/client` (§5) |
| [Debian](https://www.debian.org/) trixie | base of the session and remote-box images | 13 | various | |
| [distroless](https://github.com/GoogleContainerTools/distroless) `static-debian13:nonroot` | base of the gateway image | | Apache-2.0 | no shell: the health check is `/gateway healthcheck` |
| Go and Node images | build stages | `golang:1.26.10-trixie`, `node:24-trixie-slim` | BSD-3, MIT | |
| [Caddy](https://caddyserver.com/) | TLS termination and reverse proxy (server) | `caddy:2` | Apache-2.0 | automatic certificates need ports 80 and 443 reachable |
| [tecnativa/docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy) | limits the Engine API surface exposed to the gateway | 0.3 | Apache-2.0 | allows containers, images, volumes, networks, exec, info, POST |
| [tini](https://github.com/krallin/tini) | PID 1 in session and remote-box containers | Debian package | MIT | |
| [gVisor](https://gvisor.dev/) (optional) | stronger isolation for public servers | current | Apache-2.0 | slower file IO; verify builds still meet lesson timings |
| [SQLite](https://sqlite.org/) via modernc.org/sqlite | gateway store (server mode) | see §5 | Public domain / BSD-3 | single writer; fine at this scale |

## 2. Emacs and the terminal

| Software | Role | Version | License | Caveats |
|---|---|---|---|---|
| [GNU Emacs](https://www.gnu.org/software/emacs/) | the editor; built from source: native compilation (AOT), tree-sitter, modules, Lucid toolkit with Cairo | 31.1 (fallback 30.2, unused) | GPL-3.0-or-later | tree-sitter grammars for rust, python, toml, json, bash, yaml at the revisions Emacs pins |
| [tmux](https://github.com/tmux/tmux) | session persistence and attach for the terminal frame | 3.5a (Debian) | ISC | `extended-keys always`, `extended-keys-format csi-u`; the build checks the version |
| [TigerVNC Xvnc](https://tigervnc.org/) | X display for the GUI frame | Debian trixie package | GPL-2.0 | bound to localhost, `SecurityTypes None`; only sessiond reaches it |
| [xdotool](https://github.com/jordansissel/xdotool) | types demos into the GUI frame | Debian package | BSD-3 | |
| [kkp](https://github.com/benjaminor/kkp) | decodes CSI u (kitty keyboard) keys in Emacs | MELPA snapshot (packages.lock) | GPL-3.0 | needed as long as Emacs does not decode CSI u itself |
| [Iosevka Nerd Font](https://www.nerdfonts.com/) | terminal and GUI font with icon glyphs; web font for the terminal pane | 3.5.1 | OFL-1.1 | the gateway image serves WOFF2 conversions under `/fonts/` |
| [Noto CJK](https://fonts.google.com/noto) | CJK fallback | `fonts-noto-cjk` (Debian) | OFL-1.1 | |
| [Source Serif 4](https://github.com/adobe-fonts/source-serif) | chrome typeface | 4.005R | OFL-1.1 | self-hosted by the gateway, not Google Fonts |
| Noto Color Emoji, [Libertinus](https://github.com/alerque/libertinus) | emoji fallback; serif for Typst samples | | OFL-1.1 | *planned* |

## 3. Emacs packages (by module)

All GPL-3.0-or-later unless noted. Archives: GNU ELPA (G), NonGNU ELPA (N), MELPA (M), MELPA Stable (MS), built in (B). `emacs.d/packages.el` maps modules to packages; `emacs.d/packages.lock` pins each package's version, archive and SHA-256 (MELPA snapshot tarballs are vendored in `emacs.d/vendor/`), and the image build installs exactly the lock.

| Module | Packages in the image | *Planned* |
|---|---|---|
| base | kkp (M), clipetty (MS); which-key, repeat, savehist, recentf, saveplace, autorevert, uniquify (B) | |
| editing | avy, expreg, vundo (G); multiple-cursors, iedit, ws-butler (N); visual-replace (M) | |
| completion | vertico, orderless, marginalia, consult, embark, embark-consult, corfu, cape, tempel (G) | |
| files | dirvish (MS), consult-dir (M); project, bookmark, ibuffer, dired (B) | |
| windows | ace-window, popper (G); burly (M); olivetti (MS); windmove, winner, tab-bar, tab-line, follow (B) | |
| git | magit, magit-section, with-editor, cond-let, llama (N); diff-hl (G); git-timemachine (MS) | forge |
| terminals | eat (N); eshell, compile (B) | vterm (needs cmake and libvterm in the build stage) |
| lsp | dape (G); apheleia (MS, MIT), consult-eglot (MS); eldoc-box (M); eglot, flymake, xref, eldoc (B) | |
| lang-rust | rust-ts-mode (B), cargo through compile | |
| lang-python | pet (MS, with s, f from MS and dash from G); python-ts-mode (B) | |
| ui | nerd-icons, nerd-icons-dired, nerd-icons-completion, nerd-icons-corfu, nerd-icons-ibuffer, helpful, elisp-refs, ligature (M); breadcrumb, pulsar, ef-themes, modus-themes (G) | |
| writing | markdown-mode (N) | edit-indirect, typst-ts-mode, auctex, cdlatex, reftex (B), pdf-tools, citar, jinx |
| lang-ocaml | | tuareg (N, GPL-2+), dune, utop, merlin-eldoc |
| org | | org (B), org-roam, org-present, ob-rust |
| power | wdired, ediff, calc, ielm, re-builder (B) | wgrep |
| remote | tramp, tramp-container (B) | |
| input | | rime (needs librime) |

Caveats: packages whose last stable tag predates Emacs 30 or their companions are taken from MELPA snapshots (`:prefer-snapshot` in packages.el); pdf-tools will build `epdfinfo` at image time (poppler and cairo dev packages) and jinx compiles a module against enchant; pet optionally uses `dasel` and `sqlite3`; dape needs the debug adapters on PATH.

## 4. Language servers and tools

| Tool | Language | Version | License |
|---|---|---|---|
| Rust toolchain through rustup: rustc, cargo, [rust-analyzer](https://rust-analyzer.github.io/), clippy, rustfmt, rust-src | Rust | toolchain 1.99.1, rustup 1.29.2 | MIT / Apache-2.0 |
| [uv](https://docs.astral.sh/uv/), [basedpyright](https://github.com/DetachHead/basedpyright), [ruff](https://docs.astral.sh/ruff/), Debian python3 | Python | uv 0.13.5, basedpyright 1.40.2, ruff 0.17.2 | MIT / Apache-2.0, PSF |
| ripgrep, fd, git, jq, gcc, make | utilities | Debian packages | MIT / Unlicense, GPL-2.0, MIT, GPL-3.0 |
| *planned:* [ocaml-lsp-server](https://github.com/ocaml/ocaml-lsp), merlin, ocamlformat, utop, dune (opam switch 5.3) | OCaml | | ISC / MIT |
| *planned:* [tinymist](https://github.com/Myriad-Dreamin/tinymist), [typst](https://typst.app/) | Typst | | Apache-2.0 |
| *planned:* [texlab](https://github.com/latex-lsp/texlab), latexmk, TeX Live scheme-medium | LaTeX | | GPL-3.0 / LPPL and others |
| *planned:* [marksman](https://github.com/artempyanykh/marksman), [bash-language-server](https://github.com/bash-lsp/bash-language-server), shellcheck, debugpy, [codelldb](https://github.com/vadimcn/codelldb), [ocamlearlybird](https://github.com/hackwaly/ocamlearlybird), delta, pandoc, enchant, poppler | Markdown, shell, debugging, utilities | | MIT, GPL-3.0 and others |

## 5. Go modules

Go 1.26 (`go.mod`; toolchain 1.26.10 in the images and CI). Every direct module is imported by `internal/deps/deps.go` so `go mod tidy` keeps it.

| Module | Version | Role | License | Caveats |
|---|---|---|---|---|
| [github.com/coder/websocket](https://github.com/coder/websocket) | v1.8.15 | WebSockets for pty, events, vnc | ISC | context-based API; compression off for binary streams |
| [github.com/creack/pty](https://github.com/creack/pty) | v1.1.24 | pseudo-terminals for tmux attach | MIT | |
| [github.com/moby/moby/client](https://pkg.go.dev/github.com/moby/moby/client), `moby/moby/api` | v0.6.2, v1.56.1 | Docker Engine API | Apache-2.0 | the module path moved from `github.com/docker/docker/client`; `containerd/errdefs` v1.0.0 classifies its errors |
| [github.com/fsnotify/fsnotify](https://github.com/fsnotify/fsnotify) | v1.10.1 | workspace watching | BSD-3 | not recursive; directories are added as they appear |
| [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) | v1.61.0 | SQLite without cgo | BSD-3 | |
| [github.com/coreos/go-oidc/v3](https://github.com/coreos/go-oidc), [golang.org/x/oauth2](https://pkg.go.dev/golang.org/x/oauth2) | v3.21.0, v0.37.0 | OIDC login (*planned*: answers 501) | Apache-2.0, BSD-3 | |
| [github.com/invopop/jsonschema](https://github.com/invopop/jsonschema) | v0.14.0 | `deploy/config.schema.json` from the config struct | MIT | TypeScript types come from `internal/protocol/tsgen`, not from JSON Schema |
| [github.com/goccy/go-yaml](https://github.com/goccy/go-yaml) | v1.19.2 | scenario, index and table parsing | MIT | |
| [github.com/klauspost/compress](https://github.com/klauspost/compress) | v1.20.3 | zstd fixture tarballs | Apache-2.0, BSD-3 | |
| [github.com/prometheus/client_golang](https://github.com/prometheus/client_golang) | v1.25.0 | optional metrics | Apache-2.0 | |
| [github.com/testcontainers/testcontainers-go](https://github.com/testcontainers/testcontainers-go) | v0.44.0 | sessiond integration test | MIT | |
| Standard library | | `net/http` mux with patterns, `net/http/httputil.ReverseProxy`, `os.Root`, `log/slog`, `database/sql`, `embed` | BSD-3 | |

## 6. Frontend packages

Node 24, pnpm 11.28.8 (`packageManager`, through corepack). Exact versions are pinned in `web/package.json`.

| Package | Role | Version | License | Caveats |
|---|---|---|---|---|
| react, react-dom | UI | 19.3.1 | MIT | |
| vite, @vitejs/plugin-react | build and dev server | 8.3.5, 6.1.2 | MIT | |
| typescript | types | 5.9.3 | Apache-2.0 | typescript-eslint 8.72 supports TypeScript below 6.1, so TypeScript 7 is out |
| react-router | routing | 7.18.4 | MIT | |
| zustand | live state | 5.0.16 | MIT | |
| @tanstack/react-query | server state | 5.105.0 | MIT | |
| [@xterm/xterm](https://xtermjs.org/) | terminal | 6.1.0-beta.315 | MIT | addons from the same upstream commit: fit 0.12.0-beta.311, webgl 0.20.0-beta.309, unicode11 0.10.0-beta.310, clipboard 0.3.0-beta.313, web-links 0.13.0-beta.310; re-pin all six together |
| [@novnc/novnc](https://github.com/novnc/noVNC) | GUI frame | 1.7.0 | MPL-2.0 | ES module import of `core/rfb.js`; lazy loaded |
| [pdfjs-dist](https://mozilla.github.io/pdf.js/) | PDF preview | 5.7.284 | Apache-2.0 | the worker is bundled as a separate asset |
| react-markdown, remark-gfm, remark-directive, unified, unist-util-visit | lesson rendering | 10.1.0, 4.0.1, 4.0.0, 11.0.5, 5.2.0 | MIT | |
| react-i18next, i18next | UI strings | 17.1.9, 26.5.11 | MIT | |
| vitest, jsdom, @testing-library/react, jest-dom, user-event | unit tests | 5.0.5, 30.1.3, 16.3.3, 7.0.1, 14.6.8 | MIT | |
| msw, vitest-websocket-mock | HTTP and WebSocket mocks in tests | 3.0.3, 0.8.0 | MIT | |
| playwright-core | browser scenarios in `web/e2e/` over CDP | 1.64.1 | Apache-2.0 | drives an external Chrome; no bundled browsers |
| eslint, @eslint/js, typescript-eslint, eslint-plugin-react-hooks, eslint-plugin-react-refresh, eslint-plugin-jsx-a11y, prettier | lint and format | 9.39.5, 9.39.5, 8.72.1, 7.1.1, 0.5.7, 6.10.2, 3.9.10 | MIT | ESLint 9 because eslint-plugin-jsx-a11y 6.10 does not declare ESLint 10 |
| @playwright/test, json-schema-to-typescript | (pinned, not used by the code) | 1.64.1, 16.0.0 | Apache-2.0, MIT | candidates for removal through integration |

## 7. External services

None required. Optional: an OIDC provider for server login; a container registry (GHCR) for images; Let's Encrypt through Caddy for certificates. No telemetry leaves a deployment unless `features.telemetryUpload` is enabled by the operator.
