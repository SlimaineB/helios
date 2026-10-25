.PHONY: build build-agent build-server ui build-local test test-race test-ui smoke loop-test compose-up compose-down compose-logs compose-clean clean

## Local development (no docker)
ui:
	cd ui && npm install && npm run build

build-agent:
	cd agent && go build -o ansiblehub-agent ./cmd/agent

build-server:
	cd server && go build -o ansiblehub-server ./cmd/server

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

## Running compose stack (start it first: make compose-up)
smoke:
	./hack/smoke.sh

loop-test:
	./hack/looptest.sh

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
	rm -f agent/ansiblehub-agent server/ansiblehub-server ansiblehub.db
	rm -rf ui/dist ui/node_modules
