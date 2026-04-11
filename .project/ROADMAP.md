# Project Roadmap

> Based on comprehensive codebase analysis performed on 2026-04-11
> This roadmap prioritizes work needed to bring the project to production quality.

## Current State Assessment

Monsoon is not a toy. It already has a real DHCP/IPAM core, a modern dashboard, a functioning REST/gRPC/MCP surface, migrations, and a credible HA starting point. The problem is not lack of code volume. The problem is that production hardening and truth-in-documentation have not caught up to the feature build-out.

Current blockers for production readiness:
- Authorization logic fails open in REST, gRPC, and MCP
- No TLS support
- Insecure sample/default auth and CORS posture
- No CI and no frontend tests
- Documentation materially overstates implemented capability

What is working well:
- Local build and standard test run
- Core subnet/reservation/lease workflows
- Embedded dashboard and advanced API surfaces
- HA active-passive foundation

## Phase 1: Critical Fixes (Week 1-2)

### Must-fix items blocking basic functionality

- [ ] Fix REST mutation authorization so missing identity is denied. Affected files: `internal/api/rest/router.go`, `internal/api/rest/middleware.go`, `internal/api/rest/router_test.go`. Effort: 4h.
- [ ] Fix gRPC authorization so missing identity is denied for unary and streaming methods. Affected files: `internal/api/grpc/server.go`, `internal/api/grpc/server_test.go`. Effort: 6h.
- [ ] Fix MCP authorization so missing identity is denied for write tools. Affected files: `internal/api/mcp/handlers.go`, `internal/api/mcp/server_test.go`. Effort: 4h.
- [ ] Add websocket auth and origin validation. Affected files: `internal/api/websocket/client.go`, `cmd/monsoon/main.go`, tests in `internal/api/websocket/websocket_test.go`. Effort: 8h.
- [ ] Remove insecure bootstrap defaults. Require explicit admin bootstrap or generated one-time secret instead of implicit `admin/admin`. Affected files: `internal/auth/local.go`, `configs/monsoon.yaml`, `README.md`. Effort: 8h.
- [ ] Replace wildcard production CORS defaults with explicit documented configuration and fail-safe sample config. Affected files: `internal/config/config.go`, `configs/monsoon.yaml`, `internal/api/rest/middleware.go`. Effort: 4h.

## Phase 2: Core Completion (Week 3-6)

### Complete missing core features from specification

- [ ] Implement VLAN domain model and CRUD endpoints. Gap: SPEC VLAN sections and task #40/#53 are completely missing. Affected area: `internal/ipam`, `internal/api/rest`, optional gRPC/MCP extension. Effort: 24-40h.
- [ ] Implement real address management workflows beyond list/get. Gap: spec promises richer address CRUD/history. Affected area: `internal/ipam`, REST, dashboard. Effort: 20-32h.
- [ ] Implement DDNS or remove it from product claims. Gap: SPEC and README promise RFC 2136 support; no code exists. Affected area: new `internal/ddns` package plus DHCP/lease hooks. Effort: 24-48h.
- [ ] Implement LDAP support or explicitly de-scope it. Gap: config/spec promise LDAP; code does not. Affected area: `internal/auth`. Effort: 16-24h.
- [ ] Reconcile migration support against actual importer capabilities and complete missing ingestion cases. Affected area: `internal/migrate`, CLI docs. Effort: 16-24h.
- [ ] Decide whether storage docs should be corrected or the storage engine should be upgraded toward the documented B+Tree design. Affected area: `internal/storage`, docs. Effort: 8h for doc correction or 80h+ for engine work.

## Phase 3: Hardening (Week 7-8)

### Security, error handling, edge cases

- [ ] Add HTTPS support or a clear reverse-proxy-only deployment story with secure cookie handling rules. Affected area: server startup/config/docs. Effort: 16-32h.
- [ ] Tighten auth/session behavior: persistent session store or documented single-node limitation, session rotation, logout invalidation tests. Affected area: `internal/auth`. Effort: 12-20h.
- [ ] Extend rate limiting beyond REST, at minimum to auth endpoints, websocket upgrade, and MCP/gRPC ingress. Effort: 12-18h.
- [ ] Audit config update endpoint for unsafe whole-document mutation and add validation/error-path tests. Affected area: `cmd/monsoon/main.go`, REST tests. Effort: 6-10h.
- [ ] Replace fake utilization math with pool-capacity-based calculations. Affected area: `internal/ipam/engine.go`, `web/src/pages/overview-page.tsx`. Effort: 4h.

## Phase 4: Testing (Week 9-10)

### Comprehensive test coverage

- [ ] Add tests for packages with zero coverage: `cmd/monsoon`, `internal/config`, `internal/events`, `internal/metrics`, `internal/storage`. Effort: 24-40h.
- [ ] Add explicit authorization regression tests for REST, gRPC, MCP, and websocket. Effort: 12h.
- [ ] Add frontend component tests for auth gate, settings, and live data flow. Effort: 16-24h.
- [ ] Add end-to-end smoke tests for primary operator flows: login, create subnet, create reservation, discovery trigger, audit export. Effort: 20-32h.
- [ ] Fix local race-test prerequisites so `go test -race ./...` is part of the normal validation workflow. Effort: 2-4h.
- [ ] Fix the Go toolchain mismatch affecting `go test -cover ./...`. Effort: 2-4h.

## Phase 5: Performance & Optimization (Week 11-12)

### Performance tuning and optimization

- [ ] Optimize address listing and capacity calculations to avoid full pool synthesis on every request. Effort: 12-24h.
- [ ] Add pagination/filtering strategy for large audit and address datasets. Effort: 8-16h.
- [ ] Add route-based code splitting to the dashboard. Effort: 4-8h.
- [ ] Add cleanup/eviction to in-memory REST rate limiter structures. Effort: 4-6h.
- [ ] Review websocket drop behavior and event durability expectations. Effort: 4-8h.

## Phase 6: Documentation & DX (Week 13-14)

### Documentation and developer experience

- [ ] Rewrite README to distinguish implemented features from roadmap features. Effort: 8-12h.
- [ ] Update `.project/SPECIFICATION.md`, `.project/IMPLEMENTATION.md`, and `.project/TASKS.md` to match reality. Effort: 8-12h.
- [ ] Add API reference derived from actual routes and gRPC methods. Effort: 8-16h.
- [ ] Add deployment guide covering reverse proxy, TLS, cookies, ports, and HA caveats. Effort: 8-12h.
- [ ] Document storage architecture honestly. Effort: 4-8h.

## Phase 7: Release Preparation (Week 15-16)

### Final production preparation

- [ ] Add CI pipeline: build, vet, unit tests, frontend build, race tests where available, staticcheck. Effort: 12-20h.
- [ ] Harden Docker image: non-root user, runtime directories, cert strategy, healthcheck guidance. Effort: 8-12h.
- [ ] Add release automation and version embedding validation. Effort: 8-12h.
- [ ] Add observability surface: `/metrics`, structured logs, request correlation, documented health model. Effort: 16-28h.
- [ ] Decide on production HA scope for v1.0: active-passive only, or complete load-sharing/WAL streaming. Effort: planning 4h, implementation much larger if chosen.

## Beyond v1.0: Future Enhancements

- [ ] Real rogue DHCP detection with passive listener rather than API placeholder.
- [ ] Prefix delegation and IPv6 operational UX improvements in dashboard.
- [ ] Historical capacity forecasting instead of point-in-time utilization only.
- [ ] Optional external state backends if horizontal scale becomes a real requirement.

## Effort Summary

| Phase | Estimated Hours | Priority | Dependencies |
|---|---:|---|---|
| Phase 1 | 34h | CRITICAL | None |
| Phase 2 | 88h | HIGH | Phase 1 |
| Phase 3 | 46h | HIGH | Phase 1 |
| Phase 4 | 74h | HIGH | Phase 1-3 |
| Phase 5 | 34h | MEDIUM | Phase 2 |
| Phase 6 | 40h | MEDIUM | Phase 2 |
| Phase 7 | 52h | HIGH | Phase 1-6 |
| **Total** | **368h** |  |  |

## Risk Assessment

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| Authorization bug ships to production unchanged | High | High | Fix fail-open authz before any deployment and add regression tests |
| README/spec continue to drive wrong expectations | High | Medium | Rewrite docs immediately after security fixes |
| HA features are assumed complete when only partial | Medium | High | Declare active-passive-only support until WAL streaming and load-sharing are real |
| Local-only sessions break clustered deployment | Medium | Medium | Persist sessions or scope deployment to single active node |
| Hand-rolled transports accumulate subtle protocol bugs | Medium | Medium | Expand transport tests and consider simplifying where possible |
