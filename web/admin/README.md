# Brevyn Admin Web

Standalone Brevyn Cloud admin console for operators.

```bash
npm install
npm run dev -- --host 127.0.0.1
npm run build
npm run lint
```

Local URL: `http://127.0.0.1:5173/admin`

Docker integrated runtime:

```bash
docker compose up -d --build
open http://127.0.0.1:4000/admin
```

The console talks only to Brevyn Cloud APIs. It must not call Sub2API directly
and must never contain Sub2API admin credentials.

Current pages:

- `/admin/login`
- `/admin`
- `/admin/users`
- `/admin/users/:id`
- `/admin/redeem-codes`
- `/admin/redemptions`
- `/admin/usage`
- `/admin/models`
- `/admin/gateway`
- `/admin/audit-logs`
- `/admin/settings`
