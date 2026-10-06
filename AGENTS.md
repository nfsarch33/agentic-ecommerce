# agentic-ecommerce — Agent Guidelines

## Versioning & rollout (rule 00-p1-semver-seamless-rollout)
- SemVer 2.0.0; the version is derived from conventional commits (feat=minor, fix=patch, BREAKING=major) by tooling — never typed by hand.
- Tag vX.Y.Z + a CHANGELOG section per release.
- Go builds: -buildvcs=true; go version -m must show vcs.revision=<merge sha>, modified=false.
- Containers: tag=version, digest-pinned in CaC; `latest` is forbidden.
- Build once, promote the same artefact; deploy keeps .prev / previous image tag.
- Post-deploy health gate (/healthz + one business probe); rollback restores .prev.
- Schema/config changes expand-and-contract (additive first, remove one release later).
- Behaviour changes behind a flag, canary lane (B) first, control (A) second.
- API/config compatibility window: one minor version + deprecation note.
- A release closes only when the running artefact reports the merge sha (merged != deployed).
