# Airgap / offline install

Helios can be installed on a host with no internet access. Build a bundle on a
connected machine, copy it to the target, and run the offline installer.

## Supported targets

| Distribution | Release | Package family |
|--------------|---------|----------------|
| Debian | 12 | `deb` |
| Ubuntu | 22.04, 24.04 | `deb` |
| Rocky Linux | 9 | `rpm` |
| AlmaLinux | 9 | `rpm` |

Build the bundle for the same distribution, release and architecture as the
target. The default architecture is `amd64`; `arm64` builds require Docker
buildx/QEMU on the build host.

## Build a bundle

On a connected machine with Docker, `curl`, `tar` and `sha256sum`:

```sh
git clone https://github.com/SlimaineB/helios.git
cd helios

./hack/airgap-bundle.sh --distro debian --release 12 --version v0.1.1
./hack/airgap-bundle.sh --distro ubuntu --release 24.04 --version v0.1.1
./hack/airgap-bundle.sh --distro rocky  --release 9    --version v0.1.1

# or via make
make airgap DISTRO=rocky RELEASE=9 VERSION=v0.1.1
```

Use `--component server` or `--component agent` for a smaller bundle. The
default `--component both` includes both installers. With `make`, pass extra
flags through `AIRGAP_ARGS`, for example
`AIRGAP_ARGS='--component agent --out dist/airgap'`.

The bundle is written to `dist/airgap/`:

```text
helios-airgap-v0.1.1-debian12-amd64/
helios-airgap-v0.1.1-debian12-amd64.tar.gz
helios-airgap-v0.1.1-debian12-amd64.tar.gz.sha256
```

Each bundle contains:

- the matching `helios-server` / `helios-agent` release tarballs;
- a local APT or DNF repository with `git`, `python3`, `ca-certificates`,
  `tar`, `curl` and SSH client packages;
- a self-contained Ansible virtualenv (`ansible-core` + `ansible.posix`);
- `install-airgap.sh`, `install-server.sh`, `install-agent.sh` and systemd units;
- `MANIFEST` and `README.txt`.

Ansible versions are selected for the distribution Python:

| Target | Python | ansible-core | ansible.posix |
|--------|--------|--------------|---------------|
| Debian 12 | 3.11 | 2.18.6 | 2.2.2 |
| Ubuntu 22.04 | 3.10 | 2.17.12 | 2.2.2 |
| Ubuntu 24.04 | 3.12 | 2.20.0 | 2.2.2 |
| Rocky / AlmaLinux 9 | 3.9 | 2.15.13 | 2.1.0 |

## Install on the target

Copy the `.tar.gz` and `.sha256` files to the target, then:

```sh
sha256sum -c helios-airgap-v0.1.1-debian12-amd64.tar.gz.sha256
tar -xzf helios-airgap-v0.1.1-debian12-amd64.tar.gz
cd helios-airgap-v0.1.1-debian12-amd64
```

### Hub

```sh
sudo ./install-airgap.sh server \
  --base-url https://hub.example.com \
  --operator-password 'a-long-random-password'
```

The installer creates a local APT/DNF repository, installs the base packages,
installs the server, and starts `helios-server.service`. No network access is
used.

If the bundle was built with `--component both`, the server also publishes the
agent tarball under `/downloads/`. Other managed hosts can then fetch it from
the hub instead of copying the airgap bundle to every host:

```sh
arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
curl -fsSLO "https://hub.example.com/downloads/helios-agent-linux-${arch}.tar.gz"
tar -xzf "helios-agent-linux-${arch}.tar.gz"
sudo ./install-agent.sh --server https://hub.example.com --token '<instance-token>'
```

### Agent

Register the instance in the hub UI first, then run on the managed host:

```sh
sudo ./install-airgap.sh agent \
  --server https://hub.example.com \
  --token '<instance-token>'
```

This installs the OS packages, extracts Ansible to `/opt/helios/ansible`, writes
`/etc/helios/agent.yaml` with `ansible_bin`, and starts
`helios-agent.service`.

If the host is already provisioned with the required packages and Ansible:

```sh
sudo ./install-airgap.sh --skip-os-packages agent \
  --server https://hub.example.com \
  --token '<instance-token>' \
  --ansible-bin /path/to/ansible-playbook
```

## Notes and limits

- The target must already run systemd and match the bundle distribution and
  architecture. A Debian 12 bundle cannot install on Rocky Linux or Ubuntu.
- Only `ansible.posix` is bundled. If your playbooks use other collections,
  vendor them in the repository (`collections/`) or extend the bundle.
- Repositories fetched over SSH work because `openssh-client` /
  `openssh-clients` is included. HTTPS repositories use the bundled CA store.
- `--skip-os-packages` is useful for golden images or hosts managed by another
  package pipeline.
- SUSE/zypper targets are not covered yet.
- The build host needs internet access to pull the Docker image, OS packages,
  Ansible wheels and collection. The target does not.
