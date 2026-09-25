# Build context: repository root. Runs the agent on EC2-like hosts.
# Needs: git + ansible-playbook at runtime, agent binary from Go build.

FROM golang:1.25-alpine AS build
WORKDIR /build
COPY agent/go.mod agent/go.sum ./
RUN go mod download
COPY agent/ ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/ansiblehub-agent ./cmd/agent

FROM python:3.11-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir --break-system-packages ansible-core
COPY --from=build /out/ansiblehub-agent /usr/local/bin/ansiblehub-agent
# ansible-core 2.19 removed the built-in `json` stdout callback; it lives in ansible.posix.
# Install to a HOME-independent shared path: the runtime sets HOME=/tmp (line 23),
# so the default ~/.ansible/collections target would be invisible to the pod.
RUN ansible-galaxy collection install ansible.posix -p /usr/share/ansible/collections
COPY hack/agent-entrypoint.sh /usr/local/bin/agent-entrypoint.sh
RUN chmod +x /usr/local/bin/agent-entrypoint.sh
# The pod may run as an unprivileged UID; ansible wants a writable $HOME
# for its scratch dir (~/.ansible/tmp).
ENV HOME=/tmp
ENTRYPOINT ["/usr/local/bin/agent-entrypoint.sh"]