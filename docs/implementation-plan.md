# Brevyn Cloud Implementation Plan

Status: Draft

Last updated: 2026-06-01

## Current Implementation Snapshot

Implemented locally:

```text
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/refresh
POST /api/v1/auth/logout
GET  /api/v1/me
GET  /api/v1/me/wallet
GET  /api/v1/me/api-keys
GET  /api/v1/provider/official?externalGroupId=xxx
GET  /api/v1/api-keys/system        # deprecated/internal compatibility
POST /api/v1/redeem
```

Current structure split:

```text
internal/gateway/operations
  Durable Postgres-backed operation runner.
  Handles claim, retry backoff, dead letter, and operation result/error state.

internal/admin/gateway_operation_executor.go
  Product-specific executor for sync_redemption.
  Thin adapter from gateway operation records to internal/redeem.

internal/admin/user_service.go
  User management service for admin user lists, detail lookup, local status/delete mutations,
  and Sub2API user mirror sync.

internal/admin/user_detail_service.go
  User detail read service for admin-only wallet transactions, device records,
  linked gateway accounts, and local existence checks.

internal/admin/gateway_group_service.go
  Gateway group service for local gateway group list/create/upsert operations.
  It keeps manual gateway group semantics separate from Sub2API settings sync.

internal/admin/catalog_service.go
  Catalog service for products and redeem-code generation.
  Owns product gateway-group validation and the batch/code generation transaction.

internal/admin/redeem_query_service.go
  Redeem query service for admin redeem batches, redeem codes, and redemption read models.
  It centralizes filters, pagination, date ranges, and gateway operation status joins so HTTP handlers stay thin.

internal/admin/dashboard_query_service.go
  Dashboard query service for admin overview, local ledger totals, Sub2API usage snapshots, and model catalog reads.

internal/admin/audit_query_service.go
  Audit query service for searchable operator logs, pagination, actor labels, and readable audit summaries.

internal/admin/gateway_settings_service.go
  Gateway settings service for Sub2API config load/save, encrypted admin secrets,
  connection testing, gateway group sync, and gateway model catalog sync.
  Compatibility wrappers keep existing admin handlers calling the old helper names while implementation lives in the service.

internal/admin/gateway_model_sync_service.go
  Sub2API model catalog sync service.
  Reads Sub2API admin groups and channels, derives concrete supported models from
  channel model_mapping plus channel_model_pricing, and stores snapshots in
  gateway_channels and gateway_group_models for app-facing model catalog reads.

internal/admin/gateway_key_service.go
  Gateway key service for admin API key lists, single-key disable, cascade key disable,
  manual balance grants, and admin-triggered key rotation.
  It coordinates local key records, encrypted key storage, Sub2API user login, and remote key status updates.

internal/admin/gateway_operation_service.go
  Gateway operation admin service for queue filtering, pagination, operation detail lookup,
  manual retry/requeue, and bulk retry of retryable failed operations.
  It keeps operator queue screens separate from worker execution mechanics.

internal/admin/diagnostics_service.go
  Operations diagnostics service for API/Postgres/Redis status checks, Redis worker heartbeat,
  Sub2API live connection checks, and gateway operation queue counters.

internal/admin/totp_handler.go
  Admin TOTP lifecycle. Generates temporary setup secrets, returns an otpauth QR PNG,
  verifies six-digit codes with one-period skew, stores enabled secrets encrypted,
  and requires TOTP during admin login when enabled.

internal/redeem/gateway_sync_service.go
  GatewaySyncService for redemption sync.
  Loads redemption targets, applies balance/subscription grants to Sub2API,
  ensures linked gateway accounts, ensures gateway API keys, and updates redemption status/error fields.
  User official-provider provisioning, user API key listing, admin retry/worker sync,
  and user redeem gateway sync now use this service.

internal/audit
  Shared audit writer. Admin still keeps a thin writeAuditLog wrapper to avoid noisy call-site churn.
```

Admin UI status:

```text
User detail page
  Shows core user fields, wallet transactions, devices, linked gateway accounts,
  recent redemptions, manual balance grants, key rotation, local status changes,
  and Sub2API sync warnings.

Gateway operations page
  Shows durable sync queue records with status, operation, provider, retryability,
  error class, timestamps, user linkage, and manual retry.

Diagnostics page
  Shows API, Postgres, Redis, worker heartbeat, Sub2API health/auth, queue counters,
  stale running tasks, recent operation timestamps, and bulk retry for retryable failed tasks.

Settings security panel
  Shows current admin TOTP status. Admins can generate an Authenticator QR code,
  verify a six-digit code to enable TOTP, or disable TOTP with password plus code.
```

Working closed loop:

```text
Brevyn user registers
  -> admin generates Brevyn-owned redeem code
  -> user redeems code through Brevyn Cloud
  -> Brevyn Cloud creates or loads Sub2API user
  -> Brevyn Cloud applies balance through Sub2API Admin API
  -> Brevyn Cloud logs wallet transaction and redemption
  -> Brevyn Cloud logs in as the managed Sub2API user and creates a gateway API key
  -> Brevyn Cloud stores the key encrypted and returns it to authenticated app provider-config requests
```

Verified locally:

```text
Brevyn wallet: $4
Sub2API user balance: $4
Sub2API API key: created and bound to group_id=2
```

Known constraint:

```text
Subscription products must bind to a Sub2API group whose subscription_type is "subscription".
Current local group "brevyn综合" is "standard", so it works for balance products but not week-card subscription grants.
```

## 1. Build Target

First product version:

```text
Brevyn app user registers or logs in inside Brevyn
  -> Brevyn Cloud creates or loads the linked Sub2API user
  -> Brevyn Cloud creates or loads the device gateway API key
  -> Brevyn app receives official provider config
  -> User buys a code from LianDong Shop
  -> User redeems inside Brevyn
  -> Brevyn Cloud applies the code to Sub2API
  -> User balance and usage are visible in Brevyn
```

This is the first commercial loop. Do not build direct payment before this loop works.

Account scope:

```text
Normal users:
  Register and log in only inside the Brevyn app.
  Receive the official gateway key automatically.
  Redeem codes and view balance inside the app.

Admins:
  Log in to the Brevyn Cloud backend control console.
  Manage users, grants, redeem logs, usage, model catalog, and gateway operations.

Normal users must not see or use the backend control console.
```

Confirmed defaults:

```text
User login:
  Email + password.

Admin login:
  Separate admin email + password.
  TOTP/2FA before production hard launch.

Gateway key:
  One official gateway key per user device.
  Created by Brevyn Cloud.
  Hidden from normal users by default.

Sub2API group:
  Every official key is bound to SUB2API_DEFAULT_GROUP_ID.
  This group is the "Brevyn Official" comprehensive group.

Model access:
  Claude/Kiro and DeepSeek are exposed through the same official provider path initially.
  Available models are synced from Sub2API into Brevyn Cloud and returned by
  GET /api/v1/models/catalog according to the user's active/default gateway group.
  Users do not choose Sub2API groups.

Redeem codes:
  Prefer generating and owning redeem codes in Brevyn Cloud.
  Sell them through LianDong Shop.
  Redeem inside the Brevyn app through Brevyn Cloud.
  Apply the result to Sub2API through Admin balance/subscription APIs.

Balance:
  Display Sub2API real-time usable balance first.

Device policy:
  Track devices immediately.
  Default soft limit is 3 devices per user; enforcement can come later.
```

## 2. Project Shape

```text
apps/brevyn-cloud
  docs/
  cmd/
    api/
      main.go
    worker/
      main.go
  internal/
    app/
    config/
    common/
    http/
      middleware/
      router.go
    auth/
    users/
    sessions/
    devices/
    providers/
    gateway/
      operations/
      gatewayerror/
      sub2api/
    redeem/
    billing/
    usage/
    admin/
    audit/
    jobs/
  ent/
    schema/
  migrations/
  web/
    admin/
  test/
  docker-compose.yml
  Dockerfile
```

Recommended stack:

```text
Go
Gin
Ent
PostgreSQL
Redis
robfig/cron first; asynq later if queues grow
Docker Compose
```

## 3. Phase 1 Scope

Build only these packages first:

```text
internal/auth
internal/sessions
internal/devices
internal/gateway
internal/providers
internal/redeem
internal/usage
internal/admin
internal/audit
```

Skip for now:

```text
direct payment
full cloud message sync
reseller / school / white-label account systems
public web dashboard
self-hosted gateway replacement
```

## 4. Core Data Model

Phase 1 tables:

```text
users
sessions
devices

gateway_accounts
gateway_api_keys
gateway_balance_snapshots

model_catalog
model_capabilities

gateway_groups
products
redeem_code_batches
redeem_codes
redeem_redemptions
wallet_transactions
usage_daily_stats

admin_users
admin_roles
admin_role_permissions
audit_logs
```

Key relationships:

```text
users.id
  -> devices.user_id
  -> gateway_accounts.user_id
  -> wallet_transactions.user_id

gateway_accounts
  user_id
  provider = "sub2api"
  external_user_id = Sub2API user id
  external_email = internal Sub2API email
  default_group_id = SUB2API_DEFAULT_GROUP_ID

gateway_api_keys
  user_id
  device_id
  gateway_account_id
  provider = "sub2api"
  external_key_id = Sub2API API key id
  external_group_id = SUB2API_DEFAULT_GROUP_ID
  encrypted_api_key = encrypted gateway API key
  masked_api_key

gateway_groups
  provider = "sub2api"
  external_group_id = Sub2API group id
  platform = anthropic / openai / gemini
  subscription_type = standard / subscription
  daily_limit_usd, weekly_limit_usd, monthly_limit_usd

products
  sku
  name
  benefit_type = balance / subscription
  price_cny = shop-facing RMB price
  value = USD-like gateway credit for balance products
  validity_days = duration for subscription products
  gateway_group_id / external_group_id = target Sub2API group for subscription products

redeem_code_batches
  product_id
  source = ldxp / manual / promo
  quantity
  notes

redeem_codes
  code_hash
  code_prefix
  kind = balance / subscription
  value
  validity_days
  product_id
  batch_id
  gateway_group_id / external_group_id
  status = unused / used / expired / disabled
```

## 5. Sub2API Integration

Use HTTP APIs first. Do not read or write the Sub2API database from Brevyn Cloud.

### 5.1 Existing Useful Sub2API APIs

Admin service-to-service auth:

```text
x-api-key: admin-...
```

Useful routes already present:

```text
POST /api/v1/admin/users
GET  /api/v1/admin/users
GET  /api/v1/admin/users/:id
PUT  /api/v1/admin/users/:id
POST /api/v1/admin/users/:id/balance
GET  /api/v1/admin/users/:id/api-keys
PUT  /api/v1/admin/api-keys/:id
POST /api/v1/admin/users/:id/replace-group
POST /api/v1/admin/redeem-codes/create-and-redeem
POST /api/v1/admin/redeem-codes/generate
GET  /api/v1/admin/usage
GET  /api/v1/admin/usage/stats
```

User routes available after Sub2API login:

```text
POST /api/v1/auth/login
POST /api/v1/keys
GET  /api/v1/keys
PUT  /api/v1/keys/:id
DELETE /api/v1/keys/:id
POST /api/v1/redeem
GET  /api/v1/redeem/history
GET  /api/v1/user/profile
GET  /api/v1/usage/stats
```

Existing admin capabilities:

```text
Create a Sub2API user with allowed_groups and initial balance.
Update an existing user's allowed_groups, balance, status, limits, and group rates.
Add, subtract, or set a user's balance.
List a user's existing API keys.
Change an existing API key's group_id.
Replace old group_id with new group_id for a user's existing keys.
Generate redeem codes.
Create and redeem a fixed code for a target user.
```

Brevyn Cloud must call these APIs server-to-server with the Sub2API admin API key. A Brevyn operator should not need to log in manually to provision normal users.

### 5.2 Key Provisioning Options

Current Sub2API can create users through Admin API, but API key creation is primarily user-authenticated.

Important distinction:

```text
User allowed_groups controls which groups the user is permitted to bind.
API key group_id controls which group a model request actually uses.
```

For Brevyn's official provider, both must be true:

```text
Sub2API user has SUB2API_DEFAULT_GROUP_ID in allowed_groups.
Device API key has group_id = SUB2API_DEFAULT_GROUP_ID.
```

Current Phase 1 path:

```text
Brevyn Cloud creates a deterministic internal Sub2API password for managed users,
logs in as that user, and calls POST /api/v1/keys.
```

This works without patching Sub2API and is acceptable for the first local/prototype loop.
The long-term cleaner endpoint is still:

```text
POST /api/v1/admin/users/:id/api-keys
```

Future endpoint behavior:

```text
Accept user_id, name, group_id, quota, rate limits, expires_in_days.
Create the key using the existing APIKeyService.
Require group_id from Brevyn Cloud.
Return the newly generated key only once.
Invalidate auth cache.
Write admin audit log.
```

Current required Brevyn Cloud behavior:

```text
1. Create or load the Sub2API user through Admin API.
2. Login as the managed Sub2API user with the deterministic internal password.
3. Call POST /api/v1/keys.
4. Pass group_id from the redeemed product or default active gateway group.
5. Store the returned gateway key encrypted.
6. Return the full key only to authenticated app flows that need provider config.
```

This fallback should be replaced once the admin create-key endpoint exists.

### 5.3 Default Group Policy

```text
SUB2API_DEFAULT_GROUP_ID is required.
It points to the Brevyn Official comprehensive group.
Every official key created by Brevyn Cloud must bind to this group.
```

The user does not choose a group in the app.

### 5.4 Redeem Existing Codes

Sub2API already has:

```text
POST /api/v1/redeem
```

but it requires a Sub2API user JWT.

Sub2API also has:

```text
POST /api/v1/admin/redeem-codes/create-and-redeem
```

This can redeem an already existing unused code when the code exists, but it can also create and redeem a new code when the code does not exist. That is useful for payment callbacks, but too permissive for Brevyn app user-entered codes.

Recommended patch:

```text
POST /api/v1/admin/users/:id/redeem-existing-code
```

The endpoint should:

```text
accept code only
require the code to already exist
fail if the code does not exist
redeem it for the specified user through RedeemService.Redeem
return the redeemed code and updated user/balance summary
invalidate balance/auth caches
write admin audit log
```

If this patch is delayed, Brevyn Cloud can temporarily use the user-login fallback and call `POST /api/v1/redeem`, but that should not be the final product path.

Initial product behavior:

```text
one app user
  -> one Sub2API user
  -> one official key per device
  -> each key bound to SUB2API_DEFAULT_GROUP_ID
```

## 6. Brevyn Cloud API Surface

Phase 1 public API:

```text
POST /auth/register
POST /auth/login
POST /auth/refresh
POST /auth/logout
GET  /me

POST /devices/register
GET  /devices
DELETE /devices/:id

GET  /provider/official?externalGroupId=xxx
GET  /api-keys/system              # deprecated/internal compatibility
POST /provider/official/rotate-key

GET  /models/catalog
GET  /balance
GET  /usage/summary

POST /redeem
GET  /redeem/history
```

Phase 1 admin API:

```text
POST /admin/auth/login
POST /admin/auth/verify-2fa
GET  /admin/me
GET  /admin/users
GET  /admin/users/:id
POST /admin/users/:id/ban
POST /admin/users/:id/unban
POST /admin/users/:id/grant
GET  /admin/gateway-groups
POST /admin/gateway-groups
GET  /admin/products
POST /admin/products
GET  /admin/redeem-code-batches
GET  /admin/redeem-codes
POST /admin/redeem-codes/generate
GET  /admin/redemptions
GET  /admin/usage
GET  /admin/audit-logs
```

## 7. Important Flows

### 7.1 Register

```text
Electron -> Brevyn Cloud POST /auth/register
Brevyn Cloud creates user
Brevyn Cloud creates a 15-minute access token
Brevyn Cloud stores the 30-day refresh token hash and session family
Brevyn Cloud returns access token + refresh token
```

Sub2API user creation is attempted during registration with a short timeout, then retried by gateway_operations if needed.
The official provider endpoint is still allowed to lazily repair missing account/key state.

Sub2API does have its own public registration API:

```text
POST /api/v1/auth/register
```

Brevyn Cloud should not use it for normal Brevyn users. Brevyn Cloud is the identity source of truth, and Sub2API receives only a gateway shadow user created through Admin API.

### 7.2 Login

```text
Electron -> Brevyn Cloud POST /auth/login
Brevyn Cloud validates password
Brevyn Cloud registers or updates device
Brevyn Cloud returns tokens and basic profile
```

Refresh rotates the refresh token. Reuse of a previously rotated refresh token revokes
the remaining active tokens in that login session family.

### 7.3 Get System API Key

```text
Deprecated/internal compatibility only.
Electron should not call this endpoint in the product flow.
Use GET /provider/official instead.
```

### 7.4 Get Official Provider

```text
Electron -> Brevyn Cloud GET /provider/official?externalGroupId=xxx
Brevyn Cloud validates group ownership
Brevyn Cloud returns local encrypted key immediately when present
Brevyn Cloud rate-limits and locks provisioning when key is missing
Brevyn Cloud queues gateway operation if Sub2API is busy/unavailable
Brevyn Cloud returns provider config
Electron saves provider config through existing provider store
```

Ensure Sub2API user:

```text
1. Look up gateway_accounts by user_id and provider = "sub2api".
2. If missing, call POST /api/v1/admin/users.
3. Use an internal shadow email such as u-{brevyn_user_id}@gateway.brevyn.internal.
4. Generate a random strong password; never show it to the user.
5. Set allowed_groups = [SUB2API_DEFAULT_GROUP_ID].
6. Set initial balance = 0 unless there is a signup grant.
7. Store external_user_id, external_email, default_group_id, and status in gateway_accounts.
```

Recommended Sub2API shadow user fields:

```text
email:
  u-{brevyn_user_id}@gateway.brevyn.internal

username:
  Brevyn {short_user_id}

notes:
  source=brevyn-cloud
  brevyn_user_id={uuid}
  brevyn_user_no={short_id}
  brevyn_email_mask={masked_email}
  brevyn_email_hash={hmac_sha256_email}
  gateway_account_id={gateway_accounts.id}
  created_by=brevyn-cloud
```

Do not put the user's raw login email in the Sub2API email field. If a support operator needs to find the user inside Sub2API, they can search by short user id, masked email, Brevyn user id, or gateway account id because Sub2API admin search includes `email`, `username`, and `notes`.

Optional structured lookup:

```text
Create Sub2API user attributes:
  brevyn_user_id
  brevyn_user_no
  brevyn_email_mask
  brevyn_email_hash

After creating the shadow user, call:
  PUT /api/v1/admin/users/:id/attributes
```

This gives Sub2API admins exact attribute filters without exposing the raw user email.

Ensure device key:

```text
1. Look up gateway_api_keys by user_id + device_id + provider = "sub2api".
2. If missing, call POST /api/v1/admin/users/:id/api-keys.
3. Pass group_id = SUB2API_DEFAULT_GROUP_ID.
4. Store external_key_id, encrypted_api_key, masked_api_key, external_group_id.
5. Return the full key only to the app's secure local provider store.
```

User information sync:

```text
Brevyn Cloud -> Sub2API:
  status changes: ban/unban maps to Sub2API user status active/disabled.
  group changes: update allowed_groups only through admin/operator workflows.
  balance grants: use POST /api/v1/admin/users/:id/balance.

Sub2API -> Brevyn Cloud:
  balance display: read live from Sub2API first.
  usage records: sync through scheduled jobs.
  key status: refresh when provider config is requested or during reconciliation.
```

Brevyn email, password, sessions, devices, and admin roles stay in Brevyn Cloud. Do not treat Sub2API as the user account database.

Provider config:

```json
{
  "purpose": "agent",
  "providerKind": "custom-anthropic",
  "adapterKind": "anthropic",
  "protocol": "anthropic_messages",
  "name": "Brevyn Official",
  "baseUrl": "https://api.brevyn.org",
  "authMode": "api_key",
  "apiKey": "sk-...",
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

### 7.4 Redeem

Phase 1:

```text
Electron -> Brevyn Cloud POST /redeem { code }
Brevyn Cloud checks idempotency and user status
Brevyn Cloud validates a Brevyn-owned code
Brevyn Cloud applies balance or plan to the linked Sub2API user through Admin API
Brevyn Cloud records redeem_redemptions
Brevyn Cloud records wallet_transactions
Brevyn Cloud returns updated balance
```

If we temporarily keep using Sub2API-generated codes:

```text
Brevyn Cloud must either simulate Sub2API user login and call POST /api/v1/redeem,
or patch POST /api/v1/admin/users/:id/redeem-existing-code.
```

### 7.5 Balance

```text
Electron -> Brevyn Cloud GET /balance
Brevyn Cloud calls GatewayAdapter.getBalance
Brevyn Cloud writes gateway_balance_snapshots
Brevyn Cloud returns product-friendly balance
```

Sub2API remains the real-time usable balance source.

### 7.6 Usage

```text
Electron -> Brevyn Cloud GET /usage/summary
Brevyn Cloud reads Sub2API usage APIs or cached usage_daily_stats
Brevyn Cloud returns daily and total usage
```

Run Go scheduled sync later:

```text
every 5 minutes:
  sync recent usage
daily:
  aggregate cost, revenue, margin
```

## 8. Electron Integration

Existing app already has provider storage:

```text
ProviderService
ProviderConfigStore
ProviderSecretStore
provider IPC
agent / embedding / vision provider purposes
```

First integration should add:

```text
BrevynCloudService in Electron main process
cloud auth token secure storage
cloud IPC methods
login UI
redeem UI
balance display
"Use Brevyn Official Provider" action
```

The app can save the official provider using the existing provider save flow.

Phase 1 user-facing pages inside Electron:

```text
Login
Register
Forgot password
Official provider status
Balance
Redeem code
Usage summary
Model catalog
Devices
Account settings
```

Do not expose Sub2API groups, channels, upstream accounts, or raw gateway internals to normal users.

Do not expose:

```text
Sub2API admin API key
Sub2API internal user password
upstream account credentials
```

## 9. Admin Frontend

Build the admin console as a separate SPA:

```text
apps/brevyn-cloud/web/admin
  React
  Vite
  TypeScript
  TanStack Query
  TanStack Table
  React Router
  shadcn/ui or equivalent local UI primitives
```

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

Deployment options:

```text
Development:
  Vite dev server can proxy /api to the Go API.

Production:
  Build static files and serve them through Nginx or the Go API server.
  Recommended final domain: admin.brevyn.org.
```

Normal users must never use this admin console.

## 10. Environment Variables

Required:

```text
APP_ENV
PORT
DATABASE_URL
REDIS_URL

APP_BASE_URL
ADMIN_BASE_URL
JWT_ACCESS_SECRET
JWT_REFRESH_SECRET
SESSION_SECRET
ENCRYPTION_KEY

SUB2API_BASE_URL
SUB2API_ADMIN_API_KEY
SUB2API_DEFAULT_GROUP_ID

OFFICIAL_PROVIDER_BASE_URL
DEVICE_SOFT_LIMIT
```

Production values:

```text
PORT=4000
APP_BASE_URL=https://cloud.brevyn.org
ADMIN_BASE_URL=https://cloud.brevyn.org/admin
SUB2API_BASE_URL=http://host.docker.internal:8080
OFFICIAL_PROVIDER_BASE_URL=https://api.brevyn.org
DEVICE_SOFT_LIMIT=3
```

Production distinction:

```text
OFFICIAL_PROVIDER_BASE_URL is returned to the client and must be the public Sub2API reverse proxy.
SUB2API_BASE_URL is used by Brevyn Cloud only and must be reachable from the api/worker containers.
If Sub2API is in another compose on the same Linux host, either join a shared Docker network or enable host.docker.internal via host-gateway.
SUB2API_DEFAULT_GROUP_ID is only an optional bootstrap fallback. The product default group should be selected in the admin Settings page after syncing Sub2API groups. Runtime provisioning resolves the default group from app_settings first.
OFFICIAL_PROVIDER_DEFAULT_MODEL is optional and should normally stay empty. Available/default models should come from the synced Sub2API group model catalog and the client/user selection.
```

## 11. What To Build First

Order:

```text
1. Scaffold Go + Gin + Ent + Docker Compose.
2. Add health check and config validation.
3. Add user register/login/refresh/logout.
4. Add admin login and role guards.
5. Add device registration.
6. Patch Sub2API admin create-key endpoint.
7. Add Sub2apiGatewayAdapter.
8. Add provider official endpoint.
9. Add balance endpoint.
10. Add redeem endpoint.
11. Add minimal admin endpoints and audit logs.
12. Add Electron login + official provider save.
13. Add admin SPA login, users, grants, redeem records, and usage pages.
```

## 12. Resolved Decisions

The following decisions are locked for Phase 1:

```text
Default group:
  Use SUB2API_DEFAULT_GROUP_ID.

Key model:
  One official key per user device.

Group selection:
  Users do not select billing groups.

DeepSeek:
  Exposed through the same official provider path initially.

Redeem codes:
  Generated and owned by Brevyn Cloud first.
  Applied to Sub2API through Admin balance/subscription APIs.

Key creation:
  Patch Sub2API admin create-key endpoint.

Balance:
  Show Sub2API real-time usable balance.

Key visibility:
  Hide full key from normal UI by default.
```
