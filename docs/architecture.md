# Brevyn Cloud Architecture Design

Status: Draft

Last updated: 2026-06-01

## Current Build State

The local Brevyn Cloud backend now supports the first user-side commercial loop:

```text
User register/login
  -> 15-minute JWT access token
  -> hashed 30-day refresh token with rotation and revocation
  -> GET /api/v1/me
  -> GET /api/v1/provider/official?externalGroupId=xxx
  -> POST /api/v1/redeem
  -> local wallet/redemption records
  -> Sub2API user creation/loading
  -> Sub2API balance grant or subscription assignment
  -> Sub2API user API key creation through user-auth fallback
```

Verified working path:

```text
Balance redeem code -> Sub2API balance -> Sub2API key -> Brevyn local key record.
```

Current subscription constraint:

```text
Week-card/month-card products require a Sub2API group with subscription_type=subscription.
Standard groups are balance-billing groups and cannot be assigned as subscriptions.
```

## 1. Product Goal

Brevyn should feel like a normal commercial AI product:

- Users log in inside the Brevyn desktop app.
- After login, the app automatically receives the official Brevyn model endpoint and API key.
- The first version has only normal user accounts and admin accounts.
- Normal users only use the Brevyn app; they do not see the backend control console.
- Only admins use the Brevyn Cloud backend control console.
- Users buy balance or plans from an external shop such as LianDong Shop, then redeem the code inside the app.
- The app shows balance, plan status, usage, and available models.
- The app can distinguish chat models, embedding models, and vision-capable models.
- The user never needs to understand `sub2api`, upstream keys, Kiro accounts, DeepSeek keys, or gateway internals.

The first commercial loop is:

```text
User logs in to Brevyn
  -> Brevyn Cloud creates or loads a gateway account
  -> Brevyn Cloud returns official provider config
  -> User buys a redeem code from LianDong Shop
  -> User redeems the code in Brevyn
  -> Brevyn Cloud applies the balance or plan to the gateway
  -> User calls Claude / DeepSeek through api.brevyn.org
```

## 2. Core Architecture Principle

Separate the product control plane from the model traffic plane.

```text
Control plane:
  Brevyn Cloud
  User login, admin login, devices, products, orders, redeem codes, ledger, sync, admin console.

Traffic plane:
  Brevyn Gateway
  Model API requests, streaming, token billing, upstream routing, channel health.
```

In the first stage, `sub2api` is the traffic plane implementation. Brevyn Cloud should integrate with it through a narrow adapter layer, not by mixing product code into `sub2api`.

## 3. Phase 1 Defaults

The first production version uses these fixed decisions:

```text
User account:
  Email + password registration and login inside the Brevyn app.

Admin account:
  Separate admin email + password login for the backend control console.
  TOTP/2FA is required before production hard launch.

Gateway key:
  One official gateway key per user device.
  The key is created by Brevyn Cloud and hidden from normal users by default.

Sub2API group:
  Every official key is bound to one default "Brevyn Official" group.
  The group id is configured by SUB2API_DEFAULT_GROUP_ID.

Model access:
  Claude/Kiro and DeepSeek are exposed through the same official provider path in the app.
  Brevyn Cloud syncs model availability from Sub2API channels, model mappings, channel pricing,
  and group bindings into a local gateway model catalog.
  The app reads GET /api/v1/models/catalog from Brevyn Cloud only.
  Users do not choose Sub2API groups.

Redeem codes:
  Phase 1 redeem codes should be generated and owned by Brevyn Cloud.
  Codes are sold through LianDong Shop.
  Brevyn Cloud validates redemption, records ledger, then applies the result to Sub2API through Admin APIs.

Balance display:
  The app shows Sub2API real-time usable balance as the primary balance.

Device policy:
  Track devices from day one.
  Default soft limit is 3 devices per user, but Phase 1 can record first and enforce later.
```

## 4. Recommended Technology Stack

### Client

```text
Electron + React + TypeScript
```

Existing app: `apps/uclaw-electron`.

The client should store Brevyn sessions and official provider keys in local secure storage.

### Brevyn Cloud API

```text
Go
Gin
Ent
PostgreSQL
Redis
robfig/cron first; asynq later if queues grow
Docker Compose
```

Reasons:

- Go matches sub2api, so gateway integration, shared operational knowledge, and future sub2api patches are simpler.
- Gin matches sub2api's HTTP stack and is enough for Brevyn Cloud's API surface.
- Ent matches sub2api's data layer and gives typed schema, migrations, and safer data access.
- PostgreSQL is the source of truth for Brevyn product data.
- Redis handles sessions, rate limits, temporary state, job locks, and cache.
- robfig/cron handles early scheduled jobs such as usage sync and reconciliation; asynq can be added later for durable queues.

### Admin

```text
React + Vite + TypeScript admin SPA
TanStack Query
TanStack Table
React Router
shadcn/ui or equivalent local UI primitives
```

The admin app should use Brevyn Cloud APIs and should not talk directly to `sub2api` except through the backend. It can be built as static files and served by Nginx or by the Go API server under `admin.brevyn.org`.

Do not use Next.js in Phase 1 unless server-side rendering becomes necessary. The admin panel is an authenticated operations tool, not a content site.

### Gateway

Stage 1:

```text
sub2api
```

Stage 2:

```text
Brevyn Gateway
OpenAI-compatible API
Anthropic-compatible API
Gateway adapter for Kiro / AIClient2API / DeepSeek / other providers
```

### Operations

```text
Nginx
Cloudflare DNS / WAF
Certbot or Cloudflare Origin Certificates
Docker Compose initially
PostgreSQL backups
Sentry
Prometheus / Grafana
Loki or equivalent log collection
```

## 5. Domain Layout

Recommended final domains:

```text
api.brevyn.org      Model gateway. Client model traffic goes here.
cloud.brevyn.org    Brevyn Cloud API. Login, redeem, balance, sync.
admin.brevyn.org    Internal admin panel.
status.brevyn.org   Optional public status page.
```

Current deployment already uses:

```text
api.brevyn.org -> Nginx -> sub2api
```

Do not expose 1Panel through a public domain for now. Keep 1Panel on IP + high port, preferably restricted by IP when possible.

## 6. Service Boundaries

### Current Backend Package Structure

The current implementation is intentionally being split into product services and gateway infrastructure:

```text
cmd/api
  Starts the Brevyn Cloud HTTP API and serves the admin SPA.

cmd/worker
  Starts background workers. It does not own product logic directly.

internal/admin
  Admin HTTP handlers and admin-only orchestration.
  It should stay thin: request validation, response shaping, and calls into services.
  Current extracted services are `UserService` for admin user management and Sub2API user mirror sync,
  `GatewayGroupService` for local gateway group reads and manual group upserts,
  `CatalogService` for products plus redeem-code generation, and `GatewaySettingsService`
  for Sub2API config, encrypted admin secrets, connection testing, remote group sync,
  and remote channel/model catalog sync.
  `GatewayKeyService` owns admin key lists, key disable/rotation, cascade disable, and manual balance grants.
  `RedeemQueryService` owns admin read models for redeem batches, redeem codes, redemptions,
  and latest gateway operation state attached to redemption rows.
  `UserDetailService` owns admin-only user detail read models for wallet transactions,
  device records, and linked gateway accounts.
  `GatewayOperationService` owns the admin queue read model and manual retry/requeue actions
  for durable gateway operations.
  `DiagnosticsService` owns admin operations diagnostics: API/Postgres/Redis checks,
  worker heartbeat state, Sub2API live checks, and gateway operation queue counters.
  Admin TOTP security handlers own setup/enable/disable and login-time second-factor checks.
  TOTP secrets are encrypted with the same secret encryption path used for gateway settings.
  `DashboardQueryService` owns admin overview, usage ledger, Sub2API usage snapshots, and model catalog reads.
  `AuditQueryService` owns audit log filters, pagination, actor labels, and operator-facing summaries.

internal/auth
  User registration, login, refresh, logout, wallet/key/provider endpoints.
  It should own auth/session concerns and response shaping only; Sub2API account lookup,
  official provider key issuance, and gateway key summaries go through `redeem.GatewaySyncService`.

internal/redeem
  Redemption domain services.
  The first extracted service is GatewaySyncService: load redemption sync targets,
  apply balance/subscription to Sub2API, ensure gateway accounts, ensure gateway keys,
  issue official provider keys, list gateway keys, and update redemption gateway status.

internal/audit
  Shared audit log writer. Admin handlers call it through a small wrapper.

internal/gateway/sub2api
  Narrow Sub2API client adapter. This is the only layer that should know Sub2API HTTP details.

internal/gateway/operations
  Durable gateway operation queue and worker runner.
  It owns claim, retry, backoff, dead-letter, and operation status transitions.

internal/gateway/gatewayerror
  Gateway error classifier and retryability policy.

internal/providers
  Official provider config returned to the Brevyn app.

internal/platform
  PostgreSQL, Redis, schema bootstrap.
```

The current gateway worker shape is:

```text
worker
  -> operations.Runner
  -> admin.GatewayOperationExecutor
  -> redeem.GatewaySyncService
  -> Sub2API adapter
```

`operations.Runner` owns queue mechanics. `GatewayOperationExecutor` adapts the generic queue record into the redeem domain service. `redeem.GatewaySyncService` owns the product-specific meaning of a `sync_redemption` operation. This keeps retries and locking reusable when later adding usage sync, subscription renewal, or payment reconciliation jobs.

The current user official-provider shape is:

```text
auth.OfficialProvider / auth.APIKeys
  -> redeem.GatewaySyncService
  -> Sub2API adapter
  -> local gateway_accounts / gateway_api_keys mirror
```

### Brevyn Electron

Responsibilities:

- Register and log in.
- Store session securely.
- Request official provider config from Brevyn Cloud.
- Automatically configure the local model provider.
- Display balance, plan, usage, and available models.
- Redeem balance or plan codes.
- Later: sync chats, settings, prompts, and devices.

The Electron app should not contain sub2api admin credentials or upstream provider credentials.

### Brevyn App Frontend

Normal user pages live inside the Electron app, not in a public web dashboard.

Phase 1 user pages:

```text
Login
Register
Forgot password
Official provider setup/status
Balance
Redeem code
Usage summary
Model catalog
Devices
Account settings
```

User pages should feel like account and billing controls inside the desktop product. They should not expose Sub2API concepts such as groups, channels, upstream accounts, or raw gateway internals.

### Brevyn Admin Frontend

Admin pages live in a separate web console.

Phase 1 admin pages:

```text
Admin login
2FA verification
Overview
Users
User detail
Manual grant / adjustment
Redeem codes
Redemption records
Usage and cost
Model catalog
Gateway accounts / keys
Audit logs
Settings
```

The admin UI should be dense, quiet, and operational. Use tables, filters, drawers, and modals instead of marketing-style pages.

### Public Website

Do not build a public user dashboard in Phase 1.

Optional public pages later:

```text
Landing page
Docs
Pricing
Status page
Download page
```

For Phase 1 sales, LianDong Shop can be the purchase surface, and Brevyn App can be the redemption surface.

### Brevyn Cloud

Responsibilities:

- User accounts.
- Sessions and refresh tokens.
- Device registration.
- Product definitions.
- Orders and payment events.
- Redeem code validation.
- Ledger and wallet records.
- Gateway account provisioning.
- Gateway key provisioning.
- Balance and usage aggregation.
- Admin APIs.
- Cloud sync APIs.

### sub2api / Brevyn Gateway

Responsibilities:

- Model API compatibility.
- Streaming responses.
- API key authentication for model requests.
- Real-time token billing.
- Model pricing.
- Group and channel routing.
- Upstream account pool dispatch.
- Usage records.

## 7. Important Request Flows

### 7.1 Login And Key Provisioning

```text
Electron -> Brevyn Cloud: POST /auth/login
Brevyn Cloud -> PostgreSQL: verify user
Brevyn Cloud -> GatewayAdapter: ensure gateway user exists
Brevyn Cloud -> GatewayAdapter: ensure device-bound gateway key exists with SUB2API_DEFAULT_GROUP_ID
Brevyn Cloud -> Electron: return session + official provider config
Electron -> local secure storage: save provider config
```

`sub2api` has its own public registration endpoint, but Brevyn Cloud should not use it as the product registration path. Brevyn Cloud owns user identity; `sub2api` only gets a shadow gateway user created by the gateway adapter through Admin API.

Current shadow user provisioning:

```text
POST /api/v1/admin/users
  email: user's Brevyn login email
  password: deterministic internal password derived by Brevyn Cloud
  username: Brevyn display name when available
  balance: 0 unless signup grant applies
  allowed_groups: redeemed product group or default active gateway group
  notes: Managed by Brevyn Cloud user {public_id}
```

Later, if we want stronger privacy inside Sub2API, change `external_email` to an internal alias and move user lookup data into notes/attributes.

Then Brevyn Cloud stores the mapping:

```text
gateway_accounts.external_user_id = Sub2API user id
gateway_accounts.external_email = shadow email
gateway_accounts.default_group_id = SUB2API_DEFAULT_GROUP_ID
```

Identity sync is intentionally one-way for Phase 1:

```text
Brevyn Cloud is source of truth:
  email, password, sessions, devices, admin roles, bans.

Sub2API is gateway state:
  balance, API keys, group binding, usage, gateway status.
```

Sub2API admin search already includes user `email`, `username`, `notes`, and API key text. Current local implementation uses the Brevyn login email as the Sub2API email for simpler support lookup; an internal alias is still a good production hardening option.

Example provider response:

```json
{
  "purpose": "agent",
  "providerKind": "custom-anthropic",
  "adapterKind": "anthropic",
  "protocol": "anthropic_messages",
  "name": "Brevyn Official",
  "baseUrl": "https://api.brevyn.org",
  "authMode": "api_key",
  "apiKey": "sk-user-device-key",
  "models": [
    {
      "id": "claude-sonnet-4-5",
      "name": "Claude Sonnet 4.5",
      "enabled": true,
      "supportsVision": true
    },
    {
      "id": "deepseek-v4-flash",
      "name": "DeepSeek V4 Flash",
      "enabled": true
    }
  ],
  "selectedModel": "claude-sonnet-4-5",
  "enabled": true
}
```

Use one gateway key per device. This makes device revocation and abuse handling much easier.

Key rule:

```text
Every official gateway key must have group_id = SUB2API_DEFAULT_GROUP_ID.
```

### 7.2 Model Request

Model requests should not pass through Brevyn Cloud.

```text
Electron -> api.brevyn.org -> sub2api / Gateway -> upstream provider
```

This keeps streaming fast and keeps Brevyn Cloud from becoming the bottleneck.

### 7.3 Redeem Code

Current decision: Brevyn Cloud owns commercial redeem codes from day one.

`sub2api` is still the real-time gateway and billing engine, but its redeem-code
table is not the product source of truth. Brevyn Cloud owns the shop-facing
concepts:

```text
gateway_groups:
  Local mirror of Sub2API groups.
  external_group_id maps to sub2api.groups.id.

products:
  Shop-facing items sold in LianDong Shop.
  benefit_type uses Sub2API-compatible redeem semantics: balance or subscription.

redeem_code_batches:
  Operational batch for one product/source, such as ldxp-0528.

redeem_codes:
  One-time code records.
  Store code_hash and code_prefix only; full plaintext is returned only at generation time.

redeem_redemptions:
  Audit record for a successful app redemption and gateway sync.
```

The app flow is:

```text
LianDong Shop sells Brevyn code
User enters code in Brevyn
Electron -> Brevyn Cloud: POST /redeem
Brevyn Cloud validates code hash and status
Brevyn Ledger records recharge or plan grant
Brevyn Cloud calls GatewayAdapter to add balance or subscription in Sub2API
Gateway returns new usable balance
Brevyn Cloud marks code used and returns updated entitlement summary
```

Implemented endpoint behavior:

```text
POST /api/v1/redeem
  validates Brevyn-owned code_hash
  marks code used locally
  inserts redeem_redemptions
  inserts wallet_transactions for balance codes
  applies add_balance through Sub2API Admin API
  assigns subscription only when the target Sub2API group is a subscription group
  creates a gateway key by logging in as the managed Sub2API user and calling POST /api/v1/keys
```

Sub2API semantic alignment:

```text
balance code:
  value = USD-like gateway credit amount.

subscription code:
  group_id / external_group_id = Sub2API subscription group.
  validity_days = subscription duration granted after redeeming.

standard group:
  balance billing mode.

subscription group:
  time-window subscription limit mode.
```

### 7.4 Balance Display

`sub2api` or the future gateway is the real-time usage source.

Brevyn Cloud should return a product-friendly balance view:

```text
Brevyn Ledger:
  What the user bought, refunded, received, or redeemed.

Gateway Balance:
  What the user can currently spend on model requests.
```

Phase 1 app display:

```text
Primary balance:
  Sub2API real-time usable balance.

History:
  Brevyn redeem logs and wallet transactions where available.
```

### 7.5 Usage Sync

```text
Go scheduled job
  -> GatewayAdapter reads recent gateway usage
  -> Brevyn Cloud stores daily usage snapshots
  -> Admin dashboard shows cost, revenue, margin, abuse signals
```

### 7.6 Provider Capability Routing

Do not model providers as only "OpenAI" or "Anthropic". Model them by capability.

Recommended capability types:

```text
chat:
  Text generation and code generation.

vision_input:
  Chat models that can accept images, screenshots, PDFs rendered as images, or mixed text-image prompts.

embedding:
  Text embedding for semantic search, memory, document retrieval, and possible public embedding APIs.

rerank:
  Optional later capability for search quality.

image_generation:
  Optional later capability for image output.
```

This lets the product answer questions such as:

```text
Can this model chat?
Can this model read images?
Can this model create embeddings?
Should this model appear in the normal chat model selector?
Should this model be used silently for cloud search?
```

The provider registry should store capability metadata, not just provider names.

Example model catalog records:

```text
claude-sonnet-4-5:
  provider_family: anthropic-compatible
  capabilities: chat, vision_input
  public_visible: true

deepseek-v4-flash:
  provider_family: anthropic-compatible
  capabilities: chat
  public_visible: true

text-embedding-3-small:
  provider_family: openai-compatible
  capabilities: embedding
  public_visible: false by default

bge-m3:
  provider_family: openai-compatible
  capabilities: embedding
  public_visible: false by default
```

### 7.7 Embedding Provider Strategy

Embedding should be treated separately from chat.

Recommended final design:

```text
Internal product embeddings:
  Brevyn Cloud calls an embedding provider directly for cloud search, memory, and sync indexing.

User-facing embedding API:
  Do not sell this in the first product version.
  If we sell embedding API access later, route it through Brevyn Gateway with OpenAI-compatible /v1/embeddings.
```

First-stage recommendation:

```text
Do not use Kiro/Claude accounts for embeddings.
Use a dedicated embedding provider with stable price and high throughput.
Keep embedding cost separate from chat cost in usage statistics.
Treat embedding as a bundled product benefit, not a separate paid SKU.
```

Possible provider categories:

```text
OpenAI-compatible embedding providers:
  Good for standard /v1/embeddings compatibility.

Chinese model platforms:
  Useful if the main users are in China and need better latency or payment support.

Self-hosted embedding service:
  Useful later for cost control, but not necessary for the first commercial version.
```

For the Brevyn app, embeddings are initially an internal feature for:

```text
cloud conversation search
memory retrieval
document search
future knowledge base features
```

Billing rule:

```text
User-facing billing:
  No separate charge in the first commercial version.

Internal cost accounting:
  input token count * embedding model price
  no output-token billing
  no cache billing unless the provider explicitly supports it

Fair-use controls:
  per-user daily embedding job limit
  per-user stored chunk limit
  per-document size limit
  queue-level throttling
```

### 7.8 Vision Provider Strategy

Vision should not be treated as a separate product provider unless the use case is image generation or OCR-only processing.

For Brevyn's first product, "vision" mainly means:

```text
The user uploads a screenshot or image.
The selected chat model can understand image input.
The request still uses the normal chat endpoint.
The gateway records image input usage separately when possible.
```

First-stage recommendation:

```text
Use Claude/Kiro-compatible models for vision-capable chat.
Only show the image upload button when the selected model has vision_input capability.
Do not route image requests to text-only DeepSeek models.
Treat vision access as a bundled product benefit, not a separate paid SKU.
```

The client should ask Brevyn Cloud for model capabilities:

```text
GET /models/catalog
```

The response should include:

```json
{
  "models": [
    {
      "id": "claude-sonnet-4-5",
      "displayName": "Claude Sonnet 4.5",
      "capabilities": ["chat", "vision_input"],
      "supportsStreaming": true
    },
    {
      "id": "deepseek-v4-flash",
      "displayName": "DeepSeek V4 Flash",
      "capabilities": ["chat"],
      "supportsStreaming": true
    }
  ]
}
```

Billing rule:

```text
User-facing billing:
  Do not create a separate "vision package" in the first commercial version.
  The user can use vision when their selected chat model supports vision_input.

Gateway billing:
  The gateway may still deduct normal model usage because vision requests consume real upstream tokens.
  If we want vision to be truly free, the gateway must support separate image-token subsidies or automatic bonus grants.

Internal cost accounting:
  text input tokens + image input tokens + output tokens
  trust upstream usage if reported
  estimate or mark usage as untrusted if upstream does not report image tokens

Fair-use controls:
  per-user daily image count limit
  max image size
  max images per request
  abuse detection for repeated screenshots or OCR-style bulk usage
```

### 7.9 Bundled Benefit Policy

Embedding and vision should be positioned as bundled Brevyn benefits, not standalone paid products.

Commercial policy:

```text
Chat balance:
  The main thing users buy.

Embedding:
  Included for app features such as search, memory, and document indexing.
  Not shown as a separate balance to normal users.

Vision:
  Included as a capability of supported Claude/Kiro chat models.
  Not sold as a separate package.

Admin dashboard:
  Still tracks embedding and vision cost separately for margin analysis.
```

This keeps the product simple while preserving cost visibility.

Important distinction:

```text
"Bundled" does not mean "unlimited".
It means users do not buy embedding or vision separately.
The backend still enforces reasonable quotas and abuse limits.
```

## 8. Brevyn Cloud Packages

Recommended Go packages:

```text
cmd/api
cmd/worker
internal/config
internal/http
internal/auth
internal/users
internal/sessions
internal/devices
internal/providers
internal/gateway
internal/products
internal/billing
internal/redeem
internal/usage
internal/sync
internal/admin
internal/audit
internal/jobs
ent/schema
```

Package responsibilities:

```text
internal/auth:
  Register, login, refresh token rotation, logout.

internal/users:
  Normal user profile, status, ban state.

internal/sessions:
  Access token and refresh token lifecycle.

internal/devices:
  Device registration, device-bound gateway keys, revocation.

internal/providers:
  Model catalog, capabilities, public visibility, provider families, route policies.

internal/gateway:
  Adapter over sub2api today, Brevyn Gateway tomorrow.

internal/products:
  Balance packages, weekly plans, monthly plans, pricing rules.

internal/billing:
  Orders, payment events, wallet transactions, refunds, grants.

internal/redeem:
  Redeem code verification, redemption logs, anti-replay.

internal/usage:
  Usage summaries, daily stats, gateway reconciliation.

internal/sync:
  Cloud messages, settings, prompts, conversation sync.

internal/admin:
  Internal admin APIs, RBAC guards, support tools, finance tools.

internal/audit:
  Security and admin operation logs.
```

## 9. Account Model And Authorization

The first product version supports only two account surfaces:

```text
User account:
  Created and used inside the Brevyn desktop app.
  Users register, log in, redeem codes, view balance, and receive the official gateway key.

Admin account:
  Used only by Brevyn operators in the backend control console.
  Admins manage users, grants, redeem records, usage, model catalog, and gateway operations.
```

Do not expose the backend console to normal users.

Multi-tenant features such as school editions, reseller shops, white-label domains, or per-tenant pricing are future extensions. They should not be built into Phase 1 tables or APIs.

### 9.1 Account Model

Recommended model:

```text
users:
  Normal Brevyn app users.

admin_users:
  Brevyn backend console users.

devices:
  Desktop app devices linked to normal users.

gateway_accounts:
  Mapping from Brevyn user to Sub2API user.
  Stores external_user_id, external_email, default_group_id, status, last_synced_at.

gateway_api_keys:
  Device-bound Sub2API gateway keys.
  Stores external_key_id, encrypted_api_key, masked_api_key, external_group_id.
```

Request context should resolve:

```text
user_id or admin_user_id
session_id
device_id when the request comes from the desktop app
role
permissions
```

### 9.2 Authentication Types

Brevyn Cloud has three different authentication surfaces.

```text
User auth:
  Used by the Electron app.
  Bearer JWT access token + refresh token rotation.

Admin auth:
  Used by the web admin panel.
  Separate admin login, stronger session policy, 2FA required before production.

Service auth:
  Used by Brevyn Cloud to call sub2api or future gateway admin APIs.
  Stored only in server environment variables or secret storage.
```

Do not reuse the same token type for all three.

### 9.3 User Authentication

User login should use:

```text
email + password in phase 1
short-lived JWT access token
long-lived refresh token stored as a hash
refresh token rotation
device registration
remote logout
```

Recommended token lifetimes:

```text
access token: 15 minutes
refresh token: 30 days
device-bound gateway key: long-lived, revocable
```

The Electron app stores:

```text
Brevyn refresh token in secure storage
Brevyn access token in memory
gateway API key in secure storage
```

The Electron app must not store:

```text
sub2api admin token
upstream provider keys
database credentials
```

### 9.4 Admin Authentication

Admin should be treated as a separate security boundary.

Admin requirements:

```text
separate admin login surface
2FA/TOTP required before production
shorter session lifetime
IP allowlist optional but recommended
all balance changes logged
all user bans logged
all key rotations logged
all product and price changes logged
```

Recommended admin roles:

```text
owner:
  Full access, including system settings and admin management.

admin:
  User management, products, orders, redeems, gateway operations.

support:
  View users, view orders, help with redeem issues, cannot change prices.

finance:
  View orders, payments, revenue, refunds, cost and margin.

operator:
  Gateway health, model catalog, channel status, no financial permissions.
```

### 9.5 Authorization Model

Use RBAC first, with optional permission flags for sensitive operations.

Basic shape:

```text
role -> permissions
admin_user -> role -> permissions
request -> authenticated subject -> permission guard
```

Example permissions:

```text
user:read
user:ban
device:revoke
order:read
redeem:read
redeem:create
billing:grant
billing:refund
product:write
model:write
gateway:read
gateway:write
admin:manage
audit:read
```

Sensitive operations should require both role permission and audit reason:

```text
manual balance grant
refund
user ban
device key rotation
admin creation
model price change
gateway route change
```

### 9.6 Gateway API Key Authorization

Model requests use gateway API keys, not Brevyn Cloud JWTs.

```text
Electron -> cloud.brevyn.org:
  Uses Brevyn JWT.

Electron -> api.brevyn.org:
  Uses gateway API key.
```

The gateway key should be:

```text
bound to a Brevyn user
bound to a device when possible
revocable from Brevyn Cloud
rotatable from Brevyn Cloud
hidden from normal UI when possible
```

This separation is important because model traffic is high-volume and streaming-heavy, while product auth is account/session oriented.

## 10. Database Design

Brevyn Cloud must use its own PostgreSQL database or schema. Do not modify sub2api tables directly in normal product code.

Suggested tables:

```text
users
auth_identities
sessions
devices

gateway_accounts
gateway_api_keys
gateway_balance_snapshots

model_catalog
model_capabilities
model_route_policies

products
orders
payment_events
redeem_codes
redeem_redemptions
wallet_transactions

usage_events
usage_daily_stats

cloud_threads
cloud_messages
cloud_settings
cloud_embedding_jobs
cloud_embedding_chunks

admin_users
admin_roles
admin_role_permissions
audit_logs
```

### Key Table Meanings

```text
users:
  Normal Brevyn app users.

auth_identities:
  Login identities for users, initially email/password and later OAuth providers if needed.

sessions:
  User refresh sessions and session metadata.

devices:
  One row per logged-in desktop device.

gateway_accounts:
  Maps a Brevyn user to a sub2api user or future gateway user.

gateway_api_keys:
  Maps a device to a gateway key. Store only encrypted key material or masked key when possible.

model_catalog:
  Product-facing model list and display metadata.

model_capabilities:
  Capability tags such as chat, vision_input, embedding, rerank, image_generation.

model_route_policies:
  Which gateway group, provider family, or upstream route should serve each capability.

products:
  Balance packs, weekly cards, monthly cards, trial grants.

orders:
  Orders from LianDong Shop, manual admin grants, or future direct payment.

payment_events:
  Raw payment webhook records after direct payment is added.

redeem_codes:
  Codes generated by Brevyn Cloud in stage 2. Store code hashes, not plain codes.

redeem_redemptions:
  One redemption attempt/result per code.

wallet_transactions:
  Business ledger: recharge, consume adjustment, refund, gift, correction.

usage_daily_stats:
  Aggregated usage for dashboard and margin calculation.

cloud_embedding_jobs:
  Async embedding tasks for cloud sync, search, memory, and future knowledge base features.

cloud_embedding_chunks:
  Text chunks and vector references for cloud-side semantic search.

admin_users:
  Backend control console accounts. These are separate from normal users.

admin_roles:
  Admin roles such as owner, admin, support, finance, operator.

admin_role_permissions:
  Permission flags assigned to admin roles.

audit_logs:
  Admin actions, device revocations, balance changes, security events.
```

## 11. Public API Draft

### Auth

```text
POST /auth/register
POST /auth/login
POST /auth/refresh
POST /auth/logout
GET  /me
```

### Official Provider

```text
GET  /provider/official?externalGroupId=xxx
GET  /api-keys/system              # deprecated/internal compatibility
POST /provider/official/rotate-key
```

### Balance And Usage

```text
GET /balance
GET /usage/summary
GET /usage/daily
```

### Model Catalog

```text
GET /models/catalog
GET /models/capabilities
```

### Redeem

```text
POST /redeem
GET  /redeem/history
```

### Devices

```text
GET    /devices
DELETE /devices/:id
POST   /devices/:id/rotate-key
```

### Cloud Sync

```text
GET  /sync/settings
PUT  /sync/settings
GET  /sync/threads
POST /sync/threads
GET  /sync/threads/:id/messages
POST /sync/threads/:id/messages
```

### Admin

```text
POST /admin/auth/login
POST /admin/auth/verify-2fa
POST /admin/auth/logout
GET  /admin/me
GET  /admin/users
GET  /admin/users/:id
POST /admin/users/:id/ban
POST /admin/users/:id/unban
POST /admin/users/:id/grant
GET  /admin/orders
GET  /admin/redeems
GET  /admin/usage
GET  /admin/gateway/accounts
GET  /admin/audit-logs
```

## 12. Gateway Adapter Contract

Brevyn Cloud should only know an interface, not sub2api internals.

Required adapter methods:

```text
ensureUser(brevynUser): GatewayUser
ensureDeviceKey(user, device, groupId): GatewayApiKey
revokeDeviceKey(keyId): void
rotateDeviceKey(keyId): GatewayApiKey
getBalance(user): GatewayBalance
redeemCode(user, code): RedeemResult
grantBalance(user, amount, reason): GatewayBalance
grantPlan(user, plan, reason): GatewayBalance
getUsage(user, range): UsageSummary
listAvailableModels(user, capability?): Model[]
listModelCapabilities(user): ModelCapability[]
```

Stage 1 implementation:

```text
Sub2apiGatewayAdapter
```

Stage 1 key creation rule:

```text
ensureDeviceKey must pass SUB2API_DEFAULT_GROUP_ID when creating the Sub2API key.
```

The gateway adapter should authenticate to Sub2API with a server-side admin API key. Normal users and Brevyn app clients must never receive Sub2API admin credentials, and operators should not need to manually log in to Sub2API to provision users.

Stage 2 implementation:

```text
BrevynGatewayAdapter
```

This is the main protection against sub2api updates breaking the Brevyn product layer.

## 13. Sub2API Customization Strategy

Sub2API already provides a mature gateway admin system:

```text
Go backend
Vue admin frontend
PostgreSQL
Redis
user keys
groups
channels
model pricing
redeem codes
payments
usage statistics
```

It is valuable, but it should not become the entire Brevyn product backend.

### 13.1 Decision

Recommended approach:

```text
Brevyn Cloud:
  Owns product identity, login, devices, orders, redeem UX, ledger, sync, users, and admin permissions.

Sub2API:
  Acts as gateway and operational gateway console.

Brevyn Sub2API Fork:
  Light fork for the admin create-key endpoint, branding, missing integration endpoints, gateway-specific patches, and admin convenience.
```

Avoid this approach:

```text
Turn Sub2API into the main Brevyn Cloud backend.
```

Reason:

```text
Sub2API is optimized for gateway management.
Brevyn Cloud needs product account logic, app sync, devices, admin permissions, ledger, and long-term payment/order ownership.
Mixing these into Sub2API would make upstream updates harder and product code dependent on gateway internals.
```

### 13.2 What We Can Reuse

Reuse directly:

```text
admin UI for channels, accounts, groups, model pricing
API keys and real-time gateway billing
redeem code engine in phase 1
payment module only if we later decide not to use LianDong Shop
usage statistics as the first source of gateway usage
```

Expose to Brevyn admins:

```text
Brevyn Admin can link to or embed Sub2API admin pages for gateway operations.
Only internal operators should see this.
Normal users should never see Sub2API branding or admin pages.
```

### 13.3 What Should Stay In Brevyn Cloud

Keep these outside Sub2API:

```text
Brevyn user login
normal user account management
admin RBAC
device management
official provider provisioning flow
LianDong Shop order mapping
Brevyn ledger
cloud conversation sync
embedding jobs and cloud search
product-facing model catalog
support workflow
audit logs for product actions
```

### 13.4 Acceptable Sub2API Modifications

Existing Sub2API admin APIs already cover:

```text
create user with allowed_groups and initial balance
update user allowed_groups, status, balance, limits, and group rates
add, subtract, or set user balance
list a user's existing API keys
update an existing API key's group_id
replace a user's existing key group from old_group_id to new_group_id
generate redeem codes
create and redeem a fixed code for a target user
```

Keep this distinction clear:

```text
allowed_groups belongs to the user and means "may use this group".
group_id belongs to the API key and means "this request uses this group".
```

For Brevyn official traffic, Brevyn Cloud should ensure both:

```text
Sub2API user allowed_groups includes SUB2API_DEFAULT_GROUP_ID.
Sub2API device key group_id equals SUB2API_DEFAULT_GROUP_ID.
```

Light modifications are acceptable:

```text
branding and title changes
hide irrelevant menu items
add Brevyn operator shortcuts
add integration endpoints if existing APIs are insufficient
add POST /api/v1/admin/users/:id/api-keys for Brevyn Cloud key provisioning
add POST /api/v1/admin/users/:id/redeem-existing-code for strict Brevyn app code redemption
add webhook/callback hooks for user/key/balance changes
add gateway metadata fields if needed
patch provider adapters or request transforms
```

`POST /api/v1/admin/users/:id/api-keys` is the only must-have patch for clean automatic official provider provisioning. `POST /api/v1/admin/users/:id/redeem-existing-code` is only needed if we keep using Sub2API-generated redeem codes. If Brevyn Cloud owns codes, redemption can call existing Sub2API Admin balance/subscription APIs instead.

High-risk modifications:

```text
replace Sub2API auth with Brevyn Cloud auth
move Brevyn orders and ledger into Sub2API tables
make Electron rely on Sub2API user sessions
deeply change Sub2API database ownership
modify core billing semantics without tests
```

### 13.5 Fork Management

If we maintain a fork, use this policy:

```text
origin:
  Brevyn fork repository.

upstream:
  Wei-Shaw/sub2api.

branch:
  main follows our production fork.
  upstream-sync is used for pulling upstream changes.
  brevyn/* branches hold focused patches.
```

Rules:

```text
Keep Brevyn changes small and isolated.
Document every patch in docs/sub2api-patches.md.
Prefer config, adapter code, or frontend branding over core rewrites.
Run Sub2API tests before updating production.
Never edit production container files by hand.
```

### 13.6 License Note

The local Sub2API source is licensed under LGPL-3.0.

Product implication:

```text
Calling Sub2API through HTTP APIs is clean separation.
Maintaining a server-side fork is possible, but modified distributed binaries/images may carry source and license obligations.
Do not bundle a modified Sub2API binary inside the Electron client.
Ask a lawyer before selling a closed-source modified distribution.
```

This is another reason to keep Brevyn Cloud separate from the Sub2API fork.

## 14. Security Requirements

Minimum requirements:

- Never send sub2api admin credentials to the Electron client.
- Never send upstream provider keys to the Electron client.
- Use HTTPS for all public domains.
- Use refresh token rotation.
- Store refresh tokens as hashes.
- Resolve user or admin identity on every authenticated request.
- Enforce RBAC guards on all admin routes.
- Require 2FA for admin users before production.
- Store redeem code hashes in Brevyn Cloud when Brevyn owns codes.
- Use rate limits on login, redeem, and provider key rotation.
- Add audit logs for admin balance changes, bans, key rotations, and redeem actions.
- Bind gateway keys to devices where possible.
- Allow users or admins to revoke devices.
- Use separate env secrets for local, staging, and production.
- Back up PostgreSQL daily.
- Keep 1Panel off public domain exposure.

## 15. Deployment Plan

Current server layout:

```text
/data/sub2api
/data/aiclient2api
/data/brevyn-cloud
```

Recommended deployment:

```text
Brevyn Cloud API:
  Docker Compose service
  Internal port: 4000
  Public domain: https://cloud.brevyn.org

sub2api:
  Existing Docker Compose service
  Internal port: 8080
  Public domain: https://api.brevyn.org

AIClient2API:
  Existing Docker service
  Internal upstream for sub2api channels
```

Nginx:

```text
api.brevyn.org:
  proxy_pass http://127.0.0.1:8080

cloud.brevyn.org:
  proxy_pass http://127.0.0.1:4000
```

## 16. Development Phases

### Phase 1: Login, Key, Redeem, Balance

Goal: commercial closed loop.

- Create Brevyn Cloud backend scaffold.
- Implement email/password auth.
- Implement separate normal user and admin account flows.
- Implement admin roles and permissions.
- Implement device registration.
- Implement Sub2apiGatewayAdapter.
- Login or official provider setup automatically creates sub2api user and device gateway key.
- Patch Sub2API with admin create-key endpoint.
- Bind official keys to SUB2API_DEFAULT_GROUP_ID.
- Electron receives official provider config.
- Implement redeem code proxy.
- Implement balance display.
- Add basic admin user lookup and manual user status management.

### Phase 2: Product And Ledger

Goal: product-grade accounting.

- Add products.
- Add orders.
- Add wallet transactions.
- Add Brevyn-owned redeem codes.
- Add LianDong Shop export/import workflow.
- Add daily reconciliation between Brevyn ledger and gateway balance.

### Phase 3: Cloud Sync

Goal: make Brevyn feel like a cloud product.

- Sync settings.
- Sync provider preferences.
- Sync conversation metadata.
- Sync messages if privacy policy allows.
- Add device list and remote logout.

### Phase 4: Gateway Independence

Goal: reduce dependency on sub2api.

- Build or wrap Brevyn Gateway.
- Keep sub2api compatible during migration.
- Move gateway billing and usage records to Brevyn-owned infrastructure if needed.
- Support multi-region gateway nodes.

## 17. Current Recommended Next Step

Build Phase 1 only, but keep the code shaped for the final architecture:

```text
Go service scaffold
Ent schema and migrations
internal/auth
internal/admin with role guards
internal/devices
internal/providers
internal/gateway with Sub2apiGatewayAdapter
internal/redeem
Balance endpoint
Model catalog endpoint
Official provider endpoint
```

Do not integrate direct payment yet. Use LianDong Shop redeem codes until the login and key provisioning flow is stable.
