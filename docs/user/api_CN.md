# CLIProxyAPIHome User API 文档

本文档描述 CLIProxyAPIHome 当前 DB-backed User API。User API 与 Management API 分离，不使用 Management API secret key。

基础路径：

```text
http://<host>:<port>/user
```

Home 示例端口通常为 `8327`。实际监听地址来自 runtime config、`cluster.yaml` 或 `-addr` 的最终值。

## Runtime 模型

用户记录、API key、TOTP 配置和 passkey entries 存储在 database-backed cluster repository 中。`/user/*` route group 只会在 Home 使用 database-backed runtime route set 时注册。

通过 User API 修改 API key 会更新 Home dispatch 使用的同一张 `api_key` 表，并发布 config refresh event。

## 认证

公开 routes：

| Method | Path | 说明 |
| --- | --- | --- |
| `POST` | `/register` | 创建用户并返回 bearer token。 |
| `POST` | `/login` | 用户未启用 passkey 和 TOTP 时的密码登录。 |
| `POST` | `/login/totp` | 用户启用 TOTP 时的密码 + TOTP 登录。 |
| `POST` | `/login/passkey` | Passkey 登录。 |

其他所有 `/user/*` route 都需要注册或登录成功后返回的 bearer token。
Bearer token 是使用集群根 CA 私钥签名、并使用集群根 CA 公钥验证的 RS256 JWT。替换集群根 CA 后，之前签发的 User API token 会失效。
密码会按照原始输入值进行 hash 和校验，不会去除首尾空白字符。

支持的请求头：

| Header | Value |
| --- | --- |
| `Authorization` | `Bearer <USER_TOKEN>` |

登录优先级：

| 状态 | 行为 |
| --- | --- |
| 已启用 passkey 且已启用 TOTP | 普通密码登录返回 `401 passkey_required`；`/login/passkey` 和 `/login/totp` 都可登录。 |
| 已启用 passkey 但未启用 TOTP | 普通密码登录返回 `401 passkey_required`；使用 `/login/passkey`。 |
| 未启用 passkey 但已启用 TOTP | 普通密码登录返回 `401 totp_required`；使用 `/login/totp`。 |
| 未启用 passkey 且未启用 TOTP | 普通密码登录返回 bearer token。 |

Home User API 会额外写入以下响应头：

| Header | 说明 |
| --- | --- |
| `x-cpa-home-version` | Home 构建版本。 |
| `x-cpa-home-commit` | Home 构建 commit。 |
| `x-cpa-home-build-date` | Home 构建日期。 |

## 通用响应

多数删除或简单写入接口成功时返回：

```json
{ "status": "ok" }
```

注册和登录成功时返回：

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

User API handler 通常同时返回机器可读 `error` 和可读 `message`：

```json
{ "error": "invalid_credentials", "message": "invalid credentials" }
```

常见错误：

```json
{ "error": "bearer_token_required", "message": "bearer token is required" }
{ "error": "invalid_token", "message": "invalid token" }
{ "error": "passkey_required", "message": "passkey is required" }
{ "error": "totp_required", "message": "totp code is required" }
{ "error": "invalid_body", "message": "username is required" }
```

## 已注册 Routes

以下清单来自 `internal/userapi/handler.go` 注册的 User API route group。

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

## 账号

### POST `/register`

创建用户账号，保存 bcrypt password hash，并返回 bearer token。

示例请求：

```json
{
  "username": "alice",
  "password": "secret"
}
```

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `username` | string | yes | 用户名。Aliases: `user_name`, `user-name`。 |
| `password` | string | yes | 用于生成存储密码 hash 的明文密码。 |

响应：登录响应结构。

常见错误：

```json
{ "error": "user_exists", "message": "user already exists" }
{ "error": "invalid_body", "message": "password is required" }
```

### POST `/login`

当用户未启用 passkey 且未启用 TOTP 时，使用用户名和密码登录。

示例请求：

```json
{
  "username": "alice",
  "password": "secret"
}
```

响应：登录响应结构。

常见错误：

```json
{ "error": "invalid_credentials", "message": "invalid credentials" }
{ "error": "passkey_required", "message": "passkey is required" }
{ "error": "totp_required", "message": "totp code is required" }
```

### POST `/login/totp`

使用用户名、密码和 TOTP code 登录。只要用户启用了 TOTP，即使同时有 passkey，也可以使用此 route 登录。仅启用 passkey 的用户调用此 route 会返回 `401 passkey_required`。

示例请求：

```json
{
  "username": "alice",
  "password": "secret",
  "totp_code": "123456"
}
```

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `username` | string | yes | 用户名。Aliases: `user_name`, `user-name`。 |
| `password` | string | yes | 用户密码。 |
| `totp_code` | string | yes | TOTP code。Aliases: `totp-code`, `totp`, `code`。 |

响应：登录响应结构。

常见错误：

```json
{ "error": "passkey_required", "message": "passkey is required" }
{ "error": "totp_not_enabled", "message": "totp is not enabled" }
{ "error": "invalid_totp", "message": "invalid totp code" }
```

### POST `/login/passkey`

使用存储在 `user.passkey` 中的 passkey entry 登录。

示例请求：

```json
{
  "username": "alice",
  "passkey_id": "pk_xxx",
  "passkey_secret": "one-time-returned-secret"
}
```

如果 passkey 是用 `credential` payload 创建的，此 route 也可以对比 opaque JSON `credential` 字段。

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `username` | string | yes | 用户名。Aliases: `user_name`, `user-name`。 |
| `passkey_id` | string | yes | Passkey ID。Alias: `passkey-id`；也接受 `id`。 |
| `passkey_secret` | string | conditionally | 创建 passkey 时返回的 secret。Alias: `secret`。 |
| `credential` | object | conditionally | 未存储 secret hash 时用于精确比较的 opaque credential JSON。 |

响应：登录响应结构。

常见错误：

```json
{ "error": "invalid_passkey", "message": "invalid passkey" }
{ "error": "invalid_body", "message": "username and passkey_id are required" }
```

### GET `/me`

根据 bearer token 返回当前认证用户的信息。

请求头：

```http
Authorization: Bearer user.jwt.token
```

示例响应：

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

常见错误：

```json
{ "error": "bearer_token_required", "message": "bearer token is required" }
{ "error": "invalid_token", "message": "invalid token" }
```

### POST/PATCH `/password`

修改当前认证用户的密码。

请求头：

```http
Authorization: Bearer user.jwt.token
```

示例请求：

```json
{
  "new_password": "new-secret"
}
```

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `new_password` | string | yes | 新明文密码。Alias: `new-password`。 |

示例响应：

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

返回当前认证用户的 TOTP setup data。如果 TOTP 已启用且 `regenerate` 不是 true，则返回现有 secret。

请求头：

```http
Authorization: Bearer user.jwt.token
```

Query 参数：

| Query | Type | 说明 |
| --- | --- | --- |
| `issuer` | string | otpauth URL 的可选 issuer。 |
| `regenerate` | boolean | 生成新的 setup secret，而不是返回当前 secret。 |

示例响应：

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

`GET /totp` 的 POST alias。接受同样的 bearer token 和可选 JSON 字段。

示例请求：

```json
{
  "issuer": "CLIProxyAPIHome",
  "regenerate": true
}
```

响应：同 `GET /totp`。

### POST `/totp`

校验并保存当前认证用户的 TOTP secret。

请求头：

```http
Authorization: Bearer user.jwt.token
```

示例请求：

```json
{
  "secret": "BASE32SECRET",
  "code": "123456"
}
```

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `secret` | string | yes | 来自 `/totp` 的 Base32 TOTP secret。 |
| `code` | string | yes | 当前 TOTP code。Aliases: `totp_code`, `totp-code`, `totp`。 |
| `issuer` | string | no | 存入 TOTP metadata 的 issuer。默认为 `CLIProxyAPIHome`。 |

响应：同 `POST/PATCH /password`。

常见错误：

```json
{ "error": "invalid_totp", "message": "invalid totp code" }
{ "error": "invalid_body", "message": "secret and code are required" }
```

### POST `/totp/bind`

`POST /totp` 的 alias。

### DELETE `/totp`

删除当前认证用户的 TOTP 配置。

请求头：

```http
Authorization: Bearer user.jwt.token
```

请求体：无。

响应：同 `POST/PATCH /password`；成功删除后 `user.totp_enabled` 为 `false`。

常见错误：

```json
{ "error": "bearer_token_required", "message": "bearer token is required" }
{ "error": "invalid_token", "message": "invalid token" }
```

## Billing

用户计费路由位于 `/user` 基础路径下，因此完整路径是 `/user/billing/overview` 和 `/user/billing/charges`。两个路由都需要 `/user/register` 或 `/user/login` 返回的现有 Bearer token，响应只包含当前认证 Bearer 用户的数据。

用户计费响应不包含管理员备注、全局汇总、模型价格管理数据、代理池数据、原始 API keys、脱敏 API keys、价格快照、匹配的价格规则、endpoint、`balance_before` 或其他用户的数据。

用户计费的 `from` 和 `to` 查询参数接受 `YYYY-MM-DD`、RFC3339 或 Unix 秒，并统一使用半开区间 `[from,to)`。Unix 秒值必须位于 `2000-01-01T00:00:00Z` 到 `9999-12-31T23:59:59Z` 之间；毫秒时间戳会被拒绝。只有日期的 `to` 会转换为下一个 UTC 零点，从而完整包含结束 UTC 日期。显式时间戳形式的 `to` 是精确的排他上界，不会自动扩展。需要查询完整非 UTC 自然日的客户端应发送从本地零点到下一个本地零点的 RFC3339 边界，例如 `2026-06-10T00:00:00+08:00` 到 `2026-06-11T00:00:00+08:00`。

### GET `/billing/overview`

返回当前认证用户的计费概览。

请求头：

```http
Authorization: Bearer user.jwt.token
```

查询参数：

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `from` | string | 可选开始时间：`YYYY-MM-DD`、RFC3339 或 Unix 秒。 |
| `to` | string | 可选排他结束时间；纯日期通过使用下一个 UTC 零点完整包含当天，显式时间戳精确保留。 |

响应示例：

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

概览字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `current_balance` | number | 当前认证用户余额。 |
| `today_spend` | number | 当前计费概览查询返回的消费值。 |
| `month_spend` | number | 当前计费概览查询返回的消费值。 |
| `top_models[]` | array | 模型消费条目，字段为 `id`、`label`、`amount`、`request_count`。 |

### GET `/billing/charges`

列出当前认证用户的扣费记录。

请求头：

```http
Authorization: Bearer user.jwt.token
```

查询参数：

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `from` | string | 可选开始时间：`YYYY-MM-DD`、RFC3339 或 Unix 秒。 |
| `to` | string | 可选排他结束时间；纯日期通过使用下一个 UTC 零点完整包含当天，显式时间戳精确保留。 |
| `limit` | integer | 可选分页大小；默认 `50`，最大 `200`。非法的非正数或非整数会返回 `400`。 |
| `offset` | integer | 可选分页偏移；默认 `0`。负数或非整数会返回 `400`。 |

响应结构：

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

扣费条目字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | 扣费记录 ID。 |
| `created_at` | string | 扣费创建时间。 |
| `provider` | string | Provider 名称。 |
| `model` | string | Model 名称。 |
| `input_tokens` | integer | 输入 tokens。 |
| `output_tokens` | integer | 输出 tokens。 |
| `amount` | number | 扣费金额。 |
| `balance_after` | number | 当前认证用户扣费后的余额。 |
| `request_id` | string | 与扣费关联的 request ID。 |

常见错误：

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

API key routes 只操作绑定到当前认证 `user.id` 的 API key。

### GET `/api-keys`

列出当前认证用户拥有的 API key。

请求头：

```http
Authorization: Bearer user.jwt.token
```

示例响应：

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

创建归属于当前认证用户的 API key。如果省略 `api_key`，Home 会自动生成一个。

请求头：

```http
Authorization: Bearer user.jwt.token
```

示例请求：

```json
{
  "api_key": "client-key",
  "channels": [1],
  "model_groups": [2]
}
```

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `api_key` | string | no | Client API key。Aliases: `api-key`, `key`, `value`。 |
| `channels` | array of integer | no | Channel group IDs。空数组或省略表示不限制。 |
| `model_groups` | array of integer | no | Model group IDs。Alias: `model-groups`；空数组或省略表示不限制。 |

示例响应：

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

`POST /api-keys` 的 alias。

### PATCH `/api-keys`

修改当前认证用户拥有的 API key。目标可以通过 `id` 或 API key value 指定。

示例请求：

```json
{
  "id": 1,
  "api_key": "new-client-key",
  "channels": [],
  "model_groups": []
}
```

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `id` | integer | conditionally | API key record ID。 |
| `api_key` | string | conditionally | 提供 `id` 时表示新的 API key；没有 `id` 时表示目标 API key。Aliases: `api-key`, `key`, `value`。 |
| `old` | string | conditionally | 目标 API key value。 |
| `new` | string | no | 新 API key value。 |
| `new_api_key` | string | no | 新 API key value。Alias: `new-api-key`。 |
| `channels` | array of integer | no | 替换 channel group IDs。 |
| `model_groups` | array of integer | no | 替换 model group IDs。Alias: `model-groups`。 |

响应：同 `POST /api-keys`。

常见错误：

```json
{ "error": "not_found", "message": "record not found" }
{ "error": "invalid_body", "message": "api key id or value is required" }
{ "error": "api_key_exists", "message": "api key already exists" }
```

### PATCH `/api-key`

`PATCH /api-keys` 的 alias。

### PATCH `/api-keys/:id`

按 numeric record ID 修改当前认证用户拥有的 API key。

Path 参数：

| Parameter | Type | Required | 说明 |
| --- | --- | --- | --- |
| `id` | integer | yes | `api_key.id`；必须大于 `0`。 |

响应：同 `POST /api-keys`。

### PATCH `/api-key/:id`

`PATCH /api-keys/:id` 的 alias。

### DELETE `/api-keys`

删除当前认证用户拥有的 API key。目标可以通过 `id` 或 API key value 指定。

Query 参数：

| Query | Type | 说明 |
| --- | --- | --- |
| `id` | integer | API key record ID。 |
| `api_key` | string | API key value。 |
| `api-key` | string | `api_key` 的 alias。 |
| `key` | string | `api_key` 的 alias。 |
| `value` | string | `api_key` 的 alias。 |

示例响应：

```json
{ "status": "ok" }
```

### DELETE `/api-key`

`DELETE /api-keys` 的 alias。

### DELETE `/api-keys/:id`

按 numeric record ID 删除当前认证用户拥有的 API key。

Path 参数：

| Parameter | Type | Required | 说明 |
| --- | --- | --- | --- |
| `id` | integer | yes | `api_key.id`；必须大于 `0`。 |

响应：

```json
{ "status": "ok" }
```

### DELETE `/api-key/:id`

`DELETE /api-keys/:id` 的 alias。

## Passkeys

Passkey routes 操作存储在 `user.passkey` 中的 entries。

### POST `/passkeys`

为当前认证用户创建 passkey entry。如果没有提供 `id`，Home 会自动生成。如果没有提供 `secret` 或 `credential`，Home 会生成一次性 secret，并且只在本次响应中返回。

请求头：

```http
Authorization: Bearer user.jwt.token
```

示例请求：

```json
{
  "name": "Laptop"
}
```

字段：

| Field | Type | Required | 说明 |
| --- | --- | --- | --- |
| `id` | string | no | Passkey ID。Aliases: `passkey_id`, `passkey-id`。 |
| `name` | string | no | 显示名称。 |
| `secret` | string | no | 用于 hash 并存储的 passkey 登录 secret。Alias: `passkey_secret`。 |
| `credential` | object | no | 用于 passkey 登录比较的 opaque credential JSON。 |

示例响应：

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

常见错误：

```json
{ "error": "passkey_exists", "message": "passkey already exists" }
```

### POST `/passkey`

`POST /passkeys` 的 alias。

### DELETE `/passkeys`

删除当前认证用户的 passkey entry。

Query 参数：

| Query | Type | 说明 |
| --- | --- | --- |
| `id` | string | Passkey ID。 |

示例响应：

```json
{ "status": "ok" }
```

常见错误：

```json
{ "error": "not_found", "message": "passkey not found" }
{ "error": "invalid_body", "message": "passkey id is required" }
```

### DELETE `/passkey`

`DELETE /passkeys` 的 alias。

### DELETE `/passkeys/:id`

按 ID 删除当前认证用户的 passkey entry。

Path 参数：

| Parameter | Type | Required | 说明 |
| --- | --- | --- | --- |
| `id` | string | yes | Passkey ID。 |

响应：

```json
{ "status": "ok" }
```

### DELETE `/passkey/:id`

`DELETE /passkeys/:id` 的 alias。
