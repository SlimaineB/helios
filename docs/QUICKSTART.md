# Quickstart: install Helios

This guide installs Helios on Linux servers with systemd. You do **not** need
Go, Node, Docker or build tools on the hub.

## What you need

- **Hub host:** Linux amd64/arm64, systemd, root access, port `8080` reachable
  from your browser.
- **Managed host:** Linux amd64/arm64, systemd, root access, outbound HTTPS to
  the hub and to your Git repository.
- **Playbooks:** an existing Git repository containing an Ansible playbook.

## 1. Install the hub

Run this on the hub host. Use a long random password and keep it safe.

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- server \
      --base-url http://YOUR_SERVER_IP:8080 \
      --operator-password 'REPLACE_WITH_LONG_RANDOM_PASSWORD'
```

The installer downloads the release, verifies its checksum, installs the binary
and UI, creates the `helios` system user, and starts `helios-server.service`.
The database is stored in `/var/lib/helios/helios.db`.

Open **http://YOUR_SERVER_IP:8080** and sign in as `admin` with the password from
the command above.

Verify the installation:

```sh
systemctl status helios-server
curl -fsS http://127.0.0.1:8080/api/v1/health
```

If the browser cannot connect, open TCP `8080` in the firewall or cloud
security group. If you use HTTPS, set `--base-url https://hub.example.com`
instead.

## 2. Install an agent

On the hub UI, open **Instances → Register instance**, give it a name, and copy
the token. The token is shown only once.

Then run this **on the server you want Helios to manage**:

```sh
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- agent \
      --server http://YOUR_SERVER_IP:8080 \
      --token 'PASTE_INSTANCE_TOKEN' \
      --poll-interval 30s
```

The installer installs `git`, `ansible-core` and `ansible.posix` if needed,
writes `/etc/helios/agent.yaml`, and starts `helios-agent.service`. The instance
appears in the UI within about 30 seconds.

```sh
systemctl status helios-agent
journalctl -u helios-agent -f
```

## 3. Add an application

In the UI, open **Applications → New application** and fill in:

- the Git repository URL;
- the branch, tag, or revision to track;
- the playbook path, for example `site.yml`;
- optional variables and `autoheal`.

Assign the application to the instance. Click **Check** to run a dry run, or
**Sync** to apply it now.

## Upgrade

The database is preserved. Re-run the installer with the same options.

```sh
# Hub
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- server \
      --base-url http://YOUR_SERVER_IP:8080 \
      --operator-password '<SAME-PASSWORD>'

# Agent
curl -fsSL https://github.com/SlimaineB/helios/releases/latest/download/install.sh \
  | sudo sh -s -- agent \
      --server http://YOUR_SERVER_IP:8080 \
      --token '<SAME-INSTANCE-TOKEN>'
```

To pin a release, add `HELIOS_VERSION=v0.1.1` after `sudo`, for example
`sudo env HELIOS_VERSION=v0.1.1 sh -s -- server ...`.

## Troubleshooting

| Problem | Check |
|---------|-------|
| Browser cannot reach the UI | firewall allows TCP 8080; `systemctl status helios-server` |
| Login fails | user is `admin`; password is the one passed to the installer |
| Agent never appears | `systemctl status helios-agent`; `journalctl -u helios-agent -f` |
| Agent appears but app stays unsynced | Git repo/path is correct and reachable from the managed host |
| `install.sh` fails | host is Linux amd64/arm64 with systemd; rerun with `sudo` |
| Port 8080 already used | stop the other service or use `--addr :9090` and update `--base-url` |

## Other installation methods

- [Docker Compose](INSTALL.md#option-a-single-host-docker-compose-recommended-for-a-first-install)
- [Kubernetes / Helm](INSTALL.md#option-c-kubernetes-helm)
- [Agent reference](INSTALL.md#install-an-agent)
- [Releases and packages](RELEASES.md)

Building from source, running the development demo and tests:
[Development](DEVELOPMENT.md).
