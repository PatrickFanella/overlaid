# Newsroom rebuild

Rebuild the pinned Newsroom workflow in Over-Laid’s Go/Vite stack. The owner selected the scope including Twitch ingestion and external enrichment. Source provenance and the MIT notice live in `docs/vendor/newsroom/`.

## Implemented units

1. Retained desk model and atomic command store: triage, deduplication, running order, focus/play, notes, shelves, show segments, imports and archive removal. Managed state uses tenant-scoped PostgreSQL transactions and forced RLS; file-mode state uses private atomic writes.
2. Operator UI and broadcast card: `/dashboard/newsroom`, `/overlay/newsroom`, useful demo modes, desktop and narrow layouts, and the existing route registries. The dashboard and broadcast retain their separate design systems.
3. Twitch integration: existing connection/EventSub worker, command and permission filters, ignored users, bounded queues, message replay deduplication, current-source replies, questions, raffles and announcements.
4. External providers: article/social/YouTube extraction, captions and playlists, optional Claude analysis/queue briefing, Brave related coverage and preferred sources, Discord notes with confirmation and retained delivery receipts. Provider keys remain write-only and workspace-encrypted.
5. Library and collaboration: portable shelf JSON, shelf moves/order, source shortcuts, moderator availability, independent add/share capability rotation, shared shelf page, and Chromium quick-add extension.
6. Lifecycle and operations: migration `0004_newsroom.sql`, schema/readiness guards, safe export projection, viewer deletion/retention, backup/recovery guide, and retained upstream attribution.

## Acceptance and verification

- The desk can complete submit → approve → focus → takeaway → played → notes, and retains the result across reload/process restart. Unit tests and an isolated browser fixture verify this.
- Stale commands fail instead of replacing concurrent edits; failed batches/file writes do not publish partial state. Store tests cover both boundaries.
- Workspace access, provider custody, overlay reads, add capabilities, and share capabilities have distinct scopes. PostgreSQL and HTTP tests cover isolation, forged writes, credential rotation, capability rotation, and private-read refusal.
- Chat input passes the configured gates and preserves provider failure states. Chat and provider fixtures verify parsing, replay identity, extracted text, strict analysis output, and public-address restrictions.
- Viewer deletion removes personal and derived desk state; ordinary workspace exports omit provider custody and credential hashes. Disposable PostgreSQL tests verify these consumers.
- `make verify` and `bash scripts/ci-postgres.sh` are required. The latter includes the full Go race suite and owns/removes its disposable database container.

The implementation replaces upstream Supabase accounts, Vercel jobs, and custom account admission with Over-Laid’s existing accounts, roles, background workers, and plan limits. Its app and OBS styling follow Over-Laid’s design conventions. Live Twitch, AI, Discord, extension installation and OBS capture require separate consumer qualification.
