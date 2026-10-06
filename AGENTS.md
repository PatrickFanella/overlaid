# Agent Instructions

This repository is `over-laid`, a Go full-stack app with Vite React TypeScript, Postgres, and Docker, derived from `subculture-collective/project-template`.

## Priorities

1. Keep the app runnable and minimal.
2. Preserve Open Pilot issue and PR templates.
3. Keep generated caches, secrets, and local data out of git.
4. Update this file and `README.md` when stack conventions change.

## Frontend conventions

- Overlays (`/overlay/*`) and the dashboard (`/dashboard/*`) have separate visual systems. Overlays use the SUBCULT brand pack: `sc-*` classes in `web/src/styles/overlay.css` and primitives in `web/src/overlay/`. The dashboard uses `desk-*` classes in `web/src/styles/desk.css` and primitives in `web/src/routes/dashboard/ui.tsx`. Do not mix them.
- Both stylesheets are imported into Tailwind's `components` layer so utility classes can override them.
- Brand files in `web/public/brand/subcult/` are copies; `PROVENANCE.md` there names the source commit. Do not redraw or edit them.
- Every overlay must render something useful under `?demo=1`; the dashboard previews and visual QA depend on it.

## Verification

Run:

```bash
make verify
```

Use narrower commands only when an issue explicitly asks for a smaller check.

## Operations conventions

- Keep `/healthz` as cheap process liveness for Compose. Monitoring must parse `/readyz` JSON, using `cmd/probe` or equivalent, because HTTP 200 can contain degraded readiness.
- `make build` includes the read-only `over-laid-probe` binary. Probe defaults require the full report; deployments can select explicitly required named checks. Missing named checks fail.
