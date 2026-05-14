# Architecture Decision Records

> Last verified: 2026-05-14

## Ecommerce Project ADRs (in-repo)

| Number | Title | Status | Date | Superseded By | File |
|--------|-------|--------|------|---------------|------|
| ADR-024 | v1.0.0 Release Decisions | Accepted | 2026-05-07 | — | [adr-024-v1-release-decisions.md](adr-024-v1-release-decisions.md) |
| ADR-025 | v2.0.0 Release Decisions | Accepted | 2026-05-08 | — | [adr-025-v2-release-decisions.md](adr-025-v2-release-decisions.md) |
| ADR-026 | v3.0.0 Release Decisions + Agentrace Rename | Superseded | 2026-05-09 | ADR-029 | In global-kb |
| ADR-027 | Resilience and Observability Pillar | Accepted | 2026-05-09 | — | [adr-027-resilience-pillar.md](adr-027-resilience-pillar.md) |
| ADR-028 | v4.0.0 Sprint Plan (10 Epics) | Superseded | 2026-05-10 | ADR-029 | In global-kb (PR #159) |
| ADR-029 | v4.0.0 Release Decisions | Accepted | 2026-05-10 | — | [adr-029-v4-release-decisions.md](adr-029-v4-release-decisions.md) |
| ADR-030 | v4.1.0–v5.0.0 Roadmap | Superseded | 2026-05-10 | ADR-031 | [adr-030-v5-roadmap.md](adr-030-v5-roadmap.md) |
| ADR-031 | v5.0.0 Release Decisions | Accepted | 2026-05-11 | — | [adr-031-v5-release-decisions.md](adr-031-v5-release-decisions.md) |
| ADR-032 | v6.0.0 Release Decisions | Accepted | 2026-05-11 | — | [adr-032-v6-release-decisions.md](adr-032-v6-release-decisions.md) |
| ADR-033 | v6.6.0 Release Decisions + v7 Preview | Accepted | 2026-05-11 | — | [adr-033-v660-release-decisions.md](adr-033-v660-release-decisions.md) |
| ADR-034 | v7.5.1 Release Decisions | Accepted | 2026-05-12 | — | [adr-034-v751-release-decisions.md](adr-034-v751-release-decisions.md) |
| ADR-035 | v8.0.0 Release Decisions | Accepted | 2026-05-13 | — | [adr-035-v8-release-decisions.md](adr-035-v8-release-decisions.md) |
| ADR-036 | v9.0.0 Release Decisions | Accepted | 2026-05-14 | — | [adr-036-v9-release-decisions.md](adr-036-v9-release-decisions.md) |

## Pre-Ecommerce Ecosystem ADRs (in global-kb)

These ADRs predate or run alongside the ecommerce project and reside in `nfsarch33/cursor-global-kb`.

| Number | Title | Status | Date |
|--------|-------|--------|------|
| ADR-001 | Observability Strategy + Temporal for Agent Orchestration | Accepted | 2026-04 |
| ADR-002 | Python-to-Go Migration + Commerce Stack Selection | Accepted | 2026-04 |
| ADR-003 | China VPN Staged Validation | Accepted | 2026-04 |
| ADR-004 | Mem0 Async Projection Outbox | Accepted | 2026-04 |
| ADR-005 | (Reserved) | — | — |
| ADR-006 | Personal Repo Visibility Policy | Accepted | 2026-04 |
| ADR-007 | LLM Cluster Router OSS Extraction | Accepted | 2026-04 |
| ADR-008 | (Reserved) | — | — |
| ADR-009 | (Reserved) | — | — |
| ADR-010 | (Reserved) | — | — |
| ADR-011 | vLLM v2 Router (Pending) | Proposed | 2026-04 |
| ADR-012 | (Reserved) | — | — |
| ADR-013 | Multi-Provider Email Notification | Accepted | 2026-05 |
| ADR-014 | Ansible Day-2 Fleet Drift | Accepted | 2026-05 |
| ADR-015 | (Reserved) | — | — |
| ADR-016 | Fleet SLOs | Accepted | 2026-05 |
| ADR-017 | WSL2 DERP-Only from Oracle Jump | Accepted | 2026-05 |
| ADR-018 | WSL2 DERP Gap Analysis | Accepted | 2026-05 |
| ADR-019 | Hermes Sidecar | Accepted | 2026-05 |
| ADR-020 | General OSS MCP Strategy + Claude Proxy Routing | Accepted | 2026-05 |
| ADR-021 | Mem0 Self-Host Migration | Accepted | 2026-05 |
| ADR-022 | Workspace Cleanliness Doctor | Accepted | 2026-05 |
| ADR-023 | Agentic Ecommerce Web + MiniMax Fleet Policy | Accepted | 2026-05 |

## Other In-Repo Documents

| File | Status | Date | Summary |
|------|--------|------|---------|
| [v302-001-polyrepo-split.md](v302-001-polyrepo-split.md) | Draft | 2026-05-05 | Early polyrepo split exploration for Agentic Ecommerce |

## Numbering Convention

- **ADR-001–023**: Ecosystem-level decisions in `nfsarch33/cursor-global-kb`
- **ADR-024–031**: Ecommerce-specific decisions (in-repo or cross-referenced)
- **ADR-032**: v6.0.0 release decisions
- **ADR-033**: v6.6.0 release decisions and v7 preview
- **ADR-034**: v7.5.1 release decisions and remaining v7 sprint batch
- **ADR-035**: v8.0.0 release decisions and v9 seed roadmap
- **ADR-036**: v9.0.0 release decisions and the v10 hardening runway
- **Reserved** numbers were allocated but never used during rapid sprint cycles
