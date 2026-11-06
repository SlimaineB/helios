.PHONY: build build-agent build-server ui build-local test test-race test-ui demo smoke loop-test docs docs-build docs-serve release airgap compose-up compose-down compose-logs compose-clean clean

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

## Documentation site (requires: pip install -r requirements-docs.txt)
docs: docs-build

docs-build:
	mkdocs build --strict

docs-serve:
	mkdocs serve

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

clean:
	rm -f agent/helios-agent server/helios-server helios.db
	rm -rf ui/dist ui/node_modules
