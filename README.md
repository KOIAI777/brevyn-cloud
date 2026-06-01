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

The admin console source lives in `web/admin`, and the user test app lives in
`web/app`. In Docker, the Go API image builds and serves the compiled admin
assets under `/admin` and the user app under `/app`, so the runtime shape is one
API service plus PostgreSQL and Redis.

```bash
make admin-install
make admin-dev
make app-install
make app-dev
```

Open `http://127.0.0.1:5173/admin`. During local development, the admin Vite
server proxies `/api`, `/healthz`, and `/readyz` to the Go API on port `4000`.
Open `http://127.0.0.1:5174/app` for the user app dev server.

To run the integrated Docker service:

```bash
docker compose up -d --build
open http://127.0.0.1:4000/admin
open http://127.0.0.1:4000/app
```

If Docker Hub metadata requests are slow or timing out during local work, use
the local build path. It builds the admin SPA on the host first, then builds the
API/worker image from the already-present Go image:

```bash
make docker-up-local
open http://127.0.0.1:4000/admin
open http://127.0.0.1:4000/app
```

Use the normal `docker compose up -d --build` path for production-like builds,
because it verifies the full multi-stage image including the Node admin build.
