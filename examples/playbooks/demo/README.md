# AnsibleHub — demo playbook

Demo playbook used by the docker-compose harness. It provisions a small
"webapp" (role-based, **Jinja2 templates**, line-in-file edits, a system user,
per-feature listener configs) under `/tmp/ansiblehub-demo/` so drift is
trivially demonstrable:

```
site.yml                          # entrypoint (2 plays: provision + verify)
roles/webapp/
  tasks/main.yml                  # dirs, user, template, lineinfile, feature confs
  templates/
    webapp.conf.j2               # app.name={{ app_name }}, env, release, ports, features…
    feature.conf.j2              # one listener config per enabled feature
```

`webapp.conf.j2` also renders `release={{ release }}` (a role var bumped on
each git tag, see below) so every release version produces a visibly different
file.

## Revisions: branch, tag and semver range

The application's **Revision** field (called `branch` in the API) supports
ArgoCD-style targets:

| value             | resolves to                              |
|-------------------|------------------------------------------|
| `main`            | branch tip (default)                     |
| `v1.0.0`          | that exact tag                           |
| `a1b2c3d`         | that commit SHA                          |
| `>=1.0.0`         | highest semver tag ≥ 1.0.0               |
| `>=1.0.0 <1.2.0`  | highest semver tag in range (v1.1.0)     |
| `^1.0.0` / `~1.1` | caret / tilde ranges                     |
| `1.x` / `1.2.*`   | any 1.x or 1.2.x release                 |
| `*`               | highest semver tag in the repo           |

The demo repo ships a small history with release tags:

```
v0.9.0 ── v1.0.0 ── v1.1.0 ── v1.2.0  (main)     + a non-semver "latest" tag (ignored)
```

Each tag bumps `release:`, so the rendered file shows which tag is checked out.
Try setting the app revision to `>=1.0.0 <1.2.0`: one poll later the agent
checks out and renders `release=v1.1.0`; widen the range to `>=1.0.0` and it
converges to `release=v1.2.0`. Pre-release tags like `v1.2.0-rc.1` are only
considered if the range mentions a pre-release (`>=1.2.0-rc.1`).

## What is drift, exactly

Drift = what `ansible-playbook --check --diff` reports as `changed`. The
`template:` module renders the Jinja file **in memory**, compares it (content
+ mode) to what is on disk, and flags `changed` only if they differ. So any
templated file that someone edits, that a script overwrites, or whose vars
change is picked up on the next check pass.

## Drift demonstrations

```sh
docker compose exec agent-2 sh
# 1. flip the drift marker
echo "someone changed me" > /tmp/ansiblehub-demo/state.txt
# 2. break a file RENDERED FROM A TEMPLATE (jinja)
sed -i 's/app.name=ansiblehub-demo/app.name=HACKED/' /tmp/ansiblehub-demo/webapp.conf
# 3. (optional) delete a listener conf so the template diff appears
rm /tmp/ansiblehub-demo/conf/metrics.conf
```

Wait one poll interval (the check pass also does a `git fetch`, so budget
10–30s on the dumb-HTTP demo). The dashboard flips to **drift** and lists
`webapp : Ship the main webapp config from a template` (and/or
`webapp : Render one listener config per enabled feature`). With **autoheal**
enabled the agent re-applies and converges back; the Diff modal shows the
per-file **Current / Desired** render before the fix.

## Vars flow through the templates

The **Variables** editor (Applications → Variables) holds per-app key/value
defaults (≈ `group_vars`); the **Vars / Host vars** buttons on a host add
per-assignment overrides (≈ `host_vars`) that win for that host only. The
server merges them (legacy app `vars` < app defaults < host overrides) and
passes the result to `ansible-playbook -e <json>` — extra vars **override**
the role vars declared in `site.yml`, so the Jinja templates re-render from
them. Set e.g. `app_name=param-app`, `app_env=prod` on the app:

`webapp.conf.j2` turns that into `app.name=param-app` /
`app.environment=prod`. One poll later the check sees a **drift**, autoheal
re-renders the file, and a follow-up check confirms `ok`. Revert the vars
(or set `app_env=staging`) and the same cycle converges back. Give one host
an `app_env=blue` override and it converges independently of the others.

> Role vars live in `site.yml`; per-app vars only exist where you declare
> them in the UI. Low-precedence defaults belong in
> `roles/webapp/defaults/main.yml`, app-specific names/ports in the
> application, per-machine values in the host overrides.