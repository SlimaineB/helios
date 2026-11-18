# Build context: repository root.
# docker build -f deploy/examples/runner-tunnel/Dockerfile -t helios/runner:latest .
FROM golang:1.25-alpine AS build
WORKDIR /build
COPY agent/go.mod agent/go.sum ./
RUN go mod download
COPY agent/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/helios-runner ./cmd/runner

FROM python:3.11-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir --break-system-packages ansible-core \
    && ansible-galaxy collection install ansible.posix -p /usr/share/ansible/collections \
    && groupadd -g 10001 helios \
    && useradd -u 10001 -g helios -M -s /usr/sbin/nologin helios \
    && mkdir -p /var/lib/helios/runner \
    && chown -R helios:helios /var/lib/helios
COPY --from=build /out/helios-runner /usr/local/bin/helios-runner
ENV HOME=/var/lib/helios/runner
USER helios
ENTRYPOINT ["/usr/local/bin/helios-runner"]
