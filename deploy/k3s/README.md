# Deploying Helios on k3s

The full k3s walkthrough now lives in the documentation site:
[docs/K3S.md](../../docs/K3S.md).

The manifests in this directory are the source used by that guide.

Outbound-only hosts and the bastion runner are covered in
[`deploy/examples/runner-tunnel/`](../examples/runner-tunnel/README.md): runner
env, `sshd`/`authorized_keys` snippets, a systemd install and a `hostNetwork`
Kubernetes manifest. The runner is the only Ansible executor for tunnel hosts;
the managed host runs only `sshd`, `python3` and optionally the tunnel agent.
