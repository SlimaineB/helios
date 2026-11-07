# Deploying Helios on k3s

Everything needed to run the full stack — hub server (API + UI), Postgres
backend, dumb-HTTP git repo server and pull-agents — on a k3s cluster with
Cilium LoadBalancers. Written from the reference deployment on a 10-node
miniPC cluster (`192.168.1.x`).

> This is the k3s reference deployment. For the hub/agent installation matrix,
> TLS, backup and the post-install checklist, see
> [INSTALL.md](INSTALL.md). Agent capacity and
> `maxConcurrentApps` tuning are in [CAPACITY.md](CAPACITY.md).

## Architecture

| Component          | docker-compose (lab)      | k3s (this directory)              |
|--------------------|---------------------------|-----------------------------------|
| API + UI           | `server` (`docker.io/helios/server`) | Deployment + LB Service via `helm/helios-server` |
| State              | Postgres 16 (`helios-db`)        | `postgres.yaml` (Deployment + PVC `local-path`) |
| Git-over-HTTP sink | `repo` (`hack/repo-server`)           | `repo.yaml` (Deployment + ClusterIP) |
| Container registry | n/a                       | `registry.yaml` (in-cluster `registry:2`, NodePort) |
| Agents             | `agent-N`                 | `helm/helios-agent` (Deployment or DaemonSet mode) |

Hosts outside the cluster (bare-metal, VMs) re-use the same agent binary/unit
(`deploy/install-agent.sh`) and simply point at the hub's LoadBalancer URL.

## Prerequisites

- k3s cluster, one storage class (default `local-path` is fine)
- Cilium configured as LoadBalancer (L2 announcements / LB-IPAM) — LoadBalancer
  Services must allocate stable LAN IPs
- `helm` v3 on the controller workstation
- Root SSH access to every k3s node (only needed for the one-off containerd
  mirror rollout)
- Docker on the build host, with `192.168.1.21:30500` allowed as an insecure
  registry (`/etc/docker/daemon.json` → `"insecure-registries"`)

## 1. Build and distribute images

Build the three images and tag them for the cluster registry. All examples use
`192.168.1.21:30500` (NodePort of the in-cluster registry, first control-plane)
— adjust to your cluster.

```sh
export DOCKER_HOST=unix:///var/run/docker.sock   # if applicable
REG=192.168.1.21:30500/helios
docker build -t helios/server:latest .
docker build -t helios/agent:latest  -f agent/Dockerfile .
docker build -t helios/repo:latest   -f hack/repo-server/Dockerfile .
docker tag helios/server:latest $REG/server:latest
docker tag helios/agent:latest  $REG/agent:latest
docker tag helios/repo:latest   $REG/repo:latest
```

Deploy the registry and push:

```sh
kubectl apply -f registry.yaml          # creates namespace `helios` too
docker push 192.168.1.21:30500/helios/server:latest
docker push 192.168.1.21:30500/helios/agent:latest
docker push 192.168.1.21:30500/helios/repo:latest
```

### containerd mirror on the nodes

k3s refuses plain-HTTP registries by default, so each node needs a mirror entry:

```sh
deploy/k3s/configure-registry-mirrors.sh 192.168.1.21:30500 \
  root@k3s-cp-01 root@k3s-cp-02 root@k3s-cp-03 \
  root@k3s-w-01 root@k3s-w-02 root@k3s-w-03 root@k3s-w-04 \
  root@k3s-w-06 root@k3s-w-07 root@k3s-w-08
```

The script writes `/etc/rancher/k3s/registries.yaml` (see
`registries.yaml.example`), restarts `k3s` (control-plane) or `k3s-agent`
(workers) and asserts that containerd materialised its `hosts.toml`:

```
/var/lib/rancher/k3s/agent/etc/containerd/certs.d/<registry>/hosts.toml
```

> The restart reloads containerd, so **existing pods on that node restart**.
> Rolling one node at a time keeps the cluster serving; do workers before
> control-planes (a 3-etcd cluster tolerates one CP at a time).

## 2. Backend and repo server

Create the Postgres credential as a Kubernetes Secret (do not commit it), then
apply the backend manifests:

```sh
export DB_PASSWORD="$(openssl rand -base64 32 | tr -d '/+=' | cut -c1-32)"
kubectl -n helios create secret generic helios-db \
  --from-literal=password="$DB_PASSWORD"

kubectl apply -f postgres.yaml     # Postgres 16 + PVC + ClusterIP helios-db:5432
kubectl apply -f repo.yaml         # git-over-HTTP, ClusterIP helios-repo:8000
kubectl -n helios rollout status deploy/helios-db
kubectl -n helios rollout status deploy/helios-repo
```

Sanity checks: `curl http://<repo-pod-ip>:8000/demo.git/HEAD` returns
`ref: refs/heads/main`.

## 3. Hub server (helm)

```sh
helm install helios-server helm/helios-server -n helios \
  -f deploy/k3s/server-values.yaml \
  --set database.dsn="postgres://helios:${DB_PASSWORD}@helios-db:5432/helios?sslmode=disable" \
  --set-string operatorAuth.password=<generated-strong-password>
```

- `service.type: LoadBalancer` → Cilium picks a LAN IP (e.g. `192.168.1.107`).
- `database.dsn` is supplied from the Secret-backed `${DB_PASSWORD}`; the
  server honours `HELIOS_DB` over its `-db` flag.
- Add `--set replicaCount=3` for a stateless multi-replica hub; PostgreSQL is
  already configured. See [High availability](HA.md).
- The operator password lives only in the Secret created by the chart
  (`helios-server-helios-server-operator`); omit `operatorAuth.password` to let
  the chart generate and preserve one, or use `operatorAuth.existingSecret` to
  manage it yourself.

Verify:

```sh
IP=$(kubectl -n helios get svc helios-server-helios-server \
     -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl -u admin:$PW http://$IP/api/v1/health   # {"status":"ok",...}
```

## 4. Bootstrap data (application + instances)

The hub starts with an empty repository registry. Register the demo playbook
served by the repo server, then create two agent instances (tokens are shown
once):

```sh
B=http://$IP
app=$(curl -su admin:$PW -X POST $B/api/v1/applications -H 'Content-Type: application/json' \
  -d '{"name":"demo","repo_url":"http://helios-repo:8000/demo.git","branch":"main","path":"site.yml","autoheal":true}')
app_id=$(echo "$app" | jq -r .id)

for n in 1 2; do
  reg=$(curl -su admin:$PW -X POST $B/api/v1/instances -H 'Content-Type: application/json' \
        -d "{\"name\":\"agent-k8s-$n\"}")
  id=$(echo "$reg" | jq -r .instance.id)
  curl -su admin:$PW -X POST $B/api/v1/instances/$id/assign \
       -H 'Content-Type: application/json' -d "{\"app_id\":$app_id}"
  echo "$reg" | jq -r .token > /tmp/helios-agent-$n-token.txt
done
```

## 5. Agents (helm)

Deployment mode (one agent pod, as tested). The demo playbook creates an OS
user, so these run as root with the default `emptyDir` state.

```sh
helm install agent-k8s-1 helm/helios-agent -n helios \
  --set image.repository=192.168.1.21:30500/helios/agent \
  --set image.tag=latest \
  --set config.server=http://helios-server-helios-server:80 \
  --set config.token=$(cat /tmp/helios-agent-1-token.txt) \
  --set config.instanceId=ins-xxxxxxxx --set config.pollInterval=30s \
  --set config.autoheal=true \
  --set 'securityContext.runAsNonRoot=false' \
  --set 'securityContext.runAsUser=0' --set 'securityContext.runAsGroup=0'
# repeat for agent-k8s-2 (instance ins-yyyyyyyy, its token)
```

DaemonSet mode (`--set mode=daemonset --set statePersistence.type=hostPath
--set statePersistence.hostPath=/var/lib/helios`) is the building block for
reconciling the miniPC hosts themselves; safety requirements (playbooks must
stay idempotent; drive state — see the root README's convergence notes) still
apply.

## 6. Validation

Watch the drift → autoheal → converged loop in the agent/hub logs and via the
API:

```sh
kubectl -n helios logs -l app.kubernetes.io/component=agent --tail=5
curl -su admin:$PW http://$IP/api/v1/instances |
  jq -c '.items[] | {name, state, apps: [.apps[] | {app: .application.name, check: .latest_check.status, apply: .latest_apply.status}]}'
```

Expected first cycle: `check=drift (changed:10)` → `apply=ok` → `check=ok`.
Both instances should report `state: ok`.

## Customisation notes

- **Registry reachability**: in-cluster agents pull through the containerd
  mirror; keep the NodePort/mirror IP stable, or move to a mirrored DNS name
  for portability.
- **TLS / ingress**: the chart ships an optional Ingress (Traefik is the k3s
  default controller here). If you front the hub publicly, set the DNS host
  and `HELIOS_BASE_URL` (via `extraArgs`) so OIDC redirects work.
- **OIDC + RBAC**: optional; `operatorAuth` Basic + `rbac.yaml` (ArgoCD-style
  `p/g` rules) cover a single operator. See `hack/dex/` for the lab OIDC stack.
- **Persistence**: Postgres data is on a `local-path` PVC — fine for a miniPC
  cluster; move to a real storageClass / PG operator for HA.