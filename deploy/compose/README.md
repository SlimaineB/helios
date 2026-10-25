# Single-host hub (Docker Compose)

This is the production-oriented hub compose file. For the lab/demo stack
(Postgres + Dex + demo repo + two agents), use the root `compose.yaml` and
[docs/QUICKSTART.md](../../docs/QUICKSTART.md).

## Install

```sh
cp .env.example .env
# set ANH_OPERATOR_PASSWORD and ANH_BASE_URL
$EDITOR .env
docker compose up -d --build
curl -fsS http://localhost:8080/api/v1/health
```

Open `http://localhost:8080` and sign in with `ANH_OPERATOR_USER` /
`ANH_OPERATOR_PASSWORD`.

## PostgreSQL

```sh
docker compose -f compose.yaml -f compose.postgres.yaml up -d --build
```

Set `ANH_POSTGRES_PASSWORD` in `.env` first. The Postgres volume is
`ansiblehub-postgres`.

## TLS / reverse proxy

Terminate TLS in Caddy/Nginx/Traefik and set `ANH_BASE_URL=https://hub.example.com`.
Restart with `docker compose up -d`. To bind only to localhost behind the proxy,
set `ANH_BIND_ADDR=127.0.0.1`.

## Upgrade and backup

```sh
docker compose stop server
docker run --rm -v ansiblehub-data:/data -v "$PWD":/backup alpine \
  tar czf /backup/ansiblehub-data-$(date +%F).tgz -C /data .
docker compose start server

git pull
docker compose up -d --build
```

See [docs/INSTALL.md](../../docs/INSTALL.md) for systemd/Helm installs, agent
installation, OIDC/RBAC and the post-install checklist.
