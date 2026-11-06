# Build context: repository root.
# Multi-stage: react UI -> go server -> minimal runtime serving both.

FROM node:22-alpine AS ui
WORKDIR /build
COPY ui/ ./
RUN npm ci && npm run build

FROM golang:1.25-alpine AS server
WORKDIR /build
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/helios-server ./cmd/server

FROM golang:1.25-alpine AS agentbuild
WORKDIR /build
COPY agent/go.mod agent/go.sum ./
RUN go mod download
COPY agent/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
        -o /out/amd64/helios-agent ./cmd/agent \
    && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" \
        -o /out/arm64/helios-agent ./cmd/agent
COPY deploy/install-agent.sh deploy/helios-agent.service /out/amd64/
COPY deploy/install-agent.sh deploy/helios-agent.service /out/arm64/
RUN tar -C /out/amd64 -czf /out/helios-agent-linux-amd64.tar.gz . \
    && tar -C /out/arm64 -czf /out/helios-agent-linux-arm64.tar.gz .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 helios \
    && adduser -S -D -H -u 10001 -G helios -h /app helios \
    && mkdir -p /app /app/downloads /data \
    && chown -R helios:helios /app /data
WORKDIR /app
COPY --from=server /out/helios-server /usr/local/bin/helios-server
COPY --from=ui --chown=helios:helios /build/dist /app/ui
COPY --from=agentbuild --chown=helios:helios /out/helios-agent-linux-amd64.tar.gz /app/downloads/
COPY --from=agentbuild --chown=helios:helios /out/helios-agent-linux-arm64.tar.gz /app/downloads/
ENV HELIOS_DB=/data/helios.db \
    HELIOS_DOWNLOADS_DIR=/app/downloads
VOLUME ["/data"]
EXPOSE 8080
USER 10001:10001
# ENTRYPOINT + CMD so that `helm` can put extra flags in `args` (they append to
# the entrypoint) while a plain `docker run` still gets the defaults in CMD.
ENTRYPOINT ["helios-server"]
CMD ["-addr", ":8080", "-db", "/data/helios.db", "-ui", "/app/ui"]