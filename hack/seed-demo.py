#!/usr/bin/env python3
"""Seed a Helios hub with a demo dataset covering the supported scenarios.

The script talks to a running hub over its operator/agent/runner APIs. It also
creates local Git repositories (application repo + GitOps config repo) under
--workdir, so it works without any external service.

Usage:
  python3 hack/seed-demo.py --url http://localhost:8080 \
      --user admin --password helios-demo-2026 \
      --runner-token demo-runner-token

Run it against a fresh database: names are unique and the script does not
delete existing resources.
"""

import argparse
import base64
import datetime as dt
import json
import os
import pathlib
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request

UTC = dt.timezone.utc


def now():
    return dt.datetime.now(UTC)


def iso(value):
    return value.astimezone(UTC).isoformat().replace("+00:00", "Z")


def minutes_ago(value):
    return iso(now() - dt.timedelta(minutes=value))


class APIError(RuntimeError):
    pass


class Hub:
    def __init__(self, url, user, password, runner_token):
        self.url = url.rstrip("/")
        self.user = user
        self.password = password
        self.runner_token = runner_token
        self.basic = base64.b64encode(f"{user}:{password}".encode()).decode()

    def request(self, method, path, body=None, token=None, runner_id=None,
                expect=(200, 201, 204), basic=True):
        data = None
        headers = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if basic:
            headers["Authorization"] = "Basic " + self.basic
        elif token:
            headers["Authorization"] = "Bearer " + token
        if runner_id:
            headers["X-Helios-Runner-ID"] = runner_id
        req = urllib.request.Request(self.url + path, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                payload = resp.read()
                status = resp.status
        except urllib.error.HTTPError as exc:
            payload = exc.read()
            status = exc.code
        if status not in expect:
            raise APIError(f"{method} {path}: HTTP {status}: {payload.decode(errors='replace')}")
        if not payload:
            return None
        try:
            return json.loads(payload)
        except json.JSONDecodeError:
            return payload.decode()

    def op(self, method, path, body=None, expect=(200, 201, 204)):
        return self.request(method, path, body, expect=expect)

    def agent(self, token, method, path, body=None, expect=(200, 201, 204)):
        return self.request(method, path, body, token=token, basic=False, expect=expect)

    def runner(self, method, path, body=None, runner_id=None, expect=(200, 201, 204)):
        return self.request(method, path, body, token=self.runner_token, basic=False,
                            runner_id=runner_id, expect=expect)


def git(repo, *args):
    env = dict(os.environ)
    env.update({
        "GIT_AUTHOR_NAME": "Helios Demo",
        "GIT_AUTHOR_EMAIL": "demo@helios.local",
        "GIT_COMMITTER_NAME": "Helios Demo",
        "GIT_COMMITTER_EMAIL": "demo@helios.local",
    })
    subprocess.run(["git", "-C", str(repo), *args], check=True, env=env,
                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)


def write(path, content):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)


def commit(repo, message):
    git(repo, "add", "-A")
    git(repo, "commit", "-m", message)


def init_repo(path):
    if path.exists():
        shutil.rmtree(path)
    path.mkdir(parents=True)
    git(path, "init", "-b", "main")
    return path


def build_app_repo(workdir):
    repo = init_repo(workdir / "app-repo")
    write(repo / "ansible" / "site.yml", """\
- name: Demo web stack
  hosts: all
  become: true
  tasks:
    - name: Ensure the demo directory exists
      ansible.builtin.file:
        path: /tmp/helios-demo
        state: directory
        mode: "0755"
    - name: Render the demo motd
      ansible.builtin.copy:
        dest: /etc/motd
        content: "Managed by Helios\\n"
""")
    write(repo / "ansible" / "api.yml", """\
- name: Demo API stack
  hosts: all
  become: true
  tasks:
    - name: Ensure the API config directory exists
      ansible.builtin.file:
        path: /etc/helios-demo-api
        state: directory
        mode: "0750"
""")
    write(repo / "ansible" / "legacy.yml", """\
- name: Legacy service
  hosts: legacy
  tasks:
    - name: Legacy marker
      ansible.builtin.debug:
        msg: "legacy host {{ inventory_hostname }}"
""")
    write(repo / "inventory" / "hosts.ini", """\
[web]
web-1 ansible_host=10.10.0.11 ansible_user=deploy
web-2 ansible_host=10.10.0.12 ansible_user=deploy ansible_port=2222

[db]
db-1 ansible_host=10.10.0.21 ansible_user=dba

[all:children]
web
db

[web:vars]
ansible_password=hunter2

[all:vars]
ansible_python_interpreter=/usr/bin/python3
ansible_ssh_common_args=-o ProxyJump=bastion.example.com
""")
    write(repo / "inventory" / "hosts.yml", """\
all:
  children:
    api:
      hosts:
        api-1:
          ansible_host: 10.20.0.11
          ansible_user: deploy
        api-2:
          ansible_host: 10.20.0.12
          ansible_user: deploy
      vars:
        ansible_port: 22
""")
    write(repo / "inventory" / "dir" / "00-base.ini", """\
[legacy]
legacy-2 ansible_host=10.30.0.12 ansible_user=legacy
""")
    write(repo / "inventory" / "dir" / "10-extra.ini", """\
[legacy:vars]
ansible_python_interpreter=/usr/bin/python3
""")
    write(repo / "group_vars" / "all.yml", "env: demo\n")
    write(repo / "host_vars" / "web-1.yml", "http_port: 8080\n")
    write(repo / "README.md", "# Demo application repository\n\nSeeded by hack/seed-demo.py.\n")
    commit(repo, "Initial demo application")
    first = subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"]).decode().strip()
    write(repo / "README.md", "# Demo application repository\n\nSeeded by hack/seed-demo.py. Second commit for history.\n")
    commit(repo, "Document the demo")
    second = subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"]).decode().strip()
    return repo, first, second


def build_config_repo(workdir, app_repo, instance_id):
    repo = init_repo(workdir / "config-repo")
    write(repo / "project.yaml", """\
apiVersion: helios.dev/v1alpha1
kind: Project
metadata:
  name: gitops
spec:
  repoPatterns: "*"
""")
    write(repo / "repository.yaml", f"""\
apiVersion: helios.dev/v1alpha1
kind: Repository
metadata:
  name: gitops-infra
  project: gitops
spec:
  url: {app_repo.as_uri()}
""")
    write(repo / "application.yaml", """\
apiVersion: helios.dev/v1alpha1
kind: Application
metadata:
  name: gitops-web
  project: gitops
spec:
  repoRef: gitops-infra
  branch: main
  path: ansible/site.yml
  inventoryPath: inventory/hosts.ini
  autoHeal: true
  syncInterval: 60
  vars:
    env: gitops
""")
    write(repo / "assignment.yaml", f"""\
apiVersion: helios.dev/v1alpha1
kind: Assignment
metadata:
  name: gitops-web-on-gitops-agent
  project: gitops
spec:
  application: gitops-web
  instance: {instance_id}
  autoHeal: true
""")
    commit(repo, "Seed GitOps demo")
    return repo


def ensure_project(hub, name):
    try:
        return hub.op("POST", "/api/v1/projects", {"name": name, "repo_patterns": "*"})
    except APIError as exc:
        if "HTTP 409" not in str(exc):
            raise
        for project in hub.op("GET", "/api/v1/projects"):
            if project["name"] == name:
                return project
        raise


def ensure_region(hub, name, description):
    try:
        return hub.op("POST", "/api/v1/regions", {"name": name, "description": description})
    except APIError as exc:
        if "HTTP 409" not in str(exc):
            raise
        for region in hub.op("GET", "/api/v1/regions"):
            if region["name"] == name:
                return region
        raise


def ensure_repository(hub, name, url, project):
    try:
        return hub.op("POST", "/api/v1/repositories", {
            "name": name, "url": url, "project": project,
        })
    except APIError as exc:
        if "HTTP 409" not in str(exc):
            raise
        for repo in hub.op("GET", "/api/v1/repositories"):
            if repo["name"] == name and repo.get("project") == project:
                return repo
        raise


def create_instance(hub, name, project, region, tags):
    out = hub.op("POST", "/api/v1/instances", {
        "name": name,
        "project": project,
        "region": region,
        "tags": tags,
    })
    out["instance"]["token"] = out["token"]
    return out["instance"]


def create_app(hub, body):
    return hub.op("POST", "/api/v1/applications", body)


def assign(hub, instance, app):
    hub.op("POST", f"/api/v1/instances/{instance['id']}/assign", {"app_id": app["id"]})


def set_assignment_autoheal(hub, instance, app, enabled):
    hub.op("PUT", f"/api/v1/instances/{instance['id']}/assign/{app['id']}/autoheal",
           {"enabled": enabled})


def set_assignment_revision(hub, instance, app, revision):
    hub.op("PUT", f"/api/v1/instances/{instance['id']}/assign/{app['id']}/revision",
           {"revision": revision})


def set_app_vars(hub, app, items):
    hub.op("PUT", f"/api/v1/applications/{app['id']}/variables", {"items": items})


def set_assignment_vars(hub, instance, app, items):
    hub.op("PUT", f"/api/v1/instances/{instance['id']}/assign/{app['id']}/variables",
           {"items": items})


def checkin(hub, instance):
    return hub.agent(instance["token"], "POST", "/api/v1/agent/checkin", {
        "version": "demo-seed",
        "hostname": instance["name"],
        "platform": "linux",
        "poll_interval": 30,
    })


def status(hub, instance, app, mode, state, trigger="seed", started=None, finished=None,
           summary=None, changed=None, output="", diff="", resources=None):
    body = {
        "app_id": app["id"],
        "mode": mode,
        "status": state,
        "trigger": trigger,
        "commit_before": app.get("_first_commit", ""),
        "commit_after": app.get("_second_commit", ""),
        "commit_msg": "demo commit",
        "summary": summary or {"ok": 1, "changed": 0, "failed": 0},
        "changed_tasks": changed or [],
        "output_tail": output,
        "output": output,
        "output_text": output,
        "diff": diff,
        "resources": resources or [],
        "started_at": started or minutes_ago(5),
        "finished_at": finished or minutes_ago(4),
    }
    hub.agent(instance["token"], "POST", "/api/v1/agent/status", body)


def queue(hub, instance, app, action):
    return hub.op("POST", f"/api/v1/instances/{instance['id']}/{action}", {"app_id": app["id"]})


def complete_command(hub, instance, cmd, state="done", result="ok", commit=""):
    hub.agent(instance["token"], "POST", f"/api/v1/agent/commands/{cmd['id']}/result",
              {"status": state, "result": result, "commit": commit})


def heartbeat(hub, runner_id, tags, version="demo-seed"):
    hub.runner("POST", "/api/v1/runner/heartbeat",
               {"runner_id": runner_id, "tags": tags, "version": version})


def runner_job(hub, app, mode, status=None, runner_id=None, tags=None, *, started=None,
               finished=None, summary=None, changed=None, output="", diff=""):
    """Queue a job, claim it with a matching runner and optionally report it.

    status=None leaves the job queued. status="running" claims it without
    reporting. Any other status reports the result.
    """
    job = hub.op("POST", f"/api/v1/applications/{app['id']}/run", {"mode": mode},
                 expect=(200, 201, 202))
    if status is None:
        return job
    claimed = hub.runner("POST", "/api/v1/runner/jobs/claim",
                         {"runner_id": runner_id, "tags": tags}, expect=(200, 204))
    if not claimed:
        raise APIError(f"no runner claimed {app['name']} job {job['id']}")
    job = claimed["job"]
    if status == "running":
        return job
    body = {
        "runner_id": runner_id,
        "status": status,
        "error": "" if status != "failed" else (output or "playbook failed"),
        "commit_before": app.get("_first_commit", ""),
        "commit_after": app.get("_second_commit", ""),
        "commit_msg": "demo commit",
        "summary": summary or {"ok": 1, "changed": 0, "failed": 0},
        "changed_tasks": changed or [],
        "output_tail": output,
        "output": output,
        "output_text": output,
        "diff": diff,
        "resources": [],
        "started_at": started or minutes_ago(5),
        "finished_at": finished or minutes_ago(4),
    }
    return hub.runner("POST", f"/api/v1/runner/jobs/{job['id']}/result", body)


def main():
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--url", default=os.environ.get("HELIOS_URL", "http://localhost:8080"))
    parser.add_argument("--user", default=os.environ.get("HELIOS_OPERATOR_USER", "admin"))
    parser.add_argument("--password", default=os.environ.get("HELIOS_OPERATOR_PASSWORD", "helios-demo-2026"))
    parser.add_argument("--runner-token", default=os.environ.get("HELIOS_RUNNER_TOKEN", "demo-runner-token"))
    parser.add_argument("--workdir", default="/tmp/helios-seed")
    args = parser.parse_args()

    workdir = pathlib.Path(args.workdir).resolve()
    workdir.mkdir(parents=True, exist_ok=True)
    hub = Hub(args.url, args.user, args.password, args.runner_token)

    def say(message):
        print(f"\n==> {message}")

    say("Checking the hub")
    hub.op("GET", "/api/v1/health")

    say("Building local Git repositories")
    app_repo, first_commit, second_commit = build_app_repo(workdir)
    app_url = app_repo.as_uri()
    print(f"    app repo: {app_url}")
    print(f"    commits:  {first_commit[:7]} -> {second_commit[:7]}")

    say("Creating projects and regions")
    for name in ("platform", "payments", "sandbox", "gitops"):
        ensure_project(hub, name)
    for name, description in (
        ("eu-west-1", "Paris"),
        ("us-east-1", "Virginia"),
        ("dc-paris", "On-premises Paris"),
    ):
        ensure_region(hub, name, description)

    say("Registering repositories")
    ensure_repository(hub, "platform-infra", app_url, "platform")
    ensure_repository(hub, "payments-infra", app_url, "payments")
    ensure_repository(hub, "sandbox-infra", app_url, "sandbox")

    say("Creating agent instances")
    agent_eu_1 = create_instance(hub, "agent-eu-01", "platform", "eu-west-1", ["prod", "web", "linux"])
    agent_eu_2 = create_instance(hub, "agent-eu-02", "platform", "eu-west-1", ["prod", "web", "offline"])
    agent_us_1 = create_instance(hub, "agent-us-01", "payments", "us-east-1", ["prod", "api", "linux"])
    agent_sandbox = create_instance(hub, "agent-sandbox-01", "sandbox", "dc-paris", ["dev", "legacy"])
    gitops_agent = create_instance(hub, "gitops-agent-01", "gitops", "dc-paris", ["gitops", "agent"])

    # One host explicitly opts out of autoheal at the host level.
    hub.op("PUT", f"/api/v1/instances/{agent_sandbox['id']}/autoheal", {"enabled": False})

    say("Creating applications")
    web = create_app(hub, {
        "name": "web", "project": "platform", "repo_url": app_url,
        "branch": "main", "path": "ansible/site.yml",
        "execution_mode": "runner", "runner_tags": ["eu", "web"],
        "inventory_source": "repo", "inventory_path": "inventory/hosts.ini",
        "autoheal": False, "sync_interval": 0,
        "ignore_paths": "copy /etc/motd\nwrite /etc/nginx/*",
        "vars": json.dumps({"http_port": 80}),
    })
    web["_first_commit"] = first_commit
    web["_second_commit"] = second_commit

    api = create_app(hub, {
        "name": "api", "project": "payments", "repo_url": app_url,
        "branch": "main", "path": "ansible/api.yml",
        "execution_mode": "agent",
        "autoheal": False, "sync_interval": 60,
        "vars": json.dumps({"env": "production"}),
    })
    api["_first_commit"] = first_commit
    api["_second_commit"] = second_commit

    flaky = create_app(hub, {
        "name": "flaky", "project": "payments", "repo_url": app_url,
        "branch": "main", "path": "ansible/api.yml",
        "execution_mode": "agent",
        "autoheal": True, "sync_interval": 10,
        "vars": "{}",
    })
    flaky["_first_commit"] = first_commit
    flaky["_second_commit"] = second_commit

    legacy = create_app(hub, {
        "name": "legacy", "project": "sandbox", "repo_url": app_url,
        "branch": "main", "path": "ansible/legacy.yml",
        "execution_mode": "runner", "runner_tags": ["sandbox", "legacy"],
        "inventory_source": "repo", "inventory_path": "inventory/dir",
        "autoheal": False, "sync_interval": 0,
        "vars": "{}",
    })
    legacy["_first_commit"] = first_commit
    legacy["_second_commit"] = second_commit

    agent_only = create_app(hub, {
        "name": "agent-only", "project": "platform", "repo_url": app_url,
        "branch": "main", "path": "ansible/site.yml",
        "execution_mode": "agent",
        "autoheal": False, "sync_interval": 0,
        "ignore_paths": "templates/*.j2",
        "vars": "{}",
    })
    agent_only["_first_commit"] = first_commit
    agent_only["_second_commit"] = second_commit

    hub_inventory = {
        "groups": [
            {"name": "all", "children": ["db"]},
            {"name": "db", "vars": {"ansible_user": "dba", "ansible_password": "db-secret"},
             "hosts": [
                 {"name": "db-1", "vars": {"ansible_host": "10.10.0.21"}},
                 {"name": "db-2", "vars": {"ansible_host": "10.10.0.22"}},
             ]},
        ]
    }
    db = create_app(hub, {
        "name": "db", "project": "platform", "repo_url": app_url,
        "branch": "main", "path": "ansible/site.yml",
        "execution_mode": "runner", "runner_tags": ["eu", "db"],
        "inventory_source": "hub", "inventory_path": "",
        "inventory_data": json.dumps(hub_inventory),
        "autoheal": False, "sync_interval": 0, "vars": "{}",
    })
    db["_first_commit"] = first_commit
    db["_second_commit"] = second_commit

    empty_hub = create_app(hub, {
        "name": "empty-hub", "project": "platform", "repo_url": app_url,
        "branch": "main", "path": "ansible/site.yml",
        "execution_mode": "runner", "runner_tags": ["eu"],
        "inventory_source": "hub", "inventory_path": "",
        "inventory_data": json.dumps({"groups": []}),
        "autoheal": False, "sync_interval": 0, "vars": "{}",
    })
    empty_hub["_first_commit"] = first_commit
    empty_hub["_second_commit"] = second_commit

    say("Adding variables (including write-only secrets)")
    set_app_vars(hub, web, [
        {"key": "http_port", "value": "80", "is_secret": False},
        {"key": "admin_email", "value": "ops@example.com", "is_secret": False},
    ])
    set_app_vars(hub, api, [
        {"key": "env", "value": "production", "is_secret": False},
        {"key": "db_password", "value": "super-secret", "is_secret": True},
    ])
    set_assignment_vars(hub, agent_us_1, api, [
        {"key": "replicas", "value": "3", "is_secret": False},
        {"key": "feature_flag", "value": "true", "is_secret": False},
    ])

    say("Creating assignments and overrides")
    assign(hub, agent_us_1, api)
    assign(hub, agent_us_1, flaky)
    for instance in (agent_eu_1, agent_eu_2):
        assign(hub, instance, agent_only)

    # The api host is pinned to the first commit (rollback demo).
    set_assignment_revision(hub, agent_us_1, api, first_commit)

    say("Creating a GitOps config repository")
    config_repo_dir = build_config_repo(workdir, app_repo, gitops_agent["id"])
    config_repo = hub.op("POST", "/api/v1/config-repos", {
        "name": "demo-gitops",
        "url": config_repo_dir.as_uri(),
        "branch": "main",
        "path": "",
        "poll_interval": 300,
        "enabled": False,
        "prune": True,
    })
    hub.op("POST", f"/api/v1/config-repos/{config_repo['id']}/sync")
    hub.op("PUT", f"/api/v1/config-repos/{config_repo['id']}", {"enabled": True})
    gitops_app = next((a for a in hub.op("GET", "/api/v1/applications")
                       if a["name"] == "gitops-web"), None)
    if not gitops_app:
        raise APIError("GitOps sync did not create gitops-web")
    gitops_app["_first_commit"] = first_commit
    gitops_app["_second_commit"] = second_commit

    say("Simulating agents, runners, runs and drift")
    heartbeat(hub, "runner-eu-1", ["eu", "web", "db", "linux"])
    heartbeat(hub, "runner-us-1", ["us", "api", "linux"])
    heartbeat(hub, "runner-onprem-1", ["sandbox", "legacy"])

    checkin(hub, agent_eu_1)
    checkin(hub, agent_us_1)
    checkin(hub, agent_sandbox)
    checkin(hub, gitops_agent)
    # agent-eu-02 stays offline on purpose.

    # Healthy agent-only app.
    status(hub, agent_eu_1, agent_only, "check", "ok",
           started=minutes_ago(30), finished=minutes_ago(29),
           output="PLAY RECAP: ok=2 changed=0 failed=0")

    # API failure on the agent.
    status(hub, agent_us_1, api, "check", "failed",
           started=minutes_ago(20), finished=minutes_ago(19),
           summary={"ok": 1, "changed": 0, "failed": 1},
           output="fatal: [api-1]: FAILED! => {\"msg\": \"database connection refused\"}")

    # Flaky app: three failed after-apply checks mark the assignment stuck.
    status(hub, agent_us_1, flaky, "apply", "ok", trigger="autoheal",
           started=minutes_ago(15), finished=minutes_ago(14),
           summary={"ok": 3, "changed": 1, "failed": 0},
           output="PLAY RECAP: ok=3 changed=1 failed=0")
    for index in range(3):
        status(hub, agent_us_1, flaky, "check", "drift", trigger="after_apply",
               started=minutes_ago(13 - index), finished=minutes_ago(12 - index),
               summary={"ok": 2, "changed": 1, "failed": 0},
               changed=[{"task": "Restart demo service", "host": "api-1"}],
               output="PLAY RECAP: ok=2 changed=1 failed=0 (still drifting)")

    # Runner fleet: one drift, one success, one in flight and one queued.
    runner_job(hub, web, "check", "drift", "runner-eu-1", ["eu", "web"],
               started=minutes_ago(12), finished=minutes_ago(11),
               summary={"ok": 4, "changed": 2, "failed": 0},
               changed=[
                   {"task": "Write motd", "host": "web-1"},
                   {"task": "Reload nginx", "host": "web-1"},
               ],
               output="PLAY RECAP: ok=4 changed=2 failed=0",
               diff="--- before\n+++ after\n@@ motd @@\n-Managed by Helios\n+Managed by Helios (drift)")
    runner_job(hub, web, "check", "ok", "runner-eu-1", ["eu", "web"],
               started=minutes_ago(8), finished=minutes_ago(7),
               output="PLAY RECAP: ok=4 changed=0 failed=0")
    runner_job(hub, web, "apply", "running", "runner-eu-1", ["eu", "web"])
    runner_job(hub, db, "check", "ok", "runner-eu-1", ["eu", "db"],
               started=minutes_ago(6), finished=minutes_ago(5),
               output="PLAY RECAP: ok=3 changed=0 failed=0")
    runner_job(hub, legacy, "check", "failed", "runner-onprem-1", ["sandbox", "legacy"],
               started=minutes_ago(10), finished=minutes_ago(9),
               summary={"ok": 0, "changed": 0, "failed": 1},
               output="[WARNING]: Could not match supplied host pattern, ignoring: legacy-1")
    runner_job(hub, web, "check", status=None)

    status(hub, gitops_agent, gitops_app, "check", "ok",
           started=minutes_ago(4), finished=minutes_ago(3),
           output="PLAY RECAP: ok=2 changed=0 failed=0")

    say("Exercising operator commands")
    cmd = queue(hub, agent_eu_1, agent_only, "apply")
    claimed = checkin(hub, agent_eu_1).get("commands") or []
    if claimed:
        complete_command(hub, agent_eu_1, claimed[0], "done", "apply ok", second_commit)
    status(hub, agent_eu_1, agent_only, "apply", "ok", trigger="command",
           started=minutes_ago(1), finished=minutes_ago(0),
           summary={"ok": 2, "changed": 1, "failed": 0},
           output="PLAY RECAP: ok=2 changed=1 failed=0")

    # A command queued for the offline agent stays pending.
    queue(hub, agent_eu_2, agent_only, "check")

    say("Creating sync windows")
    hub.op("POST", "/api/v1/sync-windows", {
        "name": "payments-freeze",
        "kind": "deny",
        "app_id": api["id"],
        "timezone": "UTC",
        "days": "",
        "start_min": 0,
        "end_min": 0,
        "enabled": True,
    })
    hub.op("POST", "/api/v1/sync-windows", {
        "name": "platform-sunday-maintenance",
        "kind": "deny",
        "app_id": web["id"],
        "timezone": "UTC",
        "days": "sun",
        "start_min": 120,
        "end_min": 240,
        "enabled": True,
    })

    say("Adding an RBAC example")
    hub.op("POST", "/api/v1/rbac/policies", {
        "role": "role:sandbox-viewer",
        "resource": "applications",
        "action": "get",
        "object": "sandbox",
    })
    hub.op("POST", "/api/v1/rbac/bindings", {
        "group": "sandbox-viewers",
        "role": "role:sandbox-viewer",
    })

    say("Verifying the dataset")
    apps = hub.op("GET", "/api/v1/applications")
    instances = hub.op("GET", "/api/v1/instances?limit=500")["items"]
    drift = hub.op("GET", "/api/v1/drift")
    fleet = hub.op("GET", "/api/v1/runners")
    print(f"    projects:    {len(hub.op('GET', '/api/v1/projects'))}")
    print(f"    regions:     {len(hub.op('GET', '/api/v1/regions'))}")
    print(f"    repositories:{len(hub.op('GET', '/api/v1/repositories'))}")
    print(f"    instances:   {len(instances)}")
    print(f"    applications:{len(apps)}")
    print(f"    runners:     {fleet['summary']['runners']} ({fleet['summary']['online']} online)")
    print(f"    runner jobs: {len(fleet['jobs'])} ({fleet['summary']['queued']} queued, {fleet['summary']['running']} running)")
    drift_total = len(drift) if isinstance(drift, list) else drift.get("total", 0)
    print(f"    drift items: {drift_total}")

    print(f"""
Demo dataset ready on {hub.url}

  Applications: web, api, flaky, db, legacy, agent-only, empty-hub, gitops-web
  Instances:    agent-eu-01/02, agent-us-01, agent-sandbox-01, gitops-agent-01
  Runners:      runner-eu-1, runner-us-1, runner-onprem-1
  Scenarios:    agent/runner modes, repo/hub inventories, tagged jobs, GitOps,
                secrets, per-host overrides, pinned revision, drift, failure,
                stuck autoheal, offline host, pending command, sync windows,
                RBAC example, queued/running jobs.

  Open: {hub.url}/applications
  Login: {args.user} / {args.password}
""")


if __name__ == "__main__":
    try:
        main()
    except APIError as exc:
        print(f"\nERROR: {exc}", file=sys.stderr)
        sys.exit(1)
