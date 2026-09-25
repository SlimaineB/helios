# Build context: repository root.
# Multi-stage: react UI -> go server -> minimal runtime serving both.

FROM node:20-alpine AS ui
WORKDIR /build
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.25-alpine AS server
WORKDIR /build
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/ansiblehub-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=server /out/ansiblehub-server /usr/local/bin/ansiblehub-server
COPY --from=ui /build/dist /app/ui
ENV ANSIBLEHUB_DB=/data/ansiblehub.db
VOLUME ["/data"]
EXPOSE 8080
# ENTRYPOINT + CMD so that `helm` can put extra flags in `args` (they append to
# the entrypoint) while a plain `docker run` still gets the defaults in CMD.
ENTRYPOINT ["ansiblehub-server"]
CMD ["-addr", ":8080", "-db", "/data/ansiblehub.db", "-ui", "/app/ui"]