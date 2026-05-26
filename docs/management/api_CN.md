# CLIProxyAPIHome Management API 文档

本文档只描述 CLIProxyAPIHome 提供的 Management API，覆盖 Home standalone 模式和 Home cluster 模式，只包含 Home 接口。

基础路径：

```text
http://<host>:<port>/v0/management
```

可选管理面板：

```text
GET /management.html
```

Home 示例端口通常为 `8327`。Cluster 模式下，实际监听地址来自 `cluster.yaml` 或 `-addr` 的最终值。

## 运行模式

| 模式 | 存储后端 | 说明 |
| --- | --- | --- |
| Home standalone | 本地 `config.yaml` 和本地 `auth-dir` 文件。 | 暴露 SDK-backed Home Management API，并额外提供 `GET /nodes`。 |
| Home cluster | Cluster repository，通常是 PostgreSQL。 | 暴露 DB-backed config、users、API keys、auth records、channel groups、model groups、证书注册和节点接口；runtime/log file 查询接口不注册。 |

## 认证

所有 `/v0/management/*` route 都需要 management key。

支持的请求头：

| Header | Value |
| --- | --- |
| `Authorization` | `Bearer <MANAGEMENT_KEY>` 或原始 `<MANAGEMENT_KEY>` |
| `X-Management-Key` | `<MANAGEMENT_KEY>` |

访问规则：

| 规则 | 行为 |
| --- | --- |
| 本地请求 | 仍需要有效 management key。 |
| 远程请求 | 需要启用远程管理，例如 `remote-management.allow-remote: true`，或者内部 override。 |
| API 未启用 | 未设置 `remote-management.secret-key` 且未设置 `MANAGEMENT_PASSWORD` 时，Management API route 通常返回 `404`。 |
| 失败封禁 | 同一客户端 IP 连续 5 次失败后封禁 30 分钟；封禁期间即使 key 正确也会失败。 |

常见认证错误：

```json
{ "error": "missing management key" }
{ "error": "invalid management key" }
{ "error": "remote management disabled" }
{ "error": "remote management key not set" }
{ "error": "IP banned due to too many failed attempts. Try again in 29m59s" }
```

Home 管理接口会额外写入以下响应头：

| Header | 说明 |
| --- | --- |
| `x-cpa-home-version` | Home 构建版本。 |
| `x-cpa-home-commit` | Home 构建 commit。 |
| `x-cpa-home-build-date` | Home 构建日期。 |

## 通用响应

多数写入接口成功时返回：

```json
{ "status": "ok" }
```

完整配置替换成功时返回：

```json
{ "ok": true, "changed": ["config"] }
```

Cluster DB-backed handler 通常同时返回机器可读 `error` 和可读 `message`：

```json
{ "error": "invalid body", "message": "username is required" }
```

其他常见错误结构：

```json
{ "error": "invalid body" }
{ "error": "invalid_config", "message": "validation detail" }
```

## Route 可用性

以下清单来自当前 `internal/managementhttp/server.go` 中的 Home route registry。

| Method | Path | Standalone | Cluster |
| --- | --- | --- | --- |
| `GET` | `/ampcode` | 是 | 是 |
| `GET` | `/ampcode/force-model-mappings` | 是 | 是 |
| `PATCH` | `/ampcode/force-model-mappings` | 是 | 是 |
| `PUT` | `/ampcode/force-model-mappings` | 是 | 是 |
| `DELETE` | `/ampcode/model-mappings` | 是 | 是 |
| `GET` | `/ampcode/model-mappings` | 是 | 是 |
| `PATCH` | `/ampcode/model-mappings` | 是 | 是 |
| `PUT` | `/ampcode/model-mappings` | 是 | 是 |
| `GET` | `/ampcode/restrict-management-to-localhost` | 是 | 是 |
| `PATCH` | `/ampcode/restrict-management-to-localhost` | 是 | 是 |
| `PUT` | `/ampcode/restrict-management-to-localhost` | 是 | 是 |
| `DELETE` | `/ampcode/upstream-api-key` | 是 | 是 |
| `GET` | `/ampcode/upstream-api-key` | 是 | 是 |
| `PATCH` | `/ampcode/upstream-api-key` | 是 | 是 |
| `PUT` | `/ampcode/upstream-api-key` | 是 | 是 |
| `DELETE` | `/ampcode/upstream-api-keys` | 是 | 是 |
| `GET` | `/ampcode/upstream-api-keys` | 是 | 是 |
| `PATCH` | `/ampcode/upstream-api-keys` | 是 | 是 |
| `PUT` | `/ampcode/upstream-api-keys` | 是 | 是 |
| `DELETE` | `/ampcode/upstream-url` | 是 | 是 |
| `GET` | `/ampcode/upstream-url` | 是 | 是 |
| `PATCH` | `/ampcode/upstream-url` | 是 | 是 |
| `PUT` | `/ampcode/upstream-url` | 是 | 是 |
| `GET` | `/anthropic-auth-url` | 是 | 是 |
| `GET` | `/antigravity-auth-url` | 是 | 是 |
| `POST` | `/api-call` | 是 | 是 |
| `GET` | `/api-key-usage` | 是 | 否 |
| `DELETE` | `/api-keys` | 是 | 是 |
| `GET` | `/api-keys` | 是 | 是 |
| `PATCH` | `/api-keys` | 是 | 是 |
| `PUT` | `/api-keys` | 是 | 是 |
| `DELETE` | `/auth-files` | 是 | 是 |
| `GET` | `/auth-files` | 是 | 是 |
| `POST` | `/auth-files` | 是 | 是 |
| `GET` | `/auth-files/download` | 是 | 是 |
| `PATCH` | `/auth-files/fields` | 是 | 是 |
| `GET` | `/auth-files/models` | 是 | 是 |
| `PATCH` | `/auth-files/status` | 是 | 是 |
| `POST` | `/certificates/clients` | 否 | 是 |
| `GET` | `/channel-group-details` | 否 | 是 |
| `POST` | `/channel-group-details` | 否 | 是 |
| `DELETE` | `/channel-group-details/:id` | 否 | 是 |
| `GET` | `/channel-group-details/:id` | 否 | 是 |
| `PATCH` | `/channel-group-details/:id` | 否 | 是 |
| `PUT` | `/channel-group-details/:id` | 否 | 是 |
| `GET` | `/channel-groups` | 否 | 是 |
| `POST` | `/channel-groups` | 否 | 是 |
| `DELETE` | `/channel-groups/:id` | 否 | 是 |
| `GET` | `/channel-groups/:id` | 否 | 是 |
| `PATCH` | `/channel-groups/:id` | 否 | 是 |
| `PUT` | `/channel-groups/:id` | 否 | 是 |
| `DELETE` | `/claude-api-key` | 是 | 是 |
| `GET` | `/claude-api-key` | 是 | 是 |
| `PATCH` | `/claude-api-key` | 是 | 是 |
| `PUT` | `/claude-api-key` | 是 | 是 |
| `DELETE` | `/codex-api-key` | 是 | 是 |
| `GET` | `/codex-api-key` | 是 | 是 |
| `PATCH` | `/codex-api-key` | 是 | 是 |
| `PUT` | `/codex-api-key` | 是 | 是 |
| `GET` | `/codex-auth-url` | 是 | 是 |
| `GET` | `/config` | 是 | 是 |
| `GET` | `/config.yaml` | 是 | 是 |
| `PUT` | `/config.yaml` | 是 | 是 |
| `GET` | `/debug` | 是 | 是 |
| `PATCH` | `/debug` | 是 | 是 |
| `PUT` | `/debug` | 是 | 是 |
| `GET` | `/error-logs-max-files` | 是 | 是 |
| `PATCH` | `/error-logs-max-files` | 是 | 是 |
| `PUT` | `/error-logs-max-files` | 是 | 是 |
| `GET` | `/force-model-prefix` | 是 | 是 |
| `PATCH` | `/force-model-prefix` | 是 | 是 |
| `PUT` | `/force-model-prefix` | 是 | 是 |
| `DELETE` | `/gemini-api-key` | 是 | 是 |
| `GET` | `/gemini-api-key` | 是 | 是 |
| `PATCH` | `/gemini-api-key` | 是 | 是 |
| `PUT` | `/gemini-api-key` | 是 | 是 |
| `GET` | `/gemini-cli-auth-url` | 是 | 是 |
| `GET` | `/get-auth-status` | 是 | 是 |
| `GET` | `/kimi-auth-url` | 是 | 是 |
| `GET` | `/latest-version` | 是 | 是 |
| `GET` | `/logging-to-file` | 是 | 是 |
| `PATCH` | `/logging-to-file` | 是 | 是 |
| `PUT` | `/logging-to-file` | 是 | 是 |
| `DELETE` | `/logs` | 是 | 否 |
| `GET` | `/logs` | 是 | 否 |
| `GET` | `/logs-max-total-size-mb` | 是 | 是 |
| `PATCH` | `/logs-max-total-size-mb` | 是 | 是 |
| `PUT` | `/logs-max-total-size-mb` | 是 | 是 |
| `GET` | `/max-retry-interval` | 是 | 是 |
| `PATCH` | `/max-retry-interval` | 是 | 是 |
| `PUT` | `/max-retry-interval` | 是 | 是 |
| `GET` | `/model-definitions/:channel` | 是 | 是 |
| `GET` | `/model-group-details` | 否 | 是 |
| `POST` | `/model-group-details` | 否 | 是 |
| `DELETE` | `/model-group-details/:id` | 否 | 是 |
| `GET` | `/model-group-details/:id` | 否 | 是 |
| `PATCH` | `/model-group-details/:id` | 否 | 是 |
| `PUT` | `/model-group-details/:id` | 否 | 是 |
| `GET` | `/model-groups` | 否 | 是 |
| `POST` | `/model-groups` | 否 | 是 |
| `DELETE` | `/model-groups/:id` | 否 | 是 |
| `GET` | `/model-groups/:id` | 否 | 是 |
| `PATCH` | `/model-groups/:id` | 否 | 是 |
| `PUT` | `/model-groups/:id` | 否 | 是 |
| `GET` | `/nodes` | 是 | 是 |
| `POST` | `/oauth-callback` | 是 | 是 |
| `DELETE` | `/oauth-excluded-models` | 是 | 是 |
| `GET` | `/oauth-excluded-models` | 是 | 是 |
| `PATCH` | `/oauth-excluded-models` | 是 | 是 |
| `PUT` | `/oauth-excluded-models` | 是 | 是 |
| `DELETE` | `/oauth-model-alias` | 是 | 是 |
| `GET` | `/oauth-model-alias` | 是 | 是 |
| `PATCH` | `/oauth-model-alias` | 是 | 是 |
| `PUT` | `/oauth-model-alias` | 是 | 是 |
| `DELETE` | `/openai-compatibility` | 是 | 是 |
| `GET` | `/openai-compatibility` | 是 | 是 |
| `PATCH` | `/openai-compatibility` | 是 | 是 |
| `PUT` | `/openai-compatibility` | 是 | 是 |
| `DELETE` | `/payload` | 否 | 是 |
| `GET` | `/payload` | 否 | 是 |
| `PATCH` | `/payload` | 否 | 是 |
| `PUT` | `/payload` | 否 | 是 |
| `DELETE` | `/proxy-url` | 是 | 是 |
| `GET` | `/proxy-url` | 是 | 是 |
| `PATCH` | `/proxy-url` | 是 | 是 |
| `PUT` | `/proxy-url` | 是 | 是 |
| `GET` | `/quota-exceeded/switch-preview-model` | 是 | 是 |
| `PATCH` | `/quota-exceeded/switch-preview-model` | 是 | 是 |
| `PUT` | `/quota-exceeded/switch-preview-model` | 是 | 是 |
| `GET` | `/quota-exceeded/switch-project` | 是 | 是 |
| `PATCH` | `/quota-exceeded/switch-project` | 是 | 是 |
| `PUT` | `/quota-exceeded/switch-project` | 是 | 是 |
| `GET` | `/request-error-logs` | 是 | 否 |
| `GET` | `/request-error-logs/:name` | 是 | 否 |
| `GET` | `/request-log` | 是 | 是 |
| `PATCH` | `/request-log` | 是 | 是 |
| `PUT` | `/request-log` | 是 | 是 |
| `GET` | `/request-log-by-id/:id` | 是 | 否 |
| `GET` | `/request-retry` | 是 | 是 |
| `PATCH` | `/request-retry` | 是 | 是 |
| `PUT` | `/request-retry` | 是 | 是 |
| `GET` | `/routing/strategy` | 是 | 是 |
| `PATCH` | `/routing/strategy` | 是 | 是 |
| `PUT` | `/routing/strategy` | 是 | 是 |
| `GET` | `/usage-queue` | 是 | 否 |
| `GET` | `/usage-statistics-enabled` | 是 | 是 |
| `PATCH` | `/usage-statistics-enabled` | 是 | 是 |
| `PUT` | `/usage-statistics-enabled` | 是 | 是 |
| `GET` | `/users` | 否 | 是 |
| `POST` | `/users` | 否 | 是 |
| `DELETE` | `/users/:id` | 否 | 是 |
| `GET` | `/users/:id` | 否 | 是 |
| `PATCH` | `/users/:id` | 否 | 是 |
| `PUT` | `/users/:id` | 否 | 是 |
| `DELETE` | `/vertex-api-key` | 是 | 是 |
| `GET` | `/vertex-api-key` | 是 | 是 |
| `PATCH` | `/vertex-api-key` | 是 | 是 |
| `PUT` | `/vertex-api-key` | 是 | 是 |
| `POST` | `/vertex/import` | 是 | 是 |
| `GET` | `/xai-auth-url` | 否 | 是 |

## 配置接口

### GET `/config`

返回当前 runtime config JSON。

输入：无。

输出示例：

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

带 `json:"-"` 的字段不会出现在响应中。Home 会隐藏 `host`、`port`、`allow-host`、`remote-management` 和 `auth-dir`。

### GET `/config.yaml`

返回当前 YAML 配置。

输入：无。

响应 Content-Type：

```text
application/yaml; charset=utf-8
```

Standalone 模式返回本地 `config.yaml` 原始字节，保留注释和格式。Cluster 模式从 cluster config snapshot 重新生成 YAML，不保留原注释和格式。

### PUT `/config.yaml`

替换完整配置。

输入：请求体为完整 YAML 文档。

Cluster 模式会在持久化 cluster config snapshot 前移除 credential roots，这些配置应通过 provider key 或 auth file 专用接口管理：

```text
auth-dir
gemini-api-key
vertex-api-key
codex-api-key
claude-api-key
openai-compatibility
```

输出示例：

```json
{ "ok": true, "changed": ["config"] }
```

### 简单配置 Leaf Routes

Standalone 模式写入本地 YAML。Cluster 模式写入 cluster repository 中对应 config root，并 reload Home runtime。

| Method | Path | 输入 | 输出 |
| --- | --- | --- | --- |
| `GET` | `/debug` | 无 | `{ "debug": boolean }` |
| `PUT/PATCH` | `/debug` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/logging-to-file` | 无 | `{ "logging-to-file": boolean }` |
| `PUT/PATCH` | `/logging-to-file` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/logs-max-total-size-mb` | 无 | `{ "logs-max-total-size-mb": number }` |
| `PUT/PATCH` | `/logs-max-total-size-mb` | `{ "value": number }`；负数保存为 `0` | `{ "status": "ok" }` |
| `GET` | `/error-logs-max-files` | 无 | `{ "error-logs-max-files": number }` |
| `PUT/PATCH` | `/error-logs-max-files` | `{ "value": number }`；负数保存为 `10` | `{ "status": "ok" }` |
| `GET` | `/usage-statistics-enabled` | 无 | `{ "usage-statistics-enabled": boolean }` |
| `PUT/PATCH` | `/usage-statistics-enabled` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/proxy-url` | 无 | `{ "proxy-url": string }` |
| `PUT/PATCH` | `/proxy-url` | `{ "value": string }` | `{ "status": "ok" }` |
| `DELETE` | `/proxy-url` | 无 | `{ "status": "ok" }` |
| `GET` | `/request-log` | 无 | `{ "request-log": boolean }` |
| `PUT/PATCH` | `/request-log` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/request-retry` | 无 | `{ "request-retry": number }` |
| `PUT/PATCH` | `/request-retry` | `{ "value": number }` | `{ "status": "ok" }` |
| `GET` | `/max-retry-interval` | 无 | `{ "max-retry-interval": number }` |
| `PUT/PATCH` | `/max-retry-interval` | `{ "value": number }` | `{ "status": "ok" }` |
| `GET` | `/force-model-prefix` | 无 | `{ "force-model-prefix": boolean }` |
| `PUT/PATCH` | `/force-model-prefix` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/routing/strategy` | 无 | `{ "strategy": "round-robin" }` 或 `{ "strategy": "fill-first" }` |
| `PUT/PATCH` | `/routing/strategy` | `{ "value": "round-robin" }`、`roundrobin`、`rr`、`fill-first`、`fillfirst`、`ff` | `{ "status": "ok" }` |
| `GET` | `/quota-exceeded/switch-project` | 无 | `{ "switch-project": boolean }` |
| `PUT/PATCH` | `/quota-exceeded/switch-project` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/quota-exceeded/switch-preview-model` | 无 | `{ "switch-preview-model": boolean }` |
| `PUT/PATCH` | `/quota-exceeded/switch-preview-model` | `{ "value": boolean }` | `{ "status": "ok" }` |

### `/payload` Cluster Config Root

可用性：仅 cluster。

`GET /payload` 输出：

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

`PUT /payload` 和 `PATCH /payload` 接受原始 payload object、`{ "value": <payload> }` 或 `{ "payload": <payload> }`。

`DELETE /payload` 从 config snapshot 删除该 root。

写入成功返回：

```json
{ "status": "ok" }
```

## 节点、版本和证书

### GET `/nodes`

列出当前连接到 Home 的节点。

输入：无。

输出示例：

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

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `nodes` | array | 当前活跃节点列表。 |
| `nodes[].ip` | string | 节点 IP 地址。 |
| `nodes[].connected_time` | string | 当前活跃节点条目的首次连接时间。 |

### GET `/latest-version`

通过 GitHub release API 获取 CLIProxyAPIHome 最新版本；配置 `proxy-url` 时会使用该代理。

输入：无。

输出示例：

```json
{ "latest-version": "v7.0.0" }
```

常见错误码：

```json
{ "error": "request_create_failed", "message": "detail" }
{ "error": "request_failed", "message": "detail" }
{ "error": "unexpected_status", "message": "status 502: detail" }
{ "error": "decode_failed", "message": "detail" }
{ "error": "invalid_response", "message": "missing release version" }
```

### POST `/certificates/clients`

可用性：仅 cluster。

创建 pending client certificate enrollment record，并返回节点完成证书注册所需的 Home JWT。

输入：无。

输出示例：

```json
{
  "id": "cert-uuid",
  "home_jwt": "eyJhbGciOi..."
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | Pending client certificate ID。 |
| `home_jwt` | string | 包含 Home target 信息和 enrollment secret 的注册 JWT。 |

常见错误：

```json
{ "error": "cluster_unavailable", "message": "cluster_unavailable" }
{ "error": "certificate_jwt_target_invalid", "message": "certificate_jwt_target_invalid" }
{ "error": "certificate_create_failed", "message": "detail" }
{ "error": "certificate_jwt_failed", "message": "detail" }
```

## Users

可用性：仅 cluster。

User records 存储在 cluster repository 中。

### GET `/users`

列出用户。

输入：无。

输出示例：

```json
{
  "users": [
    {
      "id": 1,
      "username": "alice",
      "password": "stored-password",
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

按数字 ID 读取单个用户。

Path 参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `id` | integer | 是 | `user.id`；必须大于 `0`。 |

输出示例：

```json
{
  "user": {
    "id": 1,
    "username": "alice",
    "password": "stored-password",
    "mfa": { "enabled": true },
    "passkey": [{ "id": "credential-id" }],
    "created_at": "2026-05-27T10:00:00Z",
    "updated_at": "2026-05-27T10:00:00Z",
    "deleted_at": null
  }
}
```

### POST `/users`

创建用户。

输入示例：

```json
{
  "username": "alice",
  "password": "stored-password",
  "mfa": { "enabled": true },
  "passkey": [{ "id": "credential-id" }]
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `username` | string | 是 | 用户名；也接受 `user_name`、`user-name`。 |
| `password` | string | 否 | 按请求值存储并通过 API 回显。 |
| `mfa` | any valid JSON | 否 | 存入 `user.mfa`。 |
| `passkey` | any valid JSON | 否 | 存入 `user.passkey`。 |

输出：与 `GET /users/:id` 相同。

### PUT/PATCH `/users/:id`

更新用户。`PUT` 和 `PATCH` 当前都是局部更新语义：只修改请求体中出现的字段。

输入示例：

```json
{
  "username": "alice-updated",
  "password": "new-stored-password",
  "mfa": { "enabled": false },
  "passkey": []
}
```

所有字段均可选；如果出现 `username`，则不能为空。

输出：与 `GET /users/:id` 相同。

### DELETE `/users/:id`

软删除用户。

输入：无 body。

输出示例：

```json
{ "status": "ok" }
```

常见错误：

```json
{ "error": "not_found", "message": "record not found" }
{ "error": "invalid body", "message": "username is required" }
```

## 客户端 API Keys

### GET `/api-keys`

返回 Home 接受的客户端 API keys。

输入：无。

Standalone 输出：

```json
{ "api-keys": ["client-key-1", "client-key-2"] }
```

Cluster 输出：

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

Cluster 字段说明：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `api-keys` | array of string | 兼容旧客户端的原始 key 列表。 |
| `items` | array of `APIKeyEntry` | 结构化 API key records。 |
| `api_key_entries` | array of `APIKeyEntry` | `items` 的 alias。 |
| `APIKeyEntry.api-key` | string | 客户端 API key。 |
| `APIKeyEntry.api_key` | string | `api-key` 的 alias。 |
| `APIKeyEntry.user-id` | integer or null | 绑定的 `user.id`；`null` 表示未绑定。 |
| `APIKeyEntry.user_id` | integer or null | `user-id` 的 alias。 |
| `APIKeyEntry.channels` | array of integer | 绑定的 channel group IDs；空数组表示不限制。 |
| `APIKeyEntry.model_groups` | array of integer | 绑定的 model group IDs；空数组表示不限制。 |

### PUT `/api-keys`

整体替换客户端 API key 列表。

Standalone 输入：

```json
["client-key-1", "client-key-2"]
```

或：

```json
{ "items": ["client-key-1", "client-key-2"] }
```

Cluster 还接受结构化 entry。Wrapper key 可以是 `items`、`api-keys`、`api_keys` 或 `api_key_entries`：

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

Cluster entry 字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `api_key` | string | 条件必填 | 客户端 API key；也接受 `api-key`、`key`、`value`。 |
| `user_id` | integer | 否 | 绑定的 `user.id`；也接受 `user-id`。 |
| `channels` | array of integer | 否 | Channel group IDs。 |
| `model_groups` | array of integer | 否 | Model group IDs；也接受 `model-groups`。 |

如果 `user_id` 引用不存在的用户，cluster 模式返回 `404 user_not_found`。

成功输出：

```json
{ "status": "ok" }
```

### PATCH `/api-keys`

按下标或旧值更新客户端 API key。使用 `old/new` 时，如果旧值不存在，会追加 `new`。Cluster 模式还可以通过此接口更新已有 API key 的 `user_id`、`channels` 和 `model_groups`。

按下标更新：

```json
{ "index": 0, "value": "new-key" }
```

按 old/new 更新：

```json
{ "old": "old-key", "new": "new-key" }
```

Cluster 绑定更新：

```json
{
  "api_key": "client-key-1",
  "user_id": 1,
  "channels": [1],
  "model_groups": [2]
}
```

Cluster 清空 user 绑定：

```json
{ "api_key": "client-key-1", "user_id": 0 }
```

字段说明：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `index` | integer | 条件必填 | 从 0 开始的下标。 |
| `value` | string or `APIKeyEntry` | 条件必填 | 和 `index` 配套的新值；cluster 可使用结构化 entry。 |
| `old` | string | 条件必填 | 要查找的旧 key。 |
| `new` | string | 条件必填 | 新 key；旧值不存在时追加。 |
| `api_key` | string | 条件必填 | Cluster 直接修改绑定时的目标 key；也接受 `api-key`、`key`。 |
| `user_id` | integer | 否 | 绑定的 `user.id`；也接受 `user-id`；传 `0` 清空绑定。 |
| `channels` | array of integer | 否 | Cluster channel group IDs。 |
| `model_groups` | array of integer | 否 | Cluster model group IDs；也接受 `model-groups`。 |

常规输出：

```json
{ "status": "ok" }
```

Cluster 直接修改绑定时输出：

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

按下标或值删除客户端 API key。

Query 参数：

| Query | 类型 | 说明 |
| --- | --- | --- |
| `index` | integer | 删除指定从 0 开始下标的 key。 |
| `value` | string | 删除 trim 后匹配的 key。 |
| `api_key` | string | `value` 的 alias。 |
| `api-key` | string | `value` 的 alias。 |
| `key` | string | `value` 的 alias。 |

输出示例：

```json
{ "status": "ok" }
```

## Provider API Key Routes

以下 route 管理上游 API-key 凭证：

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

Standalone 模式将这些 entry 存入本地 YAML。Cluster 模式会从同样的 config-like payload 合成 DB auth records。

### 凭证字段结构

`GeminiKey`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `api-key` | string | 上游 Gemini API key。 |
| `priority` | integer | 凭证选择优先级，值越大越优先。 |
| `prefix` | string | 可选模型命名空间前缀。 |
| `base-url` | string | 可选 Gemini API base URL override。 |
| `proxy-url` | string | 可选 per-key 出站代理。 |
| `models` | array of `ModelAlias` | 可选上游模型 alias。 |
| `headers` | object string to string | 额外上游请求头。 |
| `excluded-models` | array of string | 该 key 排除的模型 ID。 |
| `disable-cooling` | boolean | 对该凭证禁用 quota cooldown 调度。 |
| `auth-index` | string | Standalone runtime credential identifier。 |
| `auth_index`, `id`, `uuid` | string | Cluster DB auth identifier aliases。 |
| `disabled` | boolean | Cluster auth disabled flag。 |

`ClaudeKey`、`CodexKey` 和 `VertexCompatKey` 使用相同通用字段，额外字段如下：

| 字段 | 适用范围 | 说明 |
| --- | --- | --- |
| `cloak` | Claude | 可选请求 cloaking 配置。 |
| `experimental-cch-signing` | Claude | 为 cloaked Claude 请求启用实验性 CCH signing。 |
| `websockets` | Codex | 启用 Responses API websocket transport。 |
| `api-key` | Vertex | 作为 `x-goog-api-key` 发送。 |

`OpenAICompatibility`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `name` | string | Provider 名称。 |
| `priority` | integer | Provider 选择优先级。 |
| `disabled` | boolean | 为 `true` 时禁用该 provider。 |
| `prefix` | string | 可选模型命名空间前缀。 |
| `base-url` | string | OpenAI-compatible API base URL。 |
| `api-key-entries` | array of `OpenAICompatibilityAPIKey` | Provider API keys 和可选代理。 |
| `models` | array of `OpenAICompatibilityModel` | 模型定义和 alias。 |
| `headers` | object string to string | 额外上游 headers。 |
| `disable-cooling` | boolean | 对该 provider 禁用 quota cooldown 调度。 |
| `auth-index` | string | Standalone 中无 per-key entries 时的 runtime identifier。 |
| `auth_index`, `id`, `uuid` | string | Cluster DB auth identifier aliases。 |

共享嵌套结构：

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

输入：无。

Standalone 示例：

```json
{
  "gemini-api-key": [
    {
      "api-key": "AIza...",
      "priority": 10,
      "prefix": "team-a",
      "base-url": "https://generativelanguage.googleapis.com",
      "proxy-url": "",
      "models": [
        { "name": "gemini-2.5-pro", "alias": "gemini-pro" }
      ],
      "headers": { "X-Test": "1" },
      "excluded-models": ["gemini-1.5-pro"],
      "disable-cooling": false,
      "auth-index": "runtime-index"
    }
  ]
}
```

Cluster 示例：

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

整体替换对应 provider 的完整列表。

输入可以是数组：

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

也可以是 wrapper：

```json
{ "items": [ { "api-key": "provider-key" } ] }
```

Cluster 还接受 `{ "<route-key>": [...] }`、`{ "list": [...] }`、`{ "data": [...] }` 或单个 entry object。

成功输出：

```json
{ "status": "ok" }
```

### PATCH Provider Key Routes

更新单条 provider credential。

输入示例：

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

Selector 字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `index` | integer | provider 过滤后列表中的从 0 开始下标。 |
| `match` | string | 匹配 API-key 值。 |
| `name` | string | OpenAI-compatible provider name 或 auth label。 |
| `id` | string | Cluster DB auth ID。 |
| `uuid` | string | `id` 的 alias。 |
| query `base-url` | string | 可选 base URL，用于消除 API-key 匹配歧义。 |

Cluster `PATCH` 不使用 body 中的 `auth_index` 作为 DB ID selector；按 ID patch 请使用 `id` 或 `uuid`。

成功输出：

```json
{ "status": "ok" }
```

### DELETE Provider Key Routes

删除单条 provider credential。

Query 参数：

| Query | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | Cluster DB auth ID。 |
| `uuid` | string | `id` 的 alias。 |
| `auth_index` | string | DB auth ID 或 runtime index。 |
| `index` | integer | provider 过滤后列表中的从 0 开始下标。 |
| `api-key` | string | API-key 值。 |
| `api_key` | string | `api-key` 的 alias。 |
| `match` | string | `api-key` 的 alias。 |
| `base-url` | string | 可选 base URL，用于消除歧义。 |
| `base_url` | string | `base-url` 的 alias。 |
| `name` | string | Provider 或 compatibility name。 |

成功输出：

```json
{ "status": "ok" }
```

## Auth Files 和 OAuth

### GET `/auth-files`

列出 OAuth/file-backed credentials。

输入：无。

Standalone 示例：

```json
{
  "files": [
    {
      "id": "codex-user.json",
      "auth_index": "runtime-index",
      "name": "codex-user.json",
      "type": "codex",
      "provider": "codex",
      "label": "user@example.com",
      "status": "active",
      "status_message": "",
      "disabled": false,
      "unavailable": false,
      "runtime_only": false,
      "source": "file",
      "size": 1234,
      "email": "user@example.com",
      "account_type": "oauth",
      "account": "user@example.com",
      "created_at": "2026-05-27T10:00:00Z",
      "modtime": "2026-05-27T10:00:00Z",
      "updated_at": "2026-05-27T10:00:00Z",
      "path": "/absolute/path/to/codex-user.json",
      "priority": 10,
      "note": "operator note"
    }
  ]
}
```

Cluster 示例：

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

Cluster 对 Gemini virtual auth records 还可能返回 `virtual_primary`、`virtual_children`、`virtual`、`virtual_parent_id`、`virtual_project` 和 `project_id`。

### GET `/auth-files/models?name=<name-or-id>`

返回指定 auth file 或 auth ID 对应的模型。

Query 参数：

| Query | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | Auth filename、auth ID、display name 或 runtime index。 |

输出示例：

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

下载一个 credential JSON。

Query 参数：

| Query | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 条件必填 | Filename 或 display name。 |
| `file` | string | 条件必填 | Cluster filename alias。 |
| `filename` | string | 条件必填 | Cluster filename alias。 |
| `id` | string | 条件必填 | Cluster DB auth ID。 |
| `uuid` | string | 条件必填 | `id` 的 alias。 |
| `auth_index` | string | 条件必填 | Auth ID 或 runtime index。 |
| `index` | integer | 条件必填 | OAuth auth 的从 0 开始下标。 |

输出：`application/json; charset=utf-8` 附件。

### POST `/auth-files`

上传一个或多个 credential JSON payload。

Multipart 输入：

| Form field | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| 任意 file field | file | 是 | 一个或多个 `.json` credential 文件。 |

Cluster raw JSON 输入：请求体是 credential JSON payload；`name` 不是必填，Home 会推导或分配 UUID-backed 文件名。

Standalone raw JSON 输入也接受 `name` query 参数作为目标文件名。

输出示例：

```json
{ "status": "ok" }
```

```json
{ "status": "ok", "uploaded": 2, "files": ["a.json", "b.json"] }
```

Cluster raw JSON 输出：

```json
{ "status": "ok", "name": "uuid.json" }
```

### DELETE `/auth-files`

删除 credential records 或文件。

Query 参数：

| Query | 类型 | 说明 |
| --- | --- | --- |
| `name` | string | Filename 或 display name。 |
| `file` | string | Cluster filename alias。 |
| `filename` | string | Cluster filename alias。 |
| `id` | string | Cluster DB auth ID。 |
| `uuid` | string | `id` 的 alias。 |
| `auth_index` | string | Auth ID 或 runtime index。 |
| `index` | integer | OAuth auth 的从 0 开始下标。 |
| `all` | `true`、`1` 或 `*` | 删除全部 OAuth/file-backed credentials。 |

输出示例：

```json
{ "status": "ok" }
```

Cluster `all` 输出：

```json
{ "status": "ok", "deleted": 2 }
```

### PATCH `/auth-files/status`

启用或禁用 OAuth/file-backed auth。

输入示例：

```json
{
  "name": "codex-user.json",
  "disabled": true
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 是 | Filename、DB auth ID、runtime auth index 或 display name。 |
| `disabled` | boolean | 是 | `true` 禁用，`false` 启用。 |

Cluster 模式当前只从 `name` 读取 selector；该接口不会读取 body 中独立的 `id`、`uuid`、`auth_index` 或 `index` 字段。

输出示例：

```json
{ "status": "ok", "disabled": true }
```

### PATCH `/auth-files/fields`

更新可编辑 auth metadata。

输入示例：

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

Selector 字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `name`, `file`, `filename` | string | Filename 或 display name。 |
| `id`, `uuid`, `auth_index` | string | Cluster auth ID selector。 |
| query `index` | integer | Cluster OAuth auth 从 0 开始下标 selector。 |

可编辑字段：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `prefix` | string | 模型命名空间前缀；空值清空。 |
| `proxy_url` | string | Per-auth proxy URL；空值清空。 |
| `proxy-url` | string | Cluster 中 `proxy_url` 的 alias。 |
| `headers` | object string to string | 额外上游 headers。Cluster 模式中空字符串会删除单个 header。 |
| `priority` | integer or numeric string | 凭证选择优先级。 |
| `note` | string | 操作备注；空值清空。 |
| `websockets` | boolean or string bool | 支持的 auth 的 runtime websocket flag。 |
| `disabled` | boolean or string bool | 更新 auth disabled state 和 status。 |
| 任意 nested path | any valid JSON | Cluster 模式可以设置任意 metadata path，例如 `token.access_token`。 |

输出示例：

```json
{ "status": "ok" }
```

### OAuth 启动路由

以下 route 创建 provider 登录 URL 或 device-flow session：

```text
GET /anthropic-auth-url
GET /codex-auth-url
GET /gemini-cli-auth-url
GET /antigravity-auth-url
GET /kimi-auth-url
GET /xai-auth-url
```

当前 Home route registry 中，`GET /xai-auth-url` 仅 cluster 模式注册。

通用输出：

```json
{
  "status": "ok",
  "url": "https://provider.example/oauth/authorize?...",
  "state": "oauth-state"
}
```

`GET /gemini-cli-auth-url` 接受：

| Query | 类型 | 说明 |
| --- | --- | --- |
| `project_id` | string | 请求的 GCP project ID。特殊值包括 `ALL` 和 `GOOGLE_ONE`；空值表示自动选择。 |

`GET /kimi-auth-url` 会启动 device flow 并返回验证 URL，Home 在后台等待完成。

### GET `/get-auth-status`

返回当前 OAuth session 状态。

Query 参数：

| Query | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `state` | string | 否 | OAuth state token。 |

输出示例：

```json
{ "status": "ok" }
{ "status": "wait" }
{ "status": "error", "error": "Authentication failed" }
```

### POST `/oauth-callback`

处理 provider OAuth callback metadata。

输入示例：

```json
{
  "provider": "codex",
  "redirect_url": "http://localhost/callback?code=CODE&state=STATE",
  "code": "CODE",
  "state": "STATE",
  "error": ""
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `provider` | string | 是 | 支持 alias：`anthropic`/`claude`、`codex`/`openai`、`gemini`/`google`、`antigravity`/`anti-gravity`，cluster 模式还支持 `xai`/`x-ai`/`grok`。`kimi` 不通过该 route 完成。 |
| `redirect_url` | string | 否 | 完整 callback URL；缺失的 `code`、`state` 或 `error` 可以从中提取。 |
| `code` | string | 条件必填 | OAuth authorization code；除非提供 `error`，否则必填。 |
| `state` | string | 是 | OAuth state token。 |
| `error` | string | 条件必填 | Provider error；缺少 `code` 时必填。 |

Cluster 模式会从 DB-backed OAuth session 中读取 session data，在后台 exchange code，并把得到的 auth records 写入 DB。

输出示例：

```json
{ "status": "ok" }
```

Cluster 常见错误：

```json
{ "status": "error", "error": "invalid body" }
{ "status": "error", "error": "unsupported provider" }
{ "status": "error", "error": "unknown or expired state" }
{ "status": "error", "error": "oauth flow is not pending" }
{ "status": "error", "error": "provider does not match state" }
```

### POST `/vertex/import`

上传 Vertex service account JSON 并创建 Vertex OAuth/file-backed credential。

输入：

| Form field or query | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| form `file` | file | 是 | Vertex service account JSON。 |
| form/query `location` | string | 否 | Vertex location；默认 `us-central1`。 |

输出示例：

```json
{
  "status": "ok",
  "auth-file": "vertex-project-id.json",
  "project_id": "project-id",
  "email": "service-account@example.iam.gserviceaccount.com",
  "location": "us-central1"
}
```

Cluster 模式会把生成的 credential 作为 DB-backed OAuth auth records 存储，并在 `auth-file` 中返回生成的 `<uuid>.json` 名称。

## API Call Proxy

### POST `/api-call`

从 Home server 发起任意 HTTP 请求。该 route 本身受 Management API 认证保护。

输入示例：

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

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `auth_index`, `authIndex`, `AuthIndex` | string | 否 | 来自 `GET /auth-files` 或 provider-key routes 的 credential index，用于选择代理和替换 `$TOKEN$`。 |
| `method` | string | 是 | HTTP method；会转成大写。 |
| `url` | string | 是 | 带 scheme 和 host 的绝对 URL。 |
| `header` | object string to string | 否 | 请求头；包含 `$TOKEN$` 的 header value 会替换为选中 auth token。`Host` 会设置 request host override。 |
| `data` | string | 否 | 原始请求体字符串。 |

Cluster token 替换更严格：只要任意 header 包含 `$TOKEN$`，`auth_index` 就必须解析到 DB auth 或 runtime auth，否则返回：

```json
{ "error": "auth not found" }
```

代理优先级：

1. 选中 credential 的 proxy。
2. 全局 `proxy-url`。
3. 禁用环境代理的直连 transport。

输出示例：

```json
{
  "status_code": 200,
  "header": {
    "Content-Type": ["application/json"]
  },
  "body": "{\"ok\":true}"
}
```

## 用量和日志

可用性：仅 standalone。

Cluster 模式不注册这些 runtime/log-file 查询 route。

### GET `/api-key-usage`

返回内存中的 API-key usage，按 provider 和 `<base_url>|<api_key>` 分组。

输入：无。

输出示例：

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

弹出最早排队的 usage records。

Query 参数：

| Query | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `count` | positive integer | `1` | 要弹出的记录数量。 |

输出示例：

```json
[
  {
    "request_id": "req-1",
    "model": "gpt-5.5",
    "endpoint": "/v1/responses",
    "failed": false
  }
]
```

### GET `/logs`

启用 file logging 时读取应用日志。

Query 参数：

| Query | 类型 | 说明 |
| --- | --- | --- |
| `after` | integer | Unix timestamp cutoff；解析成功时不返回早于或等于该时间的行。 |
| `limit` | integer | 最大返回行数。 |

输出示例：

```json
{
  "lines": ["2026-05-27T10:00:00Z log line"],
  "line-count": 1,
  "latest-timestamp": 1779876000
}
```

### DELETE `/logs`

删除轮转日志文件，并截断当前活跃日志。

输入：无。

输出示例：

```json
{
  "success": true,
  "message": "Logs cleared successfully",
  "removed": 3
}
```

### GET `/request-error-logs`

当详细 request logging 禁用时列出 `error-*.log` 文件；详细 request logging 启用时返回空列表。

输入：无。

输出示例：

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

下载 request error log 文件。

Path 参数：

| Path | 类型 | 说明 |
| --- | --- | --- |
| `name` | string | 文件名必须以 `error-` 开头并以 `.log` 结尾；拒绝 slash。 |

输出：文件附件。

### GET `/request-log-by-id/:id`

下载文件名以 `-<id>.log` 结尾的 request log。

Path 参数：

| Path | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | Request ID；拒绝 slash。 |

输出：文件附件。

## 模型

### GET `/model-definitions/:channel`

返回指定 channel 的静态模型 metadata。

支持的 channel：

```text
claude
gemini
vertex
gemini-cli
codex
kimi
antigravity
xai
x-ai
grok
```

Path 或 query 参数：

| Path/query | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `channel` | string | 是 | Channel 名称。`x-ai` 和 `grok` 是 `xai` 的 alias。 |

输出示例：

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

未知 channel 输出：

```json
{ "error": "unknown channel", "channel": "bad-channel" }
```

## Channel Groups

可用性：仅 cluster。

Channel groups 限制某个客户端 API key 可以使用哪些 auth records。如果客户端 API key 的 `channels` 是空数组，则不应用 channel-group 过滤。

### GET `/channel-groups`

输出示例：

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

返回单个 channel group：

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

创建 channel group。

输入示例：

```json
{
  "channel_name": "team-a",
  "disabled": false
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `channel_name` | string | 是 | Group 名称；也接受 `name`。 |
| `disabled` | boolean | 否 | 禁用状态，默认 `false`。 |
| `enabled` | boolean | 否 | `disabled` 的反向 alias；如果同时传入，二者必须一致。 |

输出：`{ "channel_group": ... }`。

### PUT/PATCH `/channel-groups/:id`

更新 channel group。请求字段同 `POST /channel-groups`，所有字段均可选。

输出：`{ "channel_group": ... }`。

### DELETE `/channel-groups/:id`

软删除 group 及其 details。

输出：

```json
{ "status": "ok" }
```

### GET `/channel-group-details`

列出 channel group detail rows。

Query 参数：

| Query | 类型 | 说明 |
| --- | --- | --- |
| `channel_group_id` | integer | 按 group ID 过滤；也接受 `channel-group-id`、`group_id`、`group-id`。 |
| `auth_id` | string | 按 auth ID 过滤；也接受 `auth-id`。 |

输出示例：

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

返回单个 detail row：

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

创建 channel group 到 auth 的绑定。

输入示例：

```json
{
  "channel_group_id": 1,
  "auth_id": "auth-db-id"
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `channel_group_id` | integer | 是 | 已存在的 channel group ID。 |
| `auth_id` | string | 是 | Auth record ID。 |

输出：`{ "channel_group_detail": ... }`。

### PUT/PATCH `/channel-group-details/:id`

更新 detail row。

输入示例：

```json
{
  "channel_group_id": 2,
  "auth_id": "other-auth-id"
}
```

所有字段均可选；如果传入 `channel_group_id`，必须大于 `0`。

输出：`{ "channel_group_detail": ... }`。

### DELETE `/channel-group-details/:id`

软删除 detail row。

输出：

```json
{ "status": "ok" }
```

## Model Groups

可用性：仅 cluster。

Model groups 限制某个客户端 API key 可以使用哪些模型 ID。如果客户端 API key 的 `model_groups` 是空数组，则不应用模型过滤。

### GET `/model-groups`

输出示例：

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

返回单个 model group：

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

创建 model group。

输入示例：

```json
{
  "group_name": "premium-models",
  "disabled": false
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `group_name` | string | 是 | Group 名称；也接受 `name`。 |
| `disabled` | boolean | 否 | 禁用状态，默认 `false`。 |
| `enabled` | boolean | 否 | `disabled` 的反向 alias；如果同时传入，二者必须一致。 |

输出：`{ "model_group": ... }`。

### PUT/PATCH `/model-groups/:id`

更新 model group。请求字段同 `POST /model-groups`，所有字段均可选。

输出：`{ "model_group": ... }`。

### DELETE `/model-groups/:id`

软删除 model group 及其 details。

输出：

```json
{ "status": "ok" }
```

### GET `/model-group-details`

列出 model group detail rows。

Query 参数：

| Query | 类型 | 说明 |
| --- | --- | --- |
| `model_group_id` | integer | 按 model group ID 过滤；也接受 `model-group-id`、`group_id`、`group-id`。 |
| `model_id` | string | 按 model ID 过滤；也接受 `model-id`。 |

输出示例：

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

返回单个 detail row：

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

创建 model group 到 model ID 的绑定。

输入示例：

```json
{
  "model_group_id": 1,
  "model_id": "gpt-5.5"
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `model_group_id` | integer | 是 | 已存在的 model group ID。 |
| `model_id` | string | 是 | 该 group 允许的模型 ID。 |

输出：`{ "model_group_detail": ... }`。

### PUT/PATCH `/model-group-details/:id`

更新 detail row。

输入示例：

```json
{
  "model_group_id": 2,
  "model_id": "gpt-5.5-mini"
}
```

所有字段均可选；如果传入 `model_group_id`，必须大于 `0`。

输出：`{ "model_group_detail": ... }`。

### DELETE `/model-group-details/:id`

软删除 detail row。

输出：

```json
{ "status": "ok" }
```

## AmpCode

以下 route 读写 `ampcode` 配置。

`AmpCode` object：

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

Routes：

| Method | Path | 输入 | 输出 |
| --- | --- | --- | --- |
| `GET` | `/ampcode` | 无 | `{ "ampcode": AmpCode }` |
| `GET` | `/ampcode/upstream-url` | 无 | `{ "upstream-url": string }` |
| `PUT/PATCH` | `/ampcode/upstream-url` | `{ "value": string }` | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/upstream-url` | 无 | `{ "status": "ok" }` |
| `GET` | `/ampcode/upstream-api-key` | 无 | `{ "upstream-api-key": string }` |
| `PUT/PATCH` | `/ampcode/upstream-api-key` | `{ "value": string }` | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/upstream-api-key` | 无 | `{ "status": "ok" }` |
| `GET` | `/ampcode/restrict-management-to-localhost` | 无 | `{ "restrict-management-to-localhost": boolean }` |
| `PUT/PATCH` | `/ampcode/restrict-management-to-localhost` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/ampcode/model-mappings` | 无 | `{ "model-mappings": AmpModelMapping[] }` |
| `PUT` | `/ampcode/model-mappings` | `{ "value": AmpModelMapping[] }` | `{ "status": "ok" }` |
| `PATCH` | `/ampcode/model-mappings` | `{ "value": AmpModelMapping[] }`；按 `from` upsert | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/model-mappings` | `{ "value": ["from-model"] }`；body 无效或缺失时清空全部 | `{ "status": "ok" }` |
| `GET` | `/ampcode/force-model-mappings` | 无 | `{ "force-model-mappings": boolean }` |
| `PUT/PATCH` | `/ampcode/force-model-mappings` | `{ "value": boolean }` | `{ "status": "ok" }` |
| `GET` | `/ampcode/upstream-api-keys` | 无 | `{ "upstream-api-keys": AmpUpstreamAPIKeyEntry[] }` |
| `PUT` | `/ampcode/upstream-api-keys` | `{ "value": AmpUpstreamAPIKeyEntry[] }` | `{ "status": "ok" }` |
| `PATCH` | `/ampcode/upstream-api-keys` | `{ "value": AmpUpstreamAPIKeyEntry[] }`；按 `upstream-api-key` upsert | `{ "status": "ok" }` |
| `DELETE` | `/ampcode/upstream-api-keys` | `{ "value": [] }` 清空全部；`{ "value": ["key"] }` 删除匹配的 upstream keys | `{ "status": "ok" }` |

## OAuth 模型规则

### `/oauth-excluded-models`

GET 输出：

```json
{
  "oauth-excluded-models": {
    "claude": ["claude-opus-4.5"]
  }
}
```

PUT 输入：

```json
{
  "claude": ["claude-opus-4.5"]
}
```

或：

```json
{
  "items": {
    "claude": ["claude-opus-4.5"]
  }
}
```

PATCH 输入：

```json
{
  "provider": "claude",
  "models": ["claude-opus-4.5"]
}
```

DELETE query：

| Query | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `provider` | string | 是 | 要移除的 provider key。 |

写入成功返回 `{ "status": "ok" }`。

### `/oauth-model-alias`

GET 输出：

```json
{
  "oauth-model-alias": {
    "claude": [
      { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
    ]
  }
}
```

PUT 输入：

```json
{
  "claude": [
    { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
  ]
}
```

或：

```json
{
  "items": {
    "claude": [
      { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
    ]
  }
}
```

PATCH 输入：

```json
{
  "channel": "claude",
  "provider": "claude",
  "aliases": [
    { "name": "claude-sonnet-4", "alias": "sonnet", "fork": true }
  ]
}
```

DELETE query：

| Query | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `channel` | string | 条件必填 | 要移除的 alias channel。 |
| `provider` | string | 条件必填 | `channel` 的 alias。 |

写入成功返回 `{ "status": "ok" }`。

## 配置字段参考

以下字段可被 Home YAML config 接受。Cluster `PUT /config.yaml` 接受非 credential roots；credential roots 应使用 provider-key 和 auth-file route 管理。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `host` | string | 服务绑定 host/interface。 |
| `port` | integer | 服务监听端口。 |
| `allow-host` | array of string | RESP client IP allowlist；空列表表示允许所有 host。 |
| `tls.enable` | boolean | 启用 HTTPS。 |
| `tls.cert` | string | TLS certificate 路径。 |
| `tls.key` | string | TLS private key 路径。 |
| `remote-management.allow-remote` | boolean | 为 `true` 时允许非 localhost Management API 请求。 |
| `remote-management.secret-key` | string | Management key；本地配置模式下明文会在启动时 hash。 |
| `remote-management.disable-control-panel` | boolean | 禁用 `/management.html` 和 panel syncing。 |
| `remote-management.disable-auto-update-panel` | boolean | 禁用后台周期性 panel asset 更新。 |
| `remote-management.panel-github-repository` | string | Management panel GitHub repository URL 或 releases API URL。 |
| `auth-dir` | string | 本地 auth token 目录。 |
| `proxy-url` | string | 全局出站代理 URL。 |
| `disable-image-generation` | boolean or `"chat"` | `false` 启用图像生成；`true` 全局禁用；`"chat"` 只对非 image endpoints 禁用注入。 |
| `enable-gemini-cli-endpoint` | boolean | 启用 Gemini CLI internal endpoints。 |
| `force-model-prefix` | boolean | 对带 prefix 的凭证要求显式模型 prefix。 |
| `request-log` | boolean | 启用详细 request logging。 |
| `api-keys` | array of string | Home 接受的客户端 API keys。 |
| `passthrough-headers` | boolean | 将上游响应头透传给下游客户端。 |
| `streaming.keepalive-seconds` | integer | SSE heartbeat 间隔秒数；`<=0` 禁用。 |
| `streaming.bootstrap-retries` | integer | Streaming 首字节前重试次数；`<=0` 禁用。 |
| `nonstream-keepalive-interval` | integer | 非 streaming 响应的空行 keepalive 间隔秒数。 |
| `debug` | boolean | 启用 debug logging/features。 |
| `pprof.enable` | boolean | 启用 pprof server。 |
| `pprof.addr` | string | pprof listen address。 |
| `commercial-mode` | boolean | 高并发下减少高开销 middleware 行为。 |
| `logging-to-file` | boolean | 将 app logs 写入文件而非 stdout。 |
| `logs-max-total-size-mb` | integer | 日志文件总大小上限；`0` 禁用清理。 |
| `error-logs-max-files` | integer | request error log 文件保留数量。 |
| `usage-statistics-enabled` | boolean | 启用内存 usage aggregation。 |
| `redis-usage-queue-retention-seconds` | integer | Usage queue 保留窗口；默认 `60`，最大 `3600`。 |
| `disable-cooling` | boolean | 全局禁用 quota cooldown scheduling。 |
| `auth-auto-refresh-workers` | integer | 覆盖 auth auto-refresh worker 数量。 |
| `request-retry` | integer | 失败请求重试次数。 |
| `max-retry-credentials` | integer | 一个失败请求最多尝试的凭证数量；`<=0` 表示所有可用凭证。 |
| `max-retry-interval` | integer | 重试 cooled-down credentials 前的最大等待秒数。 |
| `quota-exceeded.switch-project` | boolean | Gemini quota error 时切换 project。 |
| `quota-exceeded.switch-preview-model` | boolean | Quota error 时切换到 preview model。 |
| `quota-exceeded.antigravity-credits` | boolean | Claude 最后兜底使用 Antigravity credits。 |
| `routing.strategy` | string | `round-robin` 或 `fill-first`。 |
| `routing.claude-code-session-affinity` | boolean | 已废弃的 Claude Code session affinity flag。 |
| `routing.session-affinity` | boolean | 通用 session-sticky credential routing。 |
| `routing.session-affinity-ttl` | string | Session-to-auth binding 持续时间。 |
| `antigravity-signature-cache-enabled` | boolean pointer | 启用 Antigravity thinking signature cache validation。 |
| `antigravity-signature-bypass-strict` | boolean pointer | 控制 Antigravity signature bypass 严格程度。 |
| `gemini-api-key` | array of `GeminiKey` | Gemini API-key credentials；cluster 应使用 provider-key routes。 |
| `codex-api-key` | array of `CodexKey` | Codex API-key credentials；cluster 应使用 provider-key routes。 |
| `codex-header-defaults.user-agent` | string | 默认 Codex User-Agent。 |
| `codex-header-defaults.beta-features` | string | 默认 Codex websocket beta features header。 |
| `claude-api-key` | array of `ClaudeKey` | Claude API-key credentials；cluster 应使用 provider-key routes。 |
| `claude-header-defaults.user-agent` | string | 默认 Claude User-Agent。 |
| `claude-header-defaults.package-version` | string | 默认 Claude package version。 |
| `claude-header-defaults.runtime-version` | string | 默认 Claude runtime version。 |
| `claude-header-defaults.os` | string | 默认 Claude OS fingerprint。 |
| `claude-header-defaults.arch` | string | 默认 Claude architecture fingerprint。 |
| `claude-header-defaults.timeout` | string | 默认 Claude timeout header。 |
| `claude-header-defaults.stabilize-device-profile` | boolean pointer | 启用固定 Claude device profile baseline。 |
| `openai-compatibility` | array of `OpenAICompatibility` | OpenAI-compatible providers；cluster 应使用 provider-key routes。 |
| `vertex-api-key` | array of `VertexCompatKey` | Vertex-compatible API-key credentials；cluster 应使用 provider-key routes。 |
| `ampcode` | `AmpCode` | Amp CLI integration settings。 |
| `oauth-excluded-models` | object string to array of string | 每个 provider 的 OAuth/file-backed auth 排除模型。 |
| `oauth-model-alias` | object string to array of `OAuthModelAlias` | 每个 channel 的 OAuth model aliases。 |
| `payload.default` | array of `PayloadRule` | 设置缺失的 JSON payload params。 |
| `payload.default-raw` | array of `PayloadRule` | 设置缺失的 raw JSON payload params。 |
| `payload.override` | array of `PayloadRule` | 覆盖 JSON payload params。 |
| `payload.override-raw` | array of `PayloadRule` | 覆盖 raw JSON payload params。 |
| `payload.filter` | array of `PayloadFilterRule` | 移除 JSON payload paths。 |

Payload 嵌套结构：

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
