# Deploying Helios on k3s

The full k3s walkthrough now lives in the documentation site:
[docs/K3S.md](../../docs/K3S.md).

The manifests in this directory are the source used by that guide.

SSH instances and the runner are covered in the guide's
[SSH runner section](../../docs/K3S.md#6-ssh-runner-optional) and in
[docs/RUNNERS.md](../../docs/RUNNERS.md). The managed host runs only `sshd` and
`python3`; the runner executes Ansible over SSH.
