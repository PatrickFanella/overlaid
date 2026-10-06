# Production Packaging Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Recommended path:
> dispatch a fresh subagent per task, review each result with `review-quality`,
> then continue. For complex multi-agent splits, use
> `parallel-feature-development`, `team-composition-patterns`, and
> `team-communication-protocols`. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Make a deployed over-laid stack self-identifying and easier to package, smoke-check, and back up before live use.

**Architecture:** Add a tiny backend build-info package and public JSON endpoint, inject version/commit/date through local builds and Docker image builds, then surface that version in the dashboard. Add safe Makefile targets and docs for release metadata and `api-data` backup/export without changing runtime auth boundaries.

**Tech Stack:** Go HTTP API, Vite React TypeScript dashboard, Docker Compose, Makefile, existing `make verify` / `make smoke` flow.

---

## Status

**Status:** Complete  
**Started:** 2026-06-13  
**Completed:** 2026-06-14  
**Live QA:** Dashboard build-info panel should be checked after the next deployed rebuild.

### Completion Notes

- Added public `/version` endpoint and backend build-info package.
- Injected local and Docker build metadata with Go `-ldflags`.
- Added `/version` to `make smoke`.
- Added ignored `backups/` directory and `make api-data-backup` target with missing-volume guard.
- Added dashboard overview build-info panel.
- Added production packaging runbook and README guidance.
- Verification passed:
  - `docker compose config --quiet`
  - `docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet`
  - `make verify`
  - `docker compose up -d --build`
  - `make smoke`
  - `curl --max-time 15 -fsS http://localhost:8080/version`
  - `curl --max-time 15 -fsS http://localhost:5173/dashboard >/dev/null`

---

## Acceptance Criteria

- `GET /version` returns HTTP 200 JSON with non-secret build/runtime metadata:
  - `name: "over-laid"`
  - `version`
  - `commit`
  - `builtAt`
  - `goVersion`
  - `os`
  - `arch`
- `/readyz` remains dependency diagnostics only; `/healthz` remains unchanged.
- Local `make build` injects git commit and UTC build time into the backend binary.
- Docker API image accepts `BUILD_VERSION`, `BUILD_COMMIT`, and `BUILD_DATE` build args and injects them with Go `-ldflags`.
- `make smoke` checks `/version` in addition to existing health/readiness/status/dashboard routes.
- Dashboard overview renders a compact “Build info” panel that fetches `/version` without requiring operator auth and degrades gracefully if unavailable.
- New backup target creates a timestamped tarball of the `api-data` volume without printing secrets/token contents.
- Docs explain release/build metadata, `/version`, and `api-data` backups/restores.
- `make verify`, compose config checks, rebuild, `make smoke`, `/version`, and `/dashboard` spot checks pass.

## Non-goals

- Do not add release publishing, registries, Git tags, GitHub/Gitea releases, or changelog automation.
- Do not expose secret values, token contents, or volume file contents in `/version` or dashboard UI.
- Do not make Compose healthchecks depend on `/version`.
- Do not require a non-empty git commit in local development; default values should be safe and explicit.
- Do not push commits unless explicitly instructed by the user.

## File Structure

- Create: `backend/internal/buildinfo/buildinfo.go` — package-level build variables, runtime report type, and `Report()` helper.
- Create: `backend/internal/buildinfo/buildinfo_test.go` — validates defaults and report shape.
- Modify: `backend/internal/http/handlers.go` — add `version` handler.
- Modify: `backend/internal/http/routes.go` — register `GET /version`.
- Modify: `backend/internal/http/handlers_test.go` — add version endpoint tests and assert `/healthz` remains unchanged.
- Modify: `Makefile` — add build metadata variables, backend `-ldflags`, `/version` smoke check, and `api-data-backup` target.
- Modify: `.gitignore` — ignore local backup archives that may contain token/state/history/rules data.
- Modify: `backend/Dockerfile` — add build args and inject Go `-ldflags`.
- Modify: `docker-compose.yml` — pass build args to API image.
- Modify: `web/src/lib/api.ts` — add `BuildInfo` type and `fetchBuildInfo()`.
- Create: `web/src/routes/dashboard/components/BuildInfoPanel.tsx` — SSR-safe dashboard build-info card.
- Modify: `web/src/routes/dashboard/Overview.tsx` — render the build-info card.
- Modify: `web/src/App.test.tsx` — SSR and fetch-helper tests.
- Create: `docs/runbooks/production-packaging.md` — version/build metadata and backup/restore workflow.
- Modify: `README.md` — link the new runbook and document `/version` + backup target.

## Task 1: Backend Build Info Endpoint

**Files:**
- Create: `backend/internal/buildinfo/buildinfo.go`
- Create: `backend/internal/buildinfo/buildinfo_test.go`
- Modify: `backend/internal/http/handlers.go`
- Modify: `backend/internal/http/routes.go`
- Modify: `backend/internal/http/handlers_test.go`

- [x] **Step 1: Add buildinfo package**

Create `backend/internal/buildinfo/buildinfo.go`:

```go
package buildinfo

import "runtime"

var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"builtAt"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func Report() Info {
	return Info{
		Name:      "over-laid",
		Version:   valueOrDefault(Version, "dev"),
		Commit:    valueOrDefault(Commit, "unknown"),
		BuiltAt:   valueOrDefault(BuiltAt, "unknown"),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

func valueOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
```

- [x] **Step 2: Add backend tests**

Create `backend/internal/buildinfo/buildinfo_test.go`:

```go
package buildinfo

import "testing"

func TestReportUsesSafeDefaults(t *testing.T) {
	originalVersion, originalCommit, originalBuiltAt := Version, Commit, BuiltAt
	t.Cleanup(func() { Version, Commit, BuiltAt = originalVersion, originalCommit, originalBuiltAt })

	Version, Commit, BuiltAt = "", "", ""
	info := Report()

	if info.Name != "over-laid" {
		t.Fatalf("expected over-laid, got %q", info.Name)
	}
	if info.Version != "dev" || info.Commit != "unknown" || info.BuiltAt != "unknown" {
		t.Fatalf("unexpected defaults: %#v", info)
	}
	if info.GoVersion == "" || info.OS == "" || info.Arch == "" {
		t.Fatalf("runtime metadata missing: %#v", info)
	}
}

func TestReportUsesInjectedValues(t *testing.T) {
	originalVersion, originalCommit, originalBuiltAt := Version, Commit, BuiltAt
	t.Cleanup(func() { Version, Commit, BuiltAt = originalVersion, originalCommit, originalBuiltAt })

	Version, Commit, BuiltAt = "v1", "abc123", "2026-06-13T00:00:00Z"
	info := Report()

	if info.Version != "v1" || info.Commit != "abc123" || info.BuiltAt != "2026-06-13T00:00:00Z" {
		t.Fatalf("injected values not used: %#v", info)
	}
}
```

- [x] **Step 3: Add HTTP handler and route**

In `backend/internal/http/handlers.go`, import `git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo` and add:

```go
func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, buildinfo.Report())
}
```

In `backend/internal/http/routes.go`, register it near health/readiness:

```go
s.mux.HandleFunc("GET /version", s.version)
```

- [x] **Step 4: Add endpoint tests**

In `backend/internal/http/handlers_test.go`, add a test that requests `/version` and decodes the JSON. Assert `name == "over-laid"`, `version`, `commit`, `builtAt`, `goVersion`, `os`, and `arch` are non-empty. Keep existing `/healthz` tests unchanged.

- [x] **Step 5: Verify and commit**

Run:

```bash
(cd backend && go test ./internal/buildinfo ./internal/http -run 'Report|Version|Healthz' -count=1)
make verify
```

Expected: both pass.

Commit:

```bash
git add backend/internal/buildinfo backend/internal/http/handlers.go backend/internal/http/routes.go backend/internal/http/handlers_test.go
git commit -m "feat: add build info endpoint"
```

## Task 2: Build Metadata Injection and Backup Target

**Files:**
- Modify: `Makefile`
- Modify: `.gitignore`
- Modify: `backend/Dockerfile`
- Modify: `docker-compose.yml`

- [x] **Step 1: Inject build metadata in local build**

In `Makefile`, add metadata variables near the top:

```make
VERSION ?= dev
BUILD_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GO_LDFLAGS := -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Version=$(VERSION) -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Commit=$(BUILD_COMMIT) -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.BuiltAt=$(BUILD_DATE)
BACKUP_DIR ?= backups
```

Change the backend build command to:

```make
	cd backend && go build -ldflags '$(GO_LDFLAGS)' -o ../bin/over-laid-api ./cmd/app
```

- [x] **Step 2: Inject Docker build args**

In `backend/Dockerfile`, add build args before `RUN go build`:

```dockerfile
ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_DATE=unknown
RUN go build -ldflags "-X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Version=${BUILD_VERSION} -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.Commit=${BUILD_COMMIT} -X git.subcult.tv/PatrickFanella/over-laid/internal/buildinfo.BuiltAt=${BUILD_DATE}" -o /out/api ./cmd/app
```

Replace the existing plain `RUN go build -o /out/api ./cmd/app` line.

In `docker-compose.yml`, pass API build args:

```yaml
  api:
    build:
      context: ./backend
      args:
        BUILD_VERSION: ${BUILD_VERSION:-dev}
        BUILD_COMMIT: ${BUILD_COMMIT:-unknown}
        BUILD_DATE: ${BUILD_DATE:-unknown}
```

Preserve the existing API service fields.

- [x] **Step 3: Expand smoke and add backup target**

Add `/version` to `make smoke` after `/healthz`:

```make
	curl --max-time 15 -fsS http://localhost:$(API_PORT)/version
```

Add a phony target name to `.PHONY`:

```make
api-data-backup
```

Add `.gitignore` protection for the default backup directory:

```gitignore
# Local backup archives; may contain Twitch tokens/state/history/rules
backups/
```

Add a backup target that creates a local tarball without printing file contents and fails clearly if the Compose `api-data` volume does not exist:

```make
api-data-backup: ## Back up the Compose api-data volume into ./backups
	mkdir -p "$(BACKUP_DIR)"
	volume=$$(docker volume ls -q \
		--filter label=com.docker.compose.project=$$(basename "$$PWD") \
		--filter label=com.docker.compose.volume=api-data); \
	if [[ -z "$$volume" ]]; then \
		echo "api-data Compose volume not found; start the stack or check COMPOSE_PROJECT_NAME" >&2; \
		exit 1; \
	fi; \
	docker run --rm \
		-v "$$volume:/data:ro" \
		-v "$$PWD/$(BACKUP_DIR):/backup" \
		alpine:3.22 \
		sh -c 'tar -czf /backup/api-data-$$(date -u +%Y%m%dT%H%M%SZ).tar.gz -C /data .'
```

- [x] **Step 4: Verify and commit**

Run:

```bash
make verify
docker compose config --quiet
docker compose build api
```

Expected: all pass. Do not run `api-data-backup` during verification unless explicitly needed.

Commit:

```bash
git add Makefile .gitignore backend/Dockerfile docker-compose.yml
git commit -m "chore: inject build metadata"
```

## Task 3: Dashboard Build Info Panel

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/src/routes/dashboard/components/BuildInfoPanel.tsx`
- Modify: `web/src/routes/dashboard/Overview.tsx`
- Modify: `web/src/App.test.tsx`

- [x] **Step 1: Add frontend API helper**

In `web/src/lib/api.ts`, add:

```ts
export type BuildInfo = {
  name: string;
  version: string;
  commit: string;
  builtAt: string;
  goVersion: string;
  os: string;
  arch: string;
};

export async function fetchBuildInfo(explicitBaseUrl?: string) {
  const response = await fetch(apiUrl('/version', explicitBaseUrl), { headers: { Accept: 'application/json' } });
  if (!response.ok) {
    throw new Error(`Build info failed: ${response.status}`);
  }
  return (await response.json()) as BuildInfo;
}
```

- [x] **Step 2: Add SSR-safe panel**

Create `web/src/routes/dashboard/components/BuildInfoPanel.tsx`:

```tsx
import { useEffect, useState } from 'react';
import { fetchBuildInfo, type BuildInfo } from '../../../lib/api';

type BuildInfoPanelProps = {
  apiBaseUrl: string;
};

export function BuildInfoPanel({ apiBaseUrl }: BuildInfoPanelProps) {
  const [info, setInfo] = useState<BuildInfo | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let active = true;
    fetchBuildInfo(apiBaseUrl)
      .then((next) => {
        if (active) setInfo(next);
      })
      .catch((err: unknown) => {
        if (active) setError(err instanceof Error ? err.message : 'Build info unavailable');
      });
    return () => {
      active = false;
    };
  }, [apiBaseUrl]);

  return (
    <section className="hud-panel hud-panel--flat rounded-sm p-3">
      <div className="mb-2 flex items-center justify-between gap-2">
        <h2 className="hud-label text-sm text-[hsl(var(--ink))]">Build info</h2>
        <span className="hud-chip">/version</span>
      </div>
      {error ? <p className="text-xs text-[hsl(var(--warning))]">{error}</p> : null}
      {info ? (
        <dl className="space-y-1 text-xs">
          <div className="hud-terminal-line"><dt className="hud-terminal-key">version</dt><dd className="hud-terminal-value">{info.version}</dd></div>
          <div className="hud-terminal-line"><dt className="hud-terminal-key">commit</dt><dd className="hud-terminal-value">{info.commit}</dd></div>
          <div className="hud-terminal-line"><dt className="hud-terminal-key">built</dt><dd className="hud-terminal-value">{info.builtAt}</dd></div>
          <div className="hud-terminal-line"><dt className="hud-terminal-key">runtime</dt><dd className="hud-terminal-value">{info.goVersion} · {info.os}/{info.arch}</dd></div>
        </dl>
      ) : !error ? (
        <p className="text-xs text-[hsl(var(--muted))]">Build report unavailable.</p>
      ) : null}
    </section>
  );
}
```

- [x] **Step 3: Render on overview and test**

In `web/src/routes/dashboard/Overview.tsx`, import and render `BuildInfoPanel` below `SystemStatusRail`:

```tsx
import { BuildInfoPanel } from './components/BuildInfoPanel';
...
<BuildInfoPanel apiBaseUrl={apiBaseUrl} />
```

In `web/src/App.test.tsx`, import `BuildInfoPanel`. Update overview route assertion to include `Build info` and `Build report unavailable`. Add a direct SSR test that `BuildInfoPanel` does not fetch during `renderToString` by stubbing global `fetch` and checking it is not called.

- [x] **Step 4: Verify and commit**

Run:

```bash
pnpm --dir web run test -- --run src/App.test.tsx
pnpm --dir web run build
make verify
```

Expected: all pass.

Commit:

```bash
git add web/src/lib/api.ts web/src/routes/dashboard/components/BuildInfoPanel.tsx web/src/routes/dashboard/Overview.tsx web/src/App.test.tsx
git commit -m "feat: surface build info in dashboard"
```

## Task 4: Packaging Docs and Final Completion

**Files:**
- Create: `docs/runbooks/production-packaging.md`
- Modify: `README.md`
- Modify: `docs/superpowers/plans/2026-06-13-plan-10-production-packaging.md`

- [x] **Step 1: Add runbook**

Create `docs/runbooks/production-packaging.md` covering:

- `/version` purpose and non-secret fields.
- Local build metadata: `make build VERSION=vX.Y.Z`.
- Docker build metadata: `BUILD_VERSION`, `BUILD_COMMIT`, `BUILD_DATE` in `.env` or shell.
- `make smoke` includes `/version`.
- `make api-data-backup` creates `backups/api-data-*.tar.gz` and should be run before destructive `db-reset` or host migration. The default `backups/` directory is ignored; if `BACKUP_DIR` is overridden, it must point to a private ignored path.
- Restore workflow example using an Alpine container and the `api-data` volume, with a warning to stop the API first.
- Reminder that backup tarballs contain Twitch tokens, retained state, event history, and interaction rules; do not commit or publish them.

- [x] **Step 2: Update README**

Add `/version` to route/operator docs and link `docs/runbooks/production-packaging.md`. Document `VERSION`, `BUILD_COMMIT`, `BUILD_DATE`, and `make api-data-backup` near deploy/backup guidance.

- [x] **Step 3: Review docs and commit**

Run:

```bash
make verify
```

Expected: pass.

Commit:

```bash
git add docs/runbooks/production-packaging.md README.md
git commit -m "docs: add production packaging runbook"
```

- [x] **Step 4: Final integration and completion commit**

Run:

```bash
docker compose config --quiet
docker compose -f docker-compose.yml -f docker-compose.override.example.yml config --quiet
make verify
docker compose up -d --build
make smoke
curl --max-time 15 -fsS http://localhost:8080/version
curl --max-time 15 -fsS http://localhost:5173/dashboard >/dev/null
```

Expected: all pass.

Update this plan:
- Change status to complete.
- Add completion verification notes.
- Check every task checkbox.
- Note live QA pending: dashboard build-info panel should be checked after the next deployed rebuild.

Commit:

```bash
git add docs/superpowers/plans/2026-06-13-plan-10-production-packaging.md
git commit -m "docs: mark plan 10 complete"
```

Do not push unless explicitly instructed by the user.


## October 6 isolated backup/restore evidence

A stopped native API backup was restored into a separate disposable Postgres database and private retained-state directory. A fresh API returned identical BRB/goals/ticker replay, eight history rows, a session report with three events and both readiness snapshots, shared config, interaction rules, and 25 recent audit rows (51 total). Restored dependency readiness and `make smoke API_PORT=8082` passed. The temporary restore process/database and retained fixtures were cleaned up. [Recorded evidence](../../qa/2026-10-06-local-backup-restore-drill.md) names the accepted integration/deployment limits. Restore guidance now rejects a missing api-data volume and uses `pg_restore --exit-on-error`.


## October 6 packaged image and volume verification

Rootless Podman built both Dockerfiles and exercised isolated API/Nginx containers. The web build exposed a short-name resolution failure; qualified Docker Hub base-image names fix it without changing versions. Context-local ignore files now keep environment files, installed frontend dependencies, generated assets, and test artifacts out of builds. The packaged Go 1.26 runtime repaired a root-owned named volume, ran as UID 100, persisted BRB/history across restart, served healthy product storage, and passed packaged smoke and native browser rendering. All 21 browser workflows and `make verify` passed. [Recorded evidence](../../qa/2026-10-06-container-packaging.md) names image IDs, cleanup, and remaining Compose/deployment/live-integration gates.


## October 6 Compose dependency and exposure correction

A disposable Compose project ran through the rootless Podman API with fresh volumes and loopback ports. Postgres health gated API startup; all three services became healthy, and BRB/session/readiness evidence survived an API restart. Inspection found that the documented `EXPOSURE_MODE` setting was not forwarded to the API. Compose now passes it through with the existing local default; all three modes resolve correctly, and public-mode runtime readiness reports missing operator/webhook fixture secrets. `make verify`, all 21 browser workflows, and Compose-stack smoke passed. [Recorded evidence](../../qa/2026-10-06-compose-wiring.md) distinguishes completed local wiring from remaining Docker-engine, production, and live integration checks.
