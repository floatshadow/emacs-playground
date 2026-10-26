# Third-party software and APIs

Everything external the system depends on, with its role, the version known good at design time (pin exact versions at implementation; `images/session/versions.env`, `go.mod` and `web/package.json` are the sources of truth), license, link and caveats.

## 1. Platform and infrastructure

| Software | Role | Version | License | Caveats |
|---|---|---|---|---|
| [Docker Engine](https://docs.docker.com/engine/) and Compose v2 | containers, local and server runs | Engine 27+ | Apache-2.0 | server mode needs the Engine API through a socket proxy |
| [Docker Engine API](https://docs.docker.com/engine/api/) | create, start, stop, inspect containers and volumes | v1.47+ | | used only through the official Go client |
| [Debian](https://www.debian.org/) trixie | base images | 13 | various | |
| [Caddy](https://caddyserver.com/) | TLS termination and reverse proxy (server) | 2.x | Apache-2.0 | automatic certificates need ports 80 and 443 reachable |
| [tecnativa/docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy) | limits the Engine API surface exposed to the gateway | 0.3.x | Apache-2.0 | allow only containers, images, volumes, networks, exec, post |
| [tini](https://github.com/krallin/tini) | PID 1 in session containers | 0.19 | MIT | `init: true` in Compose uses the Docker-bundled copy |
| [gVisor](https://gvisor.dev/) (optional) | stronger isolation for public servers | current | Apache-2.0 | slower file IO; verify builds still meet lesson timings |
| [SQLite](https://sqlite.org/) via modernc.org/sqlite | gateway store | 3.4x | Public domain / BSD-3 (bindings) | single-writer; fine at this scale |

## 2. Emacs and the terminal

| Software | Role | Version | License | Caveats |
|---|---|---|---|---|
| [GNU Emacs](https://www.gnu.org/software/emacs/) | the editor; built from source with native compilation and tree-sitter | 31.1 (2026-08) | GPL-3.0-or-later | released two months before this design; fallback 30.2 |
| [tmux](https://github.com/tmux/tmux) | session persistence and attach for the terminal frame | 3.5a | ISC | `extended-keys always` and `extended-keys-format csi-u` for the kitty protocol |
| [TigerVNC Xvnc](https://tigervnc.org/) | X display for the GUI frame | 1.15 | GPL-2.0 | bound to localhost, `SecurityTypes None`; only sessiond may reach it |
| [xdotool](https://github.com/jordansissel/xdotool) | inject keys into the GUI frame for demos | 3.2 | BSD-3 | |
| [kkp.el](https://github.com/benjaminor/kkp) | kitty keyboard protocol support in Emacs | 0.3+ | GPL-3.0 | check whether Emacs 31 has native support; drop if so |
| [Iosevka Nerd Font](https://www.nerdfonts.com/) | terminal and GUI font with icon glyphs | 3.x | OFL-1.1 | served to the browser as a web font |
| [Noto CJK, Noto Color Emoji](https://fonts.google.com/noto) | CJK and emoji fallback | current | OFL-1.1 | |
| [Libertinus](https://github.com/alerque/libertinus) | serif for Typst samples | 7.x | OFL-1.1 | |
| [Source Serif 4](https://github.com/adobe-fonts/source-serif) | chrome typeface | 4.x | OFL-1.1 | self-hosted, not Google Fonts, in production |

## 3. Emacs packages (by module)

All GPL-3.0-or-later unless noted. Sources: GNU ELPA (G), NonGNU ELPA (N), MELPA (M), built in (B).

| Module | Packages |
|---|---|
| base | which-key (B), modus-themes (B), kkp (M), clipetty (M, fallback), repeat (B), savehist, recentf, saveplace, autorevert, uniquify (B) |
| editing | avy (G), expreg (G), multiple-cursors (N), iedit (M), vundo (G), visual-replace (G), ws-butler (N) |
| completion | vertico, orderless, marginalia, consult, embark, embark-consult, corfu, cape, tempel (all G) |
| files | dirvish (M), consult-dir (M), project (B), bookmark (B), ibuffer (B) |
| windows | windmove, winner, tab-bar, tab-line (B), ace-window (G), popper (M), burly (M), olivetti (M), follow (B) |
| git | magit (N), diff-hl (G), git-timemachine (N), forge (N, optional) |
| terminals | eat (N), vterm (M), eshell, compile (B) |
| lsp | eglot, flymake, xref, eldoc (B), eldoc-box (M), apheleia (M, MIT), dape (G), consult-eglot (M) |
| lang-rust | rust-ts-mode (B), cargo via compile |
| lang-ocaml | tuareg (N, GPL-2+), dune (M), utop (M), merlin-eldoc (M) |
| lang-python | python-ts-mode (B), pet (M) |
| writing | markdown-mode (N), edit-indirect (M), typst-ts-mode (M), auctex (G), cdlatex (M), reftex (B), pdf-tools (N), citar (M), jinx (G), olivetti (M) |
| org | org (B), org-roam (M), org-present (M), ob-* for shell, python, ocaml, rust (M: ob-rust) |
| power | wgrep (N), wdired, ediff, calc, ielm, re-builder (B) |
| remote | tramp (B), tramp-container (B) |
| ui | nerd-icons, nerd-icons-dired, nerd-icons-completion, nerd-icons-corfu, nerd-icons-ibuffer (M), breadcrumb (G), ligature (M), ef-themes (G), pulsar (G), helpful (M) |
| input | rime (M, optional; needs librime) |

Caveats: vterm compiles a module against libvterm (cmake in the build stage); pdf-tools builds `epdfinfo` at image time (poppler and cairo dev packages in the build stage); jinx compiles a module against enchant; tree-sitter grammars for rust, python, toml, typst, markdown, ocaml are prebuilt into the image; pet optionally uses `dasel` and `sqlite3` binaries; dape needs the debug adapters on PATH.

## 4. Language servers and tools

| Tool | Language | Version | License |
|---|---|---|---|
| [rust-analyzer](https://rust-analyzer.github.io/) | Rust | rolling with the toolchain | MIT / Apache-2.0 |
| [ocaml-lsp-server](https://github.com/ocaml/ocaml-lsp), merlin, ocamlformat, utop, dune | OCaml | current opam | ISC / MIT |
| [basedpyright](https://github.com/DetachHead/basedpyright), [ruff](https://docs.astral.sh/ruff/), debugpy | Python | current | MIT |
| [tinymist](https://github.com/Myriad-Dreamin/tinymist), [typst](https://typst.app/) | Typst | current | Apache-2.0 |
| [texlab](https://github.com/latex-lsp/texlab), latexmk, TeX Live | LaTeX | 2025 | GPL-3.0 / LPPL and others |
| [marksman](https://github.com/artempyanykh/marksman) | Markdown | current | MIT |
| [bash-language-server](https://github.com/bash-lsp/bash-language-server), shellcheck | shell | current | MIT / GPL-3.0 |
| [codelldb](https://github.com/vadimcn/codelldb), [ocamlearlybird](https://github.com/hackwaly/ocamlearlybird) | debug adapters | current | MIT |
| ripgrep, fd, delta, jq, pandoc, enchant, poppler | utilities | current | MIT, Unlicense, GPL, LGPL |

## 5. Go modules

| Module | Role | License | Caveats |
|---|---|---|---|
| [github.com/coder/websocket](https://github.com/coder/websocket) | WebSockets for pty, events, vnc | ISC | context-based API; compression off for binary streams |
| [github.com/creack/pty](https://github.com/creack/pty) | pseudo-terminals for tmux attach | MIT | |
| [github.com/moby/moby/client](https://pkg.go.dev/github.com/moby/moby/client) | Docker Engine API | Apache-2.0 | module path moved from `github.com/docker/docker/client`; pin the current one |
| [github.com/fsnotify/fsnotify](https://github.com/fsnotify/fsnotify) | workspace watching | BSD-3 | not recursive; directories are added as they appear |
| [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) | SQLite without cgo | BSD-3 | |
| [github.com/coreos/go-oidc/v3](https://github.com/coreos/go-oidc), [golang.org/x/oauth2](https://pkg.go.dev/golang.org/x/oauth2) | OIDC login | Apache-2.0, BSD-3 | |
| [github.com/invopop/jsonschema](https://github.com/invopop/jsonschema) | JSON Schema from protocol structs | MIT | |
| [github.com/goccy/go-yaml](https://github.com/goccy/go-yaml) | scenario and table parsing | MIT | |
| [github.com/klauspost/compress](https://github.com/klauspost/compress) | zstd for fixture tarballs | Apache-2.0, BSD-3 | |
| [github.com/prometheus/client_golang](https://github.com/prometheus/client_golang) | optional metrics | Apache-2.0 | |
| [github.com/testcontainers/testcontainers-go](https://github.com/testcontainers/testcontainers-go) | integration tests | MIT | |
| Standard library | `net/http` mux with patterns (1.22+), `os.Root` (1.24+), `log/slog`, `database/sql`, `embed` | BSD-3 | Go 1.25 or later |

## 6. Frontend packages

| Package | Role | Version | License | Caveats |
|---|---|---|---|---|
| react, react-dom | UI | 19 | MIT | |
| vite | build and dev server | 7+ | MIT | |
| typescript | types | 5.x | Apache-2.0 | |
| react-router | routing | 7 | MIT | |
| zustand | live state | 5 | MIT | |
| @tanstack/react-query | server state | 5 | MIT | |
| [@xterm/xterm](https://xtermjs.org/) and addons (fit, webgl, unicode11, clipboard, web-links) | terminal | 6.1 | MIT | kitty keyboard protocol (`vtExtensions.kittyKeyboard`) is in 6.1 betas only until a tagged release; pin a beta and re-pin on release |
| [@novnc/novnc](https://github.com/novnc/noVNC) | GUI frame | 1.6 | MPL-2.0 | ES module import of `core/rfb.js`; lazy loaded |
| [pdfjs-dist](https://mozilla.github.io/pdf.js/) | PDF preview | 5.x | Apache-2.0 | worker must be bundled as a separate asset |
| react-markdown, remark-gfm, remark-directive, unified | lesson rendering | current | MIT | |
| react-i18next, i18next | UI strings | current | MIT | |
| vitest, @testing-library/react, msw, vitest-websocket-mock | tests | current | MIT | |
| @playwright/test | end to end | 1.5x | Apache-2.0 | |
| typescript-eslint, eslint-plugin-react-hooks, eslint-plugin-jsx-a11y, prettier | lint and format | current | MIT | |
| json-schema-to-typescript | protocol types | current | MIT | run by `playctl protocol typescript` |

## 7. External services

None required. Optional: an OIDC provider for server login; container registry (GHCR) for images; Let's Encrypt through Caddy for certificates. No telemetry leaves a deployment unless `features.telemetryUpload` is enabled by the operator.
