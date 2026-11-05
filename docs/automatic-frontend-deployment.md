# Automatic frontend delivery

Native Gitea CI retains `scripts/ci-postgres.sh`, full application verification,
the uncached PostgreSQL race pass and the 35-minute verification bound. A frontend
job directly depends on verification and builds/validates the image on PRs.
Main pushes and manual main dispatch deploy the exact revision to the existing
NUC `overlaid-web-1`, Compose service `web`.

The operator-owned frontend-only baseline retains host networking, the read-only
production nginx bind, LAN listener `10.0.0.56:3083`, existing loopback API proxy
at `127.0.0.1:3084`, restart policy, memory limit and logging. The build explicitly uses
`VITE_API_BASE_URL=https://overlaid.subcult.tv`; an empty value would make the
overlay client fall back to port8080. Overlay SUBCULT assets and app-specific Overlaid
assets remain their reviewed copies with provenance; this workflow edits neither.

The backend, database, migrations, managed roles, vault/provider credentials,
chat/moderation modes and runtime/consumer activation remain on their existing
release procedures. No provider socket, sender, moderation worker or admission
path is enabled by this frontend deployment. CI tests against fixtures do not
qualify all features against an older running API. Review API compatibility and
managed release gates separately before a backend cutover.

An app-specific restricted fleet-deploy key permits only this frontend and the
current main revision. The receiver refuses runtime drift, checks image identity,
static public HTML and runtime configuration, and restores the previous image on
failed readiness. A retry reuses the accepted artifact. Main-only credentials are
guarded at step level. Gitea stores `HOMELAB_DEPLOY_KEY` and pinned
`HOMELAB_KNOWN_HOSTS`; the homelab operator owns the key, initially due for rotation
February 3, 2027. Register/test the profile before merging. Private host receipts
retain image IDs, diagnostics and rollback commands.

After rollout compare public HTML/branding/build files and representative dashboard
and overlay shell routes with the running image. API monitoring must parse
`/readyz` JSON using the existing probe and explicit required checks; HTTP200 alone
can be degraded and must not be reported as backend readiness. Static frontend
delivery does not qualify chat execution, provider delivery or full platform load.
