# Inventory for runner-mode applications

> **Status: implemented (v1).** A runner executes a playbook against a standard
> Ansible inventory, over SSH, with no agent on the managed hosts. The old
> SSH-instance model (one instance per inventory host, leases, per-instance
> `--limit`) is removed.

This page covers the **runner (push)** path: how the runner obtains an inventory
and how the hub-managed inventory editor works.

## Inventory sources

A runner-mode application chooses where its inventory comes from:

| `inventory_source` | Meaning |
|--------------------|---------|
| `repo` (default) | Read `inventory_path` from the application repository at run time. |
| `hub` | Use the inventory edited in Helios (stored in the hub database). |

### Repository inventory

The inventory is versioned in the application's Git repository together with
the playbook, `group_vars` and `host_vars`.

| Field | Meaning |
|-------|---------|
| `inventory_path` | Path (file or directory) inside the repo, relative to the repo root. Required when `inventory_source=repo`. |

`inventory_path` may point at a single file (`inventory/hosts.ini`,
`inventory/hosts.yml`) or a directory of inventory sources, as Ansible supports.

### Helios-managed inventory

With `inventory_source=hub`, operators edit groups, hosts and variables directly
in the application page. Helios stores the inventory as a JSON document on the
application row (`inventory_data`), so it can change without a Git commit.

The editor supports:

- groups, nested groups (`children`), and group variables;
- hosts with arbitrary Ansible connection variables (`ansible_host`,
  `ansible_user`, `ansible_port`, `ansible_ssh_common_args`, ...);
- adding/removing groups and hosts inline, with validation before saving.

At claim time the hub sends the document to the runner. The runner renders it to
a private temporary INI file (`0600`), passes `-i <file>` to Ansible, and removes
the file after the run. Pull agents never see hub-managed inventory data.

The API is `GET`/`PUT /api/v1/applications/{appId}/inventory`. `PUT` accepts
`{"groups":[...]}` in the same canonical shape returned by `GET`. Values shown as
`••••••` are restored from the stored inventory on save, so editing a preview
does not overwrite a secret with the placeholder.

## Previewing the inventory in the UI

The application page has an **Inventory** panel. For `repo`, the hub reads the
static inventory from its repo snapshot cache and lists groups, children and
hosts. For `hub`, the same panel is an editor. Values for
password/secret/token variables are redacted in both modes.

Warnings explain an empty `inventory_path`, a disabled snapshot cache, unreadable
files or parse errors.

## Invocation

The runner runs the playbook against the inventory as a whole:

```sh
ansible-playbook \
  -i <repo>/<inventory_path or rendered hub inventory> \
  --connection ssh \
  --private-key <runner SSH key> \
  --check --diff \
  -e '<merged vars>' \
  <playbook>
```

There is no per-instance `--limit`: a runner job targets the application's
inventory, and one run produces one `runs` row with `runner_id` set. Use
inventory groups, `hosts:` patterns or `--limit`-style vars inside the playbook
when a job should touch a subset.

## SSH credentials

SSH credentials belong to the **inventory**, not to a hub-side instance:

- set `ansible_user` / `ansible_ssh_private_key_file` (or
  `ansible_ssh_common_args`) in the inventory, `group_vars` or `host_vars`;
- or mount a key on the runner and pass it with `--ssh-key`.

The hub does not store or inject per-instance private keys for runner jobs. Hub
inventories may still contain passwords or key paths as Ansible variables; they
are sent only to runners, never to pull agents. Treat a runner as trusted
infrastructure because it receives application variables and inventory data.

```ini
# inventory/hosts.ini
[web]
web-1 ansible_host=10.0.0.11 ansible_user=deploy
web-2 ansible_host=10.0.0.12 ansible_user=deploy

[web:vars]
ansible_ssh_common_args=-o ProxyJump=bastion.example.com
```

## Validation

- A missing or unreadable `inventory_path` fails the job with an explicit error.
- A hub inventory with no groups or hosts is allowed to save, but the run will
  fail if Ansible cannot match any host.
- Ansible parse errors are returned in the job result and stored on the run.

## Variables precedence

Helios variables are passed as extra vars (`-e`), which have the **highest**
Ansible precedence. They therefore override inventory `group_vars` /
`host_vars`. Application variables win over the repo's inventory vars. This is
documented so teams do not expect the reverse.

## Scope (v1)

- **Two sources**: repository files/directories or a hub-managed inventory
  document edited in Helios.
- External Ansible dynamic inventory plugins/scripts and cloud provider sync are
  not wired yet; they can later feed the hub inventory without changing the
  runner contract.
- One inventory per application.
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
  executionMode: runner
  runnerTags: [eu, web]
  inventoryPath: inventory/hosts.ini
```

See [Runners and tagged job execution](RUNNERS.md) for scheduling, tags, capacity
and security.
