.PHONY: build build-agent build-server ui build-local test test-race test-ui demo smoke loop-test docs docs-build docs-serve release airgap compose-up compose-down compose-logs compose-clean example-env example-up example-smoke example-demo example-logs example-psql example-down example-clean clean

## Local development (no docker)
ui:
	cd ui && npm install && npm run build

build-agent:
	cd agent && go build -o helios-agent ./cmd/agent

build-server:
	cd server && go build -o helios-server ./cmd/server

# Full local build of all binaries + UI (git/ansible must be installed on host).
build-local: build-agent build-server ui

test:
	cd server && go vet ./... && go test ./...
	cd agent && go vet ./... && go test ./...

test-race:
	cd server && go test -race ./...
	cd agent && go test -race ./...

test-ui:
	cd ui && npm ci && npm run build

## Documentation site (Astro + Starlight)
docs: docs-build

docs-build:
	npm ci
	npm run build

docs-serve:
	npm install
	npm run dev

## One-command lab demo (build + start + wait + smoke)
demo:
	./hack/demo.sh

## Running compose stack (start it first: make compose-up)
smoke:
	./hack/smoke.sh

loop-test:
	./hack/looptest.sh

## Release: make release TAG=v0.1.2 (tags current main and pushes it)
release:
	@test -n "$(TAG)" || { echo "usage: make release TAG=v0.1.2" >&2; exit 1; }
	@printf '%s' "$(TAG)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "TAG must look like v0.1.2" >&2; exit 1; }
	@test "$$(git branch --show-current)" = main || { echo "release from the main branch" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "working tree is dirty; commit and push first" >&2; exit 1; }
	@git rev-parse -q --verify "refs/tags/$(TAG)" >/dev/null && { echo "tag $(TAG) already exists" >&2; exit 1; } || true
	git push origin main
	git tag -a "$(TAG)" -m "Helios $(TAG)"
	git push origin "$(TAG)"

## Offline bundle: make airgap DISTRO=rocky RELEASE=9 VERSION=v0.1.1
DISTRO ?= debian
RELEASE ?= 12
airgap:
	./hack/airgap-bundle.sh --distro "$(DISTRO)" --release "$(RELEASE)" \
		$(if $(VERSION),--version "$(VERSION)") $(AIRGAP_ARGS)

## docker compose harness
compose-up:
	docker compose up -d --build

compose-logs:
	docker compose logs -f

compose-down:
	docker compose down

compose-clean:
	docker compose down -v
	@rm -rf hdata

## Full Dex + PostgreSQL example (deploy/examples/dex-postgres)
EXAMPLE_DIR := deploy/examples/dex-postgres
EXAMPLE_COMPOSE := docker compose --env-file $(EXAMPLE_DIR)/.env -f $(EXAMPLE_DIR)/compose.yaml

example-env:
	@test -f $(EXAMPLE_DIR)/.env || { \
		cp $(EXAMPLE_DIR)/.env.example $(EXAMPLE_DIR)/.env; \
		echo "created $(EXAMPLE_DIR)/.env (edit it to change the lab credentials)"; \
	}

example-up: example-env
	$(EXAMPLE_COMPOSE) up -d --build
	@printf '\nHelios full example starting\n  UI:      http://localhost:8081\n  SSO:     http://localhost:5557\n  Postgres: localhost:5433\n'
	@printf 'Run make example-smoke to wait for bootstrap and verify the stack.\n'

example-smoke: example-env
	./$(EXAMPLE_DIR)/smoke.sh

example-demo: example-up example-smoke

example-logs: example-env
	$(EXAMPLE_COMPOSE) logs -f

example-psql: example-env
	$(EXAMPLE_COMPOSE) exec db psql -U helios -d helios -p 5433

example-down: example-env
	$(EXAMPLE_COMPOSE) down

example-clean: example-env
	$(EXAMPLE_COMPOSE) down -v

clean:
	rm -f agent/helios-agent server/helios-server helios.db
	rm -rf ui/dist ui/node_modules
