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

## Production Notes

Set `APP_ENV=production` and replace all secrets in `.env`. Production startup
requires HTTPS, non-local values for `APP_BASE_URL`, `ADMIN_BASE_URL`, and
`OFFICIAL_PROVIDER_BASE_URL`.

Do not use `CORS_ALLOWED_ORIGINS=*` in production. List exact HTTPS origins
instead. Leave `TRUSTED_PROXIES` empty for direct container access; when Brevyn
Cloud sits behind a reverse proxy, set it to the proxy IP/CIDR values that are
allowed to provide `X-Forwarded-For`.

Keep the two Sub2API URLs separate:

- `OFFICIAL_PROVIDER_BASE_URL` is returned to clients and should be the public
  model gateway, for example `https://api.brevyn.org`.
- `SUB2API_BASE_URL` is used only by the API and worker to call Sub2API Admin
  APIs. It must be reachable from the containers, for example
  `http://sub2api:8080` on a shared Docker network or
  `http://host.docker.internal:8080` with `host-gateway`.

PostgreSQL and Redis are local infrastructure. The default compose file binds
their host ports to `127.0.0.1` for debugging and should not be exposed publicly.
