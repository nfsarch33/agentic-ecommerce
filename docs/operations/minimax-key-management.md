# MiniMax API Key Management

> Last verified: 2026-05-11

## Overview

The EC stack uses MiniMax for LLM-powered content generation (description_gen, video script, hashtag/caption, coaching tips, AI payment advisor). Two API keys provide quota redundancy via automatic failover.

## Key Storage

Keys are stored in **1Password <vault-name> vault**:

| Alias | 1Password Item | Env Var |
|-------|---------------|---------|
| `minimax-api-1` | `minimax-api-1` in <vault-name> | `EC_MINIMAX_API_KEY_1` |
| `minimax-api-2` | `minimax-api-2` in <vault-name> | `EC_MINIMAX_API_KEY_2` |

**Security rules:**
- Keys are NEVER committed to code or config files
- Keys are NEVER passed on command-line arguments (no shell leak)
- Keys stay in process memory only; Redis stores key *state metadata* (exhausted/cooldown), not the keys themselves

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `EC_MINIMAX_API_KEY_1` | Yes | — | Primary MiniMax API key |
| `EC_MINIMAX_API_KEY_2` | Yes | — | Secondary MiniMax API key |
| `EC_MINIMAX_STICKY_KEY` | No | `1` | Which key to prefer (`1` or `2`) |
| `EC_MINIMAX_COOLDOWN_SECONDS` | No | `3600` | Seconds before re-checking an exhausted key |

## Operator Workflow

### Initial Setup

```bash
# Export keys from 1Password to env (operator machine only)
export EC_MINIMAX_API_KEY_1="$(op read 'op://<vault-name>/minimax-api-1/credential')"
export EC_MINIMAX_API_KEY_2="$(op read 'op://<vault-name>/minimax-api-2/credential')"
```

### Key Rotation

When a key is renewed in 1Password:

1. Update the credential in 1Password
2. Re-export the env var on affected workers
3. Restart the affected EC stack workers
4. Cooldown state in Redis auto-clears on restart

### Checking Key Status

```bash
# Via the existing runx minimax surface
runx minimax status          # Shows active key, cooldown state, failover history
runx minimax force-failover  # Manually switch to alternate key
runx minimax quota           # View remaining quota per key
```

## Architecture

```
┌──────────────────────────────────────────────────────┐
│ EC Stack Worker                                       │
│                                                       │
│  ┌─────────────────┐    ┌──────────────────────┐     │
│  │ LLMFailoverChain│───▶│ MinimaxAdapter       │     │
│  │                 │    │ ┌──────────────────┐ │     │
│  │ ironclaw ──────▶│    │ │ Key 1 (sticky)   │ │     │
│  │ minimax ───────▶│    │ │ Key 2 (failover) │ │     │
│  │ template ──────▶│    │ └──────────────────┘ │     │
│  └─────────────────┘    └──────────┬───────────┘     │
│                                    │                  │
│                         ┌──────────▼───────────┐     │
│                         │ Redis (key state)    │     │
│                         │ minimax:key_state:1  │     │
│                         │ minimax:key_state:2  │     │
│                         └──────────────────────┘     │
└──────────────────────────────────────────────────────┘
```

## Failover Behaviour

| HTTP Status | Meaning | Action |
|-------------|---------|--------|
| 429 | Rate limited | Switch to alternate key |
| 402 | Quota exhausted | Switch to alternate key, start cooldown |
| Other 4xx/5xx | Transient error | Retry via circuit breaker |

## Monitoring

Grafana dashboard: `monitoring/grafana/minimax-quota.json`

| Panel | Metric |
|-------|--------|
| Request Rate per Key | `ec_minimax_requests_total` |
| Failover Timeline | `ec_minimax_failover_events_total` |
| Quota Status | `ec_minimax_key_cooldown_remaining_seconds` |
| Latency | `ec_minimax_request_duration_seconds` |
| Active Key | `ec_minimax_active_key` |

## Integration with EC Stack

The adapter at `internal/adapter/llm/minimax_adapter.go` reads `EC_MINIMAX_API_KEY_1` and `EC_MINIMAX_API_KEY_2` from environment at startup. The `LLMFailoverChain` at `internal/adapter/llm/failover_enhanced.go` orchestrates the IronClaw → MiniMax → template fallback order.
