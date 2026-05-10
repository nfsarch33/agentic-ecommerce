# Skill Dedup Recommendations v4.16.0

Generated: 2026-05-11 | Based on: skill-inventory-audit-v4160.md

## Recommendations Summary

| Action | Count |
|--------|-------|
| Merge (consolidate multiple → one) | 3 |
| Retire (superseded, remove) | 4 |
| Description update (routing accuracy) | 5 |
| Extract to EC repo (docs/skills/) | 10 |
| **Total recommendations** | **22** |

---

## 1. Merge Recommendations

### 1.1 Memory skills → `memory-unified` + `mem0-ops`

**Current state:** 5 separate skills (memory-system, memory-and-kb, memory-hygiene, context-mode, mem0-selfhost-ops)

**Proposed action:** Merge memory-system + memory-and-kb + memory-hygiene + context-mode into a single `memory-unified` skill. Keep `mem0-selfhost-ops` separate (infrastructure-specific).

**Rationale:** These skills share 60%+ overlap in trigger phrases ("memory", "routing", "store", "retrieve"). Agents frequently activate the wrong one. A single skill with internal sections reduces token waste from duplicate preambles.

**Estimated effort:** 2 hours (combine content, update 00-index, test routing accuracy)

### 1.2 Research skills → `research-unified` + `ai-research-manager`

**Current state:** 5 separate skills (research-pipeline, research-automation, autonomous-research, aris-research-integration, ai-research-manager)

**Proposed action:** Merge research-pipeline + research-automation + autonomous-research + aris-research-integration into `research-unified`. Keep `ai-research-manager` (it's an installer, different function).

**Rationale:** "Run research" triggers 4 skills simultaneously. Pipeline and automation are effectively the same workflow documented twice.

**Estimated effort:** 2 hours

### 1.3 Academic skills → `academic-pipeline`

**Current state:** 4 separate skills (academic-assessment, academic-essay-writer, academic-humanizer, agent-self-evaluation)

**Proposed action:** Merge all 4 into `academic-pipeline` with sections for assessment → writing → humanizing → self-evaluation.

**Rationale:** These form a sequential pipeline. Having them separate forces the agent to activate multiple skills for a single essay task. One skill with the full pipeline is more effective.

**Estimated effort:** 1.5 hours

---

## 2. Retire Recommendations

### 2.1 `gh-address-comments` → absorbed into `code-review-pro`

**Current state:** Still installed in `~/.cursor/skills/`. Description says "SUPERSEDED by code-review-pro" but file remains.

**Proposed action:** Delete `~/.cursor/skills/gh-address-comments/SKILL.md`. The `code-review-pro` skill fully covers PR comment addressing with additional capabilities.

**Rationale:** Leaving superseded skills causes routing confusion. The description note is insufficient since the skill still appears in the manifest.

**Estimated effort:** 5 minutes

### 2.2 `pptx-handler` → absorbed into `pptx-mastery`

**Current state:** Both installed. `pptx-mastery` is a superset with visual QA, brand templates, and slide-to-image.

**Proposed action:** Delete `~/.cursor/skills/pptx-handler/SKILL.md`.

**Rationale:** Same as 2.1. `pptx-mastery` explicitly covers all handler functionality plus more.

**Estimated effort:** 5 minutes

### 2.3 Remove `.claude/skills/` duplicates (4 files)

**Current state:** mem0-selfhost-ops, oracle-cloud-recon, subagent-incident-review, workspace-hygiene-doctor exist in both `.claude/skills/` and `.cursor/skills/`.

**Proposed action:** Delete all 4 from `~/.claude/skills/`. The Cursor location is the canonical install path.

**Rationale:** Dual-location skills cause unpredictable routing depending on which agent surface is active. Single source of truth principle.

**Estimated effort:** 5 minutes

### 2.4 `temporal-developer.bak.*` backup file

**Current state:** A `.bak.20260429T011504Z` backup directory exists alongside the live `temporal-developer` skill.

**Proposed action:** Delete the backup directory.

**Rationale:** Backup files in the skill tree waste index space and could confuse scanners.

**Estimated effort:** 2 minutes

---

## 3. Description Update Recommendations

Per `agent-skills-optimization` skill guidance, these skills have descriptions that are too vague or missing key trigger phrases for accurate routing:

### 3.1 `research-automation`

**Current:** No description (empty SKILL.md body after heading)

**Proposed:** "Run automated academic research with web scraping, PDF extraction, and LLM-powered essay generation. Use when: automating research workflows, scraping academic sources, extracting data from PDFs."

### 3.2 `ironclaw-multi-agent`

**Current:** No description

**Proposed:** "Multi-agent coordination patterns for IronClaw: task delegation, parallel execution, conflict resolution, and result aggregation. Use when: designing multi-agent workflows, coordinating parallel IronClaw tasks, handling agent conflicts."

### 3.3 `tailscale-fleet`

**Current:** No description

**Proposed:** "Manage Tailscale mesh network for the distributed agent fleet: device enrollment, ACL policies, exit nodes, subnet routing. Use when: configuring Tailscale connectivity, managing fleet network access, troubleshooting mesh routing."

### 3.4 `academic-essay-writer`

**Current:** No description

**Proposed:** "Generate academic essays with proper structure, citations, and academic voice. Use when: writing essays from scratch, structuring academic arguments, adding citations. Note: for full pipeline (draft → humanize → evaluate), use academic-pipeline."

### 3.5 `mem0-selfhost-ops`

**Current:** No description

**Proposed:** "Deploy and operate self-hosted Mem0 instance: Docker setup, Qdrant vector store, API key management, backup/restore, health monitoring. Use when: deploying Mem0, maintaining the vector store, troubleshooting Mem0 API connectivity."

---

## 4. EC-Stack Extraction Recommendations

These skills are relevant to `agentic-ecommerce` development and should have reference entries in `docs/skills/`:

| Skill | EC Usage | Extract As |
|-------|----------|-----------|
| go-clean-architecture | All new Go packages | Reference doc |
| go-performance-optimization | Hot-path code | Reference doc |
| go-security-review | Auth, crypto, webhooks | Reference doc |
| temporal-developer | All Temporal workflows | Reference doc |
| temporal-orchestration | Task queue design | Reference doc |
| docker-ops | Dockerfile/compose | Reference doc |
| monitoring-observability | Prometheus/Grafana | Reference doc |
| react-best-practices | Frontend components | Reference doc |
| project-management | Sprint workflow | Reference doc |
| zbt-e2e-testing | E2E testing | Reference doc |

**Note:** Actual SKILL.md files stay in their install locations. The EC repo only contains a catalog (`docs/skills/README.md`) pointing agents to the correct skill for each task type.

---

## Implementation Priority

| Priority | Action | Impact |
|----------|--------|--------|
| P0 (now) | Retire gh-address-comments, pptx-handler | Eliminates active routing confusion |
| P0 (now) | Remove .claude/skills duplicates | Single source of truth |
| P1 (next sprint) | Merge memory skills | Reduces 5→2 skills, saves ~8K tokens |
| P1 (next sprint) | Update empty descriptions (5 skills) | Improves routing accuracy |
| P2 (backlog) | Merge research skills | Reduces 5→2 skills |
| P2 (backlog) | Merge academic skills | Reduces 4→1 skill |
