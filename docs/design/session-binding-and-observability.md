# Session Binding and Session Observability Design

> Status: proposal  
> Scope: CLIProxyAPIHome (Home) + Home-Management-Center (HomeUI)  
> Related runtime: CLIProxyAPI (CPA) for extraction/reporting at the edge  
> Research base: agent session ID gateway research (2026-07) + claude-code-hub (CCH) reference  
> Date: 2026-07

## 1. Problem

Home already has optional session-sticky credential routing (`routing.session-affinity`), but it is incomplete for production diagnostics:

1. **Session extraction is incomplete** relative to current clients (Claude Code, Codex, OpenCode, pi, etc.).
2. **Bindings are process-local** (`SessionCache` in memory), so multi-Home clusters do not share sticky state.
3. **Usage / request logs do not store a durable client session identity**, so operators cannot filter “this conversation” the way CCH does.
4. **HomeUI has request-level filters** (`request_id`, user, client key, provider, model) but **no session-centric conversation view**.

Operators currently cannot answer:

- Which credential did this Claude Code / Codex / pi session stick to?
- Show me all turns of session `11111111-...` in order.
- Did this conversation fail only after compact / subagent / model switch?
- Which request logs belong to the same client conversation?

## 2. Goals

### Must have

1. Reliable extraction of client session identity with source + confidence.
2. Correct session → credential binding for sticky routing (with failover).
3. Persist session identity on Home usage records so it is queryable.
4. Management API support for session list / filter / detail.
5. HomeUI ability to filter and inspect a conversation by session ID, similar to CCH.

### Non-goals (v1)

1. Full message transcript storage/rebuild as a product chat product.
2. Guessing session from full `messages` hash / IP / User-Agent as a high-confidence key.
3. Treating session ID as an authentication credential.
4. Replacing request-event logging (#HomeUI 31); this design complements it.

## 3. Current State

### 3.1 Home binding today

| Area | Current behavior | Gap |
| --- | --- | --- |
| Config | `routing.session-affinity`, `session-affinity-ttl`, legacy `claude-code-session-affinity` | OK as feature flags |
| Selector | `SessionAffinitySelector` wraps fallback selector | Extraction list incomplete |
| Cache | in-process `SessionCache` with TTL refresh | Not cluster-safe |
| Cache key | `provider::sessionID::model` | Good for multi-model; missing tenant/API-key namespace |
| CPA dispatch | `session_id` already present on auth dispatch request; Home may promote to `X-Session-ID` | Incomplete sources before dispatch |

Current extraction priority in Home (`internal/cliproxy/auth/selector.go`):

```text
1. metadata.user_id Claude legacy / JSON session_id
2. X-Session-ID
3. Session_id (Codex underscore)
4. X-Client-Request-Id (pi / request-scoped; not a stable root session)
5. bare metadata.user_id
6. conversation_id
7. message content hash fallback
```

Missing high-value sources:

```text
x-gateway-session-id
x-claude-code-session-id
session-id / thread-id (Codex hyphen form; measured production form)
x-opencode-session / x-session-id / x-session-affinity
x-hermes-session-id / x-openclaw-session-id
body.prompt_cache_key
body.conversation / previous_response_id chain mapping
```

### 3.2 Observability today

Home `usage` table stores `request_id`, provider, model, api_key, auth, endpoint, home/cpa ownership, tokens, failures, payload JSON — but **no first-class `session_id` / `thread_id` / `session_source` / `session_confidence` columns**.

HomeUI `/admin/usage` can filter by request id and many operational dimensions, but cannot:

- query `session_id=...`
- open a conversation timeline for one session
- see sticky binding metadata for that session

### 3.3 CCH reference UX (desired operator experience)

CCH stores `message_request.session_id` and indexes it. UI supports:

1. Session list (`/dashboard/sessions`) active / inactive.
2. Session conversation detail (`/dashboard/sessions/:sessionId/messages`) with request sequence sidebar.
3. Usage logs filter by `sessionId`.
4. Aggregate session stats (tokens, cost, providers, models, duration, request count).

Home should provide an equivalent **operator path**, not necessarily a pixel-identical UI:

```text
Session list / search
  → Session detail timeline (ordered usage records)
    → single request detail + request-log download
```

## 4. Concepts

### 4.1 Identity model

```go
type ExtractedSession struct {
    // Client-provided root conversation id when available.
    ClientSessionID string

    // Optional thread / subagent id (Codex thread-id, gateway thread header).
    ThreadID string

    // Optional parent thread for fork/subagent graphs.
    ParentThreadID string

    // Normalized client family: claude-code | codex | opencode | pi | hermes | openclaw | gateway | unknown
    ClientType string

    // Where the id came from.
    Source string // gateway_header | claude_header | claude_metadata | codex_header | ...

    // high | medium | low
    Confidence string

    // session | thread | user | transport | generated
    Scope string

    // true if client provided the value (not gateway-generated fingerprint).
    ClientProvided bool

    // Display / filter value operators paste from client logs.
    // Prefer raw client id without internal prefixes when confidence is high.
    DisplaySessionID string
}
```

### 4.2 Routing key vs display session id

Do **not** use bare client session id as a global Redis/DB key.

```text
routing_key = HMAC-SHA256(
  secret,
  tenant_or_api_key_id + "\0" +
  client_type + "\0" +
  client_session_id + "\0" +
  provider_pool_or_provider + "\0" +
  model_family_or_model
)
```

Rules:

1. Sticky routing uses `routing_key` (or an equivalent namespaced key).
2. Observability stores both:
   - `session_id` = client display id (searchable, operator-facing)
   - `session_routing_key` optional / hashed if needed for audit
3. Same client session across two API keys must not share sticky bindings.
4. Thread ids may be stored for observation; root session id remains sticky unless a future policy opts into thread-scoped stickiness.

### 4.3 Confidence policy

| Confidence | Allowed sticky behavior | Stored for filters |
| --- | --- | --- |
| high | full session sticky | yes |
| medium | sticky with weaker TTL / easier failover | yes |
| low | **no session sticky** (tenant/default selector only) | optional, tagged low |
| none | default selector | no fake session id |

Low-confidence sources (message hash, IP+UA fingerprint, bare `metadata.user_id` without Claude multi-signal confirmation) must not silently merge unrelated conversations.

## 5. Extraction Design

### 5.1 Header normalization

1. Header names are case-insensitive.
2. Prefer hyphenated headers over underscored legacy headers.
3. Validate opaque ids:
   - length 8–256
   - printable ASCII, no CR/LF
   - allow `A-Za-z0-9._:-`
4. Never interpolate raw session ids into SQL/filenames without escaping/parameterization.
5. Logs may truncate/hash session ids; Management API returns full ids only to authorized operators.

### 5.2 Recommended extraction order

#### Anthropic / Claude path

```text
1. x-gateway-session-id                         high
2. x-claude-code-session-id                     high
3. parse(metadata.user_id JSON).session_id      high when Claude Code multi-signal confirmed
4. parse(metadata.user_id legacy).session_id    high when Claude Code multi-signal confirmed
5. metadata.session_id                          medium
6. no high-confidence id                        → no sticky
```

Claude Code multi-signal confirmation (borrowed from CCH, not User-Agent alone):

```text
x-app == cli
AND User-Agent starts with claude-cli/
AND anthropic-beta present
AND metadata.user_id is string
```

#### Codex / Responses path

```text
1. x-gateway-session-id                         high
2. session-id                                   high   (measured current Codex form)
3. session_id                                   high/legacy
4. x-session-id                                 medium/high
5. prompt_cache_key                             medium (if equals known session form / policy enabled)
6. metadata.session_id                          medium
7. previous_response_id chain map               medium/high for continuity only
8. thread-id                                    thread scope; do not replace root session when both exist
```

#### OpenCode path

```text
1. x-gateway-session-id
2. x-opencode-session
3. x-session-id
4. x-session-affinity
```

#### pi path

```text
1. x-gateway-session-id                         high (recommended extension)
2. session_id / session-id from responses mode  high when present
3. prompt_cache_key                             medium
4. x-client-request-id                          transport/request scope only; not root sticky by default
```

#### OpenAI SDK / Responses generic

```text
1. x-gateway-session-id
2. body.conversation.id / body.conversation string
3. previous_response_id → response chain map
4. no messages hash
```

### 5.3 Explicitly rejected as sticky session keys

- full `messages` content hash (privacy + compact/fork collisions)
- IP / NAT fingerprint alone
- User-Agent alone
- API key alone (tenant sticky is a different policy)
- stainless / goog client headers
- bare `metadata.user_id` for non-Claude clients (user identity ≠ conversation)

### 5.4 Implementation split: extract / bind / observe

```text
extractClientSession(req)
  → ExtractedSession | null

bindSessionToAuth(routingKey, candidateAuths)
  → auth with SETNX/TTL semantics

observeSession(usageRecord, ExtractedSession, bindResult)
  → persist queryable fields
```

Keep these layers separate, as CCH does with extractor / completer / sticky binding.

## 6. Binding Logic (Home)

### 6.1 Desired algorithm

```text
extracted = extractClientSession(...)
if extracted == null OR confidence == low:
    return fallbackSelector.Pick(...)

routingKey = buildRoutingKey(apiKeyOwner, extracted, provider, model)
if boundAuth = store.Get(routingKey); boundAuth available and compatible:
    store.RefreshTTL(routingKey)
    return boundAuth

// optional inheritance: first-turn short key / previous_response chain
if inherited = store.Get(fallbackKey); inherited available:
    store.SetNX(routingKey, inherited)
    return inherited

auth = fallbackSelector.Pick(...)
store.SetNX(routingKey, auth.ID, ttl)
return auth
```

Compatibility checks before reuse (already partially present; keep/expand):

- auth still enabled / not cooldown
- model supported by auth
- provider/format compatible
- not circuit-broken
- session reuse not disabled for that auth/pool

### 6.2 Cluster storage

Replace process-local-only sticky state for multi-Home:

| Deployment | Store |
| --- | --- |
| single-node SQLite Home | DB table or local cache OK |
| multi-Home cluster | shared DB (PostgreSQL preferred) or existing cluster coordination store |

Suggested table:

```text
session_bindings
  routing_key        text PK
  session_id         text not null      -- display/client id
  client_type        text
  source             text
  confidence         text
  api_key_hash/id    text not null
  provider           text not null
  model              text not null
  auth_id            text not null
  auth_index         text
  created_at         timestamptz
  updated_at         timestamptz
  expires_at         timestamptz
  last_request_id    text
  bind_version       int
```

Indexes:

```text
(session_id, updated_at desc)
(auth_id, expires_at)
(expires_at)
(api_key_hash/id, session_id)
```

Semantics:

1. `INSERT ... ON CONFLICT DO NOTHING` / conditional update for first-writer wins.
2. TTL refresh on successful use.
3. Invalidate by `auth_id` when credential becomes unhealthy.
4. Optional management endpoint to inspect / force-expire bindings.

### 6.3 Failover and migration

Sticky is a preference, not a hard lock forever:

1. If bound auth fails health/model checks → reselect and rebind.
2. Emit observability event: `session_rebound` with old/new auth.
3. Do not thrash: short negative cache or backoff after repeated rebinds.
4. Keep previous response-chain map independent from auth binding map.

## 7. Observability Data Model

### 7.1 Usage record extensions (Home DB)

Add first-class columns on `usage` (or derived sibling table if payload growth is a concern):

| Column | Type | Purpose |
| --- | --- | --- |
| `session_id` | text, indexed | operator-facing client session id |
| `thread_id` | text, indexed nullable | Codex/OpenCode/pi thread |
| `parent_thread_id` | text nullable | fork/subagent parent |
| `session_source` | text | extraction source |
| `session_confidence` | text | high/medium/low |
| `client_type` | text, indexed | claude-code/codex/... |
| `bound_auth_id` | text nullable | sticky target used |
| `session_request_seq` | int nullable | optional per-session sequence if available |

Minimum viable v1: `session_id`, `thread_id`, `session_source`, `session_confidence`, `client_type`.

Indexes inspired by CCH:

```text
idx_usage_session_time (session_id, timestamp desc)
idx_usage_session_request (session_id, request_id)
idx_usage_client_type_time (client_type, timestamp desc)
```

### 7.2 Payload compatibility

CPA / Home usage push should include:

```json
{
  "request_id": "...",
  "session_id": "11111111-1111-4111-8111-111111111111",
  "thread_id": "...",
  "session_source": "claude_header",
  "session_confidence": "high",
  "client_type": "claude-code",
  "bound_auth_id": "auth-..."
}
```

Rules:

1. Missing fields remain backward compatible.
2. Home parser accepts aliases (`session-id`, `clientSessionId`).
3. Do not invent high-confidence session ids during ingest.
4. Request-log text may continue as file-backed payload; session filters use DB columns, not full-text only.

### 7.3 Request-log correlation

Existing path:

```text
usage.request_id → /request-log-by-id/:id → raw request log file
```

After session fields exist:

```text
session_id → list usage records ordered by time/request_seq
           → each record links request_id / request log / error body
```

This is enough to reconstruct an operator “conversation” without storing full chat history twice.

## 8. Management API (Home)

### 8.1 Extend existing usage APIs

`GET /usage/records` and related observability queries gain filters:

```text
session_id=
thread_id=
client_type=
session_confidence=
session_source=
```

Response records include the new fields.

Also extend:

- `/usage/overview` optional group-by session top-N later
- `/request-logs` index metadata if cheap
- `/usage/records/:id` detail payload with session block

### 8.2 New session-centric endpoints (recommended)

```text
GET /sessions
  ?state=active|inactive|all
  &session_id=
  &client_type=
  &user=
  &client_key=
  &provider=
  &model=
  &from=&to=
  &limit=&offset=

GET /sessions/:sessionId
  summary + latest binding + aggregate stats

GET /sessions/:sessionId/requests
  ordered usage records / request timeline
  ?order=asc|desc&limit=&offset=

GET /sessions/:sessionId/bindings
  current/historical sticky bindings (if retained)

POST /sessions/:sessionId/bindings/expire
  operator force unstick
```

Active definition (v1):

```text
active = last_seen_at within session-affinity-ttl or a dedicated active window (e.g. 15m)
```

Stats fields (CCH-like):

```text
session_id
client_type
first_seen_at
last_seen_at
request_count
failed_count
total_tokens
total_cost (if billing available)
providers[]
models[]
bound_auth_ids[]
api_keys[] / users[]
```

### 8.3 AuthZ

Management secret / existing Home Management auth remains required. Session ids are operator data, not public user identifiers by themselves, but may correlate user activity; do not expose outside Management.

## 9. HomeUI Design

Static mockups for review:

- `docs/design/session-ui-mockups/usage-session-filter.png`
- `docs/design/session-ui-mockups/sessions-list.png`
- `docs/design/session-ui-mockups/session-detail.png`
- HTML sources under `docs/design/session-ui-mockups/`

### 9.1 Product surfaces

1. **Usage workbench enhancement** (`/admin/usage`)
   - add Session ID filter input (exact match + optional prefix later)
   - show Session ID / Client Type columns
   - click session id → open session timeline

2. **Session workbench** (new page preferred)
   - route: `/admin/sessions`
   - list cards/table: session id, client, user/key, providers, models, request count, last seen, status
   - search by session id paste from Claude Code / Codex logs

3. **Session detail**
   - route: `/admin/sessions/$sessionId`
   - left: request sequence list (time, model, status, latency, auth)
   - right/main: selected request detail sheet (reuse usage detail components)
   - actions: download request log, copy session id, filter usage by this session, expire sticky binding (if API available)

This mirrors CCH’s mental model:

```text
Sessions list → one session conversation → one request payload/log
```

without requiring CCH’s full message snapshot system on day one.

### 9.2 Minimum UI acceptance (v1)

- Paste a Claude Code / Codex session id and see only that conversation’s requests.
- Open a request from that conversation and download its request log when available.
- See which credential(s) served the session.
- Distinguish missing-session requests from low-confidence tagged ones.

### 9.3 Optional later

- live active sessions panel on dashboard
- origin/parent thread graph
- before/after payload snapshots like CCH
- terminate concurrent session tracking

## 10. CPA Boundary

Home cannot invent accurate session fields if CPA never reports them.

Required CPA follow-ups (separate issue/PR if not done in the same change set):

1. Share the same extractor implementation/order as Home.
2. Put extracted session fields into usage publish payload.
3. Preserve client headers on Home dispatch path without overwriting higher-priority ids with generated ones.
4. Prefer measured headers:
   - Claude Code: `X-Claude-Code-Session-Id`
   - Codex: `session-id` / `thread-id`
   - OpenCode: `x-session-id` / `x-session-affinity`
   - pi responses: `session_id` + optional gateway header injection

Home issue can land schema/API first with defensive parsers; full fidelity needs CPA reporting.

## 11. Phased Delivery

### Phase 0 — Spec freeze

- Agree extractor priority and confidence rules.
- Agree DB columns and Management API shapes.
- Document client matrix from research + probe captures.

### Phase 1 — Home extraction + binding correctness

- Upgrade extractor in Home selector.
- Disable low-confidence sticky by default.
- Add tests for Claude Code / Codex / OpenCode / pi header fixtures.
- Optional: cluster-safe binding store.

### Phase 2 — Persist + query

- Add usage session columns + indexes.
- Accept session fields from usage payload.
- Extend `/usage/records` filters/response.
- Backfill: none required; new data only is fine.

### Phase 3 — Session APIs

- `/sessions`, `/sessions/:id`, `/sessions/:id/requests`.
- Aggregate stats.
- Binding inspect/expire.

### Phase 4 — HomeUI

- Session filter in usage page.
- `/admin/sessions` list + detail timeline.
- Deep links from usage rows and diagnostics.

### Phase 5 — Hardening

- probe-based compatibility tests per client version.
- multi-Home sticky consistency test.
- privacy review for session id retention TTL.

## 12. Testing Plan

### Unit

- extractor fixtures for each client header/body form
- confidence classification
- routing key namespace isolation across API keys
- no sticky on low confidence

### Integration

- two concurrent sessions stick to different auths when multiple auths available
- same session reuses auth across turns
- auth cooldown triggers rebind
- usage ingest stores session fields
- Management API filter by session_id returns only matching records

### UI

- paste session id filter
- open session detail timeline
- request log download from a session request
- empty/low-confidence states

### Compatibility probe

Reuse local capture methodology from session-id probe:

1. two sessions × two turns
2. stable same-session ids
3. different sessions remain distinct
4. subagent/thread semantics recorded but root sticky preserved
5. compact/resume/retry does not invent a new high-confidence session

## 13. Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| Cross-tenant session id collision | namespace by API key/tenant in routing key |
| Low-confidence false merges | sticky only for high/medium; mark low |
| Multi-Home split brain sticky | shared DB bindings + TTL |
| Header stripping by proxies | prefer hyphen headers; document required hop-by-hop allowlists |
| Privacy of session ids in logs | truncate in runtime logs; full id only in Management |
| CPA/Home extractor drift | shared test vectors / single documented matrix |
| Huge session timelines | pagination + time-range defaults |

## 14. Issue Split

### Home (`router-for-me/CLIProxyAPIHome`)

Backend ownership:

1. extractor + confidence model
2. cluster-safe sticky binding store
3. usage schema + ingest
4. Management API filters and `/sessions*` endpoints
5. docs under `docs/management/api.md`

### HomeUI (`router-for-me/Home-Management-Center`)

Frontend ownership:

1. usage filters/columns for session id
2. `/admin/sessions` list and detail
3. deep link from usage/diagnostics to session timeline
4. request-log actions inside session detail

### CPA (`router-for-me/CLIProxyAPI`) — dependency note

Edge extraction + usage payload fields. Track separately if needed; Home parsers should tolerate absence.

## 15. Suggested Operator Workflow After Delivery

```text
1. User reports bad answer / 429 / wrong account in Claude Code session S
2. Operator opens HomeUI → Sessions (or Usage → Session filter)
3. Paste S
4. Inspect ordered requests, credentials, models, failures
5. Open failing request log
6. If sticky target is unhealthy, expire binding / disable auth
7. Confirm next turn rebinds cleanly
```

## 16. Decision Summary

1. **Session binding and session observability are one product surface**, not two unrelated features.
2. Prefer **native client session headers** and explicit gateway headers; do not guess from messages.
3. Store **operator-facing session_id** on usage records so HomeUI can filter conversations like CCH.
4. Keep sticky routing **namespaced, TTL’d, health-aware, and cluster-safe**.
5. Deliver Home API first, then HomeUI session workbench.

## 17. References

- Local research: `/Users/sususu/agent-session-id-gateway-research.md`
- CCH session UI/API: `ding113/claude-code-hub` sessions dashboard + `message_request.session_id`
- Home current selector: `internal/cliproxy/auth/selector.go`
- Home usage model: `internal/cluster/usage.go`
- Home usage observability: `internal/cluster/usage_observability.go`
- HomeUI usage workbench: `Home-Management-Center` `/admin/usage`
- Related HomeUI issue: request event logs (`#31`)
