# CLIProxyAPIHome (RESP Scheduler)

This project is a central scheduling/dispatch service for **CLIProxyAPI (CPA)** that speaks **Redis RESP** over TCP.
CPA can send a request (model + full HTTP headers) to this service, and receive back:

- The resolved upstream model name (after model-alias resolution)
- The selected credential (either `access_token` **or** `base_url` + `api_key`)

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
{"type":"auth","model":"<requested-model>","headers":{"authorization":"Bearer ...","x-api-key":"..."}}
```

Return (RESP bulk string, JSON):

- Success:
  - `{"model":"<upstream-model>","provider":"<provider>","auth_index":"<auth-id>","auth":{...}}`
- Error:
  - `{"error":{"message":"..."}}`

Notes:

- Returned `auth` is sanitized for downstream CPA nodes: `refresh_token` and Vertex `service_account` are removed.
- Legacy `{"type":"access_token",...}` is still supported and returns the minimal credential shape.

### 3) `LPRUSH usage <json>` (also accepts `LPUSH`)

Accepts a usage record JSON blob. Currently discarded.

- Returns: `OK`

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
  - `{"error":{"message":"..."}}`
