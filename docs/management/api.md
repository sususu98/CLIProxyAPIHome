# CLIProxyAPIHome Cluster Management API

This document describes the current DB-backed Management API exposed by CLIProxyAPIHome. Home startup initializes a runtime database and registers the database-backed management route set used by the Home runtime.

Base URL:

```text
http://<host>:<port>/v0/management
```

Optional management panel:

```text
GET /management.html
```

Home examples usually use port `8327`. The effective listen address comes from runtime config, `cluster.yaml`, or the final `-addr` value.

## Runtime Model

Home management state is stored in the database-backed cluster repository. When `cluster.yaml` is present, the repository uses the configured backend, such as PostgreSQL or SQLite. When no cluster config is present, Home still opens a local SQLite runtime database and uses the same DB-backed management handlers.

The route list below is the database-backed route set registered by `cmd/home` through `WithDatabaseManagement`.

## Authentication

Every `/v0/management/*` route requires a management key.

Supported request headers:

| Header | Value |
| --- | --- |
| `Authorization` | `Bearer <MANAGEMENT_KEY>` or raw `<MANAGEMENT_KEY>` |
| `X-Management-Key` | `<MANAGEMENT_KEY>` |

Access rules:

| Rule | Behavior |
| --- | --- |
| Local requests | Still require a valid management key. |
| Remote requests | Require remote management to be enabled, such as `remote-management.allow-remote: true`, or an internal override. |
| API disabled | If neither `remote-management.secret-key` nor `MANAGEMENT_PASSWORD` is set, Management API routes normally return `404`. |
| Failed-auth ban | The same client IP is banned for 30 minutes after 5 consecutive failed attempts. During the ban, a correct key still fails. |

Common auth errors:

```json
{ "error": "missing management key" }
{ "error": "invalid management key" }
{ "error": "remote management disabled" }
{ "error": "remote management key not set" }
{ "error": "IP banned due to too many failed attempts. Try again in 29m59s" }
```

Home also adds these response headers on management routes:

| Header | Description |
| --- | --- |
| `x-cpa-home-version` | Home build version. |
| `x-cpa-home-commit` | Home build commit. |
| `x-cpa-home-build-date` | Home build date. |

## Response Conventions

Most successful write operations return:

```json
{ "status": "ok" }
```

Full config replacement returns:

```json
{ "ok": true, "changed": ["config"] }
```

DB-backed handlers usually return both a machine-readable `error` code and a human-readable `message`:

```json
{ "error": "invalid body", "message": "username is required" }
```

Other common error shapes:

```json
{ "error": "invalid body" }
{ "error": "invalid_config", "message": "validation detail" }
```

## Registered Routes

The table below is extracted from the final Home route registry built by `internal/managementhttp/server.go` for `cmd/home`.

| Method | Path |
| --- | --- |
| `GET` | `/ampcode` |
| `GET` | `/ampcode/force-model-mappings` |
| `PATCH` | `/ampcode/force-model-mappings` |
| `PUT` | `/ampcode/force-model-mappings` |
| `DELETE` | `/ampcode/model-mappings` |
| `GET` | `/ampcode/model-mappings` |
| `PATCH` | `/ampcode/model-mappings` |
| `PUT` | `/ampcode/model-mappings` |
| `GET` | `/ampcode/restrict-management-to-localhost` |
| `PATCH` | `/ampcode/restrict-management-to-localhost` |
| `PUT` | `/ampcode/restrict-management-to-localhost` |
| `DELETE` | `/ampcode/upstream-api-key` |
| `GET` | `/ampcode/upstream-api-key` |
| `PATCH` | `/ampcode/upstream-api-key` |
| `PUT` | `/ampcode/upstream-api-key` |
| `DELETE` | `/ampcode/upstream-api-keys` |
| `GET` | `/ampcode/upstream-api-keys` |
| `PATCH` | `/ampcode/upstream-api-keys` |
| `PUT` | `/ampcode/upstream-api-keys` |
| `DELETE` | `/ampcode/upstream-url` |
| `GET` | `/ampcode/upstream-url` |
| `PATCH` | `/ampcode/upstream-url` |
| `PUT` | `/ampcode/upstream-url` |
| `GET` | `/anthropic-auth-url` |
| `GET` | `/antigravity-auth-url` |
| `POST` | `/api-call` |
| `GET` | `/api-key-usage` |
| `DELETE` | `/api-keys` |
| `GET` | `/api-keys` |
| `PATCH` | `/api-keys` |
| `PUT` | `/api-keys` |
| `DELETE` | `/auth-files` |
| `GET` | `/auth-files` |
| `POST` | `/auth-files` |
| `GET` | `/auth-files/download` |
| `PATCH` | `/auth-files/fields` |
| `GET` | `/auth-files/models` |
| `PATCH` | `/auth-files/status` |
| `POST` | `/certificates/clients` |
| `GET` | `/channel-group-details` |
| `POST` | `/channel-group-details` |
| `DELETE` | `/channel-group-details/:id` |
| `GET` | `/channel-group-details/:id` |
| `PATCH` | `/channel-group-details/:id` |
| `PUT` | `/channel-group-details/:id` |
| `GET` | `/channel-groups` |
| `POST` | `/channel-groups` |
| `DELETE` | `/channel-groups/:id` |
| `GET` | `/channel-groups/:id` |
| `PATCH` | `/channel-groups/:id` |
| `PUT` | `/channel-groups/:id` |
| `DELETE` | `/claude-api-key` |
| `GET` | `/claude-api-key` |
| `PATCH` | `/claude-api-key` |
| `PUT` | `/claude-api-key` |
| `DELETE` | `/codex-api-key` |
| `GET` | `/codex-api-key` |
| `PATCH` | `/codex-api-key` |
| `PUT` | `/codex-api-key` |
| `GET` | `/codex-auth-url` |
| `GET` | `/config` |
| `GET` | `/config.yaml` |
| `PUT` | `/config.yaml` |
| `GET` | `/debug` |
| `PATCH` | `/debug` |
| `PUT` | `/debug` |
| `GET` | `/error-logs-max-files` |
| `PATCH` | `/error-logs-max-files` |
| `PUT` | `/error-logs-max-files` |
| `GET` | `/force-model-prefix` |
| `PATCH` | `/force-model-prefix` |
| `PUT` | `/force-model-prefix` |
| `DELETE` | `/gemini-api-key` |
| `GET` | `/gemini-api-key` |
| `PATCH` | `/gemini-api-key` |
| `PUT` | `/gemini-api-key` |
| `GET` | `/gemini-cli-auth-url` |
| `GET` | `/get-auth-status` |
| `GET` | `/kimi-auth-url` |
| `GET` | `/latest-version` |
| `GET` | `/logging-to-file` |
| `PATCH` | `/logging-to-file` |
| `PUT` | `/logging-to-file` |
| `DELETE` | `/logs` |
| `GET` | `/logs` |
| `GET` | `/logs-max-total-size-mb` |
| `PATCH` | `/logs-max-total-size-mb` |
| `PUT` | `/logs-max-total-size-mb` |
| `GET` | `/max-retry-interval` |
| `PATCH` | `/max-retry-interval` |
| `PUT` | `/max-retry-interval` |
| `GET` | `/model-definitions/:channel` |
| `GET` | `/model-group-details` |
| `POST` | `/model-group-details` |
| `DELETE` | `/model-group-details/:id` |
| `GET` | `/model-group-details/:id` |
| `PATCH` | `/model-group-details/:id` |
| `PUT` | `/model-group-details/:id` |
| `GET` | `/model-groups` |
| `POST` | `/model-groups` |
| `DELETE` | `/model-groups/:id` |
| `GET` | `/model-groups/:id` |
| `PATCH` | `/model-groups/:id` |
| `PUT` | `/model-groups/:id` |
| `GET` | `/models` |
| `GET` | `/nodes` |
| `POST` | `/oauth-callback` |
| `DELETE` | `/oauth-excluded-models` |
| `GET` | `/oauth-excluded-models` |
| `PATCH` | `/oauth-excluded-models` |
| `PUT` | `/oauth-excluded-models` |
| `DELETE` | `/oauth-model-alias` |
| `GET` | `/oauth-model-alias` |
| `PATCH` | `/oauth-model-alias` |
| `PUT` | `/oauth-model-alias` |
| `DELETE` | `/openai-compatibility` |
| `GET` | `/openai-compatibility` |
| `PATCH` | `/openai-compatibility` |
| `PUT` | `/openai-compatibility` |
| `DELETE` | `/payload` |
| `GET` | `/payload` |
| `PATCH` | `/payload` |
| `PUT` | `/payload` |
| `DELETE` | `/proxy-url` |
| `GET` | `/proxy-url` |
| `PATCH` | `/proxy-url` |
| `PUT` | `/proxy-url` |
| `GET` | `/quota-exceeded/switch-preview-model` |
| `PATCH` | `/quota-exceeded/switch-preview-model` |
| `PUT` | `/quota-exceeded/switch-preview-model` |
| `GET` | `/quota-exceeded/switch-project` |
| `PATCH` | `/quota-exceeded/switch-project` |
| `PUT` | `/quota-exceeded/switch-project` |
| `GET` | `/request-error-logs` |
| `GET` | `/request-error-logs/:name` |
| `GET` | `/request-log` |
| `PATCH` | `/request-log` |
| `PUT` | `/request-log` |
| `GET` | `/request-log-by-id/:id` |
| `GET` | `/request-retry` |
| `PATCH` | `/request-retry` |
| `PUT` | `/request-retry` |
| `GET` | `/routing/strategy` |
| `PATCH` | `/routing/strategy` |
| `PUT` | `/routing/strategy` |
| `GET` | `/usage-queue` |
| `GET` | `/usage-statistics-enabled` |
| `PATCH` | `/usage-statistics-enabled` |
| `PUT` | `/usage-statistics-enabled` |
| `GET` | `/users` |
| `POST` | `/users` |
| `DELETE` | `/users/:id` |
| `GET` | `/users/:id` |
| `PATCH` | `/users/:id` |
| `PUT` | `/users/:id` |
| `DELETE` | `/vertex-api-key` |
| `GET` | `/vertex-api-key` |
| `PATCH` | `/vertex-api-key` |
| `PUT` | `/vertex-api-key` |
| `POST` | `/vertex/import` |
| `GET` | `/xai-auth-url` |

## Config APIs

### GET `/config`

Returns the current runtime config as JSON.

Input: none.

Example response:

```json
{
  "proxy-url": "http://127.0.0.1:7890",
  "disable-image-generation": false,
  "enable-gemini-cli-endpoint": false,
  "force-model-prefix": false,
  "request-log": false,
  "api-keys": ["client-key"],
  "passthrough-headers": false,
  "streaming": {
    "keepalive-seconds": 0,
    "bootstrap-retries": 0
  },
  "nonstream-keepalive-interval": 0,
  "tls": {
    "enable": false,
    "cert": "",
    "key": ""
  },
  "debug": false,
  "pprof": {
    "enable": false,
    "addr": "127.0.0.1:8316"
  },
  "commercial-mode": false,
  "logging-to-file": false,
  "logs-max-total-size-mb": 0,
  "error-logs-max-files": 10,
  "usage-statistics-enabled": false,
  "redis-usage-queue-retention-seconds": 60,
  "disable-cooling": false,
  "auth-auto-refresh-workers": 0,
  "request-retry": 0,
  "max-retry-credentials": 0,
  "max-retry-interval": 0,
  "quota-exceeded": {
    "switch-project": false,
    "switch-preview-model": false,
    "antigravity-credits": false
  },
  "routing": {
    "strategy": "round-robin",
    "claude-code-session-affinity": false,
    "session-affinity": false,
    "session-affinity-ttl": "1h"
  },
  "antigravity-signature-cache-enabled": true,
  "antigravity-signature-bypass-strict": false,
  "gemini-api-key": [],
  "codex-api-key": [],
  "codex-header-defaults": {
    "user-agent": "",
    "beta-features": ""
  },
  "claude-api-key": [],
  "claude-header-defaults": {
    "user-agent": "",
    "package-version": "",
    "runtime-version": "",
    "os": "",
    "arch": "",
    "timeout": "",
    "stabilize-device-profile": true
  },
  "openai-compatibility": [],
  "vertex-api-key": [],
  "ampcode": {
    "upstream-url": "",
    "upstream-api-key": "",
    "upstream-api-keys": [],
    "restrict-management-to-localhost": false,
    "model-mappings": [],
    "force-model-mappings": false
  },
  "oauth-excluded-models": {
    "claude": ["model-id"]
  },
  "oauth-model-alias": {
    "claude": [
      { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
    ]
  },
  "payload": {
    "default": [],
    "default-raw": [],
    "override": [],
    "override-raw": [],
    "filter": []
  }
}
```

Fields with `json:"-"` are not returned. Home hides `host`, `port`, `allow-host`, `remote-management`, and `auth-dir` from this JSON response.

### GET `/config.yaml`

Returns the current YAML config.

Input: none.

Response content type:

```text
application/yaml; charset=utf-8
```

The response is reconstructed from the persisted config snapshot, so original YAML comments and formatting are not preserved.

### PUT `/config.yaml`

Replaces the full config.

Input: a complete YAML document in the request body.

Home persists non-credential roots into the config snapshot. Credential roots included in the uploaded YAML are synchronized into DB-backed auth records, while omitted credential roots are left unchanged. Send an empty list for a credential root to clear the corresponding provider-key records:

```text
auth-dir
gemini-api-key
vertex-api-key
codex-api-key
claude-api-key
openai-compatibility
```

`auth-dir` is still treated as an import/export path and is not persisted into the runtime config snapshot.

Example response:

```json
{ "ok": true, "changed": ["config", "auth"] }
```

### Simple Config Leaf Routes

These routes write the corresponding config root into the cluster repository and reload the Home runtime.

| Method | Path | Input | Output |
| --- | --- | --- | --- |
| `GET` | `/debug` | none | `{ "debug": boolean }` |
| `PUT/PATCH` | `/debug` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/logging-to-file` | none | `{ "logging-to-file": boolean }` |
| `PUT/PATCH` | `/logging-to-file` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/logs-max-total-size-mb` | none | `{ "logs-max-total-size-mb": number }` |
| `PUT/PATCH` | `/logs-max-total-size-mb` | `{ "value": number }`; negative values are saved as `0` | `{ "status": "ok" }` |
| `GET` | `/error-logs-max-files` | none | `{ "error-logs-max-files": number }` |
| `PUT/PATCH` | `/error-logs-max-files` | `{ "value": number }`; negative values are saved as `10` | `{ "status": "ok" }` |
| `GET` | `/usage-statistics-enabled` | none | `{ "usage-statistics-enabled": boolean }` |
| `PUT/PATCH` | `/usage-statistics-enabled` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/proxy-url` | none | `{ "proxy-url": string }` |
| `PUT/PATCH` | `/proxy-url` | `{ "value": string }` | `{ "status": "ok" }` |
| `DELETE` | `/proxy-url` | none | `{ "status": "ok" }` |
| `GET` | `/request-log` | none | `{ "request-log": boolean }` |
| `PUT/PATCH` | `/request-log` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/request-retry` | none | `{ "request-retry": number }` |
| `PUT/PATCH` | `/request-retry` | `{ "value": number }` | `{ "status": "ok" }` |
| `GET` | `/max-retry-interval` | none | `{ "max-retry-interval": number }` |
| `PUT/PATCH` | `/max-retry-interval` | `{ "value": number }` | `{ "status": "ok" }` |
| `GET` | `/force-model-prefix` | none | `{ "force-model-prefix": boolean }` |
| `PUT/PATCH` | `/force-model-prefix` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/routing/strategy` | none | `{ "strategy": "round-robin" }` or `{ "strategy": "fill-first" }` |
| `PUT/PATCH` | `/routing/strategy` | `{ "value": "round-robin" }`, `roundrobin`, `rr`, `fill-first`, `fillfirst`, or `ff` | `{ "status": "ok" }` |
| `GET` | `/quota-exceeded/switch-project` | none | `{ "switch-project": boolean }` |
| `PUT/PATCH` | `/quota-exceeded/switch-project` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/quota-exceeded/switch-preview-model` | none | `{ "switch-preview-model": boolean }` |
| `PUT/PATCH` | `/quota-exceeded/switch-preview-model` | `{ "value": boolean }` | `{ "status": "ok" }` |

### `/payload` Config Root

`GET /payload` returns:

```json
{
  "payload": {
    "default": [
      {
        "models": [{ "name": "gpt-*", "protocol": "responses" }],
        "params": { "reasoning.effort": "high" }
      }
    ],
    "default-raw": [],
    "override": [],
    "override-raw": [],
    "filter": [
      {
        "models": [{ "name": "*", "protocol": "responses" }],
        "params": ["metadata.debug"]
      }
    ]
  }
}
```

`PUT /payload` and `PATCH /payload` accept either a raw payload object, `{ "value": <payload> }`, or `{ "payload": <payload> }`.

`DELETE /payload` removes the root from the config snapshot.

Successful writes return:

```json
{ "status": "ok" }
```

## Nodes, Version, and Certificates

### GET `/nodes`

Lists nodes currently connected to Home.

Input: none.

Example response:

```json
{
  "nodes": [
    {
      "ip": "10.0.0.12",
      "connected_time": "2026-05-27T10:30:00Z"
    }
  ]
}
```

| Field | Type | Description |
| --- | --- | --- |
| `nodes` | array | Active node list. |
| `nodes[].ip` | string | Node IP address. |
| `nodes[].connected_time` | string | First connection time for the active node entry. |

### GET `/latest-version`

Fetches the latest CLIProxyAPIHome release from GitHub. If `proxy-url` is configured, the request uses that proxy.

Input: none.

Example response:

```json
{ "latest-version": "v7.0.0" }
```

Common error codes:

```json
{ "error": "request_create_failed", "message": "detail" }
{ "error": "request_failed", "message": "detail" }
{ "error": "unexpected_status", "message": "status 502: detail" }
{ "error": "decode_failed", "message": "detail" }
{ "error": "invalid_response", "message": "missing release version" }
```

### POST `/certificates/clients`

Creates a pending client certificate enrollment record and returns a Home JWT that a node can use to finish client-certificate enrollment.

Input: none.

Example response:

```json
{
  "id": "cert-uuid",
  "home_jwt": "eyJhbGciOi..."
}
```

| Field | Type | Description |
| --- | --- | --- |
| `id` | string | Pending client certificate ID. |
| `home_jwt` | string | Enrollment JWT containing Home target information and enrollment secret. |

Common errors:

```json
{ "error": "cluster_unavailable", "message": "cluster_unavailable" }
{ "error": "certificate_jwt_target_invalid", "message": "certificate_jwt_target_invalid" }
{ "error": "certificate_create_failed", "message": "detail" }
{ "error": "certificate_jwt_failed", "message": "detail" }
```

## Users

User records are stored in the cluster repository.

### GET `/users`

Lists users.

Input: none.

Example response:

```json
{
  "users": [
    {
      "id": 1,
      "username": "alice",
      "password_set": true,
      "credits": 10.5,
      "mfa": { "enabled": true },
      "passkey": [{ "id": "credential-id" }],
      "created_at": "2026-05-27T10:00:00Z",
      "updated_at": "2026-05-27T10:00:00Z",
      "deleted_at": null
    }
  ]
}
```

### GET `/users/:id`

Reads one user by numeric ID.

Path parameters:

| Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `id` | integer | yes | `user.id`; must be greater than `0`. |

Example response:

```json
{
  "user": {
    "id": 1,
    "username": "alice",
    "password_set": true,
    "credits": 10.5,
    "mfa": { "enabled": true },
    "passkey": [{ "id": "credential-id" }],
    "created_at": "2026-05-27T10:00:00Z",
    "updated_at": "2026-05-27T10:00:00Z",
    "deleted_at": null
  }
}
```

### POST `/users`

Creates a user.

Example request:

```json
{
  "username": "alice",
  "password": "plaintext-password",
  "credits": 10.5,
  "mfa": { "enabled": true },
  "passkey": [{ "id": "credential-id" }]
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `username` | string | yes | Username. Aliases: `user_name`, `user-name`. |
| `password` | string | no | Non-empty plaintext is stored as a bcrypt hash. Existing valid bcrypt hashes are preserved for migration. Responses do not return password material; they return `password_set`. |
| `credits` | number | no | User credit balance. Defaults to `0`. When a client API key is bound to this user and credits are `<= 0`, RESP `RPOP auth` returns `user_credits_insufficient`. |
| `mfa` | any valid JSON | no | Stored in `user.mfa`. |
| `passkey` | any valid JSON | no | Stored in `user.passkey`. |

Response: same shape as `GET /users/:id`.

### PUT/PATCH `/users/:id`

Updates a user. `PUT` and `PATCH` currently have the same partial-update behavior: only fields present in the body are modified.

Example request:

```json
{
  "username": "alice-updated",
  "password": "new-plaintext-password",
  "credits": 20,
  "mfa": { "enabled": false },
  "passkey": []
}
```

All request fields are optional, but `username`, if present, must not be empty. `credits`, if present, replaces the user's current credit balance.

Response: same shape as `GET /users/:id`.

### DELETE `/users/:id`

Soft-deletes a user.

Input: no body.

Example response:

```json
{ "status": "ok" }
```

Common errors:

```json
{ "error": "not_found", "message": "record not found" }
{ "error": "invalid body", "message": "username is required" }
```

## Client API Keys

### GET `/api-keys`

Returns client API keys accepted by Home.

Input: none.

Example response:

```json
{
  "api-keys": ["client-key-1"],
  "items": [
    {
      "api-key": "client-key-1",
      "api_key": "client-key-1",
      "user-id": 1,
      "user_id": 1,
      "channels": [1],
      "model_groups": [2]
    }
  ],
  "api_key_entries": [
    {
      "api-key": "client-key-1",
      "api_key": "client-key-1",
      "user-id": 1,
      "user_id": 1,
      "channels": [1],
      "model_groups": [2]
    }
  ]
}
```

Fields:

| Field | Type | Description |
| --- | --- | --- |
| `api-keys` | array of string | Compatibility list of raw client keys. |
| `items` | array of `APIKeyEntry` | Structured API key records. |
| `api_key_entries` | array of `APIKeyEntry` | Alias of `items`. |
| `APIKeyEntry.api-key` | string | Client API key. |
| `APIKeyEntry.api_key` | string | Alias of `api-key`. |
| `APIKeyEntry.user-id` | integer or null | Bound `user.id`; `null` means unbound. |
| `APIKeyEntry.user_id` | integer or null | Alias of `user-id`. |
| `APIKeyEntry.channels` | array of integer | Bound channel group IDs. An empty array is non-restrictive. |
| `APIKeyEntry.model_groups` | array of integer | Bound model group IDs. An empty array is non-restrictive. |

### PUT `/api-keys`

Replaces the complete client API key list.

Input can be a raw string array:

```json
["client-key-1", "client-key-2"]
```

or:

```json
{ "items": ["client-key-1", "client-key-2"] }
```

Structured entries are also accepted. Wrapper keys can be `items`, `api-keys`, `api_keys`, or `api_key_entries`:

```json
{
  "api_key_entries": [
    {
      "api_key": "client-key-1",
      "user_id": 1,
      "channels": [1],
      "model_groups": [2]
    }
  ]
}
```

Entry fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `api_key` | string | conditionally | Client API key. Aliases: `api-key`, `key`, `value`. |
| `user_id` | integer | no | Bound `user.id`. Alias: `user-id`. |
| `channels` | array of integer | no | Channel group IDs. |
| `model_groups` | array of integer | no | Model group IDs. Alias: `model-groups`. |

If `user_id` references a missing user, the API returns `404 user_not_found`.

Successful response:

```json
{ "status": "ok" }
```

### PATCH `/api-keys`

Updates one client API key by index or by old value. When `old/new` is used and the old value does not exist, `new` is appended. This route can also update `user_id`, `channels`, and `model_groups` for an existing API key.

Index update:

```json
{ "index": 0, "value": "new-key" }
```

Old/new update:

```json
{ "old": "old-key", "new": "new-key" }
```

Binding update:

```json
{
  "api_key": "client-key-1",
  "user_id": 1,
  "channels": [1],
  "model_groups": [2]
}
```

Clear user binding:

```json
{ "api_key": "client-key-1", "user_id": 0 }
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `index` | integer | conditionally | Zero-based index. |
| `value` | string or `APIKeyEntry` | conditionally | New value paired with `index`. Structured entries are accepted. |
| `old` | string | conditionally | Old key to find. |
| `new` | string | conditionally | New key; appended when `old` is not found. |
| `api_key` | string | conditionally | Direct-binding target. Aliases: `api-key`, `key`. |
| `user_id` | integer | no | Bound `user.id`. Alias: `user-id`; `0` clears the binding. |
| `channels` | array of integer | no | Channel group IDs. |
| `model_groups` | array of integer | no | Model group IDs. Alias: `model-groups`. |

Normal response:

```json
{ "status": "ok" }
```

Direct binding update response:

```json
{
  "api_key": {
    "api-key": "client-key-1",
    "api_key": "client-key-1",
    "user-id": 1,
    "user_id": 1,
    "channels": [1],
    "model_groups": [2]
  }
}
```

### DELETE `/api-keys`

Deletes a client API key by index or value.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `index` | integer | Delete the key at this zero-based index. |
| `value` | string | Delete the key whose trimmed value matches. |
| `api_key` | string | Alias of `value`. |
| `api-key` | string | Alias of `value`. |
| `key` | string | Alias of `value`. |

Example response:

```json
{ "status": "ok" }
```

## Provider API Key Routes

These routes manage upstream API-key credentials:

```text
GET    /gemini-api-key
PUT    /gemini-api-key
PATCH  /gemini-api-key
DELETE /gemini-api-key

GET    /claude-api-key
PUT    /claude-api-key
PATCH  /claude-api-key
DELETE /claude-api-key

GET    /codex-api-key
PUT    /codex-api-key
PATCH  /codex-api-key
DELETE /codex-api-key

GET    /vertex-api-key
PUT    /vertex-api-key
PATCH  /vertex-api-key
DELETE /vertex-api-key

GET    /openai-compatibility
PUT    /openai-compatibility
PATCH  /openai-compatibility
DELETE /openai-compatibility
```

Home synthesizes DB auth records from these config-like payloads.

### Credential Field Structures

`GeminiKey`:

| Field | Type | Description |
| --- | --- | --- |
| `api-key` | string | Upstream Gemini API key. |
| `priority` | integer | Higher priority credentials are selected first. |
| `prefix` | string | Optional model namespace prefix. |
| `base-url` | string | Optional Gemini API base URL override. |
| `proxy-url` | string | Optional per-key outbound proxy. |
| `models` | array of `ModelAlias` | Optional upstream model aliases. |
| `headers` | object string to string | Extra upstream request headers. |
| `excluded-models` | array of string | Model IDs excluded from this key. |
| `disable-cooling` | boolean | Disable quota cooldown scheduling for this credential. |
| `auth-index` | string | Compatibility credential identifier. |
| `auth_index`, `id`, `uuid` | string | DB auth identifier aliases. |
| `disabled` | boolean | DB auth disabled flag. |

`ClaudeKey`, `CodexKey`, and `VertexCompatKey` use the same common fields. Additional notable fields:

| Field | Applies to | Description |
| --- | --- | --- |
| `cloak` | Claude | Optional request cloaking config. |
| `experimental-cch-signing` | Claude | Enables experimental CCH signing for cloaked Claude requests. |
| `websockets` | Codex | Enables Responses API websocket transport. |
| `api-key` | Vertex | Sent as `x-goog-api-key`. |

`OpenAICompatibility`:

| Field | Type | Description |
| --- | --- | --- |
| `name` | string | Provider name. |
| `priority` | integer | Higher priority providers are selected first. |
| `disabled` | boolean | Disables this provider when true. |
| `prefix` | string | Optional model namespace prefix. |
| `base-url` | string | OpenAI-compatible API base URL. |
| `api-key-entries` | array of `OpenAICompatibilityAPIKey` | Provider API keys and optional proxies. |
| `models` | array of `OpenAICompatibilityModel` | Model definitions and aliases. |
| `headers` | object string to string | Extra upstream headers. |
| `disable-cooling` | boolean | Disable quota cooldown scheduling for this provider. |
| `auth-index` | string | Compatibility credential identifier when no per-key entries exist. |
| `auth_index`, `id`, `uuid` | string | DB auth identifier aliases. |

Shared nested structures:

```json
{
  "ModelAlias": {
    "name": "upstream-model",
    "alias": "client-visible-model"
  },
  "OpenAICompatibilityAPIKey": {
    "api-key": "provider-key",
    "proxy-url": "http://127.0.0.1:7890"
  },
  "OpenAICompatibilityModel": {
    "name": "upstream-model",
    "alias": "client-visible-model",
    "thinking": {
      "min": 0,
      "max": 24576,
      "zero_allowed": true,
      "dynamic_allowed": true,
      "levels": ["low", "medium", "high"]
    }
  },
  "CloakConfig": {
    "mode": "auto",
    "strict-mode": false,
    "sensitive-words": ["word"],
    "cache-user-id": true
  }
}
```

### GET Provider Key Routes

Input: none.

Example response:

```json
{
  "gemini-api-key": [
    {
      "auth_index": "auth-db-id",
      "id": "auth-db-id",
      "uuid": "auth-db-id",
      "api-key": "AIza...",
      "base-url": "https://generativelanguage.googleapis.com",
      "prefix": "team-a",
      "proxy-url": "",
      "disabled": false,
      "priority": 10,
      "headers": { "X-Test": "1" }
    }
  ]
}
```

### PUT Provider Key Routes

Replaces the full list for the route provider.

Input can be an array:

```json
[
  {
    "api-key": "provider-key",
    "base-url": "https://api.example.com",
    "models": [
      { "name": "upstream-model", "alias": "alias-model" }
    ]
  }
]
```

or a wrapper:

```json
{ "items": [ { "api-key": "provider-key" } ] }
```

Home also accepts `{ "<route-key>": [...] }`, `{ "list": [...] }`, `{ "data": [...] }`, or a single entry object.

Successful response:

```json
{ "status": "ok" }
```

### PATCH Provider Key Routes

Updates one provider credential.

Example request:

```json
{
  "index": 0,
  "match": "old-api-key",
  "name": "openai-provider-name",
  "value": {
    "api-key": "new-api-key",
    "base-url": "https://api.example.com",
    "proxy-url": "",
    "headers": { "X-Test": "1" },
    "excluded-models": ["model-a"]
  }
}
```

Selector fields:

| Field | Type | Description |
| --- | --- | --- |
| `index` | integer | Zero-based index in the filtered provider list. |
| `match` | string | API-key value to match. |
| `name` | string | OpenAI-compatible provider name or auth label. |
| `id` | string | DB auth ID. |
| `uuid` | string | Alias of `id`. |
| query `base-url` | string | Optional base URL to disambiguate API-key matches. |

`PATCH` does not use body `auth_index` as the DB ID selector. Use `id` or `uuid` for ID-based patching.

Successful response:

```json
{ "status": "ok" }
```

### DELETE Provider Key Routes

Deletes one provider credential.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `id` | string | DB auth ID. |
| `uuid` | string | Alias of `id`. |
| `auth_index` | string | DB auth ID or runtime index. |
| `index` | integer | Zero-based index in the filtered provider list. |
| `api-key` | string | API-key value. |
| `api_key` | string | Alias of `api-key`. |
| `match` | string | Alias of `api-key`. |
| `base-url` | string | Optional base URL to disambiguate. |
| `base_url` | string | Alias of `base-url`. |
| `name` | string | Provider or compatibility name. |

Successful response:

```json
{ "status": "ok" }
```

## Auth Files and OAuth

### GET `/auth-files`

Lists OAuth/file-backed credentials.

Input: none.

Example response:

```json
{
  "files": [
    {
      "id": "auth-db-id",
      "auth_index": "auth-db-id",
      "name": "auth-db-id.json",
      "file_name": "auth-db-id.json",
      "type": "codex",
      "provider": "codex",
      "label": "user@example.com",
      "status": "active",
      "status_message": "",
      "disabled": false,
      "unavailable": false,
      "runtime_only": false,
      "source": "db",
      "email": "user@example.com",
      "priority": 10,
      "note": "operator note",
      "created_at": "2026-05-27T10:00:00Z",
      "updated_at": "2026-05-27T10:00:00Z",
      "modtime": "2026-05-27T10:00:00Z"
    }
  ]
}
```

Gemini virtual auth records may also return `virtual_primary`, `virtual_children`, `virtual`, `virtual_parent_id`, `virtual_project`, and `project_id`.

### GET `/auth-files/models?name=<name-or-id>`

Returns models associated with an auth file or auth ID.

Query parameters:

| Query | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Auth filename, auth ID, display name, or runtime index. |

Example response:

```json
{
  "models": [
    {
      "id": "gpt-5.5",
      "display_name": "GPT-5.5",
      "type": "codex",
      "owned_by": "openai"
    }
  ]
}
```

### GET `/auth-files/download`

Downloads one credential JSON.

Query parameters:

| Query | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | conditionally | Filename or display name. |
| `file` | string | conditionally | Alias for filename. |
| `filename` | string | conditionally | Alias for filename. |
| `id` | string | conditionally | DB auth ID. |
| `uuid` | string | conditionally | Alias of `id`. |
| `auth_index` | string | conditionally | Auth ID or runtime index. |
| `index` | integer | conditionally | Zero-based OAuth auth index. |

Response: `application/json; charset=utf-8` attachment.

### POST `/auth-files`

Uploads one or more credential JSON payloads.

Multipart input:

| Form field | Type | Required | Description |
| --- | --- | --- | --- |
| any file field | file | yes | One or more `.json` credential files. |

Raw JSON input: the request body is the credential JSON payload. `name` is not required; Home derives or allocates a UUID-backed filename.

Example responses:

```json
{ "status": "ok" }
```

```json
{ "status": "ok", "uploaded": 2, "files": ["a.json", "b.json"] }
```

Raw JSON response:

```json
{ "status": "ok", "name": "uuid.json" }
```

### DELETE `/auth-files`

Deletes credential records or files.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `name` | string | Filename or display name. |
| `file` | string | Alias for filename. |
| `filename` | string | Alias for filename. |
| `id` | string | DB auth ID. |
| `uuid` | string | Alias of `id`. |
| `auth_index` | string | Auth ID or runtime index. |
| `index` | integer | Zero-based OAuth auth index. |
| `all` | `true`, `1`, or `*` | Delete all OAuth/file-backed credentials. |

Example responses:

```json
{ "status": "ok" }
```

`all` response:

```json
{ "status": "ok", "deleted": 2 }
```

### PATCH `/auth-files/status`

Enables or disables an OAuth/file-backed auth.

Example request:

```json
{
  "name": "codex-user.json",
  "disabled": true
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | yes | Filename, DB auth ID, runtime auth index, or display name. |
| `disabled` | boolean | yes | `true` disables the auth; `false` enables it. |

This route currently reads the selector from `name`; it does not read separate body `id`, `uuid`, `auth_index`, or `index` fields for this endpoint.

Example response:

```json
{ "status": "ok", "disabled": true }
```

### PATCH `/auth-files/fields`

Updates editable auth metadata.

Example request:

```json
{
  "name": "codex-user.json",
  "id": "auth-db-id",
  "uuid": "auth-db-id",
  "auth_index": "auth-db-id",
  "prefix": "team-a",
  "proxy_url": "http://127.0.0.1:7890",
  "proxy-url": "http://127.0.0.1:7890",
  "headers": { "X-Test": "1" },
  "priority": 10,
  "note": "operator note",
  "websockets": true,
  "disabled": false
}
```

Selector fields:

| Field | Type | Description |
| --- | --- | --- |
| `name`, `file`, `filename` | string | Filename or display name. |
| `id`, `uuid`, `auth_index` | string | DB auth ID selector. |
| query `index` | integer | Zero-based OAuth auth index selector. |

Editable fields:

| Field | Type | Description |
| --- | --- | --- |
| `prefix` | string | Model namespace prefix; empty value clears it. |
| `proxy_url` | string | Per-auth proxy URL; empty value clears it. |
| `proxy-url` | string | Alias for `proxy_url`. |
| `headers` | object string to string | Extra upstream headers. Empty string deletes a single header. |
| `priority` | integer or numeric string | Credential selection priority. |
| `note` | string | Operator note; empty value clears it. |
| `websockets` | boolean or string bool | Runtime websocket flag for supported auths. |
| `disabled` | boolean or string bool | Updates auth disabled state and status. |
| any nested path | any valid JSON | Sets arbitrary metadata paths such as `token.access_token`. |

Example response:

```json
{ "status": "ok" }
```

### OAuth Start Routes

These routes create provider login URLs or device-flow sessions:

```text
GET /anthropic-auth-url
GET /codex-auth-url
GET /gemini-cli-auth-url
GET /antigravity-auth-url
GET /kimi-auth-url
GET /xai-auth-url
```

Common response:

```json
{
  "status": "ok",
  "url": "https://provider.example/oauth/authorize?...",
  "state": "oauth-state"
}
```

`GET /gemini-cli-auth-url` accepts:

| Query | Type | Description |
| --- | --- | --- |
| `project_id` | string | Requested GCP project ID. Special values include `ALL` and `GOOGLE_ONE`; empty value means automatic selection. |

`GET /kimi-auth-url` starts a device flow and returns the verification URL. Completion is handled by Home in the background.

### GET `/get-auth-status`

Returns the current OAuth session status.

Query parameters:

| Query | Type | Required | Description |
| --- | --- | --- | --- |
| `state` | string | no | OAuth state token. |

Example responses:

```json
{ "status": "ok" }
{ "status": "wait" }
{ "status": "error", "error": "Authentication failed" }
```

### POST `/oauth-callback`

Processes provider OAuth callback metadata.

Example request:

```json
{
  "provider": "codex",
  "redirect_url": "http://localhost/callback?code=CODE&state=STATE",
  "code": "CODE",
  "state": "STATE",
  "error": ""
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `provider` | string | yes | Supported aliases: `anthropic`/`claude`, `codex`/`openai`, `gemini`/`google`, `antigravity`/`anti-gravity`, and `xai`/`x-ai`/`grok`. `kimi` is not completed through this route. |
| `redirect_url` | string | no | Full callback URL. Missing `code`, `state`, or `error` values can be extracted from it. |
| `code` | string | conditionally | OAuth authorization code; required unless `error` is supplied. |
| `state` | string | yes | OAuth state token. |
| `error` | string | conditionally | Provider error; required when `code` is absent. |

Home reads session data from the DB-backed OAuth session, exchanges the code in the background, and stores resulting auth records in the DB.

Example response:

```json
{ "status": "ok" }
```

Common errors:

```json
{ "status": "error", "error": "invalid body" }
{ "status": "error", "error": "unsupported provider" }
{ "status": "error", "error": "unknown or expired state" }
{ "status": "error", "error": "oauth flow is not pending" }
{ "status": "error", "error": "provider does not match state" }
```

### POST `/vertex/import`

Uploads a Vertex service account JSON and creates a Vertex OAuth/file-backed credential.

Input:

| Form field or query | Type | Required | Description |
| --- | --- | --- | --- |
| form `file` | file | yes | Vertex service account JSON. |
| form/query `location` | string | no | Vertex location. Default: `us-central1`. |

Example response:

```json
{
  "status": "ok",
  "auth-file": "vertex-project-id.json",
  "project_id": "project-id",
  "email": "service-account@example.iam.gserviceaccount.com",
  "location": "us-central1"
}
```

Home stores the resulting credential as DB-backed OAuth auth records and returns the generated `<uuid>.json` name in `auth-file`.

## API Call Proxy

### POST `/api-call`

Sends an arbitrary HTTP request from the Home server. The route itself is protected by Management API authentication.

Example request:

```json
{
  "auth_index": "auth-index",
  "authIndex": "auth-index",
  "AuthIndex": "auth-index",
  "method": "GET",
  "url": "https://api.example.com/v1/ping",
  "header": {
    "Authorization": "Bearer $TOKEN$",
    "Content-Type": "application/json",
    "Host": "api.example.com"
  },
  "data": "{}"
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `auth_index`, `authIndex`, `AuthIndex` | string | no | Credential index from `GET /auth-files` or provider-key routes. Used for proxy selection and `$TOKEN$` replacement. |
| `method` | string | yes | HTTP method; normalized to uppercase. |
| `url` | string | yes | Absolute URL with scheme and host. |
| `header` | object string to string | no | Request headers. Header values containing `$TOKEN$` are replaced with the selected auth token. `Host` sets request host override. |
| `data` | string | no | Raw request body string. |

Token replacement is strict: if any header contains `$TOKEN$`, `auth_index` must resolve to a DB auth or runtime auth. Otherwise the endpoint returns:

```json
{ "error": "auth not found" }
```

Proxy priority:

1. Selected credential proxy.
2. Global `proxy-url`.
3. Direct transport with environment proxy disabled.

Example response:

```json
{
  "status_code": 200,
  "header": {
    "Content-Type": ["application/json"]
  },
  "body": "{\"ok\":true}"
}
```

## Usage and Logs

### GET `/api-key-usage`

Returns in-memory API-key usage grouped by provider and by `<base_url>|<api_key>`.

Input: none.

Example response:

```json
{
  "gemini": {
    "https://generativelanguage.googleapis.com|AIza...": {
      "success": 10,
      "failed": 1,
      "recent_requests": [
        { "time": "10:00-10:10", "success": 8, "failed": 0 }
      ]
    }
  }
}
```

### GET `/usage-queue`

Pops the oldest queued usage records.

Query parameters:

| Query | Type | Default | Description |
| --- | --- | --- | --- |
| `count` | positive integer | `1` | Number of records to pop. |

Example response:

```json
[
  {
    "request_id": "req-1",
    "executor_type": "CodexWebsocketsExecutor",
    "model": "gpt-5.5",
    "endpoint": "/v1/responses",
    "failed": false
  }
]
```

### GET `/logs`

Returns application log records from the database `log` table.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `home_ip` | string | Optional Home node IP filter. |
| `client_ip` | string | Optional CPA client IP filter. |
| `request_id` | string | Optional request ID filter. |
| `level` | string | Optional log level filter. |
| `after` | integer or RFC3339 | Optional timestamp lower bound. |
| `before` | integer or RFC3339 | Optional timestamp upper bound. |
| `limit` | integer | Maximum returned record count. Default is `100`, maximum is `1000`. |
| `offset` | integer | Pagination offset. Default is `0`. |

Example response:

```json
{
  "logs": [
    {
      "id": 1,
      "timestamp": "2026-05-29T01:02:03Z",
      "client_ip": "10.0.0.5",
      "request_id": "req-1",
      "home_ip": "192.0.2.10",
      "level": "warn",
      "line": "[2026-05-29 09:02:03] [req-1] [warn] message",
      "created_at": "2026-05-29T01:02:04Z"
    }
  ],
  "total": 1,
  "limit": 100,
  "offset": 0
}
```

### DELETE `/logs`

Deletes rotated log files and truncates the active log.

Input: none.

Example response:

```json
{
  "success": true,
  "message": "Logs cleared successfully",
  "removed": 3
}
```

### GET `/request-error-logs`

Lists `error-*.log` files when detailed request logging is disabled. Returns an empty list when detailed request logging is enabled.

Input: none.

Example response:

```json
{
  "files": [
    {
      "name": "error-2026-05-27.log",
      "size": 2048,
      "modified": 1779876000
    }
  ]
}
```

### GET `/request-error-logs/:name`

Downloads a request error log file.

Path parameters:

| Path | Type | Description |
| --- | --- | --- |
| `name` | string | Filename that starts with `error-` and ends with `.log`; slashes are rejected. |

Response: file attachment.

### GET `/request-log-by-id/:id`

Downloads a Home request log file from that Home's local `logs` directory. `home_ip` identifies which Home owns the file; when it is not the current Home, the current Home forwards the request to the target Home over an internal mTLS-only cluster route. Files are matched by request ID, and the file system remains the source of truth, so deleted files return `404`.

Path parameters:

| Path | Type | Description |
| --- | --- | --- |
| `id` | string | Request ID; slashes are rejected. |

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `home_ip` | string | Required Home node IP that owns the request log. |

Response: file attachment.

## Models

### GET `/models?scope=available|static`

Returns model definitions from either the current runtime registry or the static model catalog.

Query parameters:

| Query | Type | Required | Description |
| --- | --- | --- | --- |
| `scope` | string | no | `available` returns models currently registered by active credentials. `static` returns static model definitions. Default: `available`. Aliases: `source`, `mode`, `type`. |
| `channel` | string | no | Static-only filter for one channel. Alias: `provider`. |

Supported `scope` aliases:

| Value | Behavior |
| --- | --- |
| `available`, `active`, `current` | Return currently available runtime models. |
| `static`, `all-static`, `definitions` | Return static model definitions. |

Example available response:

```json
{
  "scope": "available",
  "models": [
    {
      "id": "gpt-5.5",
      "object": "model",
      "created": 1704067200,
      "owned_by": "openai",
      "type": "openai",
      "display_name": "GPT-5.5"
    }
  ]
}
```

Example static response:

```json
{
  "scope": "static",
  "models": {
    "codex-pro": [
      {
        "id": "gpt-5.5",
        "object": "model",
        "created": 1704067200,
        "owned_by": "openai",
        "type": "openai",
        "display_name": "GPT-5.5"
      }
    ]
  }
}
```

### GET `/model-definitions/:channel`

Returns static model metadata for one channel.

Supported channels:

```text
claude
gemini
vertex
gemini-cli
codex
codex-free
codex-team
codex-plus
codex-pro
kimi
antigravity
xai
x-ai
grok
```

Path or query parameters:

| Path/query | Type | Required | Description |
| --- | --- | --- | --- |
| `channel` | string | yes | Channel name. `x-ai` and `grok` are aliases for `xai`. |

Example response:

```json
{
  "channel": "codex",
  "models": [
    {
      "id": "gpt-5.5",
      "object": "model",
      "created": 1704067200,
      "owned_by": "openai",
      "type": "openai",
      "display_name": "GPT-5.5",
      "name": "gpt-5.5",
      "version": "gpt-5.5",
      "description": "",
      "inputTokenLimit": 0,
      "outputTokenLimit": 0,
      "supportedGenerationMethods": [],
      "context_length": 0,
      "max_completion_tokens": 0,
      "supported_parameters": [],
      "supportedInputModalities": ["TEXT"],
      "supportedOutputModalities": ["TEXT"],
      "thinking": {
        "min": 0,
        "max": 24576,
        "zero_allowed": true,
        "dynamic_allowed": true,
        "levels": ["low", "medium", "high"]
      }
    }
  ]
}
```

Unknown channel response:

```json
{ "error": "unknown channel", "channel": "bad-channel" }
```

## Channel Groups

Channel groups restrict which auth records a client API key may use. If a client API key has an empty `channels` array, channel-group filtering is not applied.

### GET `/channel-groups`

Example response:

```json
{
  "channel_groups": [
    {
      "id": 1,
      "channel_name": "team-a",
      "disabled": false,
      "enabled": true,
      "created_at": "2026-05-27T10:00:00Z",
      "updated_at": "2026-05-27T10:00:00Z",
      "deleted_at": null
    }
  ]
}
```

### GET `/channel-groups/:id`

Returns one channel group:

```json
{
  "channel_group": {
    "id": 1,
    "channel_name": "team-a",
    "disabled": false,
    "enabled": true,
    "created_at": "2026-05-27T10:00:00Z",
    "updated_at": "2026-05-27T10:00:00Z",
    "deleted_at": null
  }
}
```

### POST `/channel-groups`

Creates a channel group.

Example request:

```json
{
  "channel_name": "team-a",
  "disabled": false
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `channel_name` | string | yes | Group name. Alias: `name`. |
| `disabled` | boolean | no | Disabled state. Default: `false`. |
| `enabled` | boolean | no | Inverse alias of `disabled`. If both are present, they must agree. |

Response: `{ "channel_group": ... }`.

### PUT/PATCH `/channel-groups/:id`

Updates a channel group. The request fields are the same as `POST /channel-groups`; all fields are optional.

Response: `{ "channel_group": ... }`.

### DELETE `/channel-groups/:id`

Soft-deletes the group and its details.

Response:

```json
{ "status": "ok" }
```

### GET `/channel-group-details`

Lists channel group detail rows.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `channel_group_id` | integer | Filter by group ID. Aliases: `channel-group-id`, `group_id`, `group-id`. |
| `auth_id` | string | Filter by auth ID. Alias: `auth-id`. |

Example response:

```json
{
  "channel_group_details": [
    {
      "id": 10,
      "channel_group_id": 1,
      "auth_id": "auth-db-id",
      "created_at": "2026-05-27T10:00:00Z",
      "updated_at": "2026-05-27T10:00:00Z",
      "deleted_at": null
    }
  ]
}
```

### GET `/channel-group-details/:id`

Returns one detail row:

```json
{
  "channel_group_detail": {
    "id": 10,
    "channel_group_id": 1,
    "auth_id": "auth-db-id",
    "created_at": "2026-05-27T10:00:00Z",
    "updated_at": "2026-05-27T10:00:00Z",
    "deleted_at": null
  }
}
```

### POST `/channel-group-details`

Creates a channel-group-to-auth binding.

Example request:

```json
{
  "channel_group_id": 1,
  "auth_id": "auth-db-id"
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `channel_group_id` | integer | yes | Existing channel group ID. |
| `auth_id` | string | yes | Auth record ID. |

Response: `{ "channel_group_detail": ... }`.

### PUT/PATCH `/channel-group-details/:id`

Updates a detail row.

Example request:

```json
{
  "channel_group_id": 2,
  "auth_id": "other-auth-id"
}
```

All fields are optional, but a supplied `channel_group_id` must be greater than `0`.

Response: `{ "channel_group_detail": ... }`.

### DELETE `/channel-group-details/:id`

Soft-deletes the detail row.

Response:

```json
{ "status": "ok" }
```

## Model Groups

Model groups restrict which model IDs a client API key may use. If a client API key has an empty `model_groups` array, model filtering is not applied.

### GET `/model-groups`

Example response:

```json
{
  "model_groups": [
    {
      "id": 1,
      "group_name": "premium-models",
      "disabled": false,
      "enabled": true,
      "created_at": "2026-05-27T10:00:00Z",
      "updated_at": "2026-05-27T10:00:00Z",
      "deleted_at": null
    }
  ]
}
```

### GET `/model-groups/:id`

Returns one model group:

```json
{
  "model_group": {
    "id": 1,
    "group_name": "premium-models",
    "disabled": false,
    "enabled": true,
    "created_at": "2026-05-27T10:00:00Z",
    "updated_at": "2026-05-27T10:00:00Z",
    "deleted_at": null
  }
}
```

### POST `/model-groups`

Creates a model group.

Example request:

```json
{
  "group_name": "premium-models",
  "disabled": false
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `group_name` | string | yes | Group name. Alias: `name`. |
| `disabled` | boolean | no | Disabled state. Default: `false`. |
| `enabled` | boolean | no | Inverse alias of `disabled`. If both are present, they must agree. |

Response: `{ "model_group": ... }`.

### PUT/PATCH `/model-groups/:id`

Updates a model group. The request fields are the same as `POST /model-groups`; all fields are optional.

Response: `{ "model_group": ... }`.

### DELETE `/model-groups/:id`

Soft-deletes the model group and its details.

Response:

```json
{ "status": "ok" }
```

### GET `/model-group-details`

Lists model group detail rows.

Query parameters:

| Query | Type | Description |
| --- | --- | --- |
| `model_group_id` | integer | Filter by model group ID. Aliases: `model-group-id`, `group_id`, `group-id`. |
| `model_id` | string | Filter by model ID. Alias: `model-id`. |

Example response:

```json
{
  "model_group_details": [
    {
      "id": 10,
      "model_group_id": 1,
      "model_id": "gpt-5.5",
      "created_at": "2026-05-27T10:00:00Z",
      "updated_at": "2026-05-27T10:00:00Z",
      "deleted_at": null
    }
  ]
}
```

### GET `/model-group-details/:id`

Returns one detail row:

```json
{
  "model_group_detail": {
    "id": 10,
    "model_group_id": 1,
    "model_id": "gpt-5.5",
    "created_at": "2026-05-27T10:00:00Z",
    "updated_at": "2026-05-27T10:00:00Z",
    "deleted_at": null
  }
}
```

### POST `/model-group-details`

Creates a model-group-to-model binding.

Example request:

```json
{
  "model_group_id": 1,
  "model_id": "gpt-5.5"
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `model_group_id` | integer | yes | Existing model group ID. |
| `model_id` | string | yes | Model ID allowed by this group. |

Response: `{ "model_group_detail": ... }`.

### PUT/PATCH `/model-group-details/:id`

Updates a detail row.

Example request:

```json
{
  "model_group_id": 2,
  "model_id": "gpt-5.5-mini"
}
```

All fields are optional, but a supplied `model_group_id` must be greater than `0`.

Response: `{ "model_group_detail": ... }`.

### DELETE `/model-group-details/:id`

Soft-deletes the detail row.

Response:

```json
{ "status": "ok" }
```

## AmpCode

These routes read and write `ampcode` config.

`AmpCode` object:

```json
{
  "upstream-url": "https://amp.example.com",
  "upstream-api-key": "upstream-key",
  "upstream-api-keys": [
    {
      "upstream-api-key": "upstream-key",
      "api-keys": ["client-key"]
    }
  ],
  "restrict-management-to-localhost": false,
  "model-mappings": [
    { "from": "claude-opus-4.5", "to": "claude-sonnet-4", "regex": false }
  ],
  "force-model-mappings": false
}
```

Routes:

| Method | Path | Input | Output |
| --- | --- | --- | --- |
| `GET` | `/ampcode` | none | `{ "ampcode": AmpCode }` |
| `GET` | `/ampcode/upstream-url` | none | `{ "upstream-url": string }` |
| `PUT/PATCH` | `/ampcode/upstream-url` | `{ "value": string }` | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/upstream-url` | none | `{ "status": "ok" }` |
| `GET` | `/ampcode/upstream-api-key` | none | `{ "upstream-api-key": string }` |
| `PUT/PATCH` | `/ampcode/upstream-api-key` | `{ "value": string }` | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/upstream-api-key` | none | `{ "status": "ok" }` |
| `GET` | `/ampcode/restrict-management-to-localhost` | none | `{ "restrict-management-to-localhost": boolean }` |
| `PUT/PATCH` | `/ampcode/restrict-management-to-localhost` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/ampcode/model-mappings` | none | `{ "model-mappings": AmpModelMapping[] }` |
| `PUT` | `/ampcode/model-mappings` | `{ "value": AmpModelMapping[] }` | `{ "status": "ok" }` |
| `PATCH` | `/ampcode/model-mappings` | `{ "value": AmpModelMapping[] }`; upsert by `from` | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/model-mappings` | `{ "value": ["from-model"] }`; invalid or missing body clears all mappings | `{ "status": "ok" }` |
| `GET` | `/ampcode/force-model-mappings` | none | `{ "force-model-mappings": boolean }` |
| `PUT/PATCH` | `/ampcode/force-model-mappings` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/ampcode/upstream-api-keys` | none | `{ "upstream-api-keys": AmpUpstreamAPIKeyEntry[] }` |
| `PUT` | `/ampcode/upstream-api-keys` | `{ "value": AmpUpstreamAPIKeyEntry[] }` | `{ "status": "ok" }` |
| `PATCH` | `/ampcode/upstream-api-keys` | `{ "value": AmpUpstreamAPIKeyEntry[] }`; upsert by `upstream-api-key` | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/upstream-api-keys` | `{ "value": [] }` clears all; `{ "value": ["key"] }` deletes matching upstream keys | `{ "status": "ok" }` |

## OAuth Model Rules

### `/oauth-excluded-models`

GET response:

```json
{
  "oauth-excluded-models": {
    "claude": ["claude-opus-4.5"]
  }
}
```

PUT input:

```json
{
  "claude": ["claude-opus-4.5"]
}
```

or:

```json
{
  "items": {
    "claude": ["claude-opus-4.5"]
  }
}
```

PATCH input:

```json
{
  "provider": "claude",
  "models": ["claude-opus-4.5"]
}
```

DELETE query:

| Query | Type | Required | Description |
| --- | --- | --- | --- |
| `provider` | string | yes | Provider key to remove. |

Successful writes return `{ "status": "ok" }`.

### `/oauth-model-alias`

GET response:

```json
{
  "oauth-model-alias": {
    "claude": [
      { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
    ]
  }
}
```

PUT input:

```json
{
  "claude": [
    { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
  ]
}
```

or:

```json
{
  "items": {
    "claude": [
      { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
    ]
  }
}
```

PATCH input:

```json
{
  "channel": "claude",
  "provider": "claude",
  "aliases": [
    { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
  ]
}
```

DELETE query:

| Query | Type | Required | Description |
| --- | --- | --- | --- |
| `channel` | string | conditionally | Alias channel to remove. |
| `provider` | string | conditionally | Alias of `channel`. |

Successful writes return `{ "status": "ok" }`.

## Config Field Reference

These fields are accepted by Home YAML config. `PUT /config.yaml` accepts non-credential roots; use provider-key and auth-file routes for credential roots.

| Field | Type | Description |
| --- | --- | --- |
| `host` | string | Service bind host/interface. |
| `port` | integer | Service listen port. |
| `allow-host` | array of string | RESP client IP allowlist. Empty list allows all hosts. |
| `tls.enable` | boolean | Enable HTTPS. |
| `tls.cert` | string | TLS certificate path. |
| `tls.key` | string | TLS private key path. |
| `remote-management.allow-remote` | boolean | Allows non-localhost Management API requests when true. |
| `remote-management.secret-key` | string | Management key. In local config mode, plaintext is hashed at startup. |
| `remote-management.disable-control-panel` | boolean | Disables `/management.html` and panel syncing. |
| `remote-management.disable-auto-update-panel` | boolean | Disables periodic background panel asset updates. |
| `remote-management.panel-github-repository` | string | Management panel GitHub repository URL or releases API URL. |
| `auth-dir` | string | Local auth token directory. |
| `proxy-url` | string | Global outbound proxy URL. |
| `disable-image-generation` | boolean or `"chat"` | `false` enables image generation; `true` disables it globally; `"chat"` disables injection for non-image endpoints. |
| `enable-gemini-cli-endpoint` | boolean | Enables Gemini CLI internal endpoints. |
| `force-model-prefix` | boolean | Requires explicit model prefixes for prefixed credentials. |
| `request-log` | boolean | Enables detailed request logging. |
| `api-keys` | array of string | Client API keys accepted by Home. |
| `passthrough-headers` | boolean | Passes upstream response headers to downstream clients. |
| `streaming.keepalive-seconds` | integer | SSE heartbeat interval in seconds; `<=0` disables it. |
| `streaming.bootstrap-retries` | integer | Streaming retries before first byte; `<=0` disables it. |
| `nonstream-keepalive-interval` | integer | Blank-line keepalive interval for non-streaming responses. |
| `debug` | boolean | Enables debug logging/features. |
| `pprof.enable` | boolean | Enables pprof server. |
| `pprof.addr` | string | pprof listen address. |
| `commercial-mode` | boolean | Reduces high-overhead middleware behavior under high concurrency. |
| `logging-to-file` | boolean | Writes app logs to files instead of stdout. |
| `logs-max-total-size-mb` | integer | Total log file size limit in MB; `0` disables cleanup. |
| `error-logs-max-files` | integer | Retained request error log file count. |
| `usage-statistics-enabled` | boolean | Enables in-memory usage aggregation. |
| `redis-usage-queue-retention-seconds` | integer | Usage queue retention window. Default `60`, max `3600`. |
| `disable-cooling` | boolean | Globally disables quota cooldown scheduling. |
| `auth-auto-refresh-workers` | integer | Overrides auth auto-refresh worker count. |
| `request-retry` | integer | Failed request retry count. |
| `max-retry-credentials` | integer | Max credentials to try per failed request; `<=0` means all available. |
| `max-retry-interval` | integer | Max wait seconds before retrying cooled-down credentials. |
| `quota-exceeded.switch-project` | boolean | Switches Gemini project on quota errors. |
| `quota-exceeded.switch-preview-model` | boolean | Switches to preview model on quota errors. |
| `quota-exceeded.antigravity-credits` | boolean | Uses Antigravity credits as last-resort Claude fallback. |
| `routing.strategy` | string | `round-robin` or `fill-first`. |
| `routing.claude-code-session-affinity` | boolean | Deprecated Claude Code session affinity flag. |
| `routing.session-affinity` | boolean | Universal session-sticky credential routing. |
| `routing.session-affinity-ttl` | string | Session-to-auth binding duration. |
| `antigravity-signature-cache-enabled` | boolean pointer | Enables Antigravity thinking signature cache validation. |
| `antigravity-signature-bypass-strict` | boolean pointer | Controls strictness of Antigravity signature bypass. |
| `gemini-api-key` | array of `GeminiKey` | Gemini API-key credentials; use provider-key routes. |
| `codex-api-key` | array of `CodexKey` | Codex API-key credentials; use provider-key routes. |
| `codex-header-defaults.user-agent` | string | Default Codex User-Agent. |
| `codex-header-defaults.beta-features` | string | Default Codex websocket beta features header. |
| `claude-api-key` | array of `ClaudeKey` | Claude API-key credentials; use provider-key routes. |
| `claude-header-defaults.user-agent` | string | Default Claude User-Agent. |
| `claude-header-defaults.package-version` | string | Default Claude package version. |
| `claude-header-defaults.runtime-version` | string | Default Claude runtime version. |
| `claude-header-defaults.os` | string | Default Claude OS fingerprint. |
| `claude-header-defaults.arch` | string | Default Claude architecture fingerprint. |
| `claude-header-defaults.timeout` | string | Default Claude timeout header. |
| `claude-header-defaults.stabilize-device-profile` | boolean pointer | Enables stable Claude device profile baseline. |
| `openai-compatibility` | array of `OpenAICompatibility` | OpenAI-compatible providers; use provider-key routes. |
| `vertex-api-key` | array of `VertexCompatKey` | Vertex-compatible API-key credentials; use provider-key routes. |
| `ampcode` | `AmpCode` | Amp CLI integration settings. |
| `oauth-excluded-models` | object string to array of string | Per-provider OAuth/file-backed auth excluded models. |
| `oauth-model-alias` | object string to array of `OAuthModelAlias` | Per-channel OAuth model aliases. |
| `payload.default` | array of `PayloadRule` | Sets missing JSON payload params. |
| `payload.default-raw` | array of `PayloadRule` | Sets missing raw JSON payload params. |
| `payload.override` | array of `PayloadRule` | Overrides JSON payload params. |
| `payload.override-raw` | array of `PayloadRule` | Overrides raw JSON payload params. |
| `payload.filter` | array of `PayloadFilterRule` | Removes JSON payload paths. |

Payload nested structure:

```json
{
  "PayloadRule": {
    "models": [
      { "name": "gpt-*", "protocol": "responses" }
    ],
    "params": {
      "reasoning.effort": "high"
    }
  },
  "PayloadFilterRule": {
    "models": [
      { "name": "gpt-*", "protocol": "responses" }
    ],
    "params": ["metadata.debug"]
  },
  "PayloadModelRule": {
    "name": "model pattern or wildcard",
    "protocol": "translator protocol"
  }
}
```
