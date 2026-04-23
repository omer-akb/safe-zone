# TSZ (Thyris Safe Zone) – Open Source Roadmap

This document outlines the work required to release TSZ as a production‑ready open‑source project and to grow a healthy community around it.

The roadmap is split into phases. Each bullet is a concrete, actionable item.

---

## Phase 0 – OSS Foundations

**Goal:** Make the current codebase safe and clear to open‑source.

- [x] Choose and apply an open‑source license (recommended: Apache 2.0)
- [x] Add `LICENSE` file and update all headers/README to reference the new license
- [x] Add `CONTRIBUTING.md` (how to run, how to submit issues/PRs, code style)
- [x] Add `CODE_OF_CONDUCT.md`
- [x] Add `SECURITY.md` with vulnerability disclosure policy
- [x] Clean secrets / private references (ensure no internal URLs, tokens, or customer data)
- [x] Create structured, enterprise‑ready documentation under `docs/`
- [x] Provide a complete Postman collection with realistic examples (`docs/TSZ_Postman_Collection.json`)

---

## Phase 1 – Core Product Hardening

**Goal:** Ensure the gateway is robust, testable and production‑ready for security‑sensitive (e.g. banking/PCI) adopters.

**Reference:** See [docs/SECURITY_ROADMAP.md](docs/SECURITY_ROADMAP.md) for detailed security hardening plan (10 weeks, Q2-Q3 2026).

### Subsection 1a: Functional Testing (Core Features)

- [ ] Define a Phase 1 test strategy (risk‑based, bank/PCI‑ready):
  - [ ] Define test categories and entry/exit criteria (unit, integration, e2e, non‑functional, security)
  - [ ] Set minimal coverage expectations for critical flows (PII/PCI, allow/mask/block decisions)
- [x] Add unit tests for core detection and decision logic:
  - [x] PII detection and redaction (emails, phones, national IDs, card numbers and other PCI‑relevant fields)
  - [x] Confidence thresholds and decision logic (allow / mask / block, including rounding and boundary conditions)
  - [x] Validators (BUILTIN, REGEX, SCHEMA, AI_PROMPT) including negative and edge cases)
  - [x] Templates import behavior (upsert semantics, idempotency and validation errors)
  - [x] Security event and SIEM model mapping)
- [x] Add integration tests (API + DB/Redis + AI client boundaries) for:
  - [x] `/detect` end‑to‑end with PII / non‑PII / borderline payloads)
  - [x] LLM gateway `/v1/chat/completions` including streaming and guardrail modes)
  - [x] Templates import + detection flow using built‑in template packs)
  - [ ] Allowlist/blocklist logic and pattern precedence)
  - [x] Auth-enabled integration mode coverage (Bearer token / RBAC headers)
- [x] Add end‑to‑end regression suites (CI‑friendly, runnable via `go test ./...` or `test-scripts/`):
  - [ ] Happy‑path flows for typical banking use cases (KYC, customer support chat, transaction memos, internal ops)
  - [x] Misuse/abuse scenarios (prompt injection, jailbreak attempts, sensitive data exfiltration)
  - [ ] Replay known incident patterns as regression tests where applicable)
- [x] Add basic benchmarks (requests per second, latency under load) (covered by `test-scripts` load test helper)
- [x] Add graceful error handling for external AI failures (timeouts, partial outages)
- [ ] Add non‑functional tests:
  - [ ] Load and stress tests for peak traffic and batch scenarios)
  - [ ] Basic resilience tests (timeouts, network failures, Redis/PostgreSQL outages)
- [x] Establish a standard test folder structure:
  - [x] Keep production code under `internal/...` and keep automated tests under `tests/` (unit, integration, e2e)
  - [x] Add `tests/integration/` for HTTP + DB/Redis + AI-boundary integration tests)
  - [x] Add `tests/e2e/` (and plan `tests/perf/`) for end‑to‑end and load tests)
- [x] Migrate existing scripts to the new structure:
  - [x] Convert `test-scripts/main.go` into `tests/e2e/sanity_suite_test.go` (keep script as an optional manual harness)
  - [x] Convert `test-scripts/gateway-test/main.go` into `tests/e2e/gateway_streaming_test.go` (or similar)
  - [x] Decide whether to keep additional demo scripts under `examples/` / `test-scripts/` as manual tools
- [ ] Document performance characteristics, suggested resource sizing and the overall test strategy
- [x] Add an end‑to‑end sanity test suite (initially `test-scripts/`, later `tests/e2e/`) that exercises patterns, allowlist/blocklist, validators, templates, admin APIs and the LLM gateway

### Subsection 1b: Security Hardening (HTTP, Auth, Rate Limiting, Encryption, Audit Logging)

**Status:** See [docs/SECURITY_ROADMAP.md](docs/SECURITY_ROADMAP.md) for full details and timeline.

- [x] **Milestone 1: HTTP Security Hardening** (Weeks 1-2)
  - [x] Add request size limits (10 MB default, configurable)
  - [x] Enforce per-handler timeouts (/detect: 30s, /chat: 5m)
  - [x] Configure HTTP server with ReadTimeout, WriteTimeout, MaxHeaderBytes
  - [x] Add security headers middleware (X-Frame-Options, X-Content-Type-Options, X-XSS-Protection, HSTS, Cache-Control)
  - [x] Add CORS middleware with configurable allowed origins
  - [x] Add input validation middleware (Content-Type, JSON body validation)

- [ ] **Milestone 2: Authentication & Authorization** (Weeks 2-3)
  - [x] Create `internal/auth/auth.go` with Bearer token and API key validation
  - [ ] Add `api_keys` database table with hashed tokens
  - [x] Implement authentication middleware for all non-health endpoints
  - [x] Define permission model (detect:read, gateway:use, patterns:admin, etc.)
  - [x] Implement role-based access control (RBAC) enforcement
  - [x] Protect admin endpoints (`/admin/*`) with admin role requirement
  - [ ] Create API key management endpoints (create, list, revoke, rotate)

- [ ] **Milestone 3: Rate Limiting & DDoS Protection** (Week 3-4)
  - [x] Implement global rate limiter in `internal/middleware/ratelimit.go`
  - [x] Configure per-endpoint rate limits (/detect: 1000 req/min, /chat: 100 req/min, /patterns: 50 req/min, /admin: 10 req/min)
  - [ ] Store rate limit state in Redis for distributed limiting
  - [x] Return 429 Too Many Requests on limit exceeded
  - [x] Implement IP-based and key-based rate limiting

- [ ] **Milestone 4: Data Protection & Encryption** (Weeks 4-5)
  - [ ] Make TLS/HTTPS mandatory (port 8443, TLS 1.3 minimum)
  - [ ] Implement HTTP -> HTTPS redirect on port 80
  - [ ] Configure database connections with SSL mode (sslmode=require)
  - [ ] Configure Redis with TLS
  - [ ] Implement hashing + salting for API keys
  - [ ] Encrypt sensitive fields in database (optional: per-field encryption)
  - [ ] Remove all hardcoded credentials from code and configs

- [ ] **Milestone 5: Audit Logging & Monitoring** (Weeks 5-6)
  - [ ] Create `audit_logs` database table with fields for user, action, resource, IP, timestamp
  - [ ] Log authentication events (successful/failed login, suspicious patterns)
  - [ ] Log authorization events (permission granted/denied, unauthorized attempts)
  - [ ] Log data access events (CRUD operations on patterns, validators, allowlist/blocklist)
  - [ ] Implement structured JSON logging in `internal/middleware/logging.go`
  - [ ] Enhance SIEM integration to forward audit logs (new `internal/guardrails/siem.go` features)
  - [ ] Implement log retention and rotation policies

- [ ] **Milestone 6: Vulnerability Management** (Week 6-7)
  - [ ] Add GitHub Actions workflow for `govulncheck` (golang.org/x/vuln)
  - [ ] Add `gosec` (Go security analyzer) to CI/CD
  - [ ] Add OWASP dependency-check to CI/CD pipeline
  - [ ] Enable GitHub Dependabot or Renovate for automated dependency updates
  - [ ] Add Trivy for container image scanning
  - [ ] Define security SLA for vulnerability fixes (CRITICAL: 24h, HIGH: 7d, MEDIUM: 30d)

- [ ] **Milestone 7: Production Hardening & Deployment** (Week 7-8)
  - [ ] Remove default credentials from `docker-compose.yml` and `init.sql`
  - [ ] Document recommended network topology (reverse proxy, TLS termination, private subnets)
  - [ ] Run containers as non-root user
  - [ ] Set resource limits in Docker/Kubernetes (memory, CPU)
  - [ ] Add security tests in `tests/security/` (auth bypass, authz bypass, injection resistance)
  - [ ] Implement automated secret rotation (90 days for API keys/DB passwords, 30 days for certs)

- [ ] **Milestone 8: Documentation & Guidelines** (Week 8)
  - [ ] Create `docs/SECURITY_OPERATIONS.md` (deployment, API key management, TLS setup, secrets management, monitoring)
  - [x] Update `docs/ARCHITECTURE_SECURITY.md` with auth, rate limiting, and audit logging details
  - [x] Add authentication examples to `docs/API_REFERENCE.md`
  - [ ] Create `docs/RUNBOOKS.md` with incident response procedures
  - [x] Update `CONTRIBUTING.md` with security checklist for PRs

---

## Phase 2 – Developer Experience & SDKs

**Goal:** Make TSZ easy to adopt from different application stacks.

- [x] Design a simple, stable public API contract (documented in `docs/API_REFERENCE.md`, including `/detect`, LLM gateway and configuration endpoints)
- [x] Create Go client helper (`tszclient-go`) for gateway and `/detect`
- [x] Create Python client (`tszclient-py`) with simple `detect()` and gateway helpers
- [x] Align Go/Python SDK authentication behavior with TSZ Bearer token + legacy admin-key compatibility
- [ ] Create Node/TypeScript client
- [x] Publish Go client usage documentation under `pkg/tszclient-go/README.md`
- [x] Add `examples/` directory with:
  - [x] Go `/detect` example (`examples/go-detect`)
  - [x] Go LLM gateway example (`examples/go-llm-gateway`)
  - [ ] Python FastAPI + TSZ integration
  - [ ] Node.js (Express/Fastify) + TSZ integration
  - [ ] Simple LLM proxy example (TSZ in front of OpenAI/Anthropic)
- [x] Document streaming and guardrail modes for the LLM gateway (`docs/concepts/STREAMING.md`)
- [x] Add a dedicated LLM gateway test harness (`test-scripts/gateway-test`) covering safe/unsafe, streaming and PII scenarios
- [ ] Document and implement in-code version reporting for SDKs (e.g. `tszclient-go` `Version` constant and aligning with tags)

---

## Phase 3 – Policy Packs & Templates

**Goal:** Ship valuable, ready‑made guardrail packs.

- [x] Define and document a stable template format (JSON) for patterns and validators (`/templates/import`, `docs/API_REFERENCE.md`)
- [x] Implement template import API with upsert semantics for patterns and validators (`POST /templates/import`)
- [ ] Provide built‑in template packs:
  - [ ] PII Starter Pack (emails, phones, national IDs, etc.)
  - [ ] PCI Pack (payment data focus)
  - [ ] GDPR / privacy‑focused pack
  - [ ] Toxicity & brand safety pack
  - [ ] Prompt injection & jailbreak protection pack
- [ ] Document each pack (what it covers, patterns/validators inside, recommended use cases)
- [ ] Add CLI or scripts to import/export templates easily (beyond the core HTTP API)

---

## Phase 4 – Observability & Operations

**Goal:** Make TSZ easy to run and operate in production, with full visibility into security events and system health.

- [ ] Add Prometheus metrics endpoint (e.g. `/metrics`):
  - [ ] Request count / latency per endpoint
  - [ ] Blocked vs allowed requests
  - [ ] Detection counts per pattern/category
  - [ ] Authentication and rate limiting metrics
- [ ] Provide example Grafana dashboards
- [ ] Improve logging structure (JSON logs option, log levels, centralized log forwarding)
- [ ] Provide production‑ready Helm chart / K8s manifests
- [x] Document backup & disaster recovery for PostgreSQL and Redis (see `docs/ARCHITECTURE_SECURITY.md`)
- [x] Add security event model and SIEM webhook integration for guardrail decisions (`internal/models/security_event.go`, `internal/guardrails/siem.go`, `SIEM_WEBHOOK_URL`)
- [ ] Enhance SIEM/webhook integration to include authentication, authorization, and audit events
- [ ] Document SIEM/webhook integration patterns and example dashboards
- [ ] Add alerting rules template for detection of suspicious activity (brute force, rate limit exceeding, privilege escalation)

---

## Phase 5 – Website & Content Hub

**Goal:** Provide a clear entry point for users to understand TSZ, discover policy packs/templates and access learning resources.

- [ ] Create a public website for TSZ:
  - [ ] High-level product overview and value proposition
  - [ ] Links to documentation, GitHub repo and SDKs
  - [ ] Getting started section for developers and security teams
- [ ] Build a "Policy Packs & Templates" hub:
  - [ ] List and describe available template packs (PII, PCI, GDPR, toxicity, jailbreak, etc.)
  - [ ] Provide links to JSON definitions and import instructions
  - [ ] Show versioning and changelog per pack
- [ ] Create a "Playground" or interactive demo page (optional initial version can be mock-only)
- [ ] Set up a blog/updates section:
  - [ ] Initial launch/announcement post (what TSZ is, why it exists)
  - [ ] Deep dives on policy packs (how to use them, design decisions)
  - [ ] Release highlights for major TSZ / SDK versions
- [ ] Decide on hosting strategy (e.g. GitHub Pages, Vercel, Netlify) and basic CI for website deployments

---

## Phase 6 – Security Certifications & Compliance

**Goal:** Build trust with enterprise and regulated customers through formal security certifications and compliance documentation.

**Note:** Phase 1 (Core Product Hardening) must be completed first. See [docs/SECURITY_ROADMAP.md](docs/SECURITY_ROADMAP.md) for the detailed security roadmap.

- [ ] Perform a formal threat model and risk assessment (document in `docs/THREAT_MODEL.md`)
- [ ] Commission or plan for an external security audit / penetration test by a third-party firm
- [x] Document recommended deployment patterns and network topologies (VPC/private subnets, API gateways, WAFs, mTLS, service meshes) in `docs/ARCHITECTURE_SECURITY.md`
- [ ] Provide configuration examples:
  - [ ] NGINX / Traefik / Envoy integration for TLS and auth
  - [ ] mTLS / service‑mesh deployment examples
  - [ ] Example Kubernetes network policies
- [ ] Build SOC2 Type II readiness:
  - [ ] Document control objectives and implementations
  - [ ] Establish audit trail and logging
  - [ ] Define SLAs for incident response and patching
- [ ] Prepare for industry certifications:
  - [ ] Plan for SOC2 Type II audit (12-month audit period)
  - [ ] Plan for FedRAMP compliance (if applicable for US government customers)
  - [ ] Consider GDPR/CCPA compliance documentation

---

## Phase 7 – Community & Releases

**Goal:** Grow an active community and maintain a healthy release cycle.

- [x] Define a versioning strategy (SemVer) and release cadence (per product: thyris-sz, tszclient-go, tszclient-py)
- [x] Set up CI/CD:
  - [x] Linting and formatting
  - [x] Tests and coverage reporting
  - [ ] Docker image build & publish (GitHub Container Registry / Docker Hub)
- [ ] Publish a clear `CHANGELOG.md`
- [ ] Add issue and PR templates
- [ ] Tag `good first issue` and `help wanted` items to welcome contributors
- [ ] Write a short blog post / announcement describing TSZ and its use cases
