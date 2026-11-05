# Releases and GitHub packages

Pushing a `v*` tag runs `.github/workflows/release.yml`, which:

1. runs the Go tests and a production UI build;
2. builds multi-arch (`linux/amd64`, `linux/arm64`) container images and
   pushes them to GHCR;
3. packages both Helm charts and pushes them as OCI artifacts to GHCR;
4. builds the bare-metal server and agent binaries for both architectures;
5. creates a GitHub Release with the bare-metal install packages, chart
   archives and `checksums.txt`.

The bare-metal release does not depend on the container image jobs, so a
Docker-only failure cannot hide the systemd packages.

Use a semver tag:

```sh
git tag v1.2.3
git push origin v1.2.3
```

A manual **Actions → Release → Run workflow** builds the images, bare-metal
packages and Helm charts without creating a GitHub Release. The packages and
charts are uploaded as workflow artifacts; images and charts are only pushed
to GHCR when the **publish** input is enabled.

## Container images

Images are published to the GitHub Container Registry:

| Image | Dockerfile | Use |
|-------|------------|-----|
| `ghcr.io/<owner>/helios-server` | `Dockerfile` | Hub API + UI |
| `ghcr.io/<owner>/helios-agent` | `agent/Dockerfile` | Managed-host agent |
| `ghcr.io/<owner>/helios-repo` | `hack/repo-server/Dockerfile` | Lab demo Git server |

Tags for a `v1.2.3` release: `1.2.3`, `1.2`, `1`, `latest` and `sha-<short>`.
Pull with:

```sh
docker pull ghcr.io/<owner>/helios-server:1.2.3
```

## Helm charts

Both charts are published as OCI artifacts in GHCR:

```text
oci://ghcr.io/<owner>/charts/helios-server
oci://ghcr.io/<owner>/charts/helios-agent
```

The chart version matches the release tag (`v1.2.3` → `1.2.3`). The packaged
charts pin the image repository to `ghcr.io/<owner>/helios-server` or
`ghcr.io/<owner>/helios-agent`, and the image tag to the release version.

```sh
helm registry login ghcr.io --username <github-user>

helm install helios-server \
  oci://ghcr.io/<owner>/charts/helios-server \
  --version 1.2.3 \
  --namespace helios --create-namespace \
  --set operatorAuth.password='a-long-random-password' \
  --set service.type=LoadBalancer

helm install agent-1 \
  oci://ghcr.io/<owner>/charts/helios-agent \
  --version 1.2.3 \
  --namespace helios \
  --set config.server=http://helios-server:80 \
  --set config.token='<instance-token>'
```

When installing from a checkout instead, use `helm/helios-server` or
`helm/helios-agent` and override `image.repository` / `image.tag` explicitly.

GitHub packages are private by default. Either make the package public in
**GitHub → Packages → Package settings → Change visibility**, or log in before
pulling:

```sh
echo "$GITHUB_TOKEN" | docker login ghcr.io -u <github-user> --password-stdin
echo "$GITHUB_TOKEN" | helm registry login ghcr.io \
  --username <github-user> --password-stdin
```

The workflow uses the repository `GITHUB_TOKEN`; no extra registry secret is
required. It needs `packages: write`, which the workflow declares explicitly.

## Bare-metal packages (no Docker)

No container runtime is required for the server or agent. Each GitHub Release
contains runnable Linux binaries, systemd installers and one-command bootstrap
script:

```text
install.sh                                   # one-command install for server/agent
helios-server-linux-amd64
helios-server-linux-arm64
helios-agent-linux-amd64
helios-agent-linux-arm64
helios-server-<version>-linux-amd64.tar.gz   # binary + ui/ + install-server.sh
helios-server-<version>-linux-arm64.tar.gz
helios-agent-<version>-linux-amd64.tar.gz    # binary + install-agent.sh
helios-agent-<version>-linux-arm64.tar.gz
helios-server-<version>.tgz                  # Helm chart
helios-agent-<version>.tgz                   # Helm chart
checksums.txt
```

Install the hub in one command:

```sh
curl -fsSL https://github.com/<owner>/helios/releases/latest/download/install.sh \
  | sudo sh -s -- server \
      --base-url https://hub.example.com \
      --operator-password 'a-long-random-password'
```

Install an agent in one command:

```sh
curl -fsSL https://github.com/<owner>/helios/releases/latest/download/install.sh \
  | sudo sh -s -- agent \
      --server https://hub.example.com \
      --token '<instance-token>'
```

Set `HELIOS_VERSION=v1.2.3` with `sudo env` to pin a release.

Or install the hub from a release tarball:

```sh
curl -LO https://github.com/<owner>/helios/releases/download/v1.2.3/helios-server-1.2.3-linux-amd64.tar.gz
mkdir -p helios-server && tar -xzf helios-server-1.2.3-linux-amd64.tar.gz \
  -C helios-server
sudo ./helios-server/install-server.sh \
  --binary ./helios-server/helios-server \
  --ui ./helios-server/ui \
  --base-url https://hub.example.com \
  --operator-password 'a-long-random-password'
```

Install an agent from a release tarball:

```sh
curl -LO https://github.com/<owner>/helios/releases/download/v1.2.3/helios-agent-1.2.3-linux-amd64.tar.gz
mkdir -p helios-agent && tar -xzf helios-agent-1.2.3-linux-amd64.tar.gz \
  -C helios-agent
sudo ./helios-agent/install-agent.sh \
  --server https://hub.example.com \
  --token '<bootstrap-token>'
```

Verify downloads with:

```sh
sha256sum -c checksums.txt
```

See [Install](INSTALL.md) for TLS, PostgreSQL, systemd and agent details.
