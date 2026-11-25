# Inventory and SSH keys

> **Status: implemented.** This describes the runner (push) mode. The runner
> reads a standard Ansible inventory either from the application repository or
> from an inventory managed in Helios, then connects over SSH; the tunnel
> behavior has been removed.

This page covers the **runner (push)** path: how a runner runs a playbook
against managed hosts using a standard Ansible inventory and SSH, without an
agent on those hosts.

## Inventory sources

An application chooses where its inventory comes from:

| `inventory_source` | Meaning |
|--------------------|---------|
| `repo` (default) | Read `inventory_path` from the application repository at run time. |
| `hub` | Use the inventory edited in Helios (stored in the hub database). |

The source is selected in the application form. The rest of this page applies to
both sources; differences are called out explicitly.

### Repository inventory

Like the playbook, the inventory is versioned in the application's Git
repository. This keeps the standard Ansible workflow: the repo contains the
playbook, `group_vars`, `host_vars` and the inventory.

| Field | Meaning |
|-------|---------|
| `inventory_path` | Path (file or directory) inside the repo, relative to the repo root. Required when `inventory_source=repo`. |

`inventory_path` may point at a single file (`inventory/hosts.ini`,
`inventory/hosts.yml`) or a directory of inventory sources, as Ansible supports.

### Helios-managed inventory

With `inventory_source=hub`, operators edit groups, hosts and variables directly
in the application page. Helios stores the inventory as a JSON document on the
application row (`inventory_data`), so it can change without a Git commit.
This is the "dynamic" path: the hub is the source of truth and the runner renders
the inventory at run time.

The editor supports:

- groups, nested groups (`children`), and group variables;
- hosts with arbitrary Ansible connection variables (`ansible_host`,
  `ansible_user`, `ansible_port`, `ansible_ssh_common_args`, ...);
- adding/removing groups and hosts inline, with validation before saving.

The runner receives the document at checkin, renders it to a private INI file
(`0600`), passes `-i <file>` to Ansible, and removes the file after the run.
Only SSH instances receive the document; pull agents never see hub inventory
secrets.

The API is `GET`/`PUT /api/v1/applications/{appId}/inventory`. `PUT` accepts
`{"groups":[...]}` in the same canonical shape returned by `GET`. Values shown as
`••••••` are restored from the stored inventory on save, so editing a preview
does not overwrite a secret with the placeholder.

## Previewing the inventory in the UI

The application page has an **Inventory** panel. For `repo`, the hub reads the
static inventory from its repo snapshot cache and lists groups, children and
hosts. For `hub`, the same panel is an editor. Values for
password/secret/token variables are redacted in both modes.

The panel links each host to the instance that uses it as `inventory_host`, and
offers **Register instance** for hosts that have none. That action opens the SSH
instance form prefilled with the host name, so operators pick from the real
inventory instead of typing it. Warnings explain an empty `inventory_path`, a
disabled snapshot cache, unreadable files or parse errors.

## Instances map to inventory hosts

An instance in runner/SSH mode does not carry connection details. It carries the
name it has **in the inventory**:

| Field | Meaning |
|-------|---------|
| `execution_mode` | `ssh` |
| `inventory_host` | The host name used for `--limit` |
| `ssh_key_id` | Reference to a hub-managed SSH key |

Everything else about the connection — `ansible_host`, `ansible_user`,
`ansible_port`, `ansible_ssh_common_args` / `ProxyJump` — belongs to the
inventory. The hub owns only the **private key**.

```ini
# inventory/hosts.ini
[web]
web-1 ansible_host=10.0.0.11 ansible_user=deploy
web-2 ansible_host=10.0.0.12 ansible_user=deploy

[web:vars]
ansible_ssh_common_args=-o ProxyJump=bastion.example.com
```

The operator registers an instance with `inventory_host = web-1`, points the
application at `inventory/hosts.ini`, and assigns one to the other.

## Invocation

For each assignment the runner executes:

```sh
ansible-playbook \
  -i <repo>/<inventory_path or rendered hub inventory> \
  --limit <inventory_host> \
  --connection ssh \
  --private-key <fetched-key> \
  --check --diff \
  -e '<merged vars>' \
  <playbook>
```

`--limit` is the key decision: Helios runs **one assignment at a time**, so each
instance keeps its own drift state, run history, autoheal and safety checks.
Helios never runs "the whole inventory" in a single run. Group and `hosts: all`
plays still work; `--limit` intersects them with the target host.

## SSH key injection

Keys are managed by the hub and encrypted at rest (`secretbox`,
`HELIOS_SECRET_KEY`):

1. The runner leases an assignment.
2. It calls the runner-authenticated key endpoint for that instance only.
3. It writes the key to a `0600` temporary file.
4. It passes `--private-key <file>` to Ansible.
5. It wipes the file after the run.

The operator API never returns key material; only the owning runner can fetch a
key, and only for assignments it holds.

## Validation

- `inventory_host` **must** exist in the inventory. If it does not, the run is
  reported as `failed` with an explicit reason — never a silent `ok`.
- A missing `inventory_path`, an unreadable inventory or a missing `ssh_key_id`
  fail the assignment the same way.
- Validation runs on each sync, so an inventory that drifts away from the
  registered instances is caught on the next check.

## Variables precedence

Helios variables are passed as extra vars (`-e`), which have the **highest**
Ansible precedence. They therefore override inventory `group_vars` /
`host_vars`. Per-host overrides edited in Helios win over the application
defaults, and both win over the repo's inventory vars. This is documented so
teams do not expect the reverse.

## Scope (v1)

- **Two sources**: repository files/directories or a hub-managed inventory
  document edited in Helios.
- External Ansible dynamic inventory plugins/scripts and cloud provider sync are
  not wired yet; they can later feed the hub inventory without changing the
  runner contract.
- One inventory per application; the instance selects its host via
  `inventory_host`.
- The connection is SSH; `local` and `docker` connections are not supported on
  this path (use the pull agent for local execution).

## Example

Application:

```yaml
apiVersion: helios.dev/v1alpha1
kind: Application
metadata:
  name: web
  project: prod
spec:
  repoURL: https://github.com/acme/web-infra.git
  branch: main
  path: ansible/site.yml
  inventoryPath: inventory/hosts.ini
```

Instance: `execution_mode: ssh`, `inventory_host: web-1`,
`ssh_key_id: prod-deploy-key`.

See [Runners and the SSH push model](RUNNERS.md) for scheduling, leases,
capacity and Kubernetes.
