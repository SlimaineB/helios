# Releases and GitHub packages

Pushing a `v*` tag runs `.github/workflows/release.yml`, which:

1. runs the Go tests and a production UI build;
2. builds multi-arch (`linux/amd64`, `linux/arm64`) container images;
3. builds server and agent binaries for both architectures;
4. creates a GitHub Release with the binaries, installer tarballs and
   `checksums.txt`.

Use a semver tag:

```sh
git tag v1.2.3
git push origin v1.2.3
```

A manual **Actions → Release → Run workflow** only builds and pushes the
container images (no GitHub Release, because there is no tag).

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

For Helm, point the charts at the registry:

```sh
helm upgrade --install helios-server helm/helios-server \
  --set image.repository=ghcr.io/<owner>/helios-server \
  --set image.tag=1.2.3
```

GitHub packages are private by default. Either make the package public in
**GitHub → Packages → Package settings → Change visibility**, or log in before
pulling:

```sh
echo "$GITHUB_TOKEN" | docker login ghcr.io -u <github-user> --password-stdin
```

The workflow uses the repository `GITHUB_TOKEN`; no extra registry secret is
required. It needs `packages: write`, which the workflow declares explicitly.

## Binaries and tarballs

Each GitHub Release contains:

```text
helios-server-linux-amd64
helios-server-linux-arm64
helios-agent-linux-amd64
helios-agent-linux-arm64
helios-server-<version>-linux-amd64.tar.gz   # binary + ui/ + install-server.sh
helios-server-<version>-linux-arm64.tar.gz
helios-agent-<version>-linux-amd64.tar.gz    # binary + install-agent.sh
helios-agent-<version>-linux-arm64.tar.gz
checksums.txt
```

Install the hub from a release tarball:

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
