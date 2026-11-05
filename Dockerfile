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

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 helios \
    && adduser -S -D -H -u 10001 -G helios -h /app helios \
    && mkdir -p /app /data \
    && chown -R helios:helios /app /data
WORKDIR /app
COPY --from=server /out/helios-server /usr/local/bin/helios-server
COPY --from=ui --chown=helios:helios /build/dist /app/ui
ENV HELIOS_DB=/data/helios.db
VOLUME ["/data"]
EXPOSE 8080
USER 10001:10001
# ENTRYPOINT + CMD so that `helm` can put extra flags in `args` (they append to
# the entrypoint) while a plain `docker run` still gets the defaults in CMD.
ENTRYPOINT ["helios-server"]
CMD ["-addr", ":8080", "-db", "/data/helios.db", "-ui", "/app/ui"]