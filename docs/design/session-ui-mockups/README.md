# Session Observability UI Mockups

Static design references for HomeUI session filtering and conversation timeline.

These are **design mockups**, not production screenshots.

## Files

| File | Surface | Purpose |
| --- | --- | --- |
| `usage-session-filter.png` | `/admin/usage` | Session ID filter + session columns on usage table |
| `sessions-list.png` | `/admin/sessions` | Session list / search workbench |
| `session-detail.png` | `/admin/sessions/:sessionId` | Ordered request timeline + turn detail + sticky binding audit |
| `index.html` | all three | Interactive HTML review page |
| `*.html` | single pages | Source used to render PNGs |

## Design intent

1. Operators paste a client session id from Claude Code / Codex / pi / OpenCode logs.
2. They see only that conversation’s requests.
3. They open one turn and jump to request log / credential / rebound history.
4. UI stays Home-native and reuses usage detail patterns; it is not a port of another product.

## Related

- Design: `../session-binding-and-observability.md`
- Home issue: https://github.com/router-for-me/CLIProxyAPIHome/issues/53
- HomeUI issue: https://github.com/router-for-me/Home-Management-Center/issues/68
