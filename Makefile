# Emacs Playground — developer entry points (deployment.md §4, AGENTS.md).
#
#   make local            write .env and deploy/config.json for local mode, create ./workspace,
#                         pull images (BUILD=1 builds them instead); then `docker compose up`
#   make server           select the server overlay (deployment.md §5); then `docker compose up -d`
#   make shell            attach a terminal to the local session's Emacs (tmux)
#   make dev              session container + gateway from source + Vite dev server
#   make images           build the gateway and session images (and remote-box with REMOTE=1)
#   make reset-workspace  move ./workspace aside and start fresh
#   make check            everything CI runs: build, vet, tests, lint, generated-file drift

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

export PATH := $(HOME)/.local/go/bin:$(HOME)/go/bin:$(HOME)/.local/bin:$(PATH)
export COREPACK_ENABLE_DOWNLOAD_PROMPT := 0

TAG ?= latest
DOMAIN ?=
GO ?= go
PNPM ?= pnpm
COMPOSE ?= docker compose
LOCAL_FILES := -f compose.yaml -f compose.local.yaml
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/zsy/emacs-playground/internal/buildinfo.Version=$(VERSION)

.PHONY: help
help: ## show targets
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

# ---- deployment modes -------------------------------------------------------

.PHONY: local
local: ## local mode: .env, deploy/config.json, ./workspace; pull images (BUILD=1 builds)
	@printf 'COMPOSE_FILE=compose.yaml:compose.local.yaml\nTAG=%s\nDOMAIN=\n' '$(TAG)' > .env
	@test -f deploy/config.json || printf '{ "mode": "local", "auth": { "mode": "none" } }\n' > deploy/config.json
	@mkdir -p workspace && chmod 777 workspace
	@if [ -n "$${BUILD:-}" ]; then $(COMPOSE) $(LOCAL_FILES) build; else $(COMPOSE) $(LOCAL_FILES) pull --ignore-buildable || true; fi
	@echo "local mode ready: docker compose up, then open http://localhost:8080"

.PHONY: server
server: ## server mode: .env with the server overlay; needs deploy/config.json and DOMAIN
	@test -n "$(DOMAIN)" || { echo "set DOMAIN=emacs.example.edu"; exit 1; }
	@printf 'COMPOSE_FILE=compose.yaml:compose.server.yaml\nTAG=%s\nDOMAIN=%s\n' '$(TAG)' '$(DOMAIN)' > .env
	@if ! grep -q '"server"' deploy/config.json 2>/dev/null; then \
	  echo "deploy/config.json is not a server config: cp deploy/config.example.json deploy/config.json and edit it"; exit 1; fi
	@mkdir -p deploy/secrets && touch deploy/secrets/oidc_client_secret && chmod 600 deploy/secrets/oidc_client_secret
	@test -f deploy/invites.txt || cp deploy/invites.example.txt deploy/invites.txt
	$(GO) run ./cmd/playctl config check deploy/config.json
	$(COMPOSE) pull --ignore-buildable || true
	@echo "server mode ready: docker compose up -d"

.PHONY: shell
shell: ## attach your terminal to the local session's Emacs
	$(COMPOSE) $(LOCAL_FILES) exec session tmux -L pg attach-session -t emacs

.PHONY: reset-workspace
reset-workspace: ## move ./workspace aside (workspace.bak-<time>) and start fresh
	-$(COMPOSE) $(LOCAL_FILES) stop session
	@if [ -d workspace ]; then mv workspace "workspace.bak-$$(date +%Y%m%d-%H%M%S)"; fi
	@mkdir -p workspace && chmod 777 workspace
	@echo "fresh ./workspace; docker compose up -d session to start again"

# ---- images -------------------------------------------------------------------

.PHONY: images
images: ## build images (gateway, session; REMOTE=1 adds remote-box)
	$(COMPOSE) $(LOCAL_FILES) build gateway session
	@if [ -n "$${REMOTE:-}" ]; then $(COMPOSE) $(LOCAL_FILES) --profile remote build remote-box; fi

.PHONY: image-gateway image-session
image-gateway: ## build only the gateway image
	docker build -f images/gateway/Dockerfile --build-arg VERSION=$(VERSION) -t ghcr.io/zsy/emacs-playground/gateway:$(TAG) .
image-session: ## build only the session image
	docker build -f images/session/Dockerfile --build-arg VERSION=$(VERSION) -t ghcr.io/zsy/emacs-playground/session:$(TAG) .

# ---- development --------------------------------------------------------------

.PHONY: dev
dev: web/node_modules ## session container (host network) + gateway from source + Vite on :5173
	@mkdir -p workspace .dev/data && chmod 777 workspace
	$(COMPOSE) $(LOCAL_FILES) -f compose.dev.yaml up -d session
	$(GO) run ./cmd/playctl curriculum build curriculum -o .dev/curriculum
	@trap 'kill 0' EXIT INT TERM; \
	  $(GO) run ./cmd/gateway -config deploy/config.dev.json & \
	  (cd web && $(PNPM) dev)

web/node_modules: web/package.json web/pnpm-lock.yaml
	cd web && $(PNPM) install --frozen-lockfile
	@touch web/node_modules

.PHONY: build go-build web-build gateway sessiond playctl
build: go-build web-build ## build Go binaries (bin/) and the web bundle (web/dist)
go-build:
	$(GO) build ./...
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/sessiond ./cmd/sessiond
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/playctl ./cmd/playctl
web-build: web/node_modules
	cd web && $(PNPM) build
gateway: web-build ## gateway binary with the embedded web bundle
	$(GO) build -trimpath -tags embedweb -ldflags '$(LDFLAGS)' -o bin/gateway ./cmd/gateway

.PHONY: protocol
protocol: ## regenerate web/src/protocol.gen.ts from internal/protocol
	$(GO) run ./cmd/playctl protocol typescript -o web/src/protocol.gen.ts

.PHONY: config-schema
config-schema: ## regenerate deploy/config.schema.json
	$(GO) run ./cmd/playctl config schema > deploy/config.schema.json

.PHONY: curriculum-validate curriculum-build
curriculum-validate: ## validate curriculum/
	$(GO) run ./cmd/playctl curriculum validate curriculum
curriculum-build: ## build the curriculum bundle into artifacts/curriculum
	$(GO) run ./cmd/playctl curriculum build curriculum -o artifacts/curriculum

# ---- quality ------------------------------------------------------------------

.PHONY: test go-test web-test lint go-lint web-lint check
test: go-test web-test ## Go and web unit tests
go-test:
	$(GO) test ./...
web-test: web/node_modules
	cd web && $(PNPM) test

lint: go-lint web-lint ## gofmt, go vet, eslint, prettier, protocol drift
go-lint:
	@out=$$(gofmt -l cmd internal web/*.go); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	$(GO) run ./cmd/playctl protocol typescript -o web/src/protocol.gen.ts -check
web-lint: web/node_modules
	cd web && $(PNPM) lint
	cd web && $(PNPM) format:check
	cd web && $(PNPM) typecheck

check: go-build lint test web-build ## everything CI runs

.PHONY: clean
clean: ## remove build output (not the workspace)
	rm -rf bin web/dist artifacts .dev/curriculum
