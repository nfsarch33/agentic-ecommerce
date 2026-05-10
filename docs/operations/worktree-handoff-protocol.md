# Worktree Handoff Protocol

> Last verified: 2026-05-11

Formal protocol for transferring worktree ownership between parallel Cursor agents operating on the same `agentic-ecommerce` repository (or any runx-managed repo).

## Context

Sprint pairs frequently involve multiple agents (e.g. a primary implementer and a QA agent) that need sequential access to the same branch. Without a structured handoff, the incoming agent cannot reliably determine what state the outgoing agent left the worktree in.

## Protocol Steps

### 1. Before Handoff (outgoing agent)

The outgoing agent creates a handoff file:

```
~/runs/worktrees/<repo>/<sanitized-branch>/.handoff.md
```

### 2. Handoff File Content

| Field | Description |
|---|---|
| `branch` | Git branch name (e.g. `feat/v4150-worktree-hardening`) |
| `head_sha` | Current HEAD commit SHA |
| `dirty_files` | List of uncommitted / unstaged changes |
| `pending_tasks` | Outstanding work items (test failures, TODO comments, etc.) |
| `operational_state` | One of: `clean`, `wip`, `blocked`, `needs-review` |
| `outgoing_agent_id` | Agent ID that is releasing the worktree |
| `incoming_agent_id` | Agent ID that should pick up the worktree |
| `timestamp` | ISO-8601 timestamp of handoff creation |
| `notes` | Free-form context for the incoming agent |

### 3. Transfer Sequence

```
Outgoing Agent                    Incoming Agent
─────────────                     ──────────────
1. Write .handoff.md
2. git add + commit (if clean)
3. Release coordination lock
   (CoordLocker.Release)
                                  4. Acquire coordination lock
                                     (CoordLocker.Acquire)
                                  5. Read .handoff.md
                                  6. Verify HEAD matches handoff
                                  7. Run `git status`
                                  8. Resume work
```

### 4. Verification (incoming agent)

The incoming agent MUST verify before resuming work:

1. **Branch HEAD matches** the `head_sha` in the handoff file
2. **Dirty files match** the `dirty_files` list (no unexpected modifications)
3. **Operational state** is acknowledged (e.g. if `blocked`, read the `notes` field)
4. **Lock acquired** via `CoordLocker.Acquire` before any git operations

### 5. Cleanup (incoming agent)

After verifying the handoff, the incoming agent deletes the handoff file:

```bash
rm ~/runs/worktrees/<repo>/<sanitized-branch>/.handoff.md
```

## Handoff File Template

```markdown
# Worktree Handoff

- **Branch**: feat/v4150-worktree-hardening
- **HEAD**: 866648b
- **Dirty files**: none
- **Pending tasks**:
  - [ ] Run integration tests
  - [ ] Update CHANGELOG
- **Operational state**: clean
- **Outgoing agent**: agent-sprint-15-impl
- **Incoming agent**: agent-sprint-15-qa
- **Timestamp**: 2026-05-11T00:55:00+10:00
- **Notes**: All unit tests passing. E2E suite not yet run.
```

## Integration with Coordination Locks

The handoff protocol requires coordination locks (see `coord_lock.go`):

- The outgoing agent MUST hold the lock during `.handoff.md` creation
- The outgoing agent MUST release the lock after writing the handoff file
- The incoming agent MUST acquire the lock before reading the handoff file
- If the lock is held when the incoming agent tries to acquire, it waits or reports a race

## Error Recovery

| Scenario | Resolution |
|---|---|
| Handoff file missing | Incoming agent starts fresh; checks `git log` and `git status` |
| HEAD mismatch | Incoming agent runs `git fetch` + `git log` to reconcile |
| Stale handoff (>2h old) | Treat as abandoned; delete handoff + acquire lock |
| Outgoing agent crashed | Stale lock auto-cleanup handles the lock; handoff file may be incomplete |

## Prometheus Metrics

| Metric | Type | Labels |
|---|---|---|
| `ec_worktree_handoffs_total` | counter | `repo`, `outcome` (success, mismatch, stale) |

## History

This protocol formalises the informal pattern used during the v3.x-v4.x sprint cycle where agents communicated worktree state through ad-hoc session handoff documents and Mem0 entries.
