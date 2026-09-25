.PHONY: build build-agent build-server ui build-local test compose-up compose-down compose-logs clean

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

## docker compose harness
compose-up:
	docker compose up -d --build

compose-logs:
	docker compose logs -f

compose-down:
	docker compose down -v

compose-clean: compose-down
	@rm -rf hdata

clean:
	rm -f agent/ansiblehub-agent server/ansiblehub-server ansiblehub.db
	rm -rf ui/dist ui/node_modules