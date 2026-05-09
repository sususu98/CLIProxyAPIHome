# CLIProxyAPIHome (RESP Scheduler)

This project is a central scheduling/dispatch service for **CLIProxyAPI (CPA)** that speaks **Redis RESP** over TCP.
CPA can send a request (model + full HTTP headers) to this service, and receive back:

- The resolved upstream model name (after model-alias resolution)
- The selected credential snapshot (`auth` JSON payload)

## Build

```bash
go build -o home ./cmd/home
```

## Run

By default it reads `config.yaml`:

```bash
./home -config config.yaml
```

Listening address can be overridden:

```bash
./home -config config.yaml -addr 0.0.0.0:8317
```

If `-addr` is not set, it listens on `host` + `port` from the config file.

## Config

The config format is intentionally kept consistent with CPA.
See `config.example.yaml` for the full schema (API keys, auth-dir, model aliases, payload rules, etc).

## RESP Commands

### 1) `SET token <json>`

Adds a credential JSON blob into the filesystem credential store (`auth-dir`).

- Returns: `OK`

### 2) `RPOP <json>`

Dispatch request:

Input JSON format:

```json
{"type":"auth","model":"<requested-model>","count":1,"session_id":"<optional-session-id>","headers":{"authorization":"Bearer ...","x-api-key":"..."}}
```

Return (RESP bulk string, JSON):

- Success:
  - `{"model":"<upstream-model>","provider":"<provider>","auth_index":"<auth-id>","auth":{...}}`
- Error:
  - `{"error":{"type":"...","message":"..."}}`

Notes:

- `count` starts at `1` for the first credential request in one CPA request. Home rejects the request when `count - 2 >= request-retry`.
- Returned `auth` is sanitized for downstream CPA nodes: `refresh_token` and Vertex `service_account` are removed.

### 3) `LPRUSH usage <json>` (also accepts `LPUSH`)

Accepts a usage record JSON blob and appends it to `./logs/usage.log` (one JSON per line).
If the payload includes a non-200 `fail.status_code`, Home updates the matching auth/model cooldown state by `auth_index`.

- Returns: integer `1`

### 4) `GET models`

Returns the current cached model catalog as JSON.

### 5) `GET <json>` (refresh)

Forces an on-demand refresh of the given auth entry on the scheduler, and returns the refreshed auth snapshot.

Input JSON format:

```json
{"type":"refresh","auth_index":"<auth-id>"}
```

Return (RESP bulk string, JSON):

- Success:
  - `{"auth_index":"<auth-id>","auth":{...}}`
- Error:
  - `{"error":{"type":"...","message":"..."}}`

### 6) `GET config`

Returns the YAML config content filtered for downstream CPA nodes.

The following keys are removed before the payload is returned:

- `remote-management`
- `api-keys`
- `auth-dir`
- `tls`
- `gemini-api-key`
- `codex-api-key`
- `claude-api-key`
- `openai-compatibility`
- `vertex-api-key`
- `oauth-model-alias`
- `oauth-excluded-models`

Return (RESP bulk string, YAML):

- Success: `<config.yaml bytes>`

### 7) `SUBSCRIBE config`

Subscribes the current TCP connection to config file changes.
Whenever the config file content changes, the server pushes a Pub/Sub `message` with the filtered YAML config (RESP array reply).

- Subscribe confirmation (RESP array reply):
  - `["subscribe","config",1]`
- Push message (RESP array reply):
  - `["message","config","<config.yaml bytes>"]`

### 8) `RPUSH request-log <json>`

Accepts a request log payload and writes the `request_log` content into `./logs/` as a standalone `.log` file.

Input JSON format:

```json
{"headers":{...http headers...},"request_log":"<request-log-data>"}
```

Notes:

- The output filename follows CPA's request-log naming style, but with a client IP prefix:
  - Example: `203.0.113.10-v1-responses-2026-05-09T120305-a1b2c3d4.log`
- `request_id` is derived from `headers["x-request-id"]` or `headers["x-cpa-request-id"]` when present; otherwise a random ID is generated.

- Returns: integer `1`
