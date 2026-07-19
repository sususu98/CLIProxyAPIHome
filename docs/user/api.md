# CLIProxyAPIHome User API

This document describes the current DB-backed User API exposed by CLIProxyAPIHome. The User API is separate from the Management API and does not use the Management API secret key.

Base URL:

```text
http://<host>:<port>/user
```

Home examples usually use port `8327`. The effective listen address comes from runtime config, `cluster.yaml`, or the final `-addr` value.

## Runtime Model

User records, API keys, TOTP settings, and passkey entries are stored in the database-backed cluster repository. The `/user/*` route group is registered only when Home is running with the database-backed runtime route set.

API key changes made through the User API update the same `api_key` table used by Home dispatch and publish a config refresh event.

## Authentication

Public routes:

| Method | Path | Description |
| --- | --- | --- |
| `POST` | `/register` | Creates a user and returns a bearer token. |
| `POST` | `/login` | Password login for users without passkey and without TOTP. |
| `POST` | `/login/totp` | Password plus TOTP login for users with TOTP enabled. |
| `POST` | `/login/passkey` | Passkey login. |

All other `/user/*` routes require a bearer token returned by a successful register or login response.
The bearer token is an RS256 JWT signed with the cluster root CA private key and verified with the cluster root CA public key. Replacing the cluster root CA invalidates previously issued User API tokens.
Password values are hashed and verified exactly as provided; leading and trailing whitespace is not trimmed.

Supported request headers:

| Header | Value |
| --- | --- |
| `Authorization` | `Bearer <USER_TOKEN>` |

Login priority:

| State | Behavior |
| --- | --- |
| Passkey enabled with TOTP enabled | Plain password login returns `401 passkey_required`; `/login/passkey` and `/login/totp` are both accepted. |
| Passkey enabled without TOTP | Plain password login returns `401 passkey_required`; use `/login/passkey`. |
| TOTP enabled without passkey | Plain password login returns `401 totp_required`; use `/login/totp`. |
| No passkey and no TOTP | Plain password login returns a bearer token. |

Home also adds these response headers on User API routes:

| Header | Description |
| --- | --- |
| `x-cpa-home-version` | Home build version. |
| `x-cpa-home-commit` | Home build commit. |
| `x-cpa-home-build-date` | Home build date. |

## Response Conventions

Most successful delete or simple write operations return:

```json
{ "status": "ok" }
```

Successful login and register responses return:

```json
{
  "token": "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_at": "2026-06-02T10:00:00Z",
  "user": {
    "id": 1,
    "username": "alice",
    "credits": 0,
    "totp_enabled": false,
    "passkey_count": 0,
    "created_at": "2026-06-02T10:00:00Z",
    "updated_at": "2026-06-02T10:00:00Z"
  }
}
```

User API handlers usually return both a machine-readable `error` code and a human-readable `message`:

```json
{ "error": "invalid_credentials", "message": "invalid credentials" }
```

Common errors:

```json
{ "error": "bearer_token_required", "message": "bearer token is required" }
{ "error": "invalid_token", "message": "invalid token" }
{ "error": "passkey_required", "message": "passkey is required" }
{ "error": "totp_required", "message": "totp code is required" }
{ "error": "invalid_body", "message": "username is required" }
```

## Registered Routes

The table below is extracted from the User API route group registered by `internal/userapi/handler.go`.

| Method | Path |
| --- | --- |
| `POST` | `/register` |
| `POST` | `/login` |
| `POST` | `/login/passkey/begin` |
| `POST` | `/login/passkey/options` |
| `POST` | `/login/totp` |
| `POST` | `/login/passkey` |
| `GET` | `/me` |
| `PATCH` | `/password` |
| `POST` | `/password` |
| `GET` | `/totp` |
| `POST` | `/totp` |
| `POST` | `/totp/show` |
| `POST` | `/totp/bind` |
| `DELETE` | `/totp` |
| `GET` | `/billing/overview` |
| `GET` | `/billing/charges` |
| `GET` | `/api-keys` |
| `POST` | `/api-keys` |
| `POST` | `/api-key` |
| `PATCH` | `/api-keys` |
| `PATCH` | `/api-key` |
| `PATCH` | `/api-keys/:id` |
| `PATCH` | `/api-key/:id` |
| `DELETE` | `/api-keys` |
| `DELETE` | `/api-key` |
| `DELETE` | `/api-keys/:id` |
| `DELETE` | `/api-key/:id` |
| `POST` | `/passkeys/begin` |
| `POST` | `/passkey/begin` |
| `POST` | `/passkeys/options` |
| `POST` | `/passkey/options` |
| `POST` | `/passkeys` |
| `POST` | `/passkey` |
| `DELETE` | `/passkeys` |
| `DELETE` | `/passkey` |
| `DELETE` | `/passkeys/:id` |
| `DELETE` | `/passkey/:id` |

## Account

### POST `/register`

Creates a user account, stores a bcrypt password hash, and returns a bearer token.

Example request:

```json
{
  "username": "alice",
  "password": "secret"
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `username` | string | yes | Username. Aliases: `user_name`, `user-name`. |
| `password` | string | yes | Plaintext password used to create the stored password hash. |

Response: login response shape.

Common errors:

```json
{ "error": "user_exists", "message": "user already exists" }
{ "error": "invalid_body", "message": "password is required" }
```

### POST `/login`

Logs in with username and password when the user has no passkey and no TOTP.

Example request:

```json
{
  "username": "alice",
  "password": "secret"
}
```

Response: login response shape.

Common errors:

```json
{ "error": "invalid_credentials", "message": "invalid credentials" }
{ "error": "passkey_required", "message": "passkey is required" }
{ "error": "totp_required", "message": "totp code is required" }
```

### POST `/login/totp`

Logs in with username, password, and TOTP code. This route is accepted when TOTP is enabled, even if the user also has passkeys. For passkey-only users, this route returns `401 passkey_required`.

Example request:

```json
{
  "username": "alice",
  "password": "secret",
  "totp_code": "123456"
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `username` | string | yes | Username. Aliases: `user_name`, `user-name`. |
| `password` | string | yes | User password. |
| `totp_code` | string | yes | TOTP code. Aliases: `totp-code`, `totp`, `code`. |

Response: login response shape.

Common errors:

```json
{ "error": "passkey_required", "message": "passkey is required" }
{ "error": "totp_not_enabled", "message": "totp is not enabled" }
{ "error": "invalid_totp", "message": "invalid totp code" }
```

### POST `/login/passkey`

Logs in with a passkey entry stored in `user.passkey`.

Example request:

```json
{
  "username": "alice",
  "passkey_id": "pk_xxx",
  "passkey_secret": "one-time-returned-secret"
}
```

The route can also compare an opaque JSON `credential` field if the passkey was created with a `credential` payload.

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `username` | string | yes | Username. Aliases: `user_name`, `user-name`. |
| `passkey_id` | string | yes | Passkey ID. Alias: `passkey-id`; `id` is also accepted. |
| `passkey_secret` | string | conditionally | Secret returned when the passkey was created. Alias: `secret`. |
| `credential` | object | conditionally | Opaque credential JSON used for exact comparison when no secret hash is stored. |

Response: login response shape.

Common errors:

```json
{ "error": "invalid_passkey", "message": "invalid passkey" }
{ "error": "invalid_body", "message": "username and passkey_id are required" }
```

### GET `/me`

Returns the authenticated user's profile from the bearer token.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Example response:

```json
{
  "status": "ok",
  "user": {
    "id": 1,
    "username": "alice",
    "credits": 0,
    "totp_enabled": false,
    "passkey_count": 1,
    "passkeys": [
      {
        "id": "passkey-1",
        "name": "MacBook Touch ID",
        "created_at": "2026-06-02T10:05:00Z",
        "updated_at": "2026-06-02T10:05:00Z"
      }
    ],
    "created_at": "2026-06-02T10:00:00Z",
    "updated_at": "2026-06-02T10:10:00Z"
  }
}
```

Common errors:

```json
{ "error": "bearer_token_required", "message": "bearer token is required" }
{ "error": "invalid_token", "message": "invalid token" }
```

### POST/PATCH `/password`

Changes the authenticated user's password.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Example request:

```json
{
  "new_password": "new-secret"
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `new_password` | string | yes | New plaintext password. Alias: `new-password`. |

Example response:

```json
{
  "status": "ok",
  "user": {
    "id": 1,
    "username": "alice",
    "totp_enabled": false,
    "passkey_count": 0,
    "created_at": "2026-06-02T10:00:00Z",
    "updated_at": "2026-06-02T10:10:00Z"
  }
}
```

## TOTP

### GET `/totp`

Returns TOTP setup data for the authenticated user. If TOTP is already enabled and `regenerate` is not true, the existing secret is returned.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `issuer` | string | Optional issuer for the otpauth URL. |
| `regenerate` | boolean | Generate a new setup secret instead of returning the current secret. |

Example response:

```json
{
  "secret": "BASE32SECRET",
  "otp_auth_url": "otpauth://totp/CLIProxyAPIHome:alice?...",
  "issuer": "CLIProxyAPIHome",
  "period": 30,
  "digits": 6,
  "algorithm": "SHA1",
  "enabled": false
}
```

### POST `/totp/show`

POST alias of `GET /totp`. Accepts the same bearer token and optional JSON fields.

Example request:

```json
{
  "issuer": "CLIProxyAPIHome",
  "regenerate": true
}
```

Response: same shape as `GET /totp`.

### POST `/totp`

Verifies and stores a TOTP secret for the authenticated user.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Example request:

```json
{
  "secret": "BASE32SECRET",
  "code": "123456"
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `secret` | string | yes | Base32 TOTP secret from `/totp`. |
| `code` | string | yes | Current TOTP code. Aliases: `totp_code`, `totp-code`, `totp`. |
| `issuer` | string | no | Issuer stored in TOTP metadata. Defaults to `CLIProxyAPIHome`. |

Response: same shape as `POST/PATCH /password`.

Common errors:

```json
{ "error": "invalid_totp", "message": "invalid totp code" }
{ "error": "invalid_body", "message": "secret and code are required" }
```

### POST `/totp/bind`

Alias of `POST /totp`.

### DELETE `/totp`

Deletes the authenticated user's TOTP configuration.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Request body: none.

Response: same shape as `POST/PATCH /password`; `user.totp_enabled` is `false` after a successful delete.

Common errors:

```json
{ "error": "bearer_token_required", "message": "bearer token is required" }
{ "error": "invalid_token", "message": "invalid token" }
```

## Billing

User billing routes are under the `/user` base path, so the full paths are `/user/billing/overview` and `/user/billing/charges`. Both routes require the existing bearer token returned by `/user/register` or `/user/login`, and responses are scoped to the authenticated bearer user only.

User billing responses do not include admin notes, global totals, model price management data, proxy-pool data, raw API keys, masked API keys, price snapshots, matched price rules, endpoint, `balance_before`, or other users' data.

User billing `from` and `to` query parameters accept `YYYY-MM-DD`, RFC3339, or Unix seconds and use the half-open interval `[from,to)`. Unix-second values must be between `2000-01-01T00:00:00Z` and `9999-12-31T23:59:59Z`; millisecond timestamps are rejected. A date-only `to` becomes the next UTC midnight so the whole ending UTC day is included. Explicit timestamp `to` values are exact exclusive boundaries and are not expanded. Clients that need a full natural day outside UTC should send RFC3339 boundaries from local midnight to the next local midnight, for example `2026-06-10T00:00:00+08:00` through `2026-06-11T00:00:00+08:00`.

### GET `/billing/overview`

Returns the authenticated user's billing overview.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Query parameters:

| Parameter | Type | Description |
| --- | --- | --- |
| `from` | string | Optional start time: `YYYY-MM-DD`, RFC3339, or Unix seconds. |
| `to` | string | Optional exclusive end time. Date-only values include the full UTC day by using the next UTC midnight; explicit timestamps are preserved exactly. |

Example response:

```json
{
  "overview": {
    "current_balance": 18.75,
    "today_spend": 1.25,
    "month_spend": 1.25,
    "top_models": [
      {
        "id": "openai/gpt-4.1-mini",
        "label": "gpt-4.1-mini",
        "amount": 1.25,
        "request_count": 1
      }
    ]
  }
}
```

Overview fields:

| Field | Type | Description |
| --- | --- | --- |
| `current_balance` | number | Current authenticated user balance. |
| `today_spend` | number | Spend value returned by the current billing overview query. |
| `month_spend` | number | Spend value returned by the current billing overview query. |
| `top_models[]` | array | Model spend entries with `id`, `label`, `amount`, and `request_count`. |

### GET `/billing/charges`

Lists the authenticated user's billing charges.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Query parameters:

| Parameter | Type | Description |
| --- | --- | --- |
| `from` | string | Optional start time: `YYYY-MM-DD`, RFC3339, or Unix seconds. |
| `to` | string | Optional exclusive end time. Date-only values include the full UTC day by using the next UTC midnight; explicit timestamps are preserved exactly. |
| `limit` | integer | Optional page size. Default `50`, max `200`. Invalid non-positive or non-integer values return `400`. |
| `offset` | integer | Optional page offset. Default `0`. Negative or non-integer values return `400`. |

Response shape:

```json
{
  "items": [
    {
      "id": "charge_xxx",
      "created_at": "2026-06-10T10:00:00Z",
      "provider": "openai",
      "model": "gpt-4.1-mini",
      "input_tokens": 1000,
      "output_tokens": 500,
      "amount": 1.25,
      "balance_after": 18.75,
      "request_id": "req_xxx"
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

Charge item fields:

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | Charge record ID. |
| `created_at` | string | Charge creation time. |
| `provider` | string | Provider name. |
| `model` | string | Model name. |
| `input_tokens` | integer | Input tokens. |
| `output_tokens` | integer | Output tokens. |
| `amount` | number | Charged amount. |
| `balance_after` | number | Authenticated user's balance after the charge. |
| `request_id` | string | Request ID associated with the charge. |

Common errors:

```json
{ "error": "bearer_token_required", "message": "bearer token is required" }
{ "error": "invalid_token", "message": "invalid token" }
{ "error": "invalid_from", "message": "from must be YYYY-MM-DD, RFC3339, or unix seconds" }
{ "error": "invalid_to", "message": "to must be YYYY-MM-DD, RFC3339, or unix seconds" }
{ "error": "invalid_time_range", "message": "from must not be after to" }
{ "error": "invalid_limit", "message": "limit must be a positive integer" }
{ "error": "invalid_offset", "message": "offset must be a non-negative integer" }
```

## API Keys

API key routes operate only on API keys bound to the authenticated `user.id`.

### GET `/api-keys`

Lists API keys owned by the authenticated user.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Example response:

```json
{
  "api_keys": [
    {
      "id": 1,
      "api-key": "client-key",
      "api_key": "client-key",
      "channels": [1],
      "model_groups": [2],
      "created_at": "2026-06-02T10:00:00Z",
      "updated_at": "2026-06-02T10:00:00Z"
    }
  ],
  "items": [
    {
      "id": 1,
      "api-key": "client-key",
      "api_key": "client-key",
      "channels": [1],
      "model_groups": [2],
      "created_at": "2026-06-02T10:00:00Z",
      "updated_at": "2026-06-02T10:00:00Z"
    }
  ]
}
```

### POST `/api-keys`

Creates an API key owned by the authenticated user. If `api_key` is omitted, Home generates one.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Example request:

```json
{
  "api_key": "client-key",
  "channels": [1],
  "model_groups": [2]
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `api_key` | string | no | Client API key. Aliases: `api-key`, `key`, `value`. |
| `channels` | array of integer | no | Channel group IDs. Empty or omitted means non-restrictive. |
| `model_groups` | array of integer | no | Model group IDs. Alias: `model-groups`; empty or omitted means non-restrictive. |

Example response:

```json
{
  "api_key": {
    "id": 1,
    "api-key": "client-key",
    "api_key": "client-key",
    "channels": [1],
    "model_groups": [2],
    "created_at": "2026-06-02T10:00:00Z",
    "updated_at": "2026-06-02T10:00:00Z"
  }
}
```

### POST `/api-key`

Alias of `POST /api-keys`.

### PATCH `/api-keys`

Updates an API key owned by the authenticated user. The target can be selected by `id` or by API key value.

Example request:

```json
{
  "id": 1,
  "api_key": "new-client-key",
  "channels": [],
  "model_groups": []
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | integer | conditionally | API key record ID. |
| `api_key` | string | conditionally | New API key when `id` is provided; target API key when no `id` is provided. Aliases: `api-key`, `key`, `value`. |
| `old` | string | conditionally | Target API key value. |
| `new` | string | no | New API key value. |
| `new_api_key` | string | no | New API key value. Alias: `new-api-key`. |
| `channels` | array of integer | no | Replacement channel group IDs. |
| `model_groups` | array of integer | no | Replacement model group IDs. Alias: `model-groups`. |

Response: same shape as `POST /api-keys`.

Common errors:

```json
{ "error": "not_found", "message": "record not found" }
{ "error": "invalid_body", "message": "api key id or value is required" }
{ "error": "api_key_exists", "message": "api key already exists" }
```

### PATCH `/api-key`

Alias of `PATCH /api-keys`.

### PATCH `/api-keys/:id`

Updates an API key owned by the authenticated user by numeric record ID.

Path parameters:

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | integer | yes | `api_key.id`; must be greater than `0`. |

Response: same shape as `POST /api-keys`.

### PATCH `/api-key/:id`

Alias of `PATCH /api-keys/:id`.

### DELETE `/api-keys`

Deletes an API key owned by the authenticated user. The target can be selected by `id` or by API key value.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `id` | integer | API key record ID. |
| `api_key` | string | API key value. |
| `api-key` | string | Alias of `api_key`. |
| `key` | string | Alias of `api_key`. |
| `value` | string | Alias of `api_key`. |

Example response:

```json
{ "status": "ok" }
```

### DELETE `/api-key`

Alias of `DELETE /api-keys`.

### DELETE `/api-keys/:id`

Deletes an API key owned by the authenticated user by numeric record ID.

Path parameters:

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | integer | yes | `api_key.id`; must be greater than `0`. |

Response:

```json
{ "status": "ok" }
```

### DELETE `/api-key/:id`

Alias of `DELETE /api-keys/:id`.

## Passkeys

Passkey routes operate on entries stored in `user.passkey`.

### POST `/passkeys`

Creates a passkey entry for the authenticated user. If no `id` is provided, Home generates one. If no `secret` or `credential` is provided, Home generates a one-time secret and returns it only in this response.

Headers:

```http
Authorization: Bearer user.jwt.token
```

Example request:

```json
{
  "name": "Laptop"
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | no | Passkey ID. Aliases: `passkey_id`, `passkey-id`. |
| `name` | string | no | Display name. |
| `secret` | string | no | Secret to hash and store for passkey login. Alias: `passkey_secret`. |
| `credential` | object | no | Opaque credential JSON to store and compare during passkey login. |

Example response:

```json
{
  "passkey": {
    "id": "pk_xxx",
    "name": "Laptop",
    "created_at": "2026-06-02T10:00:00Z"
  },
  "secret": "one-time-returned-secret"
}
```

Common errors:

```json
{ "error": "passkey_exists", "message": "passkey already exists" }
```

### POST `/passkey`

Alias of `POST /passkeys`.

### DELETE `/passkeys`

Deletes a passkey entry for the authenticated user.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `id` | string | Passkey ID. |

Example response:

```json
{ "status": "ok" }
```

Common errors:

```json
{ "error": "not_found", "message": "passkey not found" }
{ "error": "invalid_body", "message": "passkey id is required" }
```

### DELETE `/passkey`

Alias of `DELETE /passkeys`.

### DELETE `/passkeys/:id`

Deletes a passkey entry for the authenticated user by ID.

Path parameters:

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | string | yes | Passkey ID. |

Response:

```json
{ "status": "ok" }
```

### DELETE `/passkey/:id`

Alias of `DELETE /passkeys/:id`.
