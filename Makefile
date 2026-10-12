# MyCalendar
#
# `make build` produces one self-contained binary: the Vite bundle is compiled
# first, then embedded into the Go binary. The plain Go toolchain also works on
# its own (see build-backend), it just ships without the UI.

GO      ?= go
BINARY  ?= mycalendar
TAG     ?= webui_embedded

.PHONY: help all build build-backend web web-install run dev deploy test test-embed test-race vet fmt check clean

help:
	@echo 'Targets:'
	@echo '  make build          build the frontend and the binary that embeds it'
	@echo '  make build-backend  build only the Go binary, no Node toolchain needed'
	@echo '  make web-install    install frontend dependencies (npm ci)'
	@echo '  make web            build the frontend bundle only'
	@echo '  make run            build and run'
	@echo '  make dev            run the backend with the Vite dev server on top'
	@echo '  make deploy         docker compose: pull the published image, or build if that fails'
	@echo '  make check          vet, unit tests and a frontend typecheck'
	@echo '  make clean          remove the binary and the generated bundle'

all: build

# ---------------------------------------------------------------- frontend

# node_modules as a file target so `npm ci` only runs when it is missing or the
# lockfile changed.
web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci

web-install: web/node_modules

web: web/node_modules
	cd web && npm run build

# ------------------------------------------------------------------ binary

build: web
	$(GO) build -tags $(TAG) -o $(BINARY) ./cmd/mycalendar
	@echo 'built ./$(BINARY) with the frontend embedded'

build-backend:
	$(GO) build -o $(BINARY) ./cmd/mycalendar
	@echo 'built ./$(BINARY) without the frontend'

run: build
	./$(BINARY)

# The Go server on :8080 and the Vite dev server on :5173, which proxies /api,
# /help and /healthz to the backend.
dev:
	@echo 'backend: http://127.0.0.1:8080   frontend: http://127.0.0.1:5173'
	@$(MAKE) -j2 dev-backend dev-web

dev-backend: build-backend
	./$(BINARY)

dev-web: web/node_modules
	cd web && npm run dev

# ------------------------------------------------------------------ deploy

# Deploy with Docker, preferring the image CI published to ghcr.io and falling
# back to a local build when the pull cannot succeed: no registry access, not
# logged in, or the package still private.
#
# Compose cannot express this. Its pull_policy has no "pull, else build" value,
# and a plain `docker compose up` just fails when the pull does -- which is the
# error people hit. Hence a target rather than a one-liner in the compose file.
#
# The compose file itself is gitignored, so it is a prerequisite here purely to
# produce a useful message when it has not been copied from the template yet.
deploy: docker-compose.yml
	@if docker compose pull --quiet; then \
		echo 'using the published image'; \
		docker compose up -d; \
	else \
		echo 'pull failed; building locally instead'; \
		docker compose up -d --build; \
	fi
	@echo
	@echo 'http://127.0.0.1:8080/' | sed 's|^|up at |'

docker-compose.yml:
	@echo 'error: docker-compose.yml does not exist.' >&2
	@echo '       It is deliberately not committed; copy the template first:' >&2
	@echo '         cp docker-compose.yml.template docker-compose.yml' >&2
	@exit 1

# ------------------------------------------------------------------ checks

test:
	$(GO) test ./...

# Also verifies that a real bundle is embedded, which `go test` alone cannot.
test-embed: web
	$(GO) test -tags $(TAG) ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...
	gofmt -l .

check: vet test
	cd web && npm run typecheck

clean:
	rm -f $(BINARY)
	rm -rf internal/webui/dist
