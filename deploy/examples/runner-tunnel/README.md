# Tunnel mode: managed host + bastion runner

This example walks through an outbound-only managed host that is reached
through a reverse SSH tunnel. The **only Ansible executor is `helios-runner` on
the bastion**; the managed host runs `sshd` and `python3` and may run the
lightweight tunnel-only agent, which never executes Ansible.

```text
┌────────────────────┐   HTTPS poll / results   ┌──────────────────────────┐
│ Helios hub         │ ◀──────────────────────▶ │ Bastion                  │
│ API + UI           │                          │ helios-runner + Ansible  │
└────────────────────┘                          └────────────┬─────────────┘
                                                             │ ssh 127.0.0.1:22000
                                                             ▼
                                                reverse SSH tunnel endpoint
                                                             │
                                              opened outbound by managed host
                                                             │
                                                             ▼
                                                ┌──────────────────────────┐
                                                │ Managed host             │
                                                │ sshd :22 · python3       │
                                                │ optional tunnel agent    │
                                                └──────────────────────────┘
```

The UI topology mirrors this: `hub → helios-runner (bastion) → tunnel host →
applications`. Normal agent instances keep the direct `hub → agent` edge.

> **Want a local end-to-end lab?** From the repository root run
> `./hack/demo.sh --runner` (or `make demo-runner`). It starts this topology in
> Docker with a real reverse SSH tunnel, a bastion runner and a `runner-demo`
> app, then verifies a runner-executed check. See
> [Development → Tunnel mode + bastion runner demo](../../../docs/DEVELOPMENT.md#tunnel-mode--bastion-runner-demo).
> The rest of this page is the production-oriented walkthrough.

## Components and prerequisites

| Where | Needs |
|-------|-------|
| Hub | Outbound HTTPS from the runner; `HELIOS_RUNNER_TOKEN`; a gateway string such as `helios-tunnel@bastion.example.com`; a tunnel port range. |
| Bastion | `helios-runner`, `git`, `ansible-core`, `ansible.posix`, `openssh-client`, SSH server; a system account for the runner; an SSH key that can log into managed hosts. |
| Managed host | `sshd` on port 22, `python3`, outbound SSH to the bastion. The tunnel-only agent also needs `curl`, `ca-certificates`, `tar` and `openssh-client`; it does **not** need Ansible. |

There are **two different SSH identities** in this setup:

1. **Managed host → bastion** (reverse tunnel): account `helios-tunnel` on the
   bastion. The managed host's tunnel key is restricted to one loopback port.
2. **Runner → managed host** (Ansible): account configured by
   `HELIOS_RUNNER_SSH_USER` on the managed host (for example `root` or a
   sudo-capable deployment user). The runner's key is authorized there.

Do not reuse the tunnel key as the runner key.

## 1. Configure the hub

With the bare-metal installer:

```sh
sudo ./install-server.sh \
  --binary ./helios-server --ui ./ui \
  --operator-password 'a-long-random-password' \
  --runner-token "$(openssl rand -hex 32)" \
  --tunnel-gateway helios-tunnel@bastion.example.com \
  --tunnel-port-min 22000 --tunnel-port-max 22999
```

Or with Helm:

```sh
helm upgrade --install helios-server helm/helios-server -n helios \
  --set runner.token="$RUNNER_TOKEN" \
  --set runner.tunnelGateway=helios-tunnel@bastion.example.com \
  --set runner.tunnelPortMin=22000 \
  --set runner.tunnelPortMax=22999
```

`HELIOS_RUNNER_TOKEN` is the shared bearer token used by the runner. An empty
token disables `/api/v1/runner/*` with `503`.

## 2. Prepare the bastion runner

Create the runner account and install the binary, unit and env file:

```sh
sudo useradd --system --home /var/lib/helios/runner \
  --shell /usr/sbin/nologin helios-runner
sudo install -m 0755 helios-runner /usr/local/bin/helios-runner
sudo install -m 0644 deploy/helios-runner.service /etc/systemd/system/helios-runner.service
sudo install -d -m 0750 -o helios-runner -g helios-runner /etc/helios
sudo install -m 0600 -o helios-runner -g helios-runner runner_id /etc/helios/runner_id
sudo install -m 0640 -o root -g helios-runner \
  deploy/examples/runner-tunnel/runner.env.example /etc/helios/runner.env
sudo editor /etc/helios/runner.env
```

Set `HELIOS_SERVER`, `HELIOS_RUNNER_TOKEN`, `HELIOS_RUNNER_SSH_USER` (the
account on the managed host) and the key path. Install Ansible on the bastion:

```sh
sudo apt-get update
sudo apt-get install -y git ansible-core openssh-client
sudo ansible-galaxy collection install ansible.posix \
  -p /usr/share/ansible/collections
sudo systemctl daemon-reload
sudo systemctl enable --now helios-runner
```

Authorize the runner public key on every managed host:

```sh
ssh-keygen -y -f /etc/helios/runner_id   # run on the bastion
# append the output to the managed host account's ~/.ssh/authorized_keys
```

## 3. Accept reverse tunnels on the bastion

Create the `helios-tunnel` account and allow remote forwarding. The tunnel key
is only allowed to listen on the exact port allocated by the hub:

```sh
sudo useradd --create-home --shell /bin/bash helios-tunnel
```

```text
# /etc/ssh/sshd_config on the bastion
GatewayPorts no
AllowTcpForwarding remote
```

```text
# helios-tunnel:~/.ssh/authorized_keys
restrict,port-forwarding,permitlisten="127.0.0.1:22000" ssh-ed25519 AAAA... tunnel@web-prod-01
```

Reload SSH after editing the config. Repeat the `authorized_keys` line for each
managed host/port.

## 4. Register the tunnel instance and open the tunnel

In **Instances → Register instance**, choose **Tunnel · reverse SSH**. The hub
allocates a port (for example `22000`) and shows the tunnel command.

Recommended: install the tunnel-only agent on the managed host:

```sh
sudo ./install-agent.sh \
  --server https://hub.example.com \
  --token '<instance-token>' \
  --mode tunnel
```

The installer creates `/etc/helios/tunnel_id`, prints its public key, writes
`/etc/helios/agent.yaml` with `mode: tunnel`, and starts
`helios-agent.service`. Add the printed key to the bastion
`helios-tunnel` `authorized_keys` with the `permitlisten` restriction above.

Manual fallback:

```sh
ssh -N -T \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=30 \
  -o ServerAliveCountMax=3 \
  -R 127.0.0.1:22000:127.0.0.1:22 \
  helios-tunnel@bastion.example.com
```

## 5. Verify the runner path

```sh
sudo systemctl status helios-runner
sudo journalctl -u helios-runner -f
```

On the bastion, the loopback port should accept SSH through the tunnel:

```sh
sudo -u helios-runner ssh -p 22000 \
  -i /etc/helios/runner_id \
  -o StrictHostKeyChecking=accept-new \
  root@127.0.0.1 'python3 --version'
```

Check discovery and health from the hub:

```sh
curl -fsS -H "Authorization: Bearer $RUNNER_TOKEN" \
  https://hub.example.com/api/v1/runner/instances | jq
```

The instance becomes online after the runner reaches the loopback port. Checks,
applies, drift state and commands are then identical to agent mode. In the
topology, the tunnel host appears behind a **helios-runner** node; the instance
detail page also shows the gateway, port and last tunnel activity.

## Kubernetes variant (hostNetwork)

Use this only when the runner must run inside the cluster **on the bastion
node**. The reverse tunnel terminates on the bastion's loopback interface, so
the pod must use `hostNetwork: true` and be pinned to that node. A normal pod
network namespace would make `127.0.0.1:<port>` point at the pod, not the
bastion.

Build a runner image (the agent image does not include `helios-runner`):

```sh
docker build -f deploy/examples/runner-tunnel/Dockerfile \
  -t registry.example.com/helios/runner:latest .
docker push registry.example.com/helios/runner:latest
```

Create the token and runner-key Secret, then adjust `runner-k8s.yaml`:

```sh
kubectl -n helios create secret generic helios-runner \
  --from-literal=runner-token="$RUNNER_TOKEN" \
  --from-file=runner_id=/etc/helios/runner_id
kubectl apply -f deploy/examples/runner-tunnel/runner-k8s.yaml
```

The manifest pins `kubernetes.io/hostname: bastion-1`; replace it with the node
that accepts the reverse tunnels. The runner still executes Ansible; the
managed host still only needs `sshd` and `python3`.

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Runner logs `tunnel instance discovery failed` | Hub URL, `HELIOS_RUNNER_TOKEN`, hub `/api/v1/runner/instances`. |
| Instance stays offline | The reverse tunnel is not listening on the bastion loopback port; check `ss -ltnp | grep 22000`, the managed host's SSH client logs, and `permitlisten`. |
| Runner cannot SSH to `127.0.0.1:<port>` | `HELIOS_RUNNER_SSH_USER` is the managed-host account, and the runner key is in that account's `authorized_keys`. |
| Ansible works manually but not from the runner | The runner account needs `git`, `ansible-core`, `ansible.posix` and a writable `HELIOS_RUNNER_STATE_DIR`. |
| Topology shows no runner node | No instance has `execution_mode=tunnel`, or the tunnel instance has no `tunnel_gateway`; set `HELIOS_TUNNEL_GATEWAY` on the hub. |

## Security notes

- Keep the runner token secret. It can enumerate and act on every tunnel
  instance exposed by the hub.
- Use one tunnel key per managed host with `restrict,port-forwarding` and a
  single `permitlisten` port.
- Keep the runner's managed-host key separate from the tunnel key.
- Serve the hub over HTTPS.
