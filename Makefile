.PHONY: build build-agent build-server ui build-local test test-race test-ui demo smoke loop-test docs docs-build docs-serve airgap compose-up compose-down compose-logs compose-clean clean

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
