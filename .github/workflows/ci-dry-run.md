# CI Pipeline — Local Dry-Run Guide

This document describes how to simulate the GitHub Actions CI pipeline
(`.github/workflows/ci.yml`) on a local machine before pushing.

---

## Pipeline Stages

The CI runs in five sequential stages. Each stage must pass before the next begins.

```
Stage 1 → Lint (go-lint, web-lint)
Stage 2 → Test (go-test, web-test)         needs: Stage 1
Stage 3 → Security scans (govulncheck, gosec, web-audit)  needs: Stage 2
Stage 4 → Docker image build               needs: Stage 3
Stage 5 → Deploy to staging                needs: Stage 4  (main branch only)
```

---

## Stage 1 — Lint

### Go lint

```bash
make lint
# Runs: golangci-lint run ./...
# Expected: zero lint errors → exit 0
# Common failure: unused imports, gofmt drift, shadow variables
```

### Web lint & type-check

```bash
make web-lint
# Or manually:
cd web && npm ci && npm run lint && npm run type-check
# Expected: zero ESLint errors, zero TypeScript errors
```

---

## Stage 2 — Tests

### Go tests (unit only — matches CI)

```bash
make test
# Runs: go test ./... -race -cover -timeout=10m -short
# Expected: PASS, ~2175+ tests, 0 failures
# The -short flag skips integration tests (httptest.NewServer, binary builds)
```

### Go tests (unit + integration)

```bash
go test ./... -race -cover -timeout=10m
# Runs ALL tests including integration tests
# Integration tests require no external services — they use httptest.NewServer
# Expected: PASS (integration tests skip gracefully if go toolchain unavailable)
```

### Web tests

```bash
cd web && npm test -- --run
# Expected: all Vitest tests pass
```

### Environment variables required for go-test in CI

The CI `go-test` job spins up PostgreSQL, Redis, and NATS as GitHub Actions services.
For local integration testing against real infra:

```bash
make infra-up   # starts PG + Redis + NATS via Docker Compose

export POSTGRES_DSN="postgres://argus:argus@localhost:5432/argus_test?sslmode=disable"
export REDIS_URL="redis://localhost:6379"
export NATS_URL="nats://localhost:4222"

go test ./... -race -cover -timeout=10m
```

---

## Stage 3 — Security Scans

### Go vulnerability check (govulncheck)

```bash
make security-scan
# Or manually:
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

**Expected state:** The scanner may report advisories for indirect dependencies
(e.g. `stdlib` CVEs in older Go patch releases). These are informational unless
they affect code paths Argus actually exercises. Update `go.mod` / `go.sum` via
`go get -u` and re-run to clear them. CI treats non-zero exit as a blocking failure.

### Go security scan (gosec)

```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
gosec ./...
```

**Expected state:** gosec may flag `G304` (file path from variable) in backup/restore
paths and `G401`/`G501` for MD5/SHA1 in legacy hash-chain code. These are reviewed
and accepted risks documented in `docs/adrs/`. CI blocks on new HIGH/CRITICAL findings.

### Web dependency audit

```bash
cd web && npm audit --audit-level=high
# Expected: 0 high/critical advisories
# If advisories appear: npm audit fix --force (review breaking changes first)
```

---

## Stage 4 — Docker image build

### Build the image locally

```bash
docker build \
  -f infra/docker/Dockerfile.argus \
  --build-arg VERSION=local-dev \
  --build-arg GIT_SHA=$(git rev-parse --short HEAD) \
  --build-arg BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ") \
  -t argus:local \
  .
```

**Expected timings (cold build, no cache):**
- Go compile stage: 2–4 min (depends on CPU; Go mod download ~30 s on first run)
- React build stage: 1–2 min
- Final image copy: < 10 s
- Total cold: 3–6 min

**Expected timings (warm build, layer cache hit):**
- Go compile stage: 20–60 s (only re-compiles changed packages)
- Total warm: < 2 min

### Verify the image starts

```bash
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="postgres://argus:argus@host.docker.internal:5432/argus" \
  -e REDIS_URL="redis://host.docker.internal:6379" \
  -e NATS_URL="nats://host.docker.internal:4222" \
  -e JWT_SECRET="local-dev-secret-32chars-minimum!" \
  argus:local
# Expected: server starts on :8080, /health/live returns 200
```

---

## Stage 5 — Deploy to Staging

Stage 5 runs **only on pushes to `main`**. It is not triggered by pull requests.

```bash
# Requires a configured staging server. Not runnable locally without
# the staging SSH credentials. Simulate the deploy script locally with:
make deploy-staging
# (Makefile target runs docker pull + docker compose up -d on the staging host
#  via SSH — requires STAGING_SSH_HOST, STAGING_SSH_USER, STAGING_SSH_KEY)
```

---

## Required Repository Secrets

Set these in **GitHub → Settings → Secrets and variables → Actions**:

| Secret               | Purpose                                               |
|----------------------|-------------------------------------------------------|
| `STAGING_SSH_HOST`   | Hostname or IP of the staging server                  |
| `STAGING_SSH_USER`   | SSH username for staging access                       |
| `STAGING_SSH_KEY`    | PEM-encoded private SSH key for staging               |
| `GHCR_USERNAME`      | GitHub Container Registry username                    |
| `GHCR_TOKEN`         | GitHub PAT with `write:packages` scope for GHCR       |
| `TEST_TENANT_TOKEN`  | Bearer JWT for smoke-test authenticated endpoints     |

---

## Branch Protection Rules

Configure in: **GitHub → Settings → Branches → Branch protection rules → `main`**

Required settings:

- **Require a pull request before merging** — no direct pushes to `main`
- **Require status checks to pass before merging** — enable the following required checks:
  - `go-lint`
  - `web-lint`
  - `go-test`
  - `web-test`
  - `govulncheck`
  - `gosec`
  - `web-audit`
  - `build-image`
- **Require branches to be up to date before merging** — avoids stale-branch merges
- **Do not allow bypassing the above settings** — applies to administrators too

The `deploy-staging` job is **not** a required check because it only runs on
`main` (not on PRs). It runs automatically after a successful merge.

---

## Quick Reference

```bash
# Full local simulation (unit tests only, matches CI gate):
make lint && make test && make web-lint

# Full local simulation (all tests, including integration):
make lint && go test ./... -race -cover && make web-lint

# Build Docker image locally:
docker build -f infra/docker/Dockerfile.argus -t argus:local .
```
