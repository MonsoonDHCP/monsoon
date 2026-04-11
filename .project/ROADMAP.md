# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-11
> This roadmap prioritizes work needed to bring MonsoonDHCP to production quality.

## Current State Assessment

MonsoonDHCP is already beyond prototype stage. It compiles cleanly, its CI is functional, the core DHCP/IPAM path is real, and the embedded dashboard is usable. The project is not production-ready yet because the surrounding claims and operational guarantees are ahead of the implementation.

Key blockers for production readiness:

- Discovery and rogue-DHCP capabilities are overstated
- HA is present but not trustworthy enough for high-confidence failover
- Documentation materially misrepresents the implemented product
- Auth state is durable across restart on one node, but not HA-shared across peers

What is working well:

- DHCPv4 and DHCPv6 protocol handling
- General package structure and code hygiene
- Storage snapshot/WAL baseline
- Modern frontend with usable operator workflows
- Build/test pipeline baseline

## Phase 1: Critical Fixes (Week 1-2)

### Must-fix items blocking basic functionality

- [x] Persist browser sessions in storage so restart no longer drops local operator auth. Remaining gap: HA/shared-session semantics. Affected areas: `internal/auth/session.go`, REST auth cookie flows. Completed.
- [x] Fix `PUT /api/v1/system/config` so it cannot reset unspecified fields to defaults. Affected areas: `cmd/monsoon/main.go`, frontend settings save flow. Completed.
- [ ] Correct product claims around discovery, HA, storage, auth scope, and API breadth in `README.md` and project docs. Effort: 6-10h.
- [ ] Decide whether the storage engine will remain "simple WAL + snapshot + sorted map" or evolve toward the documented B+Tree/page design. Effort: 4-8h decision, much larger implementation if upgraded.
- [ ] Harden HA transport assumptions for any non-lab deployment. Minimum step: documented private-network-only requirement and explicit warnings. Better step: authenticated encrypted channel. Effort: 8-24h.

## Phase 2: Core Completion (Week 3-6)

### Complete missing core features from specification

- [x] Implement accurate subnet utilization/capacity accounting based on real subnet and DHCP pool sizes. Completed in `internal/ipam/engine.go`.
- [ ] Add missing IPAM write endpoints for addresses or remove those claims from docs and UI expectations. Spec reference: Section 4, Section 6. Effort: 12-20h.
- [ ] Implement VLAN domain model and CRUD if it remains part of v1 scope. Current code only has hints of VLAN storage/import support. Effort: 16-30h.
- [ ] Implement LDAP/AD auth or remove it from config/schema/docs. Spec reference: Section 10. Effort: 12-24h.
- [ ] Decide whether to keep MCP as a supported first-class surface for v1. If yes, improve test depth and compatibility expectations. Effort: 8-16h.
- [ ] Close the gap between actual and documented REST API surface. Effort: 12-24h depending on whether code or docs move.

## Phase 3: Hardening (Week 7-8)

### Security, error handling, edge cases

- [x] Replace HA secret equality checks with constant-time comparison. Remaining work: formalize and harden the node trust model.
- [ ] Review every `context.Background()` usage in request paths and propagate request-scoped contexts where appropriate.
- [ ] Add stronger error typing and response normalization across REST/gRPC/MCP.
- [ ] Review shell-out discovery commands for timeout, portability, and safe failure behavior.
- [ ] Tighten health/readiness semantics so "ready" means more than "engine transaction succeeds".
- [ ] Audit log redaction and log structure; move toward a real structured logger.

## Phase 4: Testing (Week 9-10)

### Comprehensive test coverage

- [ ] Add end-to-end tests for primary operator workflow: bootstrap admin, create subnet, observe lease, reserve address, backup config.
- [ ] Add integration tests for discovery persistence and HA status paths.
- [ ] Add focused tests for config patch/update semantics.
- [ ] Add frontend tests around settings, auth bootstrap, and discovery status rendering.
- [ ] Enable race testing in CI on an environment with CGO available.
- [ ] Add benchmark or load-style tests for lease lookup/store and dashboard hydration.

## Phase 5: Performance & Optimization (Week 11-12)

### Performance tuning and optimization

- [ ] Split `use-dashboard-data` into smaller hooks and reduce full-dashboard polling load.
- [ ] Add route-level lazy loading or code splitting to reduce the main frontend bundle.
- [ ] Revisit storage scaling characteristics if large address inventories are an intended use case.
- [ ] Replace heuristic IPAM summary generation with precomputed or efficient real metrics.
- [ ] Evaluate whether WebSocket + polling + SSE fallback can be simplified to reduce duplicate refresh work.

## Phase 6: Documentation & DX (Week 13-14)

### Documentation and developer experience

- [ ] Rewrite `README.md` to distinguish "implemented now" vs "planned next".
- [ ] Update `.project/SPECIFICATION.md` or replace it with an ADR-backed roadmap if scope has changed.
- [ ] Add `CONTRIBUTING.md` and a real developer setup guide.
- [ ] Document deployment requirements for DHCP privilege/networking, data persistence, TLS, and HA networking.
- [ ] Document backup/restore procedure and failure modes.
- [ ] Publish an API reference that reflects actual endpoints, not aspirational ones.

## Phase 7: Release Preparation (Week 15-16)

### Final production preparation

- [ ] Add release automation for binaries and Docker images. Current repo has `Makefile` release targets but no `.goreleaser.yml`.
- [ ] Add coverage reporting and minimum quality gates to CI.
- [ ] Add operational docs for metrics, health endpoints, backups, and failover procedure.
- [ ] Harden Docker runtime guidance: non-root where possible, capability requirements, volume layout, logging expectations.
- [ ] Define support matrix for Linux/Windows/macOS and network environment assumptions.

## Beyond v1.0: Future Enhancements

### Features and improvements for future versions

- [ ] Replace custom gRPC transport with standard `grpc-go`, or formally declare the transport intentionally custom and compatibility-limited.
- [ ] Replace hand-rolled WebSocket implementation with a hardened library or add protocol/fuzz coverage.
- [ ] Expand discovery with real passive sensors and authenticated rogue-DHCP detection signals.
- [ ] Add tracing and richer Prometheus integration.
- [ ] Consider a larger-step storage redesign only if scale justifies it.

## Effort Summary

| Phase | Estimated Hours | Priority | Dependencies |
|---|---|---|---|
| Phase 1 | 40h | CRITICAL | None |
| Phase 2 | 72h | HIGH | Phase 1 |
| Phase 3 | 40h | HIGH | Phase 1 |
| Phase 4 | 48h | HIGH | Phase 1-3 |
| Phase 5 | 32h | MEDIUM | Phase 2-4 |
| Phase 6 | 28h | MEDIUM | Phase 1-2 |
| Phase 7 | 32h | HIGH | Phase 1-6 |
| **Total** | **292h** |  |  |

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Documentation remains ahead of implementation and causes operator misconfiguration | High | High | rewrite docs early; publish a supported-feature matrix |
| HA gives false confidence during failover | High | High | narrow support claims; harden transport and replication before production |
| Session/auth behavior breaks during restart or multi-node use | High | High | replace in-memory sessions before production rollout |
| Storage design hits scale limits under real IPAM load | Medium | High | benchmark current engine; set supported scale limits; redesign only if needed |
| Discovery produces misleading results and operators trust them | High | Medium | relabel current feature set and add confidence/limitations to UI |
| Custom transport code accumulates maintenance burden | Medium | Medium | increase integration tests or standardize on battle-tested libraries |


