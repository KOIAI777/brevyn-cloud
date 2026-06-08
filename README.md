# Brevyn Cloud

Brevyn Cloud is the product control plane for Brevyn.

Phase 1 owns normal user login, admin login, devices, official provider
provisioning, redeem-code ownership, balance display, usage summaries, and the
integration layer to the model gateway.

Backend stack: Go, Gin, Ent, PostgreSQL, Redis, and Docker Compose.

Normal users register and log in inside the Brevyn app. Only admins use the
Brevyn Cloud backend control console.

The model gateway is kept separate. In the first stage, `sub2api` acts as the
gateway and real-time billing engine. Later, it can be replaced or wrapped by a
dedicated Brevyn Gateway without changing the client product flow.

Current docs:

- [Architecture](docs/architecture.md)
- [Implementation plan](docs/implementation-plan.md)

## Local Development

```bash
cp .env.example .env
docker compose up -d --build
curl http://127.0.0.1:4000/healthz
curl http://127.0.0.1:4000/readyz
```

The admin console source lives in `web/admin`. In Docker, the Go API image
builds and serves the compiled admin assets under `/admin`, so the runtime
shape is one API service plus PostgreSQL and Redis. Normal users use the
Electron app and call the Cloud APIs directly; there is no public user web app
served by this service.

```bash
make admin-install
make admin-dev
```

Open `http://127.0.0.1:5173/admin`. During local development, the admin Vite
server proxies `/api`, `/healthz`, and `/readyz` to the Go API on port `4000`.

To run the integrated Docker service:

```bash
docker compose up -d --build
open http://127.0.0.1:4000/admin
```

If Docker Hub metadata requests are slow or timing out during local work, use
the local build path. It builds the admin SPA on the host first, then builds the
API/worker image from the already-present Go image:

```bash
make docker-up-local
open http://127.0.0.1:4000/admin
```

Use the normal `docker compose up -d --build` path for production-like builds,
because it verifies the full multi-stage image including the Node admin build.
The compose stack runs a one-shot `migrate` service before the API and worker
start.

## Production Notes

Create a real `.env` before starting Docker Compose. The compose file requires
the important deployment variables instead of silently falling back to
development defaults. Set `APP_ENV=production` and replace all secrets in
`.env`. Production startup requires HTTPS, non-local values for `APP_BASE_URL`,
`ADMIN_BASE_URL`, and `OFFICIAL_PROVIDER_BASE_URL`.

For the first production version, deploy from GitHub source and build on the
server. After the repository is cloned and `.env` is created on the server,
updates can be applied with:

```bash
cd /data/brevyn-cloud
bash scripts/update-server.sh
```

The script fetches `origin/main`, performs a fast-forward merge, validates Docker
Compose, creates a Postgres backup when the database is already running, rebuilds
the services, waits for `/readyz`, and rolls back to the previous commit if the
health check fails.

Do not use `CORS_ALLOWED_ORIGINS=*` or `ADMIN_ALLOWED_ORIGINS=*` in production.
List exact HTTPS origins instead. `CORS_ALLOWED_ORIGINS` controls browser API
CORS, while `ADMIN_ALLOWED_ORIGINS` protects unsafe admin mutations from
unexpected origins. Leave `TRUSTED_PROXIES` empty for direct container access;
when Brevyn Cloud sits behind a reverse proxy, set it to the proxy IP/CIDR
values that are allowed to provide `X-Forwarded-For`.

Keep the two Sub2API URLs separate:

- `OFFICIAL_PROVIDER_BASE_URL` is returned to clients and should be the public
  model gateway v1 base, for example `https://api.brevyn.org/v1`.
- `SUB2API_BASE_URL` is used only by the API and worker to call Sub2API Admin
  APIs. It must be reachable from the containers, for example
  `http://sub2api:8080` on a shared Docker network or
  `http://host.docker.internal:8080` with `host-gateway`.

PostgreSQL and Redis are local infrastructure. The default compose file binds
their host ports to `127.0.0.1` for debugging and should not be exposed publicly.

### Database Migrations

Development can still run schema preparation during API or worker startup. In
production, keep `MIGRATE_ON_STARTUP` unset or false and run the migration
command before starting the long-running services:

```bash
docker compose run --rm migrate
docker compose up -d api worker
```

For host-based development:

```bash
make migrate
```

The API and worker validate the current schema version on production startup.
If validation fails, run `brevyn-migrate` first instead of letting the services
modify the database implicitly.

### PostgreSQL Backup And Restore

Create a backup before every production migration and keep an automated daily
backup outside the Docker volume:

```bash
make db-backup
```

By default, backups are written to `./backups/postgres` and files older than 14
days are pruned. Override with `BACKUP_DIR` and `BACKUP_RETENTION_DAYS` when
needed.

The admin Settings page also includes a Cloud Backup Center. In Docker Compose,
the API container writes local backups to `/app/backups/postgres`, mounted from
`./backups`, and can optionally upload a second copy to S3-compatible storage
such as Cloudflare R2.

Database backups are not a complete disaster-recovery bundle by themselves.
Keep the production `.env` or secret-manager export with the backups, especially
`ENCRYPTION_KEY`, database credentials, Sub2API admin credentials, and S3/R2
credentials. Stored user gateway keys and official-provider secrets cannot be
decrypted after restore without the original `ENCRYPTION_KEY`.

The API image must include PostgreSQL client tools whose major version is at
least the database server major version. After upgrading Postgres, rebuild the
API image before running Cloud backups or restores so `pg_dump`/`pg_restore`
match the server.

Restores are destructive and require an explicit confirmation flag:

```bash
ALLOW_RESTORE=1 BACKUP_FILE=./backups/postgres/brevyn-cloud-YYYYMMDDTHHMMSSZ.dump make db-restore
```

Admin-triggered restores are disabled by default. Set
`ALLOW_ADMIN_DB_RESTORE=true` only when you intentionally want to allow
password-confirmed restores from the admin page.

For production, copy backups to off-machine storage and test a restore regularly.
