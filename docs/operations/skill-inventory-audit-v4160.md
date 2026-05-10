# Skill Inventory Audit v4.16.0

> Last verified: 2026-05-11

Generated: 2026-05-11 | Sprint: v4.16.0 | Scope: Agent skill ecosystem health

## Summary

| Metric | Value |
|--------|-------|
| Total skills scanned | 155 |
| Location: `~/.cursor/skills/` | 138 |
| Location: `~/.claude/skills/` | 4 |
| Location: `~/.cursor/skills-cursor/` | 13 |
| Cross-location duplicates | 4 |
| Superseded (still installed) | 2 |
| Fragmented groups | 5 groups (27 skills) |
| EC-stack relevant | 10 |

## Inventory Table

### ~/.cursor/skills/ (138 skills)

| Skill | Description (first line) | Size | Category |
|-------|--------------------------|------|----------|
| 00-index | Master registry of all installed skills | - | Meta |
| academic-assessment | Write university assessment essays | - | Academic |
| academic-essay-writer | Academic essay writing | - | Academic |
| academic-humanizer | Detect/remove AI patterns from academic text | - | Academic |
| agent-browser | CLI browser automation via Playwright | - | Automation |
| agent-observability | Pipeline execution tracking, metrics | - | Observability |
| agent-reach | Read social platforms without paid APIs | - | Research |
| agent-self-evaluation | Heuristic self-evaluation for agent output | - | QA |
| agent-skills-optimization | Optimise skill exposure rates | - | Meta |
| ai-research-manager | Dynamic AI research skill installer | - | Research |
| architecture-decisions | Create/manage ADRs | - | Architecture |
| argocd | ArgoCD Applications and AppProjects | - | CI/CD |
| aris-research-integration | ARIS patterns with research stack | - | Research |
| automation-workflows | Identify automation opportunities | - | Automation |
| autonomous-research | Run overnight experiments | - | Research |
| aws-cloud-operations | AWS EC2, Lambda, S3, DynamoDB etc | - | Cloud |
| bayesian-decision-assistant | Bayesian belief revision | - | Decision |
| beautiful-mermaid | Render Mermaid diagrams | - | Visualization |
| canvas | Cursor Canvas live React apps | - | UI |
| ci-cd-pipelines | Makefile automation, deployment strategies | - | CI/CD |
| cicd-gitops-promotion | End-to-end GitOps promotion | - | CI/CD |
| cli-anything | Transform software into agent-controllable CLIs | - | Automation |
| cli-discipline | Use Cursor tools instead of raw shell | - | Discipline |
| cli-offload-orchestration | Route work to Codex/Claude CLI | - | Orchestration |
| cloud-architecture | Multi-cloud architecture design | - | Cloud |
| cluster-monitoring | Prometheus+Grafana for vLLM cluster | - | Monitoring |
| code-review-pro | Comprehensive code review (supersedes gh-address-comments) | - | Code Review |
| codefresh | Codefresh CI pipelines | - | CI/CD |
| context-hub | Fetch versioned API docs via chub CLI | - | Documentation |
| context-mode | Context Mode MCP usage patterns | - | Memory |
| docker-ops | Docker best practices | - | Infrastructure |
| dreamhost-manager | Manage DreamHost websites via SFTP/SSH | - | Hosting |
| find-skills | Discover and install agent skills | - | Meta |
| first-principles-thinking | Decompose problems into fundamentals | - | Decision |
| fleet-doctor | Unified agent health diagnostics | - | Operations |
| flutter-mastery | Comprehensive Flutter development | - | Mobile |
| fork-upstream-sync | Sync personal fork with upstream | - | Git |
| gcp-cloud-operations | GCP GKE, Cloud Run, Functions | - | Cloud |
| gemini-code-review | Gemini CLI for structured code review | - | Code Review |
| gh-address-comments | **SUPERSEDED** by code-review-pro | - | Code Review |
| gh-fix-ci | Debug/fix GitHub CI failures | - | CI/CD |
| git-worktree-manager | Manage git worktrees for parallel agents | - | Git |
| github-ci | GitHub Actions CI workflows | - | CI/CD |
| github-identity | GitHub identity separation | - | Git |
| go-clean-architecture | **EC-RELEVANT** Clean Architecture for Go | - | Go |
| go-mcp-server | Build MCP servers in Go | - | Go |
| go-performance-optimization | **EC-RELEVANT** High performance Go | - | Go |
| go-security-review | **EC-RELEVANT** Go security review/audit | - | Go |
| google-workspace-cli | Google Workspace automation via gws CLI | - | Automation |
| hf-hub-throughput | HuggingFace Hub download optimization | - | ML |
| homelab-k3s | k3s cluster for vLLM home lab | - | Infrastructure |
| http-discipline | Prefer WebFetch over curl/wget | - | Discipline |
| huggingface-ecosystem | HuggingFace Hub operations | - | ML |
| ironclaw-agent-dashboard | Grafana dashboards for IronClaw | - | IronClaw |
| ironclaw-ceo-agent | CEO agent patterns for IronClaw | - | IronClaw |
| ironclaw-deploy-ops | IaC deployment for Mission Control | - | IronClaw |
| ironclaw-evolver | Agent evolution traces | - | IronClaw |
| ironclaw-external-ops | IronClaw external interfaces | - | IronClaw |
| ironclaw-mission-control | Operate IronClaw Mission Control | - | IronClaw |
| ironclaw-multi-agent | IronClaw multi-agent patterns | - | IronClaw |
| ironclaw-orchestrator | Deploy/operate IronClaw | - | IronClaw |
| linkedin-job-hunt | LinkedIn job hunting workflows | - | Job Hunt |
| llm-cluster-router | Go HTTP proxy for vLLM clusters | - | Infrastructure |
| llm-model-evaluator | LLM evaluation and selection | - | ML |
| media-downloader | Download media from web platforms | - | Media |
| mem0-selfhost-ops | Mem0 self-hosted operations | - | Memory |
| memory-and-kb | Hybrid memory routing and consolidation | - | Memory |
| memory-hygiene | Audit and clean memory layers | - | Memory |
| memory-system | Mem0-first hybrid memory system | - | Memory |
| metrics-dashboard | Consolidate KPIs across agent stack | - | Monitoring |
| microservices-go | Go-first microservices architecture | - | Go |
| monitoring-observability | **EC-RELEVANT** Monitoring and alerting | - | Monitoring |
| multi-gpu-inference | Multi-GPU inference stack | - | ML |
| multi-search-engine | Multi-engine web search | - | Research |
| obsidian-tools | Obsidian-compatible content | - | Documentation |
| onepwd-form-autofill | 1Password Identity for form filling | - | Automation |
| openclaw-orchestrator | OpenClaw deployment and operation | - | Agent |
| openclaw-vllm | OpenClaw with local vLLM backends | - | Agent |
| opencode-controller | Unified model and session management | - | Agent |
| oracle-cloud-recon | Oracle Cloud reconnaissance | - | Cloud |
| pdf-operations | PDF read, merge, split, rotate | - | Documents |
| personal-repo-routing | Route work to correct repository | - | Git |
| personal-repo-shell-hygiene | Shell hygiene for personal repos | - | Git |
| php-mcp-server | Build MCP servers in PHP | - | PHP |
| podcast-reader | Transform podcast episodes to outlines | - | Media |
| post-merge-housekeeping | Post-PR merge cleanup | - | Git |
| pptx-handler | **SUPERSEDED** by pptx-mastery | - | Documents |
| pptx-mastery | PowerPoint creation/editing | - | Documents |
| product-management | Product management lifecycle | - | Management |
| project-management | Agile project management | - | Management |
| react-best-practices | **EC-RELEVANT** React/Next.js performance | - | Frontend |
| react-native-best-practices | React Native/Expo performance | - | Mobile |
| release-checklist | Tag-based release quality gates | - | Release |
| res-downloader | MITM proxy resource sniffer | - | Media |
| research-automation | Research automation | - | Research |
| research-pipeline | Academic research automation | - | Research |
| rtk-integration | rtk CLI proxy for token compression | - | Tools |
| rust-mastery | Comprehensive Rust development | - | Rust |
| seek-job-hunt | Seek.com.au job hunting | - | Job Hunt |
| self-healing-scrapers | Scrapers with auto-recovery | - | Automation |
| self-improvement-engine | Observe-Reflect-Heal-Evolve pipeline | - | Agent |
| self-improving-agent | Multi-memory self-improvement | - | Agent |
| session-handoff | Session transfer documents | - | Operations |
| session-logs | Locate/analyse prior conversations | - | Operations |
| skill-creator | Create new Agent Skills | - | Meta |
| skillvet | Security scanner for agent skills | - | Security |
| split-to-prs | Split work into reviewable PRs | - | Git |
| systematic-debugging | Root-cause-first debugging | - | Debugging |
| tailscale-fleet | Tailscale fleet management | - | Network |
| tech-meeting-summary | Technical meeting summaries | - | Documentation |
| temporal-developer | **EC-RELEVANT** Temporal applications | - | Temporal |
| temporal-orchestration | **EC-RELEVANT** Go workflow patterns for Temporal | - | Temporal |
| ui-agent-automation | Self-healing UI automation | - | Automation |
| ui-ux-pro-max | UI/UX design intelligence | - | UI |
| unified-go-logging | Standardized slog patterns | - | Go |
| update-cursor-settings | Modify Cursor/VSCode settings | - | Meta |
| upwork-job-hunt | Upwork job hunting | - | Job Hunt |
| vidbee-manager | VidBee media downloading | - | Media |
| web-search-plus | Multi-source web search | - | Research |
| workspace-hygiene-doctor | Workspace cleanliness audit | - | Operations |
| wp-blocks | WordPress block development | - | WordPress |
| wp-development | WordPress plugin/REST development | - | WordPress |
| wp-ops | WordPress WP-CLI and operations | - | WordPress |
| wsl-gpu-ops | NVIDIA GPU on WSL | - | Infrastructure |
| wsl-onboarding | Bootstrap WSL machine | - | Operations |
| youtube-downloader | Download YouTube videos | - | Media |
| zbt-e2e-testing | **EC-RELEVANT** Zendesk Browser Tests | - | Testing |

### ~/.claude/skills/ (4 skills -- all duplicated in ~/.cursor/skills/)

| Skill | Size (bytes) | Modified |
|-------|-------------|----------|
| mem0-selfhost-ops | 7,091 | 2026-05-04 |
| oracle-cloud-recon | 6,841 | 2026-05-04 |
| subagent-incident-review | 4,652 | 2026-05-04 |
| workspace-hygiene-doctor | 1,167 | 2026-05-02 |

### ~/.cursor/skills-cursor/ (13 built-in Cursor skills)

| Skill | Size (bytes) |
|-------|-------------|
| babysit | 977 |
| canvas | 9,186 |
| create-hook | 9,192 |
| create-rule | 3,636 |
| create-skill | 14,412 |
| create-subagent | 6,454 |
| migrate-to-skills | 6,439 |
| sdk | 13,411 |
| shell | 867 |
| split-to-prs | 2,261 |
| statusline | 7,200 |
| update-cli-config | 3,827 |
| update-cursor-settings | 4,296 |

## Duplicates Identified

| Skill | Locations | Action |
|-------|-----------|--------|
| mem0-selfhost-ops | .cursor/skills + .claude/skills | Remove from .claude/skills (Cursor is primary) |
| oracle-cloud-recon | .cursor/skills + .claude/skills | Remove from .claude/skills |
| subagent-incident-review | .cursor/skills + .claude/skills | Remove from .claude/skills |
| workspace-hygiene-doctor | .cursor/skills + .claude/skills | Remove from .claude/skills |

## Fragmented Skill Groups

| Group | Skills | Recommendation |
|-------|--------|----------------|
| IronClaw (8) | ironclaw-mission-control, ironclaw-orchestrator, ironclaw-evolver, ironclaw-deploy-ops, ironclaw-ceo-agent, ironclaw-multi-agent, ironclaw-external-ops, ironclaw-agent-dashboard | Keep separate (distinct operational domains) |
| Memory (5) | memory-system, memory-and-kb, memory-hygiene, context-mode, mem0-selfhost-ops | Consolidate into 2: `memory-unified` (routing+hygiene+kb) + `mem0-ops` (self-host) |
| Research (5) | research-pipeline, research-automation, autonomous-research, aris-research-integration, ai-research-manager | Consolidate into 2: `research-unified` (pipeline+auto+aris) + `ai-research-manager` (installer) |
| CI/CD (5) | ci-cd-pipelines, cicd-gitops-promotion, codefresh, argocd, github-ci | Keep separate (vendor-specific, well-scoped) |
| Academic (4) | academic-assessment, academic-essay-writer, academic-humanizer, agent-self-evaluation | Consolidate into 1: `academic-pipeline` (assessment+writer+humanizer) |

## EC-Stack Specific Skills

| Skill | EC Usage |
|-------|----------|
| go-clean-architecture | All new Go packages |
| go-performance-optimization | Hot-path code (event bus, Temporal activities) |
| go-security-review | Auth, crypto, webhook handlers |
| temporal-developer | All Temporal workflows |
| temporal-orchestration | Task queue design, SDK patterns |
| docker-ops | Dockerfile and compose changes |
| monitoring-observability | Prometheus, Grafana, alerting |
| react-best-practices | Frontend components |
| project-management | Sprint workflow |
| zbt-e2e-testing | Contact Center E2E tests |

## Superseded Skills (Retire Candidates)

| Skill | Superseded By | Status |
|-------|---------------|--------|
| gh-address-comments | code-review-pro | Still installed; explicit note in description |
| pptx-handler | pptx-mastery | Still installed; no deprecation note |
