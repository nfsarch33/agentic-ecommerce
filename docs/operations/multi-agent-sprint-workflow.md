# Multi-Agent Sprint Workflow

> Last verified: 2026-05-11

How the worktree hardening (v4.15.0) integrates with the EC stack sprint cadence.

## Sprint Pair Lifecycle

Each sprint pair (MVP + QA) follows this sequence:

```
┌──────────────────────────────────────────────────────────────┐
│ 1. CREATE WORKTREE                                           │
│    runx worktree add --repo ecommerce                        │
│      --branch feat/v<version>-<name> --from main             │
│                                                              │
│ 2. LOCK ACQUIRED (automatic)                                 │
│    CoordLocker.Acquire(ctx, repo, branch, agentID)           │
│    → Lock file at ~/.locks/<repo>/<branch>.lock              │
│                                                              │
│ 3. RACE CHECK (automatic)                                    │
│    RaceDetector.Check(repo, branch, agentID, pid)            │
│    → Scans existing locks for same-branch conflicts          │
│                                                              │
│ 4. IMPLEMENT (agent works on branch)                         │
│    - TDD-first: RED tests → GREEN implementation             │
│    - Sentrux complexity gate (complex_fn ≤ 4)                │
│    - Pre-commit hooks enforced                               │
│                                                              │
│ 5. HANDOFF (if switching agents)                             │
│    - Outgoing: write .handoff.md → release lock              │
│    - Incoming: acquire lock → read .handoff.md → verify      │
│    (see worktree-handoff-protocol.md)                        │
│                                                              │
│ 6. MERGE                                                     │
│    - Push branch, create PR via gh                           │
│    - CI must pass (lint, test, sentrux, govulncheck)         │
│    - Merge on green                                          │
│                                                              │
│ 7. CLEANUP (automatic on merge)                              │
│    - Lock released: CoordLocker.Release(repo, branch, agent) │
│    - Worktree pruned: git worktree prune                     │
│    - Memory hygiene: stale Mem0 entries cleaned              │
│                                                              │
│ 8. STALE CLEANUP (start of next pair or on-demand)           │
│    - ScanStale scans ~/runs/worktrees/ for abandoned trees   │
│    - CleanupStale removes stale + merged worktrees           │
│    - Dry-run first, then apply                               │
└──────────────────────────────────────────────────────────────┘
```

## Parallel Agent Safety

The coordination lock system prevents the following failure modes:

| Failure Mode | Prevention |
|---|---|
| Two agents on same branch | `RaceDetector.Check` returns `ErrWorktreeRaceDetected` |
| Agent crash leaves lock | Stale lock detection (PID check) + TTL expiry (2h default) |
| Orphaned worktrees | `ScanStale` + `CleanupStale` at pair start |
| Handoff data loss | `.handoff.md` protocol with HEAD verification |

## Integration with plan-sync.mdc

The coordination lock is a recommended step in the plan-sync rule:

1. **Before starting a sprint pair**: verify no stale locks via `CoordLocker.Check`
2. **On pair start**: acquire lock as first operation
3. **On pair end**: release lock as part of memory hygiene
4. **On stale cleanup**: run `ScanStale` + `CleanupStale` with dry-run first

## CLI Commands

| Command | Description |
|---|---|
| `runx worktree add --repo <r> --branch <b> --from main` | Create worktree + acquire lock |
| `runx worktree list --repo <r>` | List managed worktrees |
| `runx worktree cleanup --repo <r> --dry-run` | Preview stale cleanup |
| `runx worktree cleanup --repo <r>` | Execute stale cleanup |

## Prometheus Metrics

| Metric | Type | Labels | Est. Series |
|---|---|---|---|
| `ec_worktree_lock_acquisitions_total` | counter | `repo`, `outcome` | ~20 |
| `ec_worktree_cleanups_total` | counter | `repo`, `reason` | ~10 |
| `ec_worktree_handoffs_total` | counter | `repo`, `outcome` | ~6 |

Total new series: ~36 (within the ~30 budget with margin).

## Example: Sprint Pair 15

```bash
# 1. Create worktree
runx worktree add --repo ecommerce \
  --branch feat/v4150-worktree-hardening --from main

# 2. Lock is acquired automatically
# 3. Race check passes (no other agent on this branch)

# 4. Implement stories 1-5
# ... TDD cycle ...

# 5. No handoff needed (single agent pair)

# 6. Push + PR + merge
git push -u origin feat/v4150-worktree-hardening
gh pr create --title "feat(v4150): worktree hardening"
# ... CI passes ...
gh pr merge --squash

# 7. Cleanup
# Lock released automatically on merge
# Worktree pruned in memory hygiene step

# 8. Stale cleanup at start of Pair 16
runx worktree cleanup --repo ecommerce --dry-run
runx worktree cleanup --repo ecommerce
```
