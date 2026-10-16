# Contributing

This repository is the `over-laid` application. Keep changes small, reviewable, and useful to this stack.

## Development flow

1. Create or pick an issue before making non-trivial changes.
2. Create a branch from `main`.
3. Make the smallest change that satisfies the issue.
4. Run verification:

   ```bash
   make verify
   ```

5. Open a pull request using the PR template.
6. Link the issue with `Closes #N` when appropriate.

## Commit style

Prefer concise conventional-style subjects:

```text
feat: add a Newsroom export control
docs: clarify retained sender setup
fix: preserve a queued action deadline
chore: update frontend dependencies
```

## Open Pilot issues

Use `.gitea/ISSUE_TEMPLATE/open-pilot-task.yaml` for tasks that Open Pilot should implement.

Open Pilot requires:

- `ready-for-agent`
- `agent:queued`
- a deterministic `Test Command`

The template adds `ready-for-agent`; add `agent:queued` only after the issue is complete and ready for automation.

Good Open Pilot tasks are:

- one focused outcome
- clear context and constraints
- explicit out-of-scope notes
- verifiable from the repository root

## Verification commands

Run `make verify` from the repository root. Use a narrower command only when the issue explicitly selects that scope. Go commands run from `backend/`; frontend scripts run through `pnpm --dir web`.

Default full check:

```bash
make verify
```

Examples:

```bash
(cd backend && go test ./...)
pnpm --dir web run test
docker compose config --quiet
```

Go uses the module's 1.26 toolchain (`go 1.26.0`); the frontend build image uses Node 24 and `web/package.json` selects pnpm 10.33.0. [Getting started](docs/runbooks/getting-started.md#native-development) explains running the API, database and Vite separately. `make dev` starts only Vite.

PostgreSQL integration tests skip unless `OVERLAID_TEST_DATABASE_URL` points to a disposable database. For the complete database/race gate, run:

```bash
GOTOOLCHAIN=go1.26.0 bash scripts/ci-postgres.sh
```

The script owns a disposable PostgreSQL container, runs `make verify` and the uncached application/isolated River race suites, then cleans up its own resources. Never point fixtures at an application database. Report local checks, hosted CI and live provider acceptance separately.

## Project boundaries

Keep this app focused and minimal. Do not add unrelated scaffolding unless the issue explicitly changes the role of this repository.

Stack-specific reference docs should stay under `docs/`.
